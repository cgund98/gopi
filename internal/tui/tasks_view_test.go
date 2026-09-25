package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/tools"
)

func TestTaskPanelDropsFinishedItems(t *testing.T) {
	items := []tools.Task{
		{ID: "a", Content: "Done one", Status: "completed"},
		{ID: "b", Content: "Done two", Status: "completed"},
		{ID: "c", Content: "Done three", Status: "completed"},
		{ID: "d", Content: "Still open", Status: "pending"},
		{ID: "e", Content: "Working", Status: "in_progress"},
	}
	plain := stripANSI(renderTaskPanel(items, 40))
	if !strings.Contains(plain, "3/5") || !strings.Contains(plain, "Still open") || !strings.Contains(plain, "Working") || strings.Contains(plain, "Done one") {
		t.Fatalf("panel = %q", plain)
	}
	done := []tools.Task{
		{ID: "a", Content: "Done one", Status: "completed"},
		{ID: "b", Content: "Done two", Status: "canceled"},
	}
	if renderTaskPanel(done, 40) != "" {
		t.Fatal("panel should hide when nothing is open")
	}
}

func TestFinishedTasksRenderOnceInTranscript(t *testing.T) {
	first := `{"items":[{"id":"a","content":"Add the tool","status":"pending"},{"id":"b","content":"Wire it","status":"pending"}]}`
	second := `{"items":[{"id":"a","content":"Add the tool","status":"completed"},{"id":"b","content":"Wire it","status":"in_progress"}]}`
	third := `{"items":[{"id":"a","content":"Add the tool","status":"completed"},{"id":"b","content":"Wire it","status":"completed"}]}`
	messages := []gogent.Message{
		gogent.NewAssistantMessageWithToolCalls("", []gogent.ToolCall{{ID: "c1", ToolName: "tasks"}}),
		gogent.NewToolResultMessage("c1", first),
		gogent.NewAssistantMessageWithToolCalls("", []gogent.ToolCall{{ID: "c2", ToolName: "tasks"}}),
		gogent.NewToolResultMessage("c2", second),
		gogent.NewAssistantMessageWithToolCalls("", []gogent.ToolCall{{ID: "c3", ToolName: "tasks"}}),
		gogent.NewToolResultMessage("c3", third),
	}
	cards := []toolCardView{
		{ToolCallID: "c1", ToolName: "tasks", Result: first},
		{ToolCallID: "c2", ToolName: "tasks", Result: second},
		{ToolCallID: "c3", ToolName: "tasks", Result: third},
	}
	plain := stripANSI(renderTranscript(messages, cards, -1, 80, false))
	if strings.Count(plain, "done Add the tool") != 1 || strings.Count(plain, "done Wire it") != 1 {
		t.Fatalf("transcript = %q", plain)
	}
	if strings.Contains(plain, `"items"`) {
		t.Fatalf("raw tasks json leaked: %q", plain)
	}
}

func TestTaskHeadlineUsesResultCount(t *testing.T) {
	card := toolCardView{
		ToolName: "tasks",
		Args:     json.RawMessage(`{"update":[{"id":"a","status":"completed"}]}`),
		Result:   `{"items":[{"id":"a","content":"Add the tool","status":"completed"},{"id":"b","content":"Wire it","status":"pending"}]}`,
	}
	if got := toolHeadline(card); got != "tasks 1/2" {
		t.Fatalf("headline = %q", got)
	}
}
