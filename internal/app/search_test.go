package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgund98/gopi/internal/config"
	"github.com/cgund98/gopi/internal/trust"
	"github.com/cgund98/gopi/internal/workspace"
)

// newSearchSession builds a session over a workspace that has a gitignored file
// and a protected .env, which is the pair the search engine has to get right.
func newSearchSession(t *testing.T, cfg config.Config) *Session {
	t.Helper()
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n// resume here\n")
	write("notes.txt", "resume in a gitignored file\n")
	write(".gitignore", "notes.txt\n")
	write(".env", "TOKEN=super-secret\n")

	opened, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Model = "gpt-5.6-luna"
	cfg.MaxIterations = 2
	cfg.OpenAIAPIKey = "test-key"
	cfg.HomeDir = t.TempDir()
	cfg.Network = "deny"
	session, err := New(cfg, opened, trust.WorkspaceTrusted, nil)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestSearchEngineReachesTheGrepTool(t *testing.T) {
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("ripgrep is not installed")
	}
	session := newSearchSession(t, config.Config{SearchEngine: "auto", RipgrepPath: rgPath})
	raw, err := session.registries[ModeAgent].GetTool("grep").Execute(context.Background(), json.RawMessage(`{"pattern":"resume"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Backend string `json:"backend"`
		Matches []struct {
			Path string `json:"path"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Backend != "ripgrep" {
		t.Fatalf("backend = %q", payload.Backend)
	}
	paths := map[string]bool{}
	for _, match := range payload.Matches {
		paths[match.Path] = true
	}
	if !paths["main.go"] || !paths["notes.txt"] {
		t.Fatalf("matches = %+v", payload.Matches)
	}
}

func TestGrepNeverLeaksTheProtectedFile(t *testing.T) {
	for _, rgPath := range searchEnginePaths(t) {
		session := newSearchSession(t, config.Config{SearchEngine: "auto", RipgrepPath: rgPath})
		raw, err := session.registries[ModeAgent].GetTool("grep").Execute(context.Background(), json.RawMessage(`{"pattern":"TOKEN"}`))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "super-secret") {
			t.Fatalf("grep leaked the protected file: %s", raw)
		}
		if !strings.Contains(string(raw), "access_denied") {
			t.Fatalf("grep should report the protected file as denied: %s", raw)
		}
	}
}

func TestNamedRipgrepThatIsMissingFailsAtStartup(t *testing.T) {
	root, err := workspace.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Model:         "gpt-5.6-luna",
		MaxIterations: 2,
		OpenAIAPIKey:  "test-key",
		HomeDir:       t.TempDir(),
		Network:       "deny",
		SearchEngine:  "ripgrep",
		RipgrepPath:   "/nonexistent/rg",
	}
	_, err = New(cfg, root, trust.WorkspaceTrusted, nil)
	if err == nil {
		t.Fatal("engine = ripgrep with a missing binary should fail, not fall back")
	}
	if !strings.Contains(err.Error(), "ripgrep_path") {
		t.Fatalf("error should name the key: %v", err)
	}

	// Under auto the same path is not fatal: the walker takes over quietly.
	cfg.SearchEngine = "auto"
	if _, err := New(cfg, root, trust.WorkspaceTrusted, nil); err != nil {
		t.Fatalf("auto should fall back to the walker: %v", err)
	}
}

func searchEnginePaths(t *testing.T) []string {
	t.Helper()
	paths := []string{""}
	if rgPath, err := exec.LookPath("rg"); err == nil {
		paths = append(paths, rgPath)
	}
	return paths
}
