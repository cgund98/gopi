package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/inmemory"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cgund98/gopi/internal/app"
	"github.com/cgund98/gopi/internal/config"
	"github.com/cgund98/gopi/internal/models"
	gopisecrets "github.com/cgund98/gopi/internal/secrets"
	"github.com/cgund98/gopi/internal/session"
	"github.com/cgund98/gopi/internal/tools"
	"github.com/cgund98/gopi/internal/trust"
	"github.com/cgund98/gopi/internal/workspace"
)

func TestStatusSuffixShowsModelWorkspaceAndThinking(t *testing.T) {
	idle := strings.TrimRight(stripANSI(renderStatusSuffix("gpt-6-sol", "/tmp/gopi", "", 12, false, "", 48)), " ")
	if !strings.HasPrefix(idle, "/tmp/gopi") || !strings.HasSuffix(idle, "gpt-6-sol | 12%") || strings.Contains(idle, "thinking") {
		t.Fatalf("idle suffix = %q", idle)
	}
	busy := strings.TrimRight(stripANSI(renderStatusSuffix("gpt-6-sol", "/tmp/gopi", "", 12, true, "2s", 48)), " ")
	if !strings.HasPrefix(busy, "/tmp/gopi") || !strings.HasSuffix(busy, "gpt-6-sol | 12% | thinking 2s") {
		t.Fatalf("busy suffix = %q", busy)
	}
}

func TestFinishWorkIncludesToolTime(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.width = 60
	chat.height = 20
	chat.busy = true
	chat.workStarted = time.Now().Add(-2 * time.Second)
	assistant := gogent.NewAssistantMessageWithToolCalls("", []gogent.ToolCall{{
		ID:       "call-1",
		ToolName: "shell",
	}})
	assistant.Usage = &gogent.Usage{Input: 10, Output: 1}
	final := gogent.NewAssistantMessage("done")
	final.Usage = &gogent.Usage{Input: 28400, Output: 826}
	chat.messages = []gogent.Message{
		gogent.NewUserMessage("git status"),
		assistant,
		gogent.NewToolResultMessage("call-1", `{"stdout":"ok"}`),
		final,
	}
	if chat.thinkingLabel() == "" {
		t.Fatal("timer stopped while the tool ran")
	}
	chat.busy = false
	chat.finishWork()
	if _, ok := chat.turnWork[assistant.ID]; ok {
		t.Fatal("stamped the tool-call step")
	}
	if chat.turnWork[final.ID] < 2*time.Second {
		t.Fatalf("work = %s", chat.turnWork[final.ID])
	}
	chat.refreshTranscript(true)
	plain := stripANSI(chat.View())
	if strings.Contains(plain, "Thought") || !strings.Contains(plain, "28.4k/826  Worked for") {
		t.Fatalf("view = %q", plain)
	}
}

func TestContextPercentUsesTranscript(t *testing.T) {
	if got := contextPercent("", "", nil, ""); got != 0 {
		t.Fatalf("empty = %d", got)
	}
	messages := []gogent.Message{{Content: strings.Repeat("a", models.ContextWindow("")*4)}}
	if got := contextPercent("", "", messages, ""); got != 100 {
		t.Fatalf("full = %d", got)
	}
}

func TestSubagentLabel(t *testing.T) {
	now := time.Now()
	status := tools.DelegateStatus{Started: now.Add(-42 * time.Second), ToolCalls: 3, Last: "grep resume"}
	if got := subagentLabel(status, now); got != "Subagent 42s · 3 tool calls · grep resume" {
		t.Fatalf("label = %q", got)
	}
	if got := subagentLabel(tools.DelegateStatus{Started: now}, now); got != "Subagent 0s · starting" {
		t.Fatalf("starting label = %q", got)
	}
}

