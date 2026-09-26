package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/trust"
	"github.com/cgund98/gopi/internal/workspace"
)

func TestEditRefusedWhenUntrusted(t *testing.T) {
	root := openTemp(t)
	tool := &EditFile{Root: root, Workspace: trust.WorkspaceUntrusted}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"note.txt","old":"","new":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] != "access_denied" {
		t.Fatalf("payload = %#v", payload)
	}
	if _, err := os.Stat(filepath.Join(root.Path, "note.txt")); !os.IsNotExist(err) {
		t.Fatal("untrusted edit created a file")
	}
}

func TestReadRejectsEscapePaths(t *testing.T) {
	root := openTemp(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root.Path, "escape")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFile{Root: root}
	for _, path := range []string{"../secret.txt", secret, "escape"} {
		decision, err := tool.RequiresApproval(context.Background(), json.RawMessage(`{"path":`+mustJSON(t, path)+`}`))
		if err != nil {
			t.Fatalf("path %s: %v", path, err)
		}
		if !decision.Required || !strings.Contains(decision.Reason, "outside the workspace") {
			t.Fatalf("path %s decision = %#v", path, decision)
		}
	}
}

func TestEditRefusesEmptyOldOnExistingFile(t *testing.T) {
	root := openTemp(t)
	path := filepath.Join(root.Path, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	edit := &EditFile{Root: root, Workspace: trust.WorkspaceTrusted}
	_, err := edit.Execute(context.Background(), json.RawMessage(`{"path":"main.go","old":"","new":"// fragment\n"}`))
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "package main\n" {
		t.Fatalf("file = %q, err = %v", body, err)
	}
}

func TestEditMissErrorsExplainHowToRecover(t *testing.T) {
	root := openTemp(t)
	if err := os.WriteFile(filepath.Join(root.Path, "main.go"), []byte("func main() {\n\tprintln(\"hi\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	edit := &EditFile{Root: root, Workspace: trust.WorkspaceTrusted}
	for _, tc := range []struct {
		old, want string
	}{
		{"func main() {\n    println(\"hi\")\n}", "whitespace is ignored"},
		{"println(\"bye\")", "file may have changed"},
		{"\"", "matched 2 times"},
	} {
		raw, _ := json.Marshal(map[string]string{"path": "main.go", "old": tc.old, "new": "x"})
		_, err := edit.Execute(context.Background(), raw)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("old %q: err = %v, want %q", tc.old, err, tc.want)
		}
	}
}

func TestReadAndEditInsideWorkspace(t *testing.T) {
	root := openTemp(t)
	edit := &EditFile{Root: root, Workspace: trust.WorkspaceTrusted}
	if _, err := edit.Execute(context.Background(), json.RawMessage(`{"path":"note.txt","old":"","new":"alpha"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := edit.Execute(context.Background(), json.RawMessage(`{"path":"note.txt","old":"alpha","new":"beta"}`)); err != nil {
		t.Fatal(err)
	}
	result, err := (&ReadFile{Root: root}).Execute(context.Background(), json.RawMessage(`{"path":"note.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Content != "beta" {
		t.Fatalf("content = %q", payload.Content)
	}
}

func TestEditProtectedPathRequiresApproval(t *testing.T) {
	root := openTemp(t)
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	tool := &EditFile{Root: root, Workspace: trust.WorkspaceTrusted, Rules: rules}
	raw := json.RawMessage(`{"path":".gopi/skills/lint/SKILL.md","old":"","new":"---\nname: lint\n---\n"}`)
	decision, err := tool.RequiresApproval(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Required || !strings.Contains(decision.Reason, ".gopi") {
		t.Fatalf("decision = %#v", decision)
	}
	plain, err := tool.RequiresApproval(context.Background(), json.RawMessage(`{"path":"note.txt","old":"","new":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Required {
		t.Fatalf("plain edit should not pause: %#v", plain)
	}
	if _, err := tool.Execute(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root.Path, ".gopi", "skills", "lint", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "name: lint") {
		t.Fatalf("body = %s", body)
	}
}

func TestFindListsAndFiltersFileNames(t *testing.T) {
	root := openTemp(t)
	for _, path := range []string{"note.txt", filepath.Join("pkg", "note.go")} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root.Path, path)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root.Path, path), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tool := &Find{Root: root}

	all, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(all, &listed); err != nil {
		t.Fatal(err)
	}
	if strings.Join(listed.Files, ",") != "note.txt,pkg/note.go" {
		t.Fatalf("files = %#v", listed.Files)
	}

	filtered, err := tool.Execute(context.Background(), json.RawMessage(`{"pattern":".go"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(filtered, &listed); err != nil {
		t.Fatal(err)
	}
	if strings.Join(listed.Files, ",") != "pkg/note.go" {
		t.Fatalf("filtered = %#v", listed.Files)
	}

	denied, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"../secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	if err := json.Unmarshal(denied, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] != "access_denied" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestReadReportsRangeAndContinuation(t *testing.T) {
	root := openTemp(t)
	if err := os.WriteFile(filepath.Join(root.Path, "small.txt"), []byte("a\nb\nc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 99) + "\n"
	if err := os.WriteFile(filepath.Join(root.Path, "big.txt"), []byte(strings.Repeat(line, 500)), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := &ReadFile{Root: root}
	type readPayload struct {
		Content    string `json:"content"`
		StartLine  int    `json:"start_line"`
		EndLine    int    `json:"end_line"`
		TotalLines int    `json:"total_lines"`
		Truncated  bool   `json:"truncated"`
		NextOffset int    `json:"next_offset"`
	}
	read := func(args string) readPayload {
		t.Helper()
		raw, err := tool.Execute(context.Background(), json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		var payload readPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	small := read(`{"path":"small.txt","offset":2,"limit":1}`)
	if small.Content != "b\n" || small.StartLine != 2 || small.EndLine != 2 || small.TotalLines != 3 || small.Truncated {
		t.Fatalf("small = %+v", small)
	}

	first := read(`{"path":"big.txt"}`)
	if !first.Truncated || first.TotalLines != 500 || first.StartLine != 1 || first.NextOffset != first.EndLine+1 {
		t.Fatalf("first = %+v", first)
	}
	if len(first.Content) > maxReadBytes || !strings.HasSuffix(first.Content, "\n") || strings.Count(first.Content, "\n") != first.EndLine {
		t.Fatalf("first window cut mid-line: %d bytes, end %d", len(first.Content), first.EndLine)
	}
	rest := read(fmt.Sprintf(`{"path":"big.txt","offset":%d}`, first.NextOffset))
	if rest.Truncated || rest.StartLine != first.NextOffset || rest.EndLine != 500 {
		t.Fatalf("rest = %+v", rest)
	}
	if first.Content+rest.Content != strings.Repeat(line, 500) {
		t.Fatal("windows do not join back into the file")
	}

	past := read(`{"path":"small.txt","offset":9}`)
	if past.Content != "" || past.TotalLines != 3 || past.Truncated {
		t.Fatalf("past = %+v", past)
	}
}

func TestSecretFileReadNeedsApprovalEvenWhenGranted(t *testing.T) {
	root := openTemp(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(dir, "gcal_token.json")
	if err := os.WriteFile(token, []byte(`{"refresh_token":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := policy.Build(root.Path, t.TempDir(), "", token)
	if err != nil {
		t.Fatal(err)
	}
	grants := &ReadGrants{}
	grants.Add(dir)
	tool := &ReadFile{Root: root, Rules: rules, Grants: grants}
	decision, err := tool.RequiresApproval(context.Background(), json.RawMessage(`{"path":`+mustJSON(t, token)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Required || !strings.Contains(decision.Reason, "secret "+token) {
		t.Fatalf("decision = %#v", decision)
	}
}

func openTemp(t *testing.T) workspace.Root {
	t.Helper()
	root, err := workspace.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestReadProtectedPathPausesAndRedacts(t *testing.T) {
	root := openTemp(t)
	if err := os.WriteFile(filepath.Join(root.Path, ".env"), []byte("visible=1\nother-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root.Path, ".gitignore"), []byte("!.env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	tool := &ReadFile{Root: root, Rules: rules}
	decision, err := tool.RequiresApproval(context.Background(), json.RawMessage(`{"path":".env"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Required || !strings.Contains(decision.Reason, ".env") {
		t.Fatalf("decision = %#v", decision)
	}
	body, err := WrapRedacting(tool, func(s string) string {
		return strings.ReplaceAll(s, "other-secret", "[redacted]")
	}).Execute(context.Background(), json.RawMessage(`{"path":".env"}`))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "visible=1") || strings.Contains(text, "other-secret") {
		t.Fatalf("body = %s", text)
	}
}

func TestResultsKeepHTMLCharactersLiteral(t *testing.T) {
	root := openTemp(t)
	source := "if a && b {\n\tx := <-ch\n\ts := \"\\u0026\" // APIs & Services -> x > y\n}\n"
	if err := os.WriteFile(filepath.Join(root.Path, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	body, err := WrapRedacting(&ReadFile{Root: root}, nil).Execute(context.Background(), json.RawMessage(`{"path":"main.go"}`))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, `\u0026&`) || !strings.Contains(text, "a && b") || !strings.Contains(text, "<-ch") || !strings.Contains(text, "APIs & Services -> x > y") {
		t.Fatalf("body = %s", text)
	}
	var result struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Content != source {
		t.Fatalf("content = %q, want %q", result.Content, source)
	}
}

func TestGrepOmitsProtectedFile(t *testing.T) {
	root := openTemp(t)
	if err := os.WriteFile(filepath.Join(root.Path, ".env"), []byte("super-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root.Path, "note.txt"), []byte("super-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := (&Grep{Root: root, Rules: rules}).Execute(context.Background(), json.RawMessage(`{"pattern":"super-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Matches []struct {
			Path string `json:"path"`
			Text string `json:"text"`
		} `json:"matches"`
		Denied []struct {
			Path string `json:"path"`
		} `json:"denied"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	for _, match := range payload.Matches {
		if match.Path == ".env" || strings.Contains(match.Text, "super-secret") && match.Path != "note.txt" {
			t.Fatalf("leaked match %#v", match)
		}
	}
	if len(payload.Matches) != 1 || payload.Matches[0].Path != "note.txt" {
		t.Fatalf("matches = %#v", payload.Matches)
	}
	if len(payload.Denied) != 1 || payload.Denied[0].Path != ".env" {
		t.Fatalf("denied = %#v", payload.Denied)
	}
	var hint struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(result, &hint); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hint.Message, "read_paths") {
		t.Fatalf("message = %q", hint.Message)
	}

	tool := &Grep{Root: root, Rules: rules}
	decision, err := tool.RequiresApproval(context.Background(), json.RawMessage(`{"pattern":"super-secret","read_paths":[".env"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Required || !strings.Contains(decision.Reason, ".env") {
		t.Fatalf("decision = %#v", decision)
	}
	elevated, err := tool.Execute(context.Background(), json.RawMessage(`{"pattern":"super-secret","read_paths":[".env"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(elevated, &payload); err != nil {
		t.Fatal(err)
	}
	foundEnv := false
	for _, match := range payload.Matches {
		if match.Path == ".env" {
			foundEnv = true
		}
	}
	if !foundEnv || len(payload.Denied) != 0 {
		t.Fatalf("elevated = %s", elevated)
	}
}

func TestGrepSkipsProtectedDirectory(t *testing.T) {
	root := openTemp(t)
	if err := os.WriteFile(filepath.Join(root.Path, ".gitignore"), []byte("scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(root.Path, "scratch", "demo", ".tmp", "cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(cache, name), []byte("needle\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := (&Grep{Root: root, Rules: rules}).Execute(context.Background(), json.RawMessage(`{"pattern":"needle"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Matches []struct {
			Path string `json:"path"`
		} `json:"matches"`
		Denied []struct {
			Path string `json:"path"`
		} `json:"denied"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Matches) != 0 || len(payload.Denied) != 1 || payload.Denied[0].Path != "scratch" {
		t.Fatalf("result = %s", result)
	}
}

func TestFindProtectedPathRequiresApproval(t *testing.T) {
	root := openTemp(t)
	if err := os.WriteFile(filepath.Join(root.Path, ".env"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root.Path, "note.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	tool := &Find{Root: root, Rules: rules}
	listed, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Files   []string `json:"files"`
		Message string   `json:"message"`
		Denied  []struct {
			Path string `json:"path"`
		} `json:"denied"`
	}
	if err := json.Unmarshal(listed, &payload); err != nil {
		t.Fatal(err)
	}
	if strings.Join(payload.Files, ",") != "note.txt" || len(payload.Denied) != 1 || payload.Denied[0].Path != ".env" {
		t.Fatalf("listed = %s", listed)
	}
	if !strings.Contains(payload.Message, "read_paths") {
		t.Fatalf("message = %q", payload.Message)
	}
	decision, err := tool.RequiresApproval(context.Background(), json.RawMessage(`{"read_paths":[".env"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Required || !strings.Contains(decision.Reason, ".env") {
		t.Fatalf("decision = %#v", decision)
	}
	elevated, err := tool.Execute(context.Background(), json.RawMessage(`{"read_paths":[".env"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(elevated, &payload); err != nil {
		t.Fatal(err)
	}
	if strings.Join(payload.Files, ",") != ".env,note.txt" || len(payload.Denied) != 0 {
		t.Fatalf("elevated = %s", elevated)
	}
}

func TestGrepAndFindHonorDirectoryElevation(t *testing.T) {
	root := openTemp(t)
	if err := os.WriteFile(filepath.Join(root.Path, ".gitignore"), []byte("scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(root.Path, "scratch", "demo")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "a.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "b.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	grepTool := &Grep{Root: root, Rules: rules}
	findTool := &Find{Root: root, Rules: rules}

	opened, err := grepTool.Execute(context.Background(), json.RawMessage(`{"pattern":"needle","read_paths":["scratch"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(opened), "scratch/demo/a.txt") || !strings.Contains(string(opened), "scratch/demo/b.txt") || strings.Contains(string(opened), `"denied":[{`) {
		t.Fatalf("directory grant = %s", opened)
	}
	listed, err := findTool.Execute(context.Background(), json.RawMessage(`{"read_paths":["scratch"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(listed), "scratch/demo/a.txt") || !strings.Contains(string(listed), "scratch/demo/b.txt") {
		t.Fatalf("find directory grant = %s", listed)
	}

	one, err := grepTool.Execute(context.Background(), json.RawMessage(`{"pattern":"needle","read_paths":["scratch/demo/a.txt"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var onePayload struct {
		Matches []struct {
			Path string `json:"path"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(one, &onePayload); err != nil {
		t.Fatal(err)
	}
	if len(onePayload.Matches) != 1 || onePayload.Matches[0].Path != "scratch/demo/a.txt" {
		t.Fatalf("file grant = %s", one)
	}
}

func TestGrepAndFindHonorOutsideReadPaths(t *testing.T) {
	root := openTemp(t)
	if err := os.WriteFile(filepath.Join(root.Path, ".env"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "note.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, ".env"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	grepTool := &Grep{Root: root, Rules: rules}
	blocked, err := grepTool.Execute(context.Background(), json.RawMessage(`{"pattern":"needle","path":`+mustJSON(t, outside)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blocked), "access_denied") || strings.Contains(string(blocked), "note.txt") {
		t.Fatalf("ungranted outside = %s", blocked)
	}
	decision, err := grepTool.RequiresApproval(context.Background(), json.RawMessage(`{"pattern":"needle","read_paths":[`+mustJSON(t, outside)+`]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Required || !strings.Contains(decision.Reason, outside) {
		t.Fatalf("decision = %#v", decision)
	}
	opened, err := grepTool.Execute(context.Background(), json.RawMessage(`{"pattern":"needle","read_paths":[`+mustJSON(t, outside)+`]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(opened), "note.txt") {
		t.Fatalf("outside grant = %s", opened)
	}
	if strings.Contains(string(opened), "visible-secret") {
		t.Fatalf("leaked workspace secret = %s", opened)
	}
	var payload struct {
		Matches []struct {
			Path string `json:"path"`
			Text string `json:"text"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(opened, &payload); err != nil {
		t.Fatal(err)
	}
	for _, match := range payload.Matches {
		if strings.HasSuffix(match.Path, ".env") {
			t.Fatalf("floor file opened by a parent grant: %s", opened)
		}
	}

	findTool := &Find{Root: root, Rules: rules}
	found, err := findTool.Execute(context.Background(), json.RawMessage(`{"pattern":"note.txt","read_paths":[`+mustJSON(t, outside)+`]}`))
	if err != nil {
		t.Fatal(err)
	}
	var foundPayload struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(found, &foundPayload); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(foundPayload.Files, ",")
	if !strings.Contains(joined, "note.txt") || strings.Contains(joined, ".env") {
		t.Fatalf("find outside grant = %s", found)
	}
}
