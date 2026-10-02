package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/sandbox"
	"github.com/cgund98/gopi/internal/workspace"
)

func rgEventLine(kind, path string, line int, text string) string {
	event := map[string]any{"type": kind}
	data := map[string]any{}
	if path != "" {
		data["path"] = map[string]string{"text": path}
	}
	if text != "" {
		data["lines"] = map[string]string{"text": text}
	}
	if line > 0 {
		data["line_number"] = line
	}
	if kind == "match" {
		data["submatches"] = []map[string]any{{"match": map[string]string{"text": "resume"}, "start": 0, "end": 6}}
	}
	event["data"] = data
	encoded, _ := json.Marshal(event)
	return string(encoded)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestRipgrepArgvIsLiteralUnlessRegex(t *testing.T) {
	engine := &SearchEngine{RGPath: "/usr/bin/rg"}
	req := searchRequest{Pattern: "func main", Starts: []string{"/tmp"}}

	literal := engine.argv(req)
	if !contains(literal, "-F") {
		t.Fatalf("literal argv = %v", literal)
	}
	if literal[len(literal)-1] != "/tmp" {
		t.Fatalf("search root should be last: %v", literal)
	}
	for _, want := range []string{"--json", "--no-ignore", "--hidden", "--color=never"} {
		if !contains(literal, want) {
			t.Fatalf("argv missing %s: %v", want, literal)
		}
	}

	regex := engine.argv(searchRequest{Pattern: `func \w+`, Regex: true, Starts: []string{"/tmp"}})
	if contains(regex, "-F") {
		t.Fatalf("regex argv should not force a literal: %v", regex)
	}
}

func TestRipgrepParsesJsonStream(t *testing.T) {
	root := workspace.Root{Path: t.TempDir()}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	engine := &SearchEngine{RGPath: "/usr/bin/rg"}
	stdout := strings.Join([]string{
		rgEventLine("begin", "main.go", 0, ""),
		rgEventLine("match", "main.go", 1, "package main\n"),
		rgEventLine("match", "internal/tools/grep.go", 42, "// resume here\n"),
		`{"type":"summary","data":{"stats":{"binaryFilesSearchedAndIgnored":2}}}`,
	}, "\n")

	req := searchRequest{Root: root, Rules: rules, Pattern: "resume"}
	result, err := engine.parse(req, rules, sandbox.Result{Stdout: stdout, ExitCode: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 2 {
		t.Fatalf("matches = %+v", result.Matches)
	}
	if result.Matches[1].Path != "internal/tools/grep.go" || result.Matches[1].Line != 42 {
		t.Fatalf("second match = %+v", result.Matches[1])
	}
	if result.Binary != 2 {
		t.Fatalf("binary = %d", result.Binary)
	}
}

func TestRipgrepToleratesATrailingPartialLine(t *testing.T) {
	root := workspace.Root{Path: t.TempDir()}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	engine := &SearchEngine{RGPath: "/usr/bin/rg"}
	// The last record is cut in half, which is what a cap on output looks like.
	stdout := rgEventLine("match", "main.go", 3, "resume\n") + "\n" + `{"type":"match","data":{"path":{"tex`

	result, err := engine.parse(searchRequest{Root: root, Rules: rules, Pattern: "resume"}, rules, sandbox.Result{Stdout: stdout})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 || result.Matches[0].Line != 3 {
		t.Fatalf("matches = %+v", result.Matches)
	}
}

func TestRipgrepExitCodes(t *testing.T) {
	root := workspace.Root{Path: t.TempDir()}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	engine := &SearchEngine{RGPath: "/usr/bin/rg"}

	// Exit 1 means no matches, which is a result and not an error.
	empty, err := engine.parse(searchRequest{Root: root, Rules: rules, Pattern: "nope"}, rules, sandbox.Result{ExitCode: 1})
	if err != nil {
		t.Fatalf("exit 1 should not be an error: %v", err)
	}
	if len(empty.Matches) != 0 {
		t.Fatalf("matches = %+v", empty.Matches)
	}

	// Exit 2 is a real failure and its stderr has to survive.
	if _, err := engine.parse(searchRequest{Root: root, Rules: rules, Pattern: "["}, rules, sandbox.Result{ExitCode: 2, Stderr: "regex parse error"}); err == nil {
		t.Fatal("exit 2 should be an error")
	} else if !strings.Contains(err.Error(), "regex parse error") {
		t.Fatalf("error = %v", err)
	}
}

func TestRipgrepReportsTruncationAndDenial(t *testing.T) {
	root := workspace.Root{Path: t.TempDir()}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	engine := &SearchEngine{RGPath: "/usr/bin/rg"}

	var lines []string
	for i := range maxGrepMatches + 5 {
		lines = append(lines, rgEventLine("match", "main.go", i+1, "resume\n"))
	}
	result, err := engine.parse(searchRequest{Root: root, Rules: rules, Pattern: "resume"}, rules, sandbox.Result{Stdout: strings.Join(lines, "\n")})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || len(result.Matches) != maxGrepMatches {
		t.Fatalf("matches = %d truncated = %v", len(result.Matches), result.Truncated)
	}

	// A protected file that ripgrep did report is dropped and becomes a denial.
	if err := os.WriteFile(filepath.Join(root.Path, ".env"), []byte("TOKEN=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err = policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	result, err = engine.parse(searchRequest{Root: root, Rules: rules, Pattern: "TOKEN"}, rules, sandbox.Result{
		Stdout: rgEventLine("match", ".env", 1, "TOKEN=1\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 0 {
		t.Fatalf("a protected file leaked: %+v", result.Matches)
	}
	if len(result.Denied) != 1 || result.Denied[0]["path"] != ".env" {
		t.Fatalf("denied = %+v", result.Denied)
	}
}

func TestRipgrepDenialFromStderr(t *testing.T) {
	root := workspace.Root{Path: t.TempDir()}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	engine := &SearchEngine{RGPath: "/usr/bin/rg"}
	result, err := engine.parse(searchRequest{Root: root, Rules: rules, Pattern: "x"}, rules, sandbox.Result{
		Stderr: "rg: .env: Permission denied (os error 13)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Denied) != 1 || result.Denied[0]["path"] != ".env" {
		t.Fatalf("denied = %+v", result.Denied)
	}
}

func TestRipgrepExitTwoWithADenialIsNotAFailure(t *testing.T) {
	root := workspace.Root{Path: t.TempDir()}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	engine := &SearchEngine{RGPath: "/usr/bin/rg"}
	// The sandbox denying a protected file makes ripgrep exit 2. That is an
	// expected outcome and has to come back as a denial, not as an error, or a
	// search that touched one .env would report nothing at all.
	result, err := engine.parse(searchRequest{Root: root, Rules: rules, Pattern: "resume"}, rules, sandbox.Result{
		ExitCode: 2,
		Stderr:   "rg: " + filepath.Join(root.Path, ".env") + ": Operation not permitted (os error 1)",
		Stdout:   rgEventLine("match", "main.go", 2, "// resume here\n"),
	})
	if err != nil {
		t.Fatalf("a denial should not be an error: %v", err)
	}
	if len(result.Matches) != 1 || result.Matches[0].Path != "main.go" {
		t.Fatalf("matches = %+v", result.Matches)
	}
	if len(result.Denied) != 1 || result.Denied[0]["path"] != ".env" {
		t.Fatalf("denied = %+v", result.Denied)
	}
}

func TestRipgrepProfileOpensTheBinaryAndTheRoots(t *testing.T) {
	root := workspace.Root{Path: t.TempDir()}
	outside := t.TempDir()
	engine := &SearchEngine{RGPath: "/opt/homebrew/bin/rg"}
	reads := engine.extraReads(searchRequest{Root: root, Starts: []string{root.Path, outside}})
	if !contains(reads, "/opt/homebrew/bin") {
		t.Fatalf("reads = %v", reads)
	}
	if !contains(reads, outside) {
		t.Fatalf("an outside search root should be readable: %v", reads)
	}
	if contains(reads, root.Path) {
		t.Fatalf("the workspace root is already a read root: %v", reads)
	}
}

func TestResolveEngineRejectsAnUnknownName(t *testing.T) {
	if _, err := ResolveEngine("grep2", ""); err == nil {
		t.Fatal("expected an unknown engine to fail")
	}
	if _, err := ResolveEngine(EngineRipgrep, ""); err == nil {
		t.Fatal("naming ripgrep without a binary should fail")
	}
	backend, err := ResolveEngine(EngineAuto, "")
	if err != nil || backend != BackendBuiltin {
		t.Fatalf("auto without ripgrep = %q, %v", backend, err)
	}
	backend, err = ResolveEngine(EngineAuto, "/usr/bin/rg")
	if err != nil || backend != BackendRipgrep {
		t.Fatalf("auto with ripgrep = %q, %v", backend, err)
	}
}

// TestRipgrepEndToEnd runs the real binary so the flags and the JSON contract are
// checked against ripgrep itself. It skips when rg is not installed.
func TestRipgrepEndToEnd(t *testing.T) {
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("ripgrep is not installed")
	}
	root := openTemp(t)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(root.Path, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n// resume here\n")
	write("notes.txt", "resume in a text file\n")
	// A .gitignore must not hide a file: the walker skips only .git.
	write(".gitignore", "notes.txt\n")
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	engine := &SearchEngine{RGPath: rgPath, HomeDir: t.TempDir()}
	result, err := engine.search(context.Background(), searchRequest{
		Root:    root,
		Starts:  []string{root.Path},
		Pattern: "resume",
		Rules:   rules,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	paths := map[string]bool{}
	for _, match := range result.Matches {
		paths[match.Path] = true
	}
	if !paths["main.go"] || !paths["notes.txt"] {
		t.Fatalf("matches = %+v", result.Matches)
	}
}
