package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/cgund98/gopi/internal/workspace"
)

const maxTasks = 20

const (
	taskPending    = "pending"
	taskInProgress = "in_progress"
	taskCompleted  = "completed"
	taskCancelled  = "cancelled"
)

// Task is one checklist item stored on a plan and in a tasks result.
type Task struct {
	ID      string `json:"id" jsonschema:"description=Short stable id, such as fetch-tool."`
	Content string `json:"content" jsonschema:"description=What the task is."`
	Status  string `json:"status,omitempty" jsonschema:"description=pending, in_progress, completed, or cancelled. Defaults to pending."`
}

// TaskChange is one item that finished or left the list in a single tasks call.
type TaskChange struct {
	Kind    string
	Content string
}

// TaskList is the checklist the tasks tool patches. Plan is a workspace-relative
// plan file to rewrite, or empty when the list is not tied to a plan.
type TaskList struct {
	mu    sync.Mutex
	items []Task
	plan  string
	root  workspace.Root
}

// NewTaskList returns an empty checklist for one workspace.
func NewTaskList(root workspace.Root) *TaskList {
	return &TaskList{root: root}
}

// Items returns a copy of the current checklist.
func (l *TaskList) Items() []Task {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return cloneTasks(l.items)
}

// Set replaces the checklist and remembers the plan file later calls update.
func (l *TaskList) Set(items []Task, rel string) error {
	if l == nil {
		return nil
	}
	if strings.TrimSpace(rel) != "" {
		if _, err := resolvePlan(l.root, rel); err != nil {
			return err
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = cloneTasks(items)
	l.plan = rel
	return nil
}

// Hydrate replaces the items and leaves the plan path as it is.
func (l *TaskList) Hydrate(items []Task) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = cloneTasks(items)
}

// Clear drops the items and the plan path.
func (l *TaskList) Clear() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = nil
	l.plan = ""
}

func (l *TaskList) apply(next []Task) error {
	l.mu.Lock()
	plan := l.plan
	root := l.root
	l.mu.Unlock()
	if err := syncPlanTodos(root, plan, next); err != nil {
		return err
	}
	l.mu.Lock()
	l.items = cloneTasks(next)
	l.mu.Unlock()
	return nil
}

func (l *TaskList) snapshot() []Task {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return cloneTasks(l.items)
}

