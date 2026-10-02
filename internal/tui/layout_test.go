package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/inmemory"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/cgund98/gopi/internal/session"
)

func newLayoutChat(width, height int) *chatModel {
	chat := newChatModel(context.Background(), nil, inmemory.NewMessageStore(), gogent.NewToolRegistry(), gogent.NewChannelBroadcaster())
	chat.width = width
	chat.height = height
	return chat
}

// renderedRows reads the wrapped row count back out of the textarea by finding
// the cursor row. Padding rows follow it, so the cursor row index plus one is
// the content height. found is false once the content is tall enough that the
// textarea's own viewport has moved the cursor out of view.
func renderedRows(t *testing.T, input string, width int) (int, bool) {
	t.Helper()
	lipgloss.SetColorProfile(termenv.TrueColor)
	chat := newLayoutChat(width, 40)
	chat.input.SetValue(input)
	chat.input.CursorEnd()
	// Force the cursor visible; while blinking it renders without the reverse
	// marker this helper looks for.
	chat.input.Cursor.Blink = false
	chat.syncComposer(chat.composerWidth())
	// Measure with the composer at full height so the row count does not depend
	// on the estimate under test. Content taller than this scrolls, which is
	// how the helper reports "not visible".
	chat.input.SetHeight(maxComposerLines)
	for i, row := range strings.Split(chat.input.View(), "\n") {
		if strings.Contains(row, "\x1b[7m") {
			return i + 1, true
		}
	}
	return 0, false
}

func TestComposerRowsMatchTextareaWrap(t *testing.T) {
	for _, width := range []int{20, 24, 30, 40, 60, 80} {
		for _, pattern := range []string{"a", "aa ", "hello ", "word ", "x y "} {
			for n := 0; n <= 160; n++ {
				input := strings.Repeat(pattern, n/len(pattern)+1)[:n]
				chat := newLayoutChat(width, 40)
				chat.input.SetValue(input)
				chat.input.CursorEnd()
				chat.syncComposer(chat.composerWidth())

				got := chat.composerRows()
				rows, visible := renderedRows(t, input, width)
				if !visible || rows > maxComposerLines {
					// The textarea scrolled, so the composer is capped.
					if got != maxComposerLines {
						t.Fatalf("width=%d pattern=%q n=%d: composerRows=%d, want the %d-row cap", width, pattern, n, got, maxComposerLines)
					}
					continue
				}
				if got != rows {
					t.Fatalf("width=%d pattern=%q n=%d input=%q: composerRows=%d, textarea drew %d", width, pattern, n, input, got, rows)
				}
			}
		}
	}
}

func TestComposerPromptRowStaysVisibleWhenTyping(t *testing.T) {
	for _, width := range []int{20, 24, 30, 40, 60, 80} {
		for _, pattern := range []string{"a", "aa ", "hello ", "word "} {
			chat := newLayoutChat(width, 40)
			value := strings.Repeat(pattern, 30)
			for i, r := range value {
				updated, _ := chat.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
				chat = updated.(*chatModel)
				_ = chat.View()
				if chat.composerRows() >= maxComposerLines {
					continue // the input fills the composer, so scrolling is expected
				}
				first := strings.Split(chat.input.View(), "\n")[0]
				if !strings.Contains(first, "agent") {
					t.Fatalf("width=%d pattern=%q after %q: prompt row scrolled off\nfirst=%q",
						width, pattern, value[:i+1], first)
				}
			}
		}
	}
}

func TestViewNeverExceedsTerminal(t *testing.T) {
	for _, height := range []int{8, 10, 16, 24, 40} {
		for _, width := range []int{20, 24, 40, 60, 100} {
			for n := 0; n <= 200; n += 3 {
				input := strings.Repeat("word ", n/5+1)[:n]
				chat := newLayoutChat(width, height)
				chat.input.SetValue(input)
				chat.input.CursorEnd()
				lines := strings.Split(chat.View(), "\n")
				if len(lines) > height {
					t.Fatalf("height=%d width=%d n=%d: view has %d lines", height, width, n, len(lines))
				}
				for i, line := range lines {
					if w := ansi.StringWidth(line); w > width {
						t.Fatalf("height=%d width=%d n=%d line=%d ansiW=%d", height, width, n, i, w)
					}
				}
			}
		}
	}
}

func TestSessionTitleTruncated(t *testing.T) {
	chat := &chatModel{width: 40, height: 20, sessionsHome: "/work"}
	chat.sessionRows = []session.File{{
		Title:     strings.Repeat("a very long session name ", 8),
		Workspace: "/work/project/with/a/long/path",
	}}
	view := stripANSI(chat.renderSessions())
	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > chat.width {
			t.Fatalf("line %d width %d > %d: %q", i, w, chat.width, line)
		}
	}
	if !strings.Contains(view, "…") {
		t.Fatalf("long title was not truncated:\n%s", view)
	}
}

