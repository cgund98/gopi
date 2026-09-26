package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/inmemory"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestMouseCommandTogglesCapture(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	for _, step := range []struct {
		input string
		off   bool
	}{
		{"/mouse", true},
		{"/mouse", false},
		{"/mouse off", true},
		{"/mouse off", true},
		{"/mouse on", false},
	} {
		chat.input.SetValue(step.input)
		updated, cmd := chat.Update(tea.KeyMsg{Type: tea.KeyEnter})
		chat = updated.(*chatModel)
		if cmd == nil || chat.mouseOff != step.off || chat.input.Value() != "" {
			t.Fatalf("%s: off = %v cmd = %v input = %q", step.input, chat.mouseOff, cmd, chat.input.Value())
		}
	}
	chat.input.SetValue("/mouse sideways")
	updated, cmd := chat.Update(tea.KeyMsg{Type: tea.KeyEnter})
	chat = updated.(*chatModel)
	if cmd != nil || chat.mouseOff || !strings.Contains(chat.status, "Usage") {
		t.Fatalf("bad arg: off = %v cmd = %v status = %q", chat.mouseOff, cmd, chat.status)
	}
}

func TestCompletionsFilterCommandsAndModels(t *testing.T) {
	names := map[string]bool{}
	for _, item := range completionsFor("/mo") {
		names[item.label] = true
	}
	if !names["/mode <name>"] || !names["/model <name>"] || names["/help"] {
		t.Fatalf("matches = %#v", names)
	}
	models := map[string]bool{}
	for _, item := range completionsFor("/model g") {
		models[item.label] = true
	}
	if !models["/model gpt-4o"] || !models["/model gpt-5.6-luna"] {
		t.Fatalf("models = %#v", models)
	}
	if completionsFor("hello") != nil {
		t.Fatal("plain text opened completions")
	}

	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.input.SetValue("/mo")
	chat.syncComplete()
	chat.completeIndex = 0
	chat.acceptComplete()
	if !strings.HasPrefix(chat.input.Value(), "/mo") {
		t.Fatalf("accepted = %q", chat.input.Value())
	}
}

func TestCompactKeepsLatestUserTurn(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.messages = []gogent.Message{gogent.NewUserMessage("only")}
	if cmd := chat.compact(); cmd != nil {
		t.Fatal("expected nothing to compact")
	}
	chat.messages = []gogent.Message{
		gogent.NewUserMessage("earlier"),
		gogent.NewAssistantMessage("notes"),
		gogent.NewUserMessage("latest"),
	}
	chat.summarize = func(context.Context, string) (gogent.Message, error) {
		return gogent.NewAssistantMessage("the short version"), nil
	}
	cmd := chat.compact()
	if cmd == nil {
		t.Fatal("expected compact to start")
	}
	chat = finishCompact(chat, cmd)
	if len(chat.messages) != 2 || !strings.Contains(chat.messages[0].Content, "short version") || chat.messages[1].Content != "latest" {
		t.Fatalf("messages = %#v", chat.messages)
	}
}

func finishCompact(chat *chatModel, cmd tea.Cmd) *chatModel {
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		updated, _ := chat.Update(msg)
		return updated.(*chatModel)
	}
	for _, next := range batch {
		inner := next()
		if _, done := inner.(compactDoneMsg); !done {
			continue
		}
		updated, _ := chat.Update(inner)
		chat = updated.(*chatModel)
	}
	return chat
}

func TestReviewLineWraps(t *testing.T) {
	line := renderReviewLine(1, 1, 2, reviewRowContext, plainSpans(strings.Repeat("a", 40), reviewRowContext), false, 20)
	if !strings.Contains(line, "\n") {
		t.Fatalf("line = %q", line)
	}
	if strings.Count(stripANSI(line), "a") != 40 {
		t.Fatalf("dropped characters: %q", stripANSI(line))
	}
}

func TestFinishedResponseShowsUsage(t *testing.T) {
	messages := []gogent.Message{
		gogent.NewAssistantMessageWithToolCalls("", nil),
		gogent.NewAssistantMessage("the answer"),
	}
	messages[0].Usage = &gogent.Usage{Input: 1000, Output: 10}
	messages[1].Usage = &gogent.Usage{Input: 28400, Output: 826}
	shown := stripANSI(renderTranscript(messages, nil, -1, 80, true, nil))
	if strings.Count(shown, "28.4k/826") != 1 || strings.Contains(shown, "1.0k/10") {
		t.Fatalf("transcript = %q", shown)
	}
	hidden := stripANSI(renderTranscript(messages, nil, -1, 80, false, nil))
	if strings.Contains(hidden, "28.4k/826") {
		t.Fatal("usage rendered while the agent is still working")
	}
	worked := map[string]time.Duration{messages[1].ID: 3 * time.Second}
	withWork := stripANSI(renderTranscript(messages, nil, -1, 80, true, worked))
	if !strings.Contains(withWork, "28.4k/826  Worked for 3s") || strings.Contains(withWork, "Thought") {
		t.Fatalf("transcript = %q", withWork)
	}
}

func TestErrorWrapsToWidth(t *testing.T) {
	text := strings.Repeat("failed ", 12)
	rendered := stripANSI(wrapStyled(text, errStyle, 20))
	if !strings.Contains(rendered, "\n") {
		t.Fatalf("error stayed on one line: %q", rendered)
	}
	for _, line := range strings.Split(rendered, "\n") {
		if lipgloss.Width(line) > 20 {
			t.Fatalf("line width %d: %q", lipgloss.Width(line), line)
		}
	}
}

func TestUsageStatusShowsCost(t *testing.T) {
	messages := []gogent.Message{{
		Role:  gogent.MessageRoleAssistant,
		Usage: &gogent.Usage{Input: 1_000_000, Output: 0},
	}}
	status := formatUsageStatus("gpt-5.6-luna", messages)
	if status != "1.0M/0 $0.20" {
		t.Fatalf("status = %q", status)
	}
}
