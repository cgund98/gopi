package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cgund98/gogent"
	"github.com/charmbracelet/lipgloss"

	gopisecrets "github.com/cgund98/gopi/internal/secrets"
)

func TestEditHeadlineAndDiff(t *testing.T) {
	card := toolCardView{
		ToolName: "edit_file",
		Args:     json.RawMessage(`{"path":"demo.txt","old":"alpha","new":"beta"}`),
		State:    toolCardCompleted,
		Result:   `{"path":"demo.txt","status":"updated"}`,
	}
	if got := toolHeadline(card); got != "edit demo.txt" {
		t.Fatalf("headline = %q", got)
	}
	body := renderToolBody(card, 80)
	if !strings.Contains(body, "demo.txt") || !strings.Contains(body, "alpha") || !strings.Contains(body, "beta") || !strings.Contains(body, "+1") || !strings.Contains(body, "−1") {
		t.Fatalf("diff = %q", body)
	}
	if !strings.Contains(body, "╭") || !strings.Contains(body, "1") {
		t.Fatalf("frame = %q", body)
	}
	assertFrameContainsLines(t, body)
	if !hideToolResult(card) {
		t.Fatal("successful edit should hide the JSON result")
	}
}

func TestDiffHeaderFitsFrame(t *testing.T) {
	card := toolCardView{
		ToolName: "edit_file",
		Args:     json.RawMessage(`{"path":"internal/very/long/path/that/should/not/spill/past/the/border/demo.txt","old":"alpha","new":"beta"}`),
	}
	assertFrameContainsLines(t, renderToolBody(card, 40))
}

