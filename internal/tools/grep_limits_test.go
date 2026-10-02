package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

// runGrepWithBackend runs one grep with an explicit engine and reports the
// backend the result names.
func runGrepWithBackend(t *testing.T, rgPath string, args string) (map[string]any, grepResult) {
	t.Helper()
	root := openTemp(t)
	files := map[string]string{
		"main.go":     "package main\n// resume here\n",
		"notes.txt":   "resume in a text file\n",
		".gitignore":  "notes.txt\n",
		"ignored.log": "debug log line\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root.Path, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	tool := &Grep{Root: root, Rules: rules, Engine: EngineAuto, RGPath: rgPath, HomeDir: t.TempDir()}
	raw, err := tool.Execute(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	var result grepResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return payload, result
}

func TestGrepBackendIsReportedAndFindsGitignoredFiles(t *testing.T) {
	rgPath, _ := exec.LookPath("rg")
	cases := []struct {
		name    string
		rgPath  string
		backend string
	}{
		{name: "builtin", rgPath: "", backend: BackendBuiltin},
		{name: "ripgrep", rgPath: rgPath, backend: BackendRipgrep},
	}
	for _, tc := range cases {
		if tc.backend == BackendRipgrep && tc.rgPath == "" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			payload, result := runGrepWithBackend(t, tc.rgPath, `{"pattern":"resume"}`)
			if payload["backend"] != tc.backend {
				t.Fatalf("backend = %v", payload["backend"])
			}
			// A .gitignore must not hide a file: the walker skips only .git, and
			// ripgrep is told --no-ignore to match it.
			paths := map[string]bool{}
			for _, match := range result.Matches {
				paths[match.Path] = true
			}
			if !paths["main.go"] || !paths["notes.txt"] {
				t.Fatalf("matches = %+v", result.Matches)
			}
		})
	}
}

func TestGrepRegexWorksOnBothEngines(t *testing.T) {
	rgPath, _ := exec.LookPath("rg")
	paths := []string{""}
	names := []string{"builtin"}
	if rgPath != "" {
		paths = append(paths, rgPath)
		names = append(names, "ripgrep")
	}
	for i, path := range paths {
		t.Run(names[i], func(t *testing.T) {
			_, result := runGrepWithBackend(t, path, `{"pattern":"resum\\w","regex":true}`)
			if len(result.Matches) == 0 {
				t.Fatalf("regex found nothing: %+v", result)
			}
		})
	}
}

func TestGrepRejectsAnInvalidRegex(t *testing.T) {
	root := openTemp(t)
	if _, err := (&Grep{Root: root}).Execute(context.Background(), json.RawMessage(`{"pattern":"[","regex":true}`)); err == nil {
		t.Fatal("an invalid regex should fail")
	}
}
