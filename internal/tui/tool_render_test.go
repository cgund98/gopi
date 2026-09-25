package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
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
	if !strings.Contains(body, "demo.txt") || !strings.Contains(body, "alpha") || !strings.Contains(body, "beta") {
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

func TestReadResultIsPlainText(t *testing.T) {
	card := toolCardView{ToolName: "read_file"}
	got := renderFriendlyResult(card, `{"path":"demo.txt","content":"hello"}`, 80)
	if strings.Contains(got, `"content"`) || !strings.Contains(got, "hello") {
		t.Fatalf("result = %q", got)
	}
}

func TestReadPreviewTruncates(t *testing.T) {
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	body, err := json.Marshal(map[string]string{"path": "main.go", "content": strings.Join(lines, "\n")})
	if err != nil {
		t.Fatal(err)
	}
	got := renderFriendlyResult(toolCardView{ToolName: "read_file"}, string(body), 80)
	plain := stripANSI(got)
	if strings.Contains(plain, "line 9") {
		t.Fatalf("preview included line 9: %q", plain)
	}
	if !strings.Contains(plain, "line 8") || !strings.Contains(plain, "12 more lines") {
		t.Fatalf("preview = %q", plain)
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
