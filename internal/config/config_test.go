package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gopisecrets "github.com/cgund98/gopi/internal/secrets"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

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
	if cfg.Model != "deepseek/deepseek-flash" {
		t.Fatalf("model = %q", cfg.Model)
	}
	if cfg.Effort != "none" {
		t.Fatalf("effort = %q", cfg.Effort)
	}
	if cfg.MaxIterations != 50 {
		t.Fatalf("max iterations = %d", cfg.MaxIterations)
	}
	if cfg.UserPrompt != "" {
		t.Fatalf("user prompt = %q", cfg.UserPrompt)
	}
	if cfg.DeepSeekAPIKey != "test-key" {
		t.Fatalf("deepseek api key = %q", cfg.DeepSeekAPIKey)
	}
	if cfg.Network != "deny" {
		t.Fatalf("network = %q", cfg.Network)
	}
	if cfg.RespectGitignore {
		t.Fatal("respect_gitignore should default to false")
	}
	if cfg.SearchEndpoint != "https://api.search.brave.com/res/v1/web/search" {
		t.Fatalf("search endpoint = %q", cfg.SearchEndpoint)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSystemPromptFile(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
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
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
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

func TestLoadRespectGitignore(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`
[sandbox]
network = "allowlist"
respect_gitignore = true

[sandbox.network]
allow = ["github.com"]
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RespectGitignore {
		t.Fatal("respect_gitignore = true was not read")
	}
	if cfg.Network != "allowlist" || len(cfg.AllowHosts) != 1 {
		t.Fatalf("cfg = %#v", cfg)
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

func TestLoadModeModels(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai")
	t.Setenv("KIMI_API_KEY", "")
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`
model = "gpt-5.6-luna"

[models]
agent = "gpt-4o"
build = "kimi/kimi-k2.6"
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.toml"), []byte(gopisecrets.KimiAPIKey+" = \"kimi\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModelFor("agent") != "gpt-4o" || cfg.ModelFor("ask") != "gpt-5.6-luna" || cfg.BuildModelName() != "kimi/kimi-k2.6" {
		t.Fatalf("models = %+v", cfg)
	}
	if cfg.KimiAPIKey != "kimi" {
		t.Fatalf("kimi key = %q", cfg.KimiAPIKey)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("model = \"gpt-9\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected unsupported model to fail")
	}
}

func TestLoadSearchEngineAndSubagents(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai")
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Defaults: auto search, no rg path, the documented explore caps.
	write("model = \"gpt-5.6-luna\"\n")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SearchEngine != "auto" || cfg.RipgrepPath != "" {
		t.Fatalf("search = %q %q", cfg.SearchEngine, cfg.RipgrepPath)
	}
	if cfg.ExploreMaxCalls != 6 || cfg.ExploreIterations != 40 || cfg.ExploreTimeout != 120*time.Second {
		t.Fatalf("explore caps = %d %d %s", cfg.ExploreMaxCalls, cfg.ExploreIterations, cfg.ExploreTimeout)
	}
	if cfg.ExploreModel != "" || cfg.ModelFor("explore") != "gpt-5.6-luna" {
		t.Fatalf("explore model = %q %q", cfg.ExploreModel, cfg.ModelFor("explore"))
	}

	// Every key parses, and models.explore overrides the explore model only.
	write(`
model = "gpt-5.6-luna"

[models]
explore = "gpt-4o"

[search]
engine = "builtin"
ripgrep_path = "/opt/homebrew/bin/rg"

[subagents]
explore_max_calls = 2
explore_iterations = 10
explore_timeout_seconds = 30
`)
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModelFor("explore") != "gpt-4o" || cfg.ModelFor("agent") != "gpt-5.6-luna" {
		t.Fatalf("models = %+v", cfg)
	}
	if cfg.SearchEngine != "builtin" || cfg.RipgrepPath != "/opt/homebrew/bin/rg" {
		t.Fatalf("search = %q %q", cfg.SearchEngine, cfg.RipgrepPath)
	}
	if cfg.ExploreMaxCalls != 2 || cfg.ExploreIterations != 10 || cfg.ExploreTimeout != 30*time.Second {
		t.Fatalf("explore caps = %d %d %s", cfg.ExploreMaxCalls, cfg.ExploreIterations, cfg.ExploreTimeout)
	}

	// An unknown engine is a load error, like an unknown model name.
	write("model = \"gpt-5.6-luna\"\n\n[search]\nengine = \"grep2\"\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an unknown search engine to fail")
	}

	// An invalid explore model is rejected too.
	write("model = \"gpt-5.6-luna\"\n\n[models]\nexplore = \"gpt-9\"\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("expected an unknown explore model to fail")
	}
}

func TestLoadKimiOnlySkipsOpenAIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte("model = \"kimi/kimi-k2.6\"\n")
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.toml"), []byte(gopisecrets.KimiAPIKey+" = \"kimi\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenAIAPIKey != "" || cfg.Model != "kimi/kimi-k2.6" {
		t.Fatalf("cfg model %q key %q", cfg.Model, cfg.OpenAIAPIKey)
	}
}

func TestLoadAnthropicModelRequiresKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("KIMI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte("model = \"claude-sonnet-5-5\"\n")
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected anthropic key required")
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.toml"), []byte(gopisecrets.AnthropicAPIKey+" = \"ant-key\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AnthropicAPIKey != "ant-key" || cfg.Model != "claude-sonnet-5-5" {
		t.Fatalf("cfg model %q key %q", cfg.Model, cfg.AnthropicAPIKey)
	}
}

func TestLoadEffortConfig(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`
model = "gpt-4o"
effort = "high"

[efforts]
agent = "none"
plan = "low"
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Effort != "high" {
		t.Fatalf("effort = %q", cfg.Effort)
	}
	if cfg.EffortFor("agent") != "none" {
		t.Fatalf("agent effort = %q", cfg.EffortFor("agent"))
	}
	if cfg.EffortFor("ask") != "high" {
		t.Fatalf("ask effort = %q", cfg.EffortFor("ask"))
	}
	if cfg.EffortFor("plan") != "low" {
		t.Fatalf("plan effort = %q", cfg.EffortFor("plan"))
	}
}

func TestLoadDeepSeekModelRequiresKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte("model = \"deepseek/deepseek-flash\"\n")
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected deepseek key required")
	}
	t.Setenv("DEEPSEEK_API_KEY", "ds-key")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeepSeekAPIKey != "ds-key" || cfg.Model != "deepseek/deepseek-flash" {
		t.Fatalf("cfg model %q key %q", cfg.Model, cfg.DeepSeekAPIKey)
	}
}

func TestLoadCustomSections(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`
model = "deepseek/deepseek-flash"

[calendar]
default_calendar = "primary"
lookahead_days = 7
`)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	var calendar struct {
		DefaultCalendar string `toml:"default_calendar"`
		LookaheadDays   int    `toml:"lookahead_days"`
	}
	found, err := cfg.Custom.Section("calendar", &calendar)
	if err != nil {
		t.Fatal(err)
	}
	if !found || calendar.DefaultCalendar != "primary" || calendar.LookaheadDays != 7 {
		t.Fatalf("calendar = %+v found %v", calendar, found)
	}
	if found, err := cfg.Custom.Section("models", &map[string]any{}); err != nil || found {
		t.Fatalf("models section found %v err %v", found, err)
	}
	if names := cfg.Custom.Names(); len(names) != 1 || names[0] != "calendar" {
		t.Fatalf("names = %v", names)
	}

	plain := []byte("model = \"deepseek/deepseek-flash\"\n")
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), plain, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if names := cfg.Custom.Names(); len(names) != 0 {
		t.Fatalf("names = %v", names)
	}
	if found, err := cfg.Custom.Section("calendar", &calendar); err != nil || found {
		t.Fatalf("found %v err %v", found, err)
	}
}