func TestContextPercentCountsResultsAfterLastUsage(t *testing.T) {
	window := models.ContextWindow("kimi/kimi-k2.6")
	call := gogent.NewAssistantMessageWithToolCalls("", []gogent.ToolCall{{ID: "g1", ToolName: "grep", Args: json.RawMessage(`{"pattern":"resume"}`)}})
	call.Usage = &gogent.Usage{Input: window * 3 / 100}
	result := gogent.Message{Role: gogent.MessageRoleTool, ToolCallID: "g1", Content: strings.Repeat("x", window*4/2)}
	if got := contextPercent("kimi/kimi-k2.6", "", []gogent.Message{call}, ""); got != 3 {
		t.Fatalf("before result = %d", got)
	}
	if got := contextPercent("kimi/kimi-k2.6", "", []gogent.Message{call, result}, ""); got != 53 {
		t.Fatalf("after a large tool result = %d, want 53", got)
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

func TestWheelScrollsWhileAgentRuns(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.busy = true
	chat.followChatEnd = true
	chat.width = 40
	var lines []string
	for range 30 {
		lines = append(lines, "line")
	}
	chat.messages = []gogent.Message{{
		Role:    gogent.MessageRoleUser,
		Content: strings.Join(lines, "\n"),
	}}
	chat.transcriptVP.Width = 40
	chat.transcriptVP.Height = 6
	chat.refreshTranscript(true)
	start := chat.transcriptVP.YOffset
	if start == 0 {
		t.Fatal("history is not scrollable")
	}

	updated, _ := chat.Update(tea.KeyMsg{Type: tea.KeyUp})
	chat = updated.(*chatModel)
	if !chat.busy || chat.transcriptVP.YOffset >= start || chat.followChatEnd {
		t.Fatalf("busy=%v offset %d -> %d follow=%v", chat.busy, start, chat.transcriptVP.YOffset, chat.followChatEnd)
	}

	held := chat.transcriptVP.YOffset
	chat.messages = append(chat.messages, gogent.Message{Role: gogent.MessageRoleAssistant, Content: "more output"})
	chat.refreshTranscript(chat.followChatEnd)
	if chat.transcriptVP.YOffset != held {
		t.Fatalf("new output jumped %d -> %d", held, chat.transcriptVP.YOffset)
	}
}

func TestPasteKeepsNewlines(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.width = 60
	chat.height = 24
	updated, _ := chat.Update(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("line one\r\nline two")})
	chat = updated.(*chatModel)
	if chat.input.Value() != "line one\nline two" || chat.input.LineCount() != 2 {
		t.Fatalf("value = %q lines = %d", chat.input.Value(), chat.input.LineCount())
	}
	plain := stripANSI(chat.View())
	if !strings.Contains(plain, "line one") || !strings.Contains(plain, "line two") {
		t.Fatalf("view = %q", plain)
	}
}

type shiftEnterMsg string

func (m shiftEnterMsg) String() string { return string(m) }

func TestShiftEnterInsertsNewline(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.input.SetValue("hello")
	chat.input.CursorEnd()

	updated, cmd := chat.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	chat = updated.(*chatModel)
	if cmd != nil || chat.busy || chat.input.Value() != "hello\n" {
		t.Fatalf("alt+enter cmd=%v busy=%v value=%q", cmd, chat.busy, chat.input.Value())
	}

	updated, cmd = chat.Update(shiftEnterMsg(shiftEnterKitty))
	chat = updated.(*chatModel)
	if cmd != nil || chat.busy || chat.input.Value() != "hello\n\n" {
		t.Fatalf("kitty shift+enter cmd=%v busy=%v value=%q", cmd, chat.busy, chat.input.Value())
	}

	updated, cmd = chat.Update(shiftEnterMsg(shiftEnterModifyOther))
	chat = updated.(*chatModel)
	if cmd != nil || chat.busy || chat.input.Value() != "hello\n\n\n" {
		t.Fatalf("modifyOtherKeys shift+enter cmd=%v busy=%v value=%q", cmd, chat.busy, chat.input.Value())
	}

	updated, cmd = chat.Update(tea.KeyMsg{Type: tea.KeyEnter})
	chat = updated.(*chatModel)
	if cmd == nil || !chat.busy || chat.input.Value() != "" {
		t.Fatalf("enter cmd=%v busy=%v value=%q", cmd, chat.busy, chat.input.Value())
	}

	updated, _ = chat.Update(shiftEnterMsg(shiftEnterKitty))
	if updated.(*chatModel).input.Value() != "" {
		t.Fatal("shift+enter changed the prompt while a run was in progress")
	}
}

func TestKittyKeysStayUsable(t *testing.T) {
	ctrl := translateKitty(shiftEnterMsg(csiReport("99;5u")))
	key, ok := ctrl.(tea.KeyMsg)
	if !ok || key.String() != "ctrl+c" {
		t.Fatalf("ctrl+c = %#v", ctrl)
	}
	esc := translateKitty(shiftEnterMsg(csiReport("27u")))
	key, ok = esc.(tea.KeyMsg)
	if !ok || key.String() != "esc" {
		t.Fatalf("esc = %#v", esc)
	}
	shift := translateKitty(shiftEnterMsg(shiftEnterKitty))
	if _, ok := shift.(tea.KeyMsg); ok {
		t.Fatal("shift+enter was turned into enter")
	}

	model := &programModel{
		phase: phaseChat,
		chat:  newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster()),
	}
	model.chat.input.SetValue("hello")
	model.chat.input.CursorEnd()
	updated, cmd := model.Update(shiftEnterMsg(shiftEnterKitty))
	model = updated.(*programModel)
	if cmd != nil || model.chat.busy || model.chat.input.Value() != "hello\n" {
		t.Fatalf("shift+enter cmd=%v busy=%v value=%q", cmd, model.chat.busy, model.chat.input.Value())
	}

	updated, cmd = model.Update(shiftEnterMsg(csiReport("99;5u")))
	model = updated.(*programModel)
	if cmd != nil || model.chat.input.Value() != "" {
		t.Fatalf("ctrl+c cmd=%v value=%q", cmd, model.chat.input.Value())
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

func TestEscClearsDraftAndQuitsWhenEmpty(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.input.SetValue("draft")
	updated, cmd := chat.Update(tea.KeyMsg{Type: tea.KeyEsc})
	chat = updated.(*chatModel)
	if cmd != nil || chat.input.Value() != "" {
		t.Fatalf("esc value = %q cmd = %v", chat.input.Value(), cmd)
	}
	chat.input.SetValue("/model")
	chat.syncComplete()
	updated, cmd = chat.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	chat = updated.(*chatModel)
	if cmd != nil || chat.input.Value() != "" || chat.completeOpen {
		t.Fatalf("ctrl+c value = %q open = %v cmd = %v", chat.input.Value(), chat.completeOpen, cmd)
	}
	_, cmd = chat.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc on an empty prompt should quit")
	}
}

func TestEscCancelsBusyAgent(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.width = 40
	chat.height = 12
	chat.busy = true
	canceled := false
	chat.runCancel = func() { canceled = true }

	updated, cmd := chat.Update(tea.KeyMsg{Type: tea.KeyEsc})
	chat = updated.(*chatModel)
	if cmd != nil || !canceled || chat.status != "Canceling…" {
		t.Fatalf("cancel = %v status = %q cmd = %v", canceled, chat.status, cmd)
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
	if chat.busy || !strings.Contains(chat.status, "/agent") || !strings.Contains(chat.status, "/ask") || !strings.Contains(chat.status, "/plan") || !strings.Contains(chat.status, "/sessions") || !strings.Contains(chat.status, "/plans") || !strings.Contains(chat.status, "/review") || !strings.Contains(chat.status, "/help") || !strings.Contains(chat.status, "/mode") || !strings.Contains(chat.status, "/model") || !strings.Contains(chat.status, "/compact") {
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
	cfg := config.Config{Model: "gpt-5.6-luna", MaxIterations: 2, OpenAIAPIKey: "test-key", HomeDir: home, Network: "deny"}
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
	cfg := config.Config{Model: "gpt-5.6-luna", MaxIterations: 2, OpenAIAPIKey: "test-key", HomeDir: home, Network: "deny"}
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
		ID:         "same-chat",
		Title:      "Here",
		Workspace:  current.Path,
		Mode:       "agent",
		Messages:   []gogent.Message{gogent.NewUserMessage("still here")},
		ReadGrants: []string{"/tmp/session-grant"},
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
	if !model.session.Grants.Covers("/tmp/session-grant/note.txt") {
		t.Fatalf("grants = %#v", model.session.Grants.List())
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
	grants := &tools.ReadGrants{}
	grants.Add("/tmp/session-grant")
	chat.readGrants = grants
	msg = chat.persistSession()()
	saved = msg.(sessionSavedMsg)
	if saved.Title != "Generated title" {
		t.Fatalf("title replaced: %q", saved.Title)
	}
	loaded, err = store.Load(chat.chatID)
	if err != nil || len(loaded.ReadGrants) != 1 || loaded.ReadGrants[0] != "/tmp/session-grant" {
		t.Fatalf("grants = %#v err = %v", loaded.ReadGrants, err)
	}
}

func TestSecretToggleWritesSelectedName(t *testing.T) {
	store := inmemory.NewMessageStore()
	chat := newChatModel(context.Background(), nil, store, gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.secretNames = []string{"DEPLOY_TOKEN"}
	message := gogent.NewAssistantMessageWithToolCalls("", []gogent.ToolCall{
		gogent.NewPendingToolCall("call-1", "shell", json.RawMessage(`{"command":"echo hi","secret_names":["`+gopisecrets.OpenAIAPIKey+`"]}`)),
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
	if strings.Contains(view, gopisecrets.OpenAIAPIKey) || !strings.Contains(view, "DEPLOY_TOKEN") {
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
	if strings.Contains(args, gopisecrets.OpenAIAPIKey) || !strings.Contains(args, "DEPLOY_TOKEN") {
		t.Fatalf("args = %s", args)
	}
}

func TestReviewTreePutsDirectoryAboveItsFile(t *testing.T) {
	chat := &chatModel{}
	got := stripANSI(chat.renderReviewTree([]session.ReviewEntry{
		{Path: "main.go"},
		{Path: "internal/tui/review.go"},
		{Path: "internal/diff.go"},
		{Path: "cmd/main.go"},
		{Path: "after.go"},
	}, 40, 20))
	want := "Files\nafter.go\nmain.go\n\ncmd/\n  main.go\n\ninternal/\n  diff.go\n\n  tui/\n    review.go"
	if got != want {
		t.Fatalf("tree = %q", got)
	}
}

func TestReviewScrollsWrappedFileToTop(t *testing.T) {
	work := t.TempDir()
	var before, after strings.Builder
	for i := 0; i < 40; i++ {
		line := fmt.Sprintf("line %02d\t\t%s\n", i, strings.Repeat("\twide", 20))
		before.WriteString(line)
		if i == 39 {
			line = "changed\n"
		}
		after.WriteString(line)
	}
	if err := os.WriteFile(filepath.Join(work, "wide.txt"), []byte(after.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := session.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NoteEdit("chat", "wide.txt", true, false, []byte(before.String())); err != nil {
		t.Fatal(err)
	}
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.chatID = "chat"
	chat.sessions = store
	chat.workspacePath = work
	chat.width = 80
	chat.height = 20
	chat.reloadReview()
	chat.openReview()
	press := func(key tea.KeyMsg) {
		updated, _ := chat.Update(key)
		chat = updated.(*chatModel)
	}
	press(tea.KeyMsg{Type: tea.KeyTab})
	view := stripANSI(chat.View())
	if lines := strings.Count(view, "\n") + 1; lines > chat.height {
		t.Fatalf("view is %d lines tall for height %d", lines, chat.height)
	}
	if strings.Contains(view, "line 00") {
		t.Fatal("expected the view to open at the edit, not the top")
	}
	start := chat.reviewScroll
	press(tea.KeyMsg{Type: tea.KeyShiftUp})
	if chat.reviewScroll != start-fastScrollLines {
		t.Fatalf("shift+up moved from %d to %d", start, chat.reviewScroll)
	}
	for i := 0; i < 40; i++ {
		press(tea.KeyMsg{Type: tea.KeyShiftUp})
	}
	view = stripANSI(chat.View())
	if !strings.Contains(view, "line 00") || !strings.Contains(view, "wide.txt") {
		t.Fatalf("top line or file header missing:\n%s", view)
	}
	if strings.Contains(view, "\t") {
		t.Fatal("tabs reach the terminal and wrap past the pane width")
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > chat.width {
			t.Fatalf("line wider than %d: %q", chat.width, line)
		}
	}
	if lines := strings.Count(view, "\n") + 1; lines > chat.height {
		t.Fatalf("view is %d lines tall for height %d", lines, chat.height)
	}
	press(tea.KeyMsg{Type: tea.KeyDown})
	if chat.reviewScroll != 1 {
		t.Fatalf("down moved to %d", chat.reviewScroll)
	}
	press(tea.KeyMsg{Type: tea.KeyShiftDown})
	if chat.reviewScroll != 1+fastScrollLines {
		t.Fatalf("shift+down moved to %d", chat.reviewScroll)
	}
}

func TestMouseWheelScrollsTranscript(t *testing.T) {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.transcriptVP.Width = 40
	chat.transcriptVP.Height = 5
	var body strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&body, "row %d\n", i)
	}
	chat.transcriptVP.SetContent(body.String())
	chat.transcriptVP.GotoBottom()
	bottom := chat.transcriptVP.YOffset
	chat.busy = true
	updated, _ := chat.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	chat = updated.(*chatModel)
	if chat.transcriptVP.YOffset != bottom-wheelLines || chat.followChatEnd {
		t.Fatalf("offset = %d from %d follow = %v", chat.transcriptVP.YOffset, bottom, chat.followChatEnd)
	}
	updated, _ = chat.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	chat = updated.(*chatModel)
	if chat.transcriptVP.YOffset != bottom || !chat.followChatEnd {
		t.Fatalf("offset = %d follow = %v", chat.transcriptVP.YOffset, chat.followChatEnd)
	}
}

const reviewBaseline = "one\ntwo\nthree\nfour\nfive\nsix\nseven\n"
const reviewEdited = "one\nTWO\nthree\nfour\nfive\nSIX\nseven\nEIGHT\n"

func openReviewFor(t *testing.T, current string, created bool, before []byte) (*chatModel, string, *session.Store) {
	t.Helper()
	work := t.TempDir()
	path := filepath.Join(work, "main.txt")
	if err := os.WriteFile(path, []byte(current), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := session.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NoteEdit("chat", "main.txt", true, created, before); err != nil {
		t.Fatal(err)
	}
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.chatID = "chat"
	chat.sessions = store
	chat.workspacePath = work
	chat.width = 100
	chat.height = 30
	chat.openReview()
	return chat, path, store
}

func pressReview(t *testing.T, chat *chatModel, key rune) *chatModel {
	t.Helper()
	updated, _ := chat.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	return updated.(*chatModel)
}

func TestReviewApproveFileKeepsEdits(t *testing.T) {
	chat, path, store := openReviewFor(t, reviewEdited, false, []byte(reviewBaseline))
	if _, hunks := chat.currentReview(); len(hunks) < 2 {
		t.Fatalf("hunks = %d, want several", len(hunks))
	}
	chat = pressReview(t, chat, 'A')
	body, _ := os.ReadFile(path)
	if string(body) != reviewEdited {
		t.Fatalf("file = %q", body)
	}
	loaded, err := store.Load("chat")
	if err != nil {
		t.Fatal(err)
	}
	if chat.reviewOpen || chat.pendingReviewCount() != 0 || len(loaded.Review) != 0 {
		t.Fatalf("open = %v pending = %d review = %+v", chat.reviewOpen, chat.pendingReviewCount(), loaded.Review)
	}
}

func TestReviewRejectFileRestoresBaseline(t *testing.T) {
	chat, path, store := openReviewFor(t, reviewEdited, false, []byte(reviewBaseline))
	chat = pressReview(t, chat, 'X')
	if chat.reviewErr != "" {
		t.Fatal(chat.reviewErr)
	}
	body, _ := os.ReadFile(path)
	if string(body) != reviewBaseline {
		t.Fatalf("file = %q", body)
	}
	loaded, err := store.Load("chat")
	if err != nil {
		t.Fatal(err)
	}
	if chat.reviewOpen || len(loaded.Review) != 0 {
		t.Fatalf("open = %v review = %+v", chat.reviewOpen, loaded.Review)
	}
}

func TestReviewRejectCreatedFileRemovesIt(t *testing.T) {
	chat, path, _ := openReviewFor(t, reviewEdited, true, nil)
	chat = pressReview(t, chat, 'X')
	if chat.reviewErr != "" {
		t.Fatal(chat.reviewErr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("created file still exists: %v", err)
	}
}

func TestReviewRejectFileLeavesStaleFileAlone(t *testing.T) {
	chat, path, _ := openReviewFor(t, reviewEdited, false, []byte(reviewBaseline))
	entry, hunks := chat.currentReview()
	changed := strings.Replace(reviewEdited, "SIX", "six-ish", 1)
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := chat.rejectFile(entry, hunks); err == nil {
		t.Fatal("stale hunks were applied")
	}
	body, _ := os.ReadFile(path)
	if string(body) != changed {
		t.Fatalf("file = %q", body)
	}
}

func TestReviewRejectsHunkAndClearsBanner(t *testing.T) {
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte("package beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := session.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NoteEdit("chat", "main.go", true, false, []byte("package alpha\n")); err != nil {
		t.Fatal(err)
	}
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.chatID = "chat"
	chat.sessions = store
	chat.workspacePath = work
	chat.width = 80
	chat.height = 24
	chat.reloadReview()
	if !strings.Contains(stripANSI(chat.View()), "Review pending · 1 file") {
		t.Fatalf("banner = %q", stripANSI(chat.View()))
	}
	chat.input.SetValue("/review")
	updated, _ := chat.Update(tea.KeyMsg{Type: tea.KeyEnter})
	chat = updated.(*chatModel)
	if chat.busy || !strings.Contains(stripANSI(chat.View()), "main.go") {
		t.Fatalf("review view = %q busy = %v", stripANSI(chat.View()), chat.busy)
	}
	updated, _ = chat.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	chat = updated.(*chatModel)
	body, err := os.ReadFile(filepath.Join(work, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "package alpha\n" {
		t.Fatalf("file = %q", body)
	}
	updated, _ = chat.Update(tea.KeyMsg{Type: tea.KeyEsc})
	chat = updated.(*chatModel)
	if strings.Contains(stripANSI(chat.View()), "Review pending") {
		t.Fatalf("banner stayed = %q", stripANSI(chat.View()))
	}
	loaded, err := store.Load("chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Review) != 0 {
		t.Fatalf("review = %+v", loaded.Review)
	}
	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte("package manual\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chat.reloadReview()
	if chat.pendingReviewCount() != 0 {
		t.Fatalf("manual edit pending = %d", chat.pendingReviewCount())
	}
}
