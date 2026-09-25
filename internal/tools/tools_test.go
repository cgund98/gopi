package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":`+mustJSON(t, path)+`}`))
		if err != nil {
			t.Fatalf("path %s: %v", path, err)
		}
		var payload map[string]string
		if err := json.Unmarshal(result, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["error"] != "access_denied" {
			t.Fatalf("path %s payload = %#v", path, payload)
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