// FormatPlan writes todos as YAML frontmatter above the markdown body.
// An empty list returns the body unchanged.
func FormatPlan(items []Task, body string) string {
	if len(items) == 0 {
		return body
	}
	var b strings.Builder
	b.WriteString("---\ntodos:\n")
	for _, item := range items {
		b.WriteString("  - id: ")
		b.WriteString(yamlQuote(item.ID))
		b.WriteString("\n    content: ")
		b.WriteString(yamlQuote(item.Content))
		b.WriteString("\n    status: ")
		b.WriteString(yamlQuote(item.Status))
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	b.WriteString(body)
	return b.String()
}

// SplitPlan separates todo frontmatter from the markdown body.
// A file without frontmatter returns the text as the body.
func SplitPlan(text string) ([]Task, string, error) {
	if !strings.HasPrefix(text, "---\n") {
		return nil, text, nil
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return nil, "", fmt.Errorf("plan frontmatter is not closed")
	}
	items, err := parseTodoFront(rest[:end])
	if err != nil {
		return nil, "", err
	}
	return items, rest[end+len("\n---\n"):], nil
}

// ParseTaskResult reads a successful tasks tool result.
func ParseTaskResult(content string) ([]Task, bool) {
	if strings.Contains(content, `"error"`) {
		return nil, false
	}
	var payload struct {
		Items []Task `json:"items"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil, false
	}
	if payload.Items == nil {
		return nil, false
	}
	return payload.Items, true
}

// TaskResultJSON is the tool result for a checklist.
func TaskResultJSON(items []Task) ([]byte, error) {
	if items == nil {
		items = []Task{}
	}
	return json.Marshal(map[string]any{"items": items})
}

// TaskChanges lists items that finished or left the open list between two results.
func TaskChanges(prev, next []Task) []TaskChange {
	prevByID := make(map[string]Task, len(prev))
	for _, item := range prev {
		prevByID[item.ID] = item
	}
	nextByID := make(map[string]Task, len(next))
	for _, item := range next {
		nextByID[item.ID] = item
	}
	var changes []TaskChange
	for _, item := range prev {
		found, ok := nextByID[item.ID]
		if !ok {
			if item.Status == taskPending || item.Status == taskInProgress {
				changes = append(changes, TaskChange{Kind: "removed", Content: item.Content})
			}
			continue
		}
		if item.Status != taskCompleted && found.Status == taskCompleted {
			changes = append(changes, TaskChange{Kind: "done", Content: found.Content})
		}
		if item.Status != taskCancelled && found.Status == taskCancelled {
			changes = append(changes, TaskChange{Kind: "cancelled", Content: found.Content})
		}
	}
	for _, item := range next {
		if _, ok := prevByID[item.ID]; ok {
			continue
		}
		if item.Status == taskCompleted {
			changes = append(changes, TaskChange{Kind: "done", Content: item.Content})
		}
		if item.Status == taskCancelled {
			changes = append(changes, TaskChange{Kind: "cancelled", Content: item.Content})
		}
	}
	return changes
}

// CountTasks returns how many items are finished and how many exist.
func CountTasks(items []Task) (done, total int) {
	for _, item := range items {
		if item.Status == taskCompleted || item.Status == taskCancelled {
			done++
		}
	}
	return done, len(items)
}

// OpenTasks returns items that are still pending or in progress.
func OpenTasks(items []Task) []Task {
	var open []Task
	for _, item := range items {
		if item.Status == taskPending || item.Status == taskInProgress {
			open = append(open, item)
		}
	}
	return open
}

func normalizeTasks(items []Task) ([]Task, error) {
	if len(items) > maxTasks {
		return nil, fmt.Errorf("task list is limited to %d items", maxTasks)
	}
	seen := map[string]bool{}
	out := make([]Task, 0, len(items))
	inProgress := 0
	for _, item := range items {
		next, err := normalizeTask(item)
		if err != nil {
			return nil, err
		}
		if seen[next.ID] {
			return nil, fmt.Errorf("duplicate task %s", next.ID)
		}
		seen[next.ID] = true
		if next.Status == taskInProgress {
			inProgress++
		}
		out = append(out, next)
	}
	if inProgress > 1 {
		return nil, fmt.Errorf("only one task can be in progress")
	}
	return out, nil
}

func normalizeTask(item Task) (Task, error) {
	item.ID = strings.TrimSpace(item.ID)
	item.Content = strings.TrimSpace(item.Content)
	item.Status = strings.TrimSpace(item.Status)
	if item.ID == "" || strings.ContainsAny(item.ID, " \t\n") || utf8.RuneCountInString(item.ID) > 64 {
		return Task{}, fmt.Errorf("task id must be a short slug")
	}
	if item.Content == "" || utf8.RuneCountInString(item.Content) > 200 {
		return Task{}, fmt.Errorf("task content is required")
	}
	if item.Status == "" {
		item.Status = taskPending
	}
	switch item.Status {
	case taskPending, taskInProgress, taskCompleted, taskCancelled:
	default:
		return Task{}, fmt.Errorf("task status must be pending, in_progress, completed, or cancelled")
	}
	return item, nil
}

func cloneTasks(items []Task) []Task {
	if items == nil {
		return nil
	}
	out := make([]Task, len(items))
	copy(out, items)
	return out
}

func syncPlanTodos(root workspace.Root, rel string, items []Task) error {
	if strings.TrimSpace(rel) == "" {
		return nil
	}
	full, err := resolvePlan(root, rel)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("read plan: %w", err)
	}
	_, prose, err := SplitPlan(string(body))
	if err != nil {
		return err
	}
	if err := os.WriteFile(full, []byte(FormatPlan(items, prose)), 0o644); err != nil {
		return fmt.Errorf("write plan: %w", err)
	}
	return nil
}

func resolvePlan(root workspace.Root, rel string) (string, error) {
	resolved, outside, err := root.Canonical(rel)
	if err != nil {
		return "", err
	}
	plans := filepath.Join(root.Path, ".gopi", "plans")
	if outside || !insideDir(plans, resolved) || !strings.HasSuffix(resolved, ".md") {
		return "", fmt.Errorf("path must be a markdown file under .gopi/plans")
	}
	return resolved, nil
}

func parseTodoFront(front string) ([]Task, error) {
	lines := strings.Split(front, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "todos:" {
		return nil, fmt.Errorf("plan frontmatter must be a todos list")
	}
	var items []Task
	var cur *Task
	flush := func() error {
		if cur == nil {
			return nil
		}
		next, err := normalizeTask(*cur)
		if err != nil {
			return err
		}
		items = append(items, next)
		cur = nil
		return nil
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "  - id: "):
			if err := flush(); err != nil {
				return nil, err
			}
			id, err := yamlUnquote(strings.TrimPrefix(line, "  - id: "))
			if err != nil {
				return nil, err
			}
			cur = &Task{ID: id}
		case strings.HasPrefix(line, "    content: ") && cur != nil:
			content, err := yamlUnquote(strings.TrimPrefix(line, "    content: "))
			if err != nil {
				return nil, err
			}
			cur.Content = content
		case strings.HasPrefix(line, "    status: ") && cur != nil:
			status, err := yamlUnquote(strings.TrimPrefix(line, "    status: "))
			if err != nil {
				return nil, err
			}
			cur.Status = status
		default:
			return nil, fmt.Errorf("plan frontmatter has an unexpected line")
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return normalizeTasks(items)
}

func yamlQuote(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func yamlUnquote(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", fmt.Errorf("plan frontmatter values must be quoted")
	}
	var b strings.Builder
	escaped := false
	for _, r := range value[1 : len(value)-1] {
		if escaped {
			switch r {
			case '\\', '"':
				b.WriteRune(r)
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			default:
				return "", fmt.Errorf("plan frontmatter has an unknown escape")
			}
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		return "", fmt.Errorf("plan frontmatter has a dangling escape")
	}
	return b.String(), nil
}
