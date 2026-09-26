package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cgund98/gopi/internal/policy"
)

type grepResult struct {
	Matches   []grepMatch `json:"matches"`
	Truncated bool        `json:"truncated"`
	Binary    int         `json:"binary_files_skipped"`
}

func runGrep(t *testing.T, files map[string]string, pattern string) (grepResult, int) {
	t.Helper()
	root := openTemp(t)
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root.Path, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := (&Grep{Root: root, Rules: rules}).Execute(context.Background(), json.RawMessage(fmt.Sprintf(`{"pattern":%q}`, pattern)))
	if err != nil {
		t.Fatal(err)
	}
	var result grepResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result, len(raw)
}

func TestGrepSkipsBinaryFiles(t *testing.T) {
	result, _ := runGrep(t, map[string]string{
		"demo":    "\x7fELF\x00\x00resume" + strings.Repeat("x", 90000),
		"main.go": "// resume here\n",
	}, "resume")
	if len(result.Matches) != 1 || result.Matches[0].Path != "main.go" || result.Binary != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestGrepClipsLongLinesAroundTheMatch(t *testing.T) {
	line := strings.Repeat("a", 500_000) + "resume" + strings.Repeat("b", 500_000)
	result, size := runGrep(t, map[string]string{"bundle.js": line + "\nresume again\n"}, "resume")
	if len(result.Matches) != 2 {
		t.Fatalf("matches = %d", len(result.Matches))
	}
	text := result.Matches[0].Text
	if !strings.Contains(text, "resume") || !strings.HasPrefix(text, "…") || !strings.HasSuffix(text, "…") {
		t.Fatalf("clipped text = %q", text)
	}
	if n := utf8.RuneCountInString(text); n > maxGrepLineRunes+2 {
		t.Fatalf("clipped text has %d runes", n)
	}
	if size > 4<<10 {
		t.Fatalf("result is %d bytes", size)
	}
}

func TestGrepKeepsMatchesBeforeAnOverlongLine(t *testing.T) {
	result, _ := runGrep(t, map[string]string{"huge.txt": "resume first\n" + strings.Repeat("z", 5<<20) + "\n"}, "resume")
	if len(result.Matches) != 1 || result.Matches[0].Text != "resume first" {
		t.Fatalf("result = %+v", result)
	}
}

func TestGrepStopsAtTheByteBudget(t *testing.T) {
	files := map[string]string{}
	for i := range 40 {
		files[fmt.Sprintf("f%02d.txt", i)] = "resume " + strings.Repeat("𝄞", 280) + "\n"
		files[fmt.Sprintf("g%02d.txt", i)] = strings.Repeat("𝄞", 280) + " resume\n"
	}
	result, size := runGrep(t, files, "resume")
	if !result.Truncated || len(result.Matches) >= maxGrepMatches {
		t.Fatalf("matches = %d truncated = %v", len(result.Matches), result.Truncated)
	}
	if size > maxGrepBytes+8<<10 {
		t.Fatalf("result is %d bytes", size)
	}
}

func TestClipLineKeepsShortLines(t *testing.T) {
	if got := clipLine("func resume() {}", "resume", 300); got != "func resume() {}" {
		t.Fatalf("clip = %q", got)
	}
	if got := clipLine(strings.Repeat("é", 400), "missing", 10); got != strings.Repeat("é", 10)+"…" {
		t.Fatalf("clip without match = %q", got)
	}
}
