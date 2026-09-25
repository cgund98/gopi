package tools

import (
	"context"
	"encoding/json"
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
	var payload map[string]string
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["content"] != "beta" {
		t.Fatalf("content = %q", payload["content"])
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
