package tui

import (
	"fmt"
	"strings"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/tools"
)

func (m *chatModel) syncTasks() {
	if m.tasks == nil {
		return
	}
	if items, ok := tasksAfter(m.messages, m.toolCards, m.taskEpoch); ok {
		m.tasks.Hydrate(items)
		return
	}
	if m.taskEpoch == 0 {
		m.tasks.Clear()
	}
}

func (m *chatModel) visibleTasks() []tools.Task {
	if items, ok := tasksAfter(m.messages, m.toolCards, m.taskEpoch); ok {
		return items
	}
	if m.taskEpoch > 0 {
		return m.taskSeed
	}
	return nil
}

func (m *chatModel) taskPanelHeight() int {
	panel := renderTaskPanel(m.visibleTasks(), m.width)
	if panel == "" {
		return 0
	}
	return strings.Count(panel, "\n") + 2
}

func tasksAfter(messages []gogent.Message, cards []toolCardView, after int) ([]tools.Task, bool) {
	if after < 0 {
		after = 0
	}
	if after > len(messages) {
		after = len(messages)
	}
	var items []tools.Task
	found := false
	for _, message := range messages[after:] {
		if message.Role != gogent.MessageRoleTool {
			continue
		}
		card, ok := toolCardByID(cards, message.ToolCallID)
		if !ok || card.ToolName != "tasks" {
			continue
		}
		parsed, ok := tools.ParseTaskResult(message.Content)
		if !ok {
			continue
		}
		items = parsed
		found = true
	}
	return items, found
}

func renderTaskPanel(items []tools.Task, width int) string {
	open := tools.OpenTasks(items)
	if len(open) == 0 {
		return ""
	}
	if width < 20 {
		width = 20
	}
	done, total := tools.CountTasks(items)
	var b strings.Builder
	b.WriteString(statusStyle.Render(fmt.Sprintf("%d/%d", done, total)))
	for _, item := range open {
		mark := "[ ]"
		if item.Status == "in_progress" {
			mark = "[~]"
		}
		for _, line := range wrapWidth(mark+" "+item.Content, width) {
			b.WriteByte('\n')
			b.WriteString(toolDimStyle.Render(line))
		}
	}
	return b.String()
}

func renderPlanTodos(items []tools.Task, width int) string {
	if len(items) == 0 {
		return ""
	}
	if width < 20 {
		width = 20
	}
	var b strings.Builder
	b.WriteString(planTitleStyle.Render("Todos"))
	for _, item := range items {
		mark := "[ ]"
		switch item.Status {
		case "in_progress":
			mark = "[~]"
		case "completed":
			mark = "[x]"
		case "cancelled":
			mark = "[-]"
		}
		for _, line := range wrapWidth(mark+" "+item.Content, width) {
			b.WriteByte('\n')
			b.WriteString(line)
		}
	}
	return b.String()
}

func renderFinishedTaskLines(messages []gogent.Message, cards []toolCardView, toolCallID string, width int) string {
	var prev []tools.Task
	var next []tools.Task
	found := false
	for _, message := range messages {
		if message.Role != gogent.MessageRoleTool {
			continue
		}
		card, ok := toolCardByID(cards, message.ToolCallID)
		if !ok || card.ToolName != "tasks" {
			continue
		}
		parsed, ok := tools.ParseTaskResult(message.Content)
		if !ok {
			continue
		}
		if message.ToolCallID == toolCallID {
			next = parsed
			found = true
			break
		}
		prev = parsed
	}
	if !found {
		return ""
	}
	changes := tools.TaskChanges(prev, next)
	if len(changes) == 0 {
		return ""
	}
	if width < 20 {
		width = 20
	}
	var b strings.Builder
	for i, change := range changes {
		if i > 0 {
			b.WriteByte('\n')
		}
		for j, line := range wrapWidth(change.Kind+" "+change.Content, width) {
			if j > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(toolDimStyle.Render(toolResultIndent + line))
		}
	}
	return b.String()
}

func taskHeadlineCount(result string) (done, total int) {
	items, ok := tools.ParseTaskResult(result)
	if !ok {
		return 0, 0
	}
	return tools.CountTasks(items)
}
