package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/inmemory"
	"github.com/cgund98/gopi/internal/app"
	"github.com/cgund98/gopi/internal/config"
	"github.com/cgund98/gopi/internal/session"
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

func TestModeCommandChangesPromptColor(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.width = 40
	chat.height = 20
	var switched app.Mode
	chat.switchMode = func(mode app.Mode) error {
		switched = mode
		return nil
	}
	chat.input.SetValue("/mode plan")
	updated, _ := chat.Update(tea.KeyMsg{Type: tea.KeyEnter})
	chat = updated.(*chatModel)
	if switched != app.ModePlan || chat.busy {
		t.Fatalf("switched = %s busy = %v", switched, chat.busy)
	}
	plain := stripANSI(modePrompt(chat.mode))
	if plain != "plan > " {
		t.Fatalf("prompt = %q", plain)
	}
	agent := modePrompt(app.ModeAgent)
	ask := modePrompt(app.ModeAsk)
	plan := modePrompt(app.ModePlan)
	if agent == ask || ask == plan || agent == plan {
		t.Fatal("mode colors should differ")
	}
}

func TestBusyPromptShowsThinkingSpinner(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.width = 40
	chat.height = 12
	chat.busy = true
	plain := stripANSI(chat.View())
	thinkingLine := ""
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "Thinking") {
			thinkingLine = strings.TrimRight(line, " ")
			break
		}
	}
	if thinkingLine == "" || !strings.HasSuffix(thinkingLine, "esc cancel") {
		t.Fatalf("busy prompt = %q", plain)
	}
	updated, cmd := chat.Update(spinner.TickMsg{})
	if cmd == nil {
		t.Fatal("expected the spinner to keep ticking")
	}
	if !updated.(*chatModel).busy {
		t.Fatal("tick cleared the busy state")
	}
}

func TestQTypesIntoPrompt(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.input.Focus()
	updated, _ := chat.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	chat = updated.(*chatModel)
	if chat.input.Value() != "q" {
		t.Fatalf("prompt = %q", chat.input.Value())
	}
}

