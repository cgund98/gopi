package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/inmemory"
	"github.com/charmbracelet/lipgloss"
)

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
	if !models["/model gpt-4o"] || !models["/model gpt-4o-mini"] {
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
	updated, _ := chat.Update(cmd())
	chat = updated.(*chatModel)
	if len(chat.messages) != 2 || !strings.Contains(chat.messages[0].Content, "short version") || chat.messages[1].Content != "latest" {
		t.Fatalf("messages = %#v", chat.messages)
	}
}

func TestReviewLineWraps(t *testing.T) {
	line := renderReviewLine(1, 1, 2, " ", strings.Repeat("a", 40), toolResultStyle, 20)
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
	shown := stripANSI(renderTranscript(messages, nil, -1, 80, true))
	if strings.Count(shown, "28.4k/826") != 1 || strings.Contains(shown, "1.0k/10") {
		t.Fatalf("transcript = %q", shown)
	}
	hidden := stripANSI(renderTranscript(messages, nil, -1, 80, false))
	if strings.Contains(hidden, "28.4k/826") {
		t.Fatal("usage rendered while the agent is still working")
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
	status := formatUsageStatus("gpt-4o-mini", messages)
	if status != "1.0M/0 $0.15" {
		t.Fatalf("status = %q", status)
	}
}
