package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "test-key")

	dir, err := HomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir, "builtin")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "gpt-6-sol" {
		t.Fatalf("model = %q", cfg.Model)
	}
	if cfg.MaxIterations != 10 {
		t.Fatalf("max iterations = %d", cfg.MaxIterations)
	}
	if cfg.SystemPrompt != "builtin" {
		t.Fatalf("prompt = %q", cfg.SystemPrompt)
	}
	if cfg.OpenAIAPIKey != "test-key" {
		t.Fatalf("api key = %q", cfg.OpenAIAPIKey)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSystemPromptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte("custom"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir, "builtin")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SystemPrompt != "custom" {
		t.Fatalf("prompt = %q", cfg.SystemPrompt)
	}
}

func TestEnsureHomeRefusesLoosePermissions(t *testing.T) {
	dir := t.TempDir()
	loose := filepath.Join(dir, "loose")
	if err := os.Mkdir(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureHome(loose); err == nil {
		t.Fatal("expected permission error")
	}
}