func TestApprovalTabScrollsHistory(t *testing.T) {
	chat := newLayoutChat(50, 20)
	var b strings.Builder
	for range 60 {
		b.WriteString("history line\n")
	}
	chat.messages = []gogent.Message{{Role: gogent.MessageRoleUser, Content: b.String()}}
	chat.pendingApprovals = []gogent.PendingToolCall{{
		MessageID:  "m1",
		ToolCallID: "t1",
		ToolName:   "shell",
		Args:       json.RawMessage(`{"command":"ls"}`),
	}}
	chat.activeToolCallID = "t1"
	chat.selectedTool = -1

	_ = chat.View()
	chat.refreshTranscript(true)
	if chat.transcriptVP.YOffset == 0 {
		t.Fatal("history is not scrollable, so the test cannot observe scrolling")
	}

	updated, _ := chat.Update(tea.KeyMsg{Type: tea.KeyTab})
	chat = updated.(*chatModel)
	if !chat.approvalFocusHistory {
		t.Fatal("tab did not move focus to the history")
	}

	before := chat.transcriptVP.YOffset
	updated, _ = chat.Update(tea.KeyMsg{Type: tea.KeyUp})
	chat = updated.(*chatModel)
	if chat.transcriptVP.YOffset >= before {
		t.Fatalf("up did not scroll the history: %d -> %d", before, chat.transcriptVP.YOffset)
	}
	if len(chat.pendingApprovals) != 1 {
		t.Fatal("scrolling dropped the pending approval")
	}

	updated, _ = chat.Update(tea.KeyMsg{Type: tea.KeyTab})
	chat = updated.(*chatModel)
	if chat.approvalFocusHistory {
		t.Fatal("tab did not move focus back to the choices")
	}
}

func TestApprovalPromptFitsWindow(t *testing.T) {
	for _, height := range []int{7, 8, 10, 16, 24, 40} {
		for _, width := range []int{20, 40, 60, 100} {
			chat := newLayoutChat(width, height)
			chat.pendingApprovals = []gogent.PendingToolCall{{
				MessageID:  "m1",
				ToolCallID: "t1",
				ToolName:   "shell",
				Args:       json.RawMessage(`{"command":"` + strings.Repeat("echo a-very-long-command ", 40) + `"}`),
				Reason:     strings.Repeat("why this needs approval ", 20),
			}}
			chat.activeToolCallID = "t1"
			chat.selectedTool = -1

			view := chat.View()
			if lines := strings.Split(view, "\n"); len(lines) > height {
				t.Fatalf("height=%d width=%d: approval view has %d lines", height, width, len(lines))
			}
			for i, line := range strings.Split(view, "\n") {
				if w := ansi.StringWidth(line); w > width {
					t.Fatalf("height=%d width=%d line=%d ansiW=%d", height, width, i, w)
				}
			}
			// A tall window can show the whole prompt; a short one cannot.
			if height <= 16 && !strings.Contains(stripANSI(view), "more lines") {
				t.Fatalf("height=%d width=%d: long approval prompt was not truncated", height, width)
			}
			if chat.transcriptVP.Height < 1 {
				t.Fatalf("height=%d width=%d: transcript got %d rows", height, width, chat.transcriptVP.Height)
			}
		}
	}
}

// approvalViewChat builds a chat with one pending shell approval whose command
// carries a unique token, so a test can count how often the frame renders it.
func approvalViewChat(t *testing.T, width, height int, command string) *chatModel {
	t.Helper()
	chat := newLayoutChat(width, height)
	message := gogent.Message{
		ID:   "a1",
		Role: gogent.MessageRoleAssistant,
		ToolCalls: []gogent.ToolCall{{
			ID:       "t1",
			ToolName: "shell",
			Args:     json.RawMessage(`{"command":"` + command + `"}`),
			Reason:   "Profile: sandbox -> unsandboxed",
		}},
	}
	if err := chat.store.AddMessages(context.Background(), chat.chatID, message); err != nil {
		t.Fatal(err)
	}
	if err := chat.refreshFromStore(); err != nil {
		t.Fatal(err)
	}
	if len(chat.pendingApprovals) == 0 {
		t.Fatal("the tool call was not pending approval")
	}
	chat.syncApprovalFocus()
	chat.refreshTranscript(chat.followChatEnd)
	chat.applyLayout()
	return chat
}

// The transcript renders the tool card, and the approval block renders why the
// call paused. Nothing is drawn twice.
func TestApprovalRendersCommandOnce(t *testing.T) {
	const token = "UNIQUETOKEN"
	for _, width := range []int{40, 60, 100} {
		for _, height := range []int{12, 24, 40} {
			chat := approvalViewChat(t, width, height, "printf "+token)
			view := stripANSI(chat.View())
			if got := strings.Count(view, token); got != 1 {
				t.Fatalf("width=%d height=%d: command rendered %d times, want 1\n%s", width, height, got, view)
			}
			if !strings.Contains(view, "Profile: sandbox") {
				t.Fatalf("width=%d height=%d: approval reason missing\n%s", width, height, view)
			}
		}
	}
}

func TestApprovalRendersReasonOnce(t *testing.T) {
	chat := approvalViewChat(t, 60, 24, "printf hi")
	if got := strings.Count(stripANSI(chat.View()), "Profile: sandbox -> unsandboxed"); got != 1 {
		t.Fatalf("reason rendered %d times, want 1", got)
	}
}

func TestApprovalPromptKeepsHistoryRows(t *testing.T) {
	chat := newLayoutChat(60, 20)
	chat.pendingApprovals = []gogent.PendingToolCall{{
		MessageID:  "m1",
		ToolCallID: "t1",
		ToolName:   "shell",
		Args:       json.RawMessage(`{"command":"` + strings.Repeat("echo long ", 40) + `"}`),
	}}
	chat.activeToolCallID = "t1"
	chat.selectedTool = -1
	_ = chat.View()
	if chat.transcriptVP.Height < approvalMinTranscript {
		t.Fatalf("transcript got %d rows, want at least %d", chat.transcriptVP.Height, approvalMinTranscript)
	}
}
