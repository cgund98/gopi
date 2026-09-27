package gopi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/config"
)

type stubTool struct{ name, token string }

func (t stubTool) Name() string                { return t.name }
func (t stubTool) Description() string         { return "stub" }
func (t stubTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t stubTool) RequiresApproval(context.Context, json.RawMessage) (gogent.ApprovalDecision, error) {
	return gogent.ApprovalDecision{}, nil
}
func (t stubTool) Execute(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func collect(opts ...Option) options {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

func TestBuildToolsReadsSecrets(t *testing.T) {
	cfg := config.Config{
		Secrets:     map[string]string{"gcal_token": `{"refresh_token":"r"}`, "jira": "j", "unused": "u"},
		SecretFiles: map[string]string{"gcal_token": "/secrets/gcal_token.json"},
	}
	var path string
	o := collect(
		WithTool(ModeAsk, stubTool{name: "plain"}),
		WithToolFactory(ModeAgent, func(env ToolEnv) (gogent.Tool, error) {
			token, err := env.Secret("gcal_token")
			if err != nil {
				return nil, err
			}
			if path, err = env.SecretPath("gcal_token"); err != nil {
				return nil, err
			}
			if _, err := env.SecretPath("jira"); err == nil || !strings.Contains(err.Error(), "is a string") {
				t.Errorf("SecretPath on a string secret = %v", err)
			}
			if env.Workspace != "/work" {
				t.Errorf("workspace = %q", env.Workspace)
			}
			return stubTool{name: "calendar", token: token}, nil
		}),
		WithToolFactory(ModeAgent, func(env ToolEnv) (gogent.Tool, error) {
			jira, err := env.Secret("jira")
			return stubTool{name: "jira", token: jira}, err
		}),
	)
	tools, err := buildTools(cfg, "/work", o)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools[ModeAgent]) != 2 || len(tools[ModeAsk]) != 1 {
		t.Fatalf("tools = %#v", tools)
	}
	if calendar := tools[ModeAgent][0].(stubTool); calendar.token != `{"refresh_token":"r"}` {
		t.Fatalf("calendar token = %q", calendar.token)
	}
	if path != "/secrets/gcal_token.json" {
		t.Fatalf("path = %q", path)
	}
}

func TestBuildToolsFailsOnMissingSecret(t *testing.T) {
	o := collect(WithToolFactory(ModeAgent, func(env ToolEnv) (gogent.Tool, error) {
		if _, err := env.Secret("gcal_token"); err != nil {
			return nil, err
		}
		return stubTool{name: "calendar"}, nil
	}))
	_, err := buildTools(config.Config{Secrets: map[string]string{}}, "/work", o)
	if err == nil || !strings.Contains(err.Error(), "secret gcal_token is missing from ~/.gopi/secrets.toml") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildToolsReadsConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_HOME", dir)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	body := []byte("model = \"deepseek/deepseek-flash\"\n\n[calendar]\ndefault_calendar = \"primary\"\n")
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		DefaultCalendar string `toml:"default_calendar"`
	}
	var missingErr error
	o := collect(WithToolFactory(ModeAgent, func(env ToolEnv) (gogent.Tool, error) {
		if err := env.Config("calendar", &got); err != nil {
			return nil, err
		}
		var out struct {
			DefaultCalendar string `toml:"default_calendar"`
		}
		missingErr = env.Config("nope", &out)
		return stubTool{name: "calendar"}, nil
	}))
	if _, err := buildTools(cfg, "/work", o); err != nil {
		t.Fatal(err)
	}
	if got.DefaultCalendar != "primary" {
		t.Fatalf("default_calendar = %q", got.DefaultCalendar)
	}
	if missingErr == nil || !strings.Contains(missingErr.Error(), "~/.gopi/config.toml") {
		t.Fatalf("missing section err = %v", missingErr)
	}
}
