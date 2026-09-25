package tui

import (
	"strings"

	"github.com/cgund98/gopi/internal/models"
)

type completion struct {
	insert string
	label  string
	detail string
}

func composerCommands() []completion {
	return []completion{
		{insert: "/agent", label: "/agent", detail: "switch to Agent"},
		{insert: "/ask", label: "/ask", detail: "switch to Ask"},
		{insert: "/plan", label: "/plan", detail: "switch to Plan"},
		{insert: "/mode", label: "/mode <name>", detail: "switch mode"},
		{insert: "/model", label: "/model <name>", detail: "set the model for this mode"},
		{insert: "/compact", label: "/compact", detail: "summarize earlier turns"},
		{insert: "/sessions", label: "/sessions", detail: "open saved chats"},
		{insert: "/plans", label: "/plans", detail: "open saved plans"},
		{insert: "/review", label: "/review", detail: "review file edits"},
		{insert: "/help", label: "/help", detail: "show commands"},
	}
}

func (m *chatModel) syncComplete() {
	if m.busy || m.inApprovalMode() || m.planOpen || m.plansOpen || m.reviewOpen || m.sessionsOpen {
		m.completeOpen = false
		return
	}
	items := completionsFor(m.input.Value())
	if len(items) == 0 {
		m.completeOpen = false
		m.completeItems = nil
		return
	}
	m.completeItems = items
	if m.completeIndex >= len(items) {
		m.completeIndex = 0
	}
	m.completeOpen = true
}

func completionsFor(text string) []completion {
	if !strings.HasPrefix(text, "/") {
		return nil
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return filterCompletions(composerCommands(), "/")
	}
	name := fields[0]
	trailingSpace := strings.HasSuffix(text, " ")
	if len(fields) == 1 && !trailingSpace {
		return filterCompletions(composerCommands(), name)
	}
	arg := ""
	if len(fields) > 1 {
		arg = fields[1]
	}
	switch name {
	case "/mode":
		return filterCompletions([]completion{
			{insert: "/mode agent", label: "/mode agent", detail: "Agent"},
			{insert: "/mode ask", label: "/mode ask", detail: "Ask"},
			{insert: "/mode plan", label: "/mode plan", detail: "Plan"},
		}, arg)
	case "/model":
		var items []completion
		for _, model := range models.Names() {
			items = append(items, completion{insert: "/model " + model, label: "/model " + model, detail: "model"})
		}
		return filterCompletions(items, arg)
	default:
		return nil
	}
}

func filterCompletions(items []completion, prefix string) []completion {
	prefix = strings.ToLower(prefix)
	var out []completion
	for _, item := range items {
		if prefix == "" || prefix == "/" || strings.HasPrefix(strings.ToLower(item.label), prefix) || strings.HasPrefix(strings.ToLower(item.insert), prefix) || strings.HasPrefix(strings.ToLower(completionArg(item.label)), prefix) {
			out = append(out, item)
		}
	}
	return out
}

func completionArg(label string) string {
	fields := strings.Fields(label)
	if len(fields) < 2 || strings.HasPrefix(fields[1], "<") {
		return ""
	}
	return fields[1]
}

func (m *chatModel) moveComplete(delta int) {
	if len(m.completeItems) == 0 {
		return
	}
	m.completeIndex = (m.completeIndex + delta + len(m.completeItems)) % len(m.completeItems)
}

func (m *chatModel) acceptComplete() {
	if !m.completeOpen || m.completeIndex < 0 || m.completeIndex >= len(m.completeItems) {
		return
	}
	item := m.completeItems[m.completeIndex]
	insert := item.insert
	if item.insert == "/mode" || item.insert == "/model" {
		insert += " "
	}
	m.input.SetValue(insert)
	m.input.CursorEnd()
	m.syncComplete()
}

func (m *chatModel) renderComplete() string {
	if !m.completeOpen {
		return ""
	}
	var lines []string
	for i, item := range m.completeItems {
		row := item.label
		if item.detail != "" {
			row += "  " + item.detail
		}
		row = truncateWidth(row, m.width)
		if i == m.completeIndex {
			lines = append(lines, toolSelectedStyle.Render(row))
			continue
		}
		lines = append(lines, toolDimStyle.Render(row))
	}
	return strings.Join(lines, "\n")
}

func (m *chatModel) completeLines() int {
	if !m.completeOpen {
		return 0
	}
	return len(m.completeItems)
}
