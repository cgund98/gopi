package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/inmemory"
	"github.com/cgund98/gopi/internal/app"
	"github.com/cgund98/gopi/internal/tools"
	"github.com/cgund98/gopi/internal/trust"
	"github.com/cgund98/gopi/internal/workspace"
)

func TestStatusSuffixShowsModelWorkspaceAndThinking(t *testing.T) {
	idle := strings.TrimRight(stripANSI(renderStatusSuffix("gpt-6-sol", "/tmp/gopi", 12, false, 48)), " ")
	if !strings.HasPrefix(idle, "/tmp/gopi") || !strings.HasSuffix(idle, "gpt-6-sol | 12%") || strings.Contains(idle, "thinking") {
		t.Fatalf("idle suffix = %q", idle)
	}
	busy := strings.TrimRight(stripANSI(renderStatusSuffix("gpt-6-sol", "/tmp/gopi", 12, true, 48)), " ")
	if !strings.HasPrefix(busy, "/tmp/gopi") || !strings.HasSuffix(busy, "gpt-6-sol | 12% | thinking") {
		t.Fatalf("busy suffix = %q", busy)
	}
}

func TestContextPercentUsesTranscript(t *testing.T) {
	if got := contextPercent("", nil, ""); got != 0 {
		t.Fatalf("empty = %d", got)
	}
	messages := []gogent.Message{{Content: strings.Repeat("a", contextWindowTokens*4)}}
	if got := contextPercent("", messages, ""); got != 100 {
		t.Fatalf("full = %d", got)
	}
}

func TestPromptSitsAboveStatusDivider(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.width = 40
	chat.height = 20
	chat.modelName = "gpt-6-sol"
	chat.workspacePath = "/tmp/gopi"

	view := stripANSI(chat.View())
	promptAt := strings.Index(view, ">")
	dividerAt := strings.Index(view, "────")
	statusAt := strings.Index(view, "/tmp/gopi")
	if promptAt < 0 || dividerAt < 0 || statusAt < 0 || promptAt >= dividerAt || dividerAt >= statusAt {
		t.Fatalf("prompt, divider, and status are out of order:\n%s", view)
	}
}

func TestWorkspaceChoiceAppliesWindowSize(t *testing.T) {
	home := t.TempDir()
	decisions, err := trust.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	session := &app.Session{
		Root:      workspace.Root{Path: t.TempDir()},
		Workspace: trust.WorkspaceUnknown,
		Edit:      &tools.EditFile{},
	}
	model := newProgram(context.Background(), session, decisions)
	model.chat = newChatModel(
		context.Background(),
		nil,
		inmemory.NewMessageStore(),
		gogent.NewToolRegistry(),
		gogent.NewChannelBroadcaster(),
	)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(*programModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = updated.(*programModel)

	if model.phase != phaseChat {
		t.Fatal("expected chat after trusting the workspace")
	}
	if model.chat.width != 80 || model.chat.height != 24 {
		t.Fatalf("chat size = %dx%d", model.chat.width, model.chat.height)
	}
	if view := model.View(); view == "Loading…" {
		t.Fatal("chat stayed on the loading screen")
	}
}