func TestEscCancelsBusyAgent(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.width = 40
	chat.height = 12
	chat.busy = true
	cancelled := false
	chat.runCancel = func() { cancelled = true }

	updated, cmd := chat.Update(tea.KeyMsg{Type: tea.KeyEsc})
	chat = updated.(*chatModel)
	if cmd != nil || !cancelled || chat.status != "Cancelling…" {
		t.Fatalf("cancel = %v status = %q cmd = %v", cancelled, chat.status, cmd)
	}

	updated, cmd = chat.Update(agentFinishedMsg{Err: context.Canceled})
	chat = updated.(*chatModel)
	if chat.busy || chat.err != nil || chat.status != "Interrupted" || cmd != nil {
		t.Fatalf("after cancel busy=%v err=%v status=%q", chat.busy, chat.err, chat.status)
	}
	_ = chat.startRun(func(context.Context) error { return nil })
	if chat.status != "" {
		t.Fatalf("status after next command = %q", chat.status)
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

func TestHelpAndSessionsDoNotRunAgent(t *testing.T) {
	home := t.TempDir()
	store, err := session.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	for _, file := range []session.File{
		{ID: "old", Title: "Old chat", Workspace: "/old", Mode: "agent", Updated: older},
		{ID: "new", Title: "New chat", Workspace: "/new", Mode: "ask", Updated: newer},
	} {
		if err := store.Save(file); err != nil {
			t.Fatal(err)
		}
	}
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.width = 60
	chat.height = 20
	chat.workspacePath = "/current"
	chat.sessions = store

	chat.input.SetValue("/help")
	updated, _ := chat.Update(tea.KeyMsg{Type: tea.KeyEnter})
	chat = updated.(*chatModel)
	if chat.busy || !strings.Contains(chat.status, "/agent") || !strings.Contains(chat.status, "/ask") || !strings.Contains(chat.status, "/plan") || !strings.Contains(chat.status, "/sessions") || !strings.Contains(chat.status, "/help") || !strings.Contains(chat.status, "/mode") {
		t.Fatalf("help status = %q busy = %v", chat.status, chat.busy)
	}

	chat.input.SetValue("/sessions")
	updated, _ = chat.Update(tea.KeyMsg{Type: tea.KeyEnter})
	chat = updated.(*chatModel)
	view := stripANSI(chat.View())
	if chat.busy || chat.status != "" || !strings.Contains(view, "New session") || !strings.Contains(view, "/current") {
		t.Fatalf("sessions view = %q", view)
	}
	newAt := strings.Index(view, "New session")
	newerAt := strings.Index(view, "New chat")
	olderAt := strings.Index(view, "Old chat")
	if newAt < 0 || newerAt < 0 || olderAt < 0 || newAt > newerAt || newerAt > olderAt {
		t.Fatalf("order = %q", view)
	}
}

func TestDeleteSessionFromList(t *testing.T) {
	home := t.TempDir()
	store, err := session.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(session.File{ID: "keep", Title: "Keep", Workspace: "/work", Updated: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(session.File{ID: "drop", Title: "Drop", Workspace: "/work", Updated: time.Now().UTC().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.width = 60
	chat.height = 20
	chat.workspacePath = "/work"
	chat.sessions = store
	chat.input.SetValue("/sessions")
	updated, _ := chat.Update(tea.KeyMsg{Type: tea.KeyEnter})
	chat = updated.(*chatModel)
	updated, _ = chat.Update(tea.KeyMsg{Type: tea.KeyDown})
	chat = updated.(*chatModel)
	pressX := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	updated, _ = chat.Update(pressX)
	chat = updated.(*chatModel)
	if !chat.sessionConfirm || !strings.Contains(stripANSI(chat.View()), "x delete Drop") {
		t.Fatalf("confirm view = %q", stripANSI(chat.View()))
	}
	if _, err := store.Load("drop"); err != nil {
		t.Fatal(err)
	}
	updated, _ = chat.Update(pressX)
	chat = updated.(*chatModel)
	if _, err := store.Load("drop"); err == nil {
		t.Fatal("session file was kept")
	}
	if strings.Contains(stripANSI(chat.View()), "Drop") {
		t.Fatal("deleted row is still listed")
	}
	if _, err := store.Load("keep"); err != nil {
		t.Fatal(err)
	}
}

func TestResumeSwapsWorkspace(t *testing.T) {
	current, err := workspace.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other, err := workspace.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	cfg := config.Config{Model: "gpt-6-sol", MaxIterations: 2, OpenAIAPIKey: "test-key", HomeDir: home, Network: "deny"}
	decisions, err := trust.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := decisions.Set(current.Path, true); err != nil || decisions.Set(other.Path, true) != nil {
		t.Fatal(err)
	}
	started, err := app.New(cfg, current, trust.WorkspaceTrusted, nil)
	if err != nil {
		t.Fatal(err)
	}
	model := newProgram(context.Background(), started, decisions)
	saved := session.File{
		ID:        "other-chat",
		Title:     "Elsewhere",
		Workspace: other.Path,
		Mode:      "plan",
		Updated:   time.Now().UTC(),
		Messages:  []gogent.Message{gogent.NewUserMessage("from the other workspace")},
	}
	if err := model.chat.sessions.Save(saved); err != nil {
		t.Fatal(err)
	}
	model.chat.sessionRows = []session.File{saved}
	model.chat.sessionsOpen = true
	model.chat.sessionCursor = 1
	model.chat.width = 80
	model.chat.height = 24
	updated, cmd := model.chat.Update(tea.KeyMsg{Type: tea.KeyEnter})
	chat := updated.(*chatModel)
	if !chat.sessionLoading || !strings.Contains(chat.View(), "Opening session") {
		t.Fatalf("loading view = %q", chat.View())
	}
	var steps []string
	pending := []tea.Cmd{cmd}
	for chat.sessionLoading && len(pending) > 0 {
		next := pending[0]
		pending = pending[1:]
		for _, msg := range runCmds(next) {
			if _, ok := msg.(spinner.TickMsg); ok {
				continue
			}
			if text, ok := msg.(sessionProgressMsg); ok {
				steps = append(steps, text.Text)
			}
			updated, follow := chat.Update(msg)
			chat = updated.(*chatModel)
			if _, done := msg.(sessionLoadedMsg); done {
				continue
			}
			if follow != nil {
				pending = append(pending, follow)
			}
		}
	}
	joined := strings.Join(steps, "\n")
	if !strings.Contains(joined, "Preparing markdown") || !strings.Contains(joined, "Building session") || !strings.Contains(joined, "Rendering message 1 of 1") {
		t.Fatalf("progress = %q", joined)
	}
	if chat.workspacePath != other.Path || chat.chatID != "other-chat" || chat.mode != app.ModePlan {
		t.Fatalf("workspace = %s id = %s mode = %s", chat.workspacePath, chat.chatID, chat.mode)
	}
	if len(chat.messages) != 1 || chat.messages[0].Content != "from the other workspace" {
		t.Fatalf("messages = %+v", chat.messages)
	}
	if chat.sessionsOpen || chat.busy {
		t.Fatal("list stayed open or the agent ran")
	}
}

func runCmds(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		if msg == nil {
			return nil
		}
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, next := range batch {
		out = append(out, runCmds(next)...)
	}
	return out
}

func TestResumeSameWorkspaceKeepsAgent(t *testing.T) {
	current, err := workspace.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	cfg := config.Config{Model: "gpt-4o-mini", MaxIterations: 2, OpenAIAPIKey: "test-key", HomeDir: home, Network: "deny"}
	decisions, err := trust.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := decisions.Set(current.Path, true); err != nil {
		t.Fatal(err)
	}
	started, err := app.New(cfg, current, trust.WorkspaceTrusted, nil)
	if err != nil {
		t.Fatal(err)
	}
	model := newProgram(context.Background(), started, decisions)
	agent := model.chat.agent
	saved := session.File{
		ID:        "same-chat",
		Title:     "Here",
		Workspace: current.Path,
		Mode:      "agent",
		Messages:  []gogent.Message{gogent.NewUserMessage("still here")},
	}
	if err := model.resume(saved); err != nil {
		t.Fatal(err)
	}
	if model.chat.agent != agent {
		t.Fatal("rebuilt the agent for the same workspace")
	}
	if model.chat.chatID != "same-chat" || len(model.chat.messages) != 1 {
		t.Fatalf("chat = %s messages = %d", model.chat.chatID, len(model.chat.messages))
	}
}

func TestPersistWritesAfterFinishedRun(t *testing.T) {
	home := t.TempDir()
	store, err := session.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	messages := inmemory.NewMessageStore()
	chat := newChatModel(context.Background(), nil, messages, gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.sessions = store
	chat.workspacePath = "/work"
	chat.mode = app.ModeAgent
	chat.chatTitle = func(context.Context, string, string) (string, error) {
		return "Generated title", nil
	}
	if _, err := os.ReadDir(filepath.Join(home, "sessions")); err != nil {
		t.Fatal(err)
	}
	cmd := chat.persistSession()
	if msg := cmd(); msg != nil {
		t.Fatal("saved a chat that never ran")
	}
	if err := messages.AddMessages(context.Background(), chat.chatID, gogent.NewUserMessage("hello there"), gogent.NewAssistantMessage("hi")); err != nil {
		t.Fatal(err)
	}
	msg := chat.persistSession()()
	saved, ok := msg.(sessionSavedMsg)
	if !ok || saved.Err != nil || saved.Title != "Generated title" {
		t.Fatalf("saved = %#v", msg)
	}
	loaded, err := store.Load(chat.chatID)
	if err != nil || loaded.Title != "Generated title" || len(loaded.Messages) != 2 {
		t.Fatalf("loaded = %+v err = %v", loaded, err)
	}
	chat.chatTitle = func(context.Context, string, string) (string, error) {
		return "should not replace", nil
	}
	msg = chat.persistSession()()
	saved = msg.(sessionSavedMsg)
	if saved.Title != "Generated title" {
		t.Fatalf("title replaced: %q", saved.Title)
	}
}

func TestSecretToggleWritesSelectedName(t *testing.T) {
	store := inmemory.NewMessageStore()
	chat := newChatModel(context.Background(), nil, store, gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.secretNames = []string{"DEPLOY_TOKEN"}
	message := gogent.NewAssistantMessageWithToolCalls("", []gogent.ToolCall{
		gogent.NewPendingToolCall("call-1", "shell", json.RawMessage(`{"command":"echo hi","secret_names":["openai_api_key"]}`)),
	})
	if err := store.AddMessages(context.Background(), chat.chatID, message); err != nil {
		t.Fatal(err)
	}
	chat.pendingApprovals = []gogent.PendingToolCall{{
		MessageID:  message.ID,
		ToolCallID: "call-1",
		ToolName:   "shell",
	}}
	chat.syncApprovalFocus()
	view := stripANSI(chat.approvalList.View())
	if strings.Contains(view, "openai_api_key") || !strings.Contains(view, "DEPLOY_TOKEN") {
		t.Fatalf("approval list = %q", view)
	}
	chat.approvalList.Select(0)
	chat.toggleSelectedSecret()
	if err := chat.writeSecretNames(context.Background(), message.ID, "call-1", chat.selectedSecretNames("call-1")); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetMessage(context.Background(), chat.chatID, message.ID)
	if err != nil {
		t.Fatal(err)
	}
	args := string(loaded.ToolCalls[0].Args)
	if strings.Contains(args, "openai_api_key") || !strings.Contains(args, "DEPLOY_TOKEN") {
		t.Fatalf("args = %s", args)
	}
}