func assertFrameContainsLines(t *testing.T, framed string) {
	t.Helper()
	lines := strings.Split(stripANSI(framed), "\n")
	if len(lines) == 0 {
		t.Fatal("empty frame")
	}
	limit := lipgloss.Width(lines[0])
	for _, line := range lines {
		if w := lipgloss.Width(line); w > limit {
			t.Fatalf("line width %d exceeds frame width %d: %q", w, limit, line)
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestReadResultStaysHidden(t *testing.T) {
	card := toolCardView{
		ToolName: "read_file",
		Result:   `{"path":"demo.txt","content":"hello"}`,
	}
	if !hideToolResult(card) {
		t.Fatal("successful read should hide its output")
	}
	failed := card
	failed.Result = `{"error":"access_denied","path":"demo.txt","message":"refused"}`
	if hideToolResult(failed) {
		t.Fatal("failed read should stay visible")
	}
	for _, name := range []string{"grep", "find"} {
		if !hideToolResult(toolCardView{ToolName: name, Result: `{"files":["a.go"]}`}) {
			t.Fatalf("%s result should stay hidden", name)
		}
	}
}

func TestToolErrorIsSentence(t *testing.T) {
	got := renderToolError(`{"error":"access_denied","path":"note.txt","message":"workspace is not trusted; edits are refused"}`, 80)
	plain := stripANSI(got)
	if strings.Contains(plain, `"error"`) || !strings.Contains(plain, "Access denied. note.txt: workspace is not trusted") {
		t.Fatalf("error = %q", plain)
	}
}

func TestCreateHeadline(t *testing.T) {
	card := toolCardView{
		ToolName: "edit_file",
		Args:     json.RawMessage(`{"path":"demo.txt","old":"","new":"hi"}`),
	}
	if got := toolHeadline(card); got != "create demo.txt" {
		t.Fatalf("headline = %q", got)
	}
}

func TestReadAndGrepHeadlines(t *testing.T) {
	read := toolCardView{ToolName: "read_file", Args: json.RawMessage(`{"path":"main.go"}`)}
	if got := toolHeadline(read); got != "read main.go" {
		t.Fatalf("read headline = %q", got)
	}
	grep := toolCardView{ToolName: "grep", Args: json.RawMessage(`{"pattern":"func ","path":"internal"}`)}
	if got := toolHeadline(grep); got != "grep func  in internal" {
		t.Fatalf("grep headline = %q", got)
	}
	find := toolCardView{ToolName: "find", Args: json.RawMessage(`{"pattern":".go"}`)}
	if got := toolHeadline(find); got != "find .go" {
		t.Fatalf("find headline = %q", got)
	}
	shell := toolCardView{ToolName: "shell", Args: json.RawMessage(`{"command":"go test"}`)}
	if got := toolHeadline(shell); got != "shell go test" {
		t.Fatalf("shell headline = %q", got)
	}
}

func TestShellCommandWraps(t *testing.T) {
	command := strings.Repeat("echo ", 20) + "done"
	card := toolCardView{ToolName: "shell", Args: json.RawMessage(`{"command":"` + command + `"}`)}
	rendered := stripANSI(renderInlineToolBlock(card, false, 40))
	lines := strings.Split(rendered, "\n")
	if len(lines) < 2 {
		t.Fatalf("command stayed on one line: %q", rendered)
	}
	for _, line := range lines {
		if w := lipgloss.Width(line); w > 40 {
			t.Fatalf("line width %d exceeds 40: %q", w, line)
		}
	}
	if !strings.HasPrefix(lines[0], "> shell echo") {
		t.Fatalf("first line = %q", lines[0])
	}

	stdout := strings.Repeat("x", 80)
	body := renderFriendlyResult(toolCardView{ToolName: "shell"}, fmt.Sprintf(`{"stdout":%q,"stderr":"","exit_code":0}`, stdout), 40)
	assertFrameContainsLines(t, body)
	plain := stripANSI(body)
	if strings.Count(plain, "x") != len(stdout) {
		t.Fatalf("wrapped shell output dropped characters: %q", plain)
	}
}

func TestShellOutputIsFramed(t *testing.T) {
	body := renderFriendlyResult(toolCardView{ToolName: "shell"}, `{"stdout":"ok","stderr":"","exit_code":0}`, 60)
	plain := stripANSI(body)
	if !strings.Contains(plain, "ok") || !strings.Contains(plain, "╭") {
		t.Fatalf("shell frame = %q", plain)
	}
	long := strings.Repeat("line\n", 20)
	truncated := renderFriendlyResult(toolCardView{ToolName: "shell"}, fmt.Sprintf(`{"stdout":%q,"stderr":"","exit_code":0}`, long), 60)
	plain = stripANSI(truncated)
	if strings.Count(plain, "\n│ line") != shellPreviewLines || !strings.Contains(plain, "15 more lines") {
		t.Fatalf("shell preview = %q", plain)
	}
}

func TestDelegateOutputIsFramed(t *testing.T) {
	body := renderFriendlyResult(toolCardView{ToolName: "delegate"}, `{"answer":"the helper lives in app.go","tool_calls":2,"denied":[".env: approval is not available to a subagent"]}`, 60)
	plain := stripANSI(body)
	if !strings.Contains(plain, "╭") || !strings.Contains(plain, "the helper lives in app.go") || !strings.Contains(plain, ".env:") || !strings.Contains(plain, "2 tool calls") {
		t.Fatalf("delegate frame = %q", plain)
	}
	assertFrameContainsLines(t, body)
}

func TestSearchResultStaysHidden(t *testing.T) {
	card := toolCardView{
		ToolName: "web_search",
		Result:   `{"results":[{"title":"Gopi","url":"https://example.com","snippet":"a coding agent"}]}`,
	}
	if !hideToolResult(card) {
		t.Fatal("successful search should hide its results")
	}
	failed := card
	failed.Result = `{"error":"execution_failed","message":"` + gopisecrets.SearchAPIKey + ` is missing"}`
	if hideToolResult(failed) {
		t.Fatal("failed search should stay visible")
	}
}

func TestPlanResultIsReadable(t *testing.T) {
	card := toolCardView{
		ToolName: "write_plan",
		Args:     json.RawMessage(`{"plan_name":"ship","body":"# Ship it\n\nWrite the widget."}`),
	}
	got := renderFriendlyResult(card, `{"path":".gopi/plans/ship-abc.md","status":"created","gitignore_updated":true}`, 60)
	plain := stripANSI(got)
	if !strings.Contains(plain, "╭") || !strings.Contains(plain, "Created .gopi/plans/ship-abc.md") || !strings.Contains(plain, "Ship it") || !strings.Contains(plain, ".gitignore") || strings.Contains(plain, "gitignore_updated") {
		t.Fatalf("plan result = %q", plain)
	}
}

func TestApprovalPromptShowsReason(t *testing.T) {
	view := stripANSI(renderApprovalPrompt(gogent.PendingToolCall{
		ToolName: "read_file",
		Args:     json.RawMessage(`{"path":".env"}`),
		Reason:   "Protected path **/.env: scratch/demo/.env",
	}, 60))
	if !strings.Contains(view, "read .env") || !strings.Contains(view, "Protected path **/.env: scratch/demo/.env") || strings.Contains(view, `"path"`) {
		t.Fatalf("prompt = %q", view)
	}

	shell := stripANSI(renderApprovalPrompt(gogent.PendingToolCall{
		ToolName: "shell",
		Args:     json.RawMessage(`{"command":"cat .env","read_paths":[".env"]}`),
		Reason:   "Elevated file access: read /work/.env",
	}, 60))
	if !strings.Contains(shell, "shell cat .env") || !strings.Contains(shell, "read .env") || strings.Contains(shell, `"command"`) {
		t.Fatalf("shell prompt = %q", shell)
	}

	unknown := stripANSI(renderApprovalPrompt(gogent.PendingToolCall{
		ToolName: "custom",
		Args:     json.RawMessage(`{"query":"hi"}`),
		Reason:   "approval required",
	}, 60))
	if !strings.Contains(unknown, `"query"`) {
		t.Fatalf("unknown prompt = %q", unknown)
	}
}
