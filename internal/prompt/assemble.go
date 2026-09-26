package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const truncationMarker = "[earlier instructions truncated]\n"

// Options selects which instruction files are assembled into the system prompt.
type Options struct {
	Base          string
	HomeDir       string
	Workspace     string
	Trusted       bool
	UserPrompt    string
	MaxBytes      int
	SkillDirs     []string
	FallbackFiles []string
}

// Assemble concatenates the built-in prompt, user files, the project chain, and the skill catalog.
func Assemble(opts Options) (string, error) {
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 32768
	}
	var parts []string
	parts = append(parts, strings.TrimRight(opts.Base, "\n"))
	if text := strings.TrimSpace(opts.UserPrompt); text != "" {
		parts = append(parts, "<user_prompt>\n"+text+"\n</user_prompt>")
	}
	userDoc, err := readOptional(filepath.Join(opts.HomeDir, "AGENTS.md"))
	if err != nil {
		return "", err
	}
	if userDoc != "" {
		parts = append(parts, "<user_agents>\n"+keepTail(userDoc, maxBytes)+"\n</user_agents>")
	}
	if opts.Trusted {
		chain, err := projectChain(opts.Workspace, opts.FallbackFiles)
		if err != nil {
			return "", err
		}
		if chain != "" {
			parts = append(parts, "<project_agents>\n"+keepTail(chain, maxBytes)+"\n</project_agents>")
		}
	}
	catalog, err := skillCatalog(opts)
	if err != nil {
		return "", err
	}
	if catalog != "" {
		parts = append(parts, "<skills>\n"+keepTail(catalog, maxBytes)+"\n</skills>")
	}
	return WithWorkspace(strings.Join(parts, "\n\n"), opts.Workspace), nil
}

func projectChain(workspace string, fallback []string) (string, error) {
	var blocks []string
	for _, dir := range instructionDirs(workspace) {
		body, name, err := directoryInstructions(dir, fallback)
		if err != nil {
			return "", err
		}
		if body == "" {
			continue
		}
		blocks = append(blocks, "# "+filepath.Join(dir, name)+"\n"+body)
	}
	return strings.Join(blocks, "\n\n"), nil
}

func instructionDirs(workspace string) []string {
	root := gitRoot(workspace)
	rel, err := filepath.Rel(root, workspace)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return []string{workspace}
	}
	dirs := []string{root}
	cur := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		dirs = append(dirs, cur)
	}
	return dirs
}

func gitRoot(dir string) string {
	cur := dir
	for {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return dir
		}
		cur = parent
	}
}

func directoryInstructions(dir string, fallback []string) (string, string, error) {
	name := "AGENTS.md"
	var blocks []string
	if body, err := readOptional(filepath.Join(dir, "AGENTS.override.md")); err != nil {
		return "", "", err
	} else if body != "" {
		name = "AGENTS.override.md"
		blocks = append(blocks, body)
	} else if body, err := readOptional(filepath.Join(dir, "AGENTS.md")); err != nil {
		return "", "", err
	} else if body != "" {
		blocks = append(blocks, body)
	}
	for _, extra := range fallback {
		extra = strings.TrimSpace(extra)
		if extra == "" || extra == "AGENTS.md" || extra == "AGENTS.override.md" {
			continue
		}
		body, err := readOptional(filepath.Join(dir, extra))
		if err != nil {
			return "", "", err
		}
		if body != "" {
			blocks = append(blocks, body)
		}
	}
	return strings.Join(blocks, "\n\n"), name, nil
}

type skill struct {
	Name        string
	Description string
	Path        string
}

// SkillRoots returns the directories that are searched for skills, in order.
func SkillRoots(opts Options) []string {
	var roots []string
	roots = append(roots, filepath.Join(opts.HomeDir, "skills"))
	roots = append(roots, opts.SkillDirs...)
	if opts.Trusted {
		roots = append(roots, filepath.Join(opts.Workspace, ".gopi", "skills"))
	}
	return roots
}

func skillCatalog(opts Options) (string, error) {
	roots := SkillRoots(opts)
	byName := map[string]skill{}
	var order []string
	for _, root := range roots {
		found, err := skillsIn(root)
		if err != nil {
			return "", err
		}
		for _, item := range found {
			if _, ok := byName[item.Name]; !ok {
				order = append(order, item.Name)
			}
			byName[item.Name] = item
		}
	}
	if len(order) == 0 {
		return "", nil
	}
	var lines []string
	lines = append(lines, "Read a skill with read_file before following it.")
	for _, name := range order {
		item := byName[name]
		lines = append(lines, fmt.Sprintf("- %s: %s\n  path: %s", item.Name, item.Description, item.Path))
	}
	return strings.Join(lines, "\n"), nil
}

func skillsIn(root string) ([]skill, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read skills %s: %w", root, err)
	}
	var found []skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "SKILL.md")
		item, ok, err := parseSkill(path)
		if err != nil {
			return nil, err
		}
		if ok {
			found = append(found, item)
		}
	}
	return found, nil
}

func parseSkill(path string) (skill, bool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return skill{}, false, nil
		}
		return skill{}, false, err
	}
	text := string(body)
	if !strings.HasPrefix(text, "---\n") {
		return skill{}, false, nil
	}
	rest := strings.TrimPrefix(text, "---\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return skill{}, false, nil
	}
	name, description := "", ""
	for _, line := range strings.Split(rest[:end], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "name":
			name = value
		case "description":
			description = value
		}
	}
	if name == "" || description == "" {
		return skill{}, false, nil
	}
	return skill{Name: name, Description: description, Path: path}, true, nil
}

func readOptional(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return strings.TrimRight(string(body), "\n"), nil
}

func keepTail(text string, maxBytes int) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	if len(truncationMarker) >= maxBytes {
		return truncationMarker[:maxBytes]
	}
	tail := text[len(text)-(maxBytes-len(truncationMarker)):]
	return truncationMarker + tail
}
