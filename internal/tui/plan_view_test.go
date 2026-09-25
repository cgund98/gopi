package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/inmemory"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cgund98/gopi/internal/app"
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
	if !strings.Contains(plain, "Hello plan") || !strings.Contains(plain, "b build") || !strings.Contains(plain, "esc or q back to chat") {
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

func TestSeenPlansStayClosed(t *testing.T) {
	dir := t.TempDir()
	rel := ".gopi/plans/ship-it.md"
	if err := os.MkdirAll(filepath.Join(dir, ".gopi", "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rel), []byte("# Hello plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	messages := []gogent.Message{
		gogent.NewToolResultMessage("call-1", `{"path":".gopi/plans/ship-it.md","status":"created"}`),
	}
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.workspacePath = dir
	chat.messages = messages
	chat.toolCards = []toolCardView{{ToolCallID: "call-1", ToolName: "write_plan"}}
	chat.seedSeenPlans(messages)
	chat.noticeWrittenPlans()
	if chat.planOpen {
		t.Fatal("a plan already in the transcript should stay closed")
	}
}

func TestPlansMenuOrdersAndDeletesInsidePlansDir(t *testing.T) {
	dir := t.TempDir()
	plans := filepath.Join(dir, ".gopi", "plans")
	if err := os.MkdirAll(plans, 0o755); err != nil {
		t.Fatal(err)
	}
	older := filepath.Join(plans, "older.md")
	newer := filepath.Join(plans, "newer.md")
	if err := os.WriteFile(older, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "secret.md")
	if err := os.WriteFile(outside, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := planPath(dir, "secret.md"); err == nil {
		t.Fatal("delete accepted a path outside .gopi/plans")
	}
	if _, err := planPath(dir, ".gopi/plans/../../secret.md"); err == nil {
		t.Fatal("delete accepted a traversal path")
	}

	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.width = 60
	chat.height = 20
	chat.workspacePath = dir
	chat.input.SetValue("/plans")
	updated, _ := chat.Update(tea.KeyMsg{Type: tea.KeyEnter})
	chat = updated.(*chatModel)
	view := stripANSI(chat.View())
	if chat.busy || !strings.Contains(view, ".gopi/plans/newer.md") {
		t.Fatalf("plans view = %q busy = %v", view, chat.busy)
	}
	if strings.Index(view, "newer.md") > strings.Index(view, "older.md") {
		t.Fatalf("order = %q", view)
	}
	pressX := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	updated, _ = chat.Update(pressX)
	chat = updated.(*chatModel)
	updated, _ = chat.Update(pressX)
	chat = updated.(*chatModel)
	if _, err := os.Stat(newer); !os.IsNotExist(err) {
		t.Fatal("newer plan was kept")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPlanSwitchesToAgent(t *testing.T) {
	dir := t.TempDir()
	rel := ".gopi/plans/ship-it.md"
	if err := os.MkdirAll(filepath.Join(dir, ".gopi", "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rel), []byte("# Ship\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.width = 60
	chat.height = 20
	chat.workspacePath = dir
	chat.mode = app.ModePlan
	var switched app.Mode
	chat.switchMode = func(mode app.Mode) error {
		switched = mode
		return nil
	}
	chat.openPlan(rel)
	if strings.Contains(planBuildPrompt(chat.planPath), rel) == false {
		t.Fatalf("prompt = %q", planBuildPrompt(chat.planPath))
	}
	updated, cmd := chat.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	chat = updated.(*chatModel)
	if chat.planOpen || switched != app.ModeAgent || !chat.busy || cmd == nil {
		t.Fatalf("open = %v mode = %q busy = %v cmd = %v", chat.planOpen, switched, chat.busy, cmd != nil)
	}
	if !strings.Contains(planBuildPrompt(rel), rel) {
		t.Fatal("build prompt omitted the plan path")
	}
}
