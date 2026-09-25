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
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "gpt-4o-mini" {
		t.Fatalf("model = %q", cfg.Model)
	}
	if cfg.MaxIterations != 10 {
		t.Fatalf("max iterations = %d", cfg.MaxIterations)
	}
	if cfg.UserPrompt != "" {
		t.Fatalf("user prompt = %q", cfg.UserPrompt)
	}
	if cfg.OpenAIAPIKey != "test-key" {
		t.Fatalf("api key = %q", cfg.OpenAIAPIKey)
	}
	if cfg.Network != "deny" {
		t.Fatalf("network = %q", cfg.Network)
	}
	if cfg.SearchEndpoint != "https://api.search.brave.com/res/v1/web/search" {
		t.Fatalf("search endpoint = %q", cfg.SearchEndpoint)
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
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserPrompt != "custom" {
		t.Fatalf("prompt = %q", cfg.UserPrompt)
	}
}

func TestLoadNetworkAllowlist(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`
[sandbox]
network = "allowlist"

[sandbox.network]
allow = ["github.com", "*.npmjs.org"]
deny = ["evil.example"]
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Network != "allowlist" || len(cfg.AllowHosts) != 2 || cfg.DenyHosts[0] != "evil.example" {
		t.Fatalf("cfg = %#v", cfg)
	}

	bad := []byte("[sandbox]\nnetwork = \"unrestricted\"\n")
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected unrestricted config to fail")
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
