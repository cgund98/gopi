package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/inmemory"
)

func TestPlanViewerOpensAndCloses(t *testing.T) {
	dir := t.TempDir()
	rel := ".gopi/plans/ship-it.md"
	if err := os.MkdirAll(filepath.Join(dir, ".gopi", "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rel), []byte("# Hello plan\n\nDo the thing.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.width = 60
	chat.height = 20
	chat.workspacePath = dir
	chat.toolCards = []toolCardView{{ToolCallID: "call-1", ToolName: "write_plan"}}
	chat.messages = []gogent.Message{
		gogent.NewToolResultMessage("call-1", `{"path":".gopi/plans/ship-it.md","status":"created"}`),
	}
	chat.noticeWrittenPlans()
	if !chat.planOpen {
		t.Fatal("expected the plan viewer to open")
	}
	plain := stripANSI(chat.View())
	if !strings.Contains(plain, "Hello plan") || !strings.Contains(plain, "esc or q back to chat") {
		t.Fatalf("view = %q", plain)
	}

	updated, cmd := chat.Update(tea.KeyMsg{Type: tea.KeyEsc})
	chat = updated.(*chatModel)
	if chat.planOpen || cmd != nil {
		t.Fatal("esc should return to the chat")
	}
	if strings.Contains(stripANSI(chat.View()), "esc or q back to chat") {
		t.Fatal("plan viewer still open")
	}
}
