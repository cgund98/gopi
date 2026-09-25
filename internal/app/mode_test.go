package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/config"
	"github.com/cgund98/gopi/internal/prompt"
	"github.com/cgund98/gopi/internal/trust"
	"github.com/cgund98/gopi/internal/workspace"
)

func TestModesSwitchRegistryAndKeepTranscript(t *testing.T) {
	root, err := workspace.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := New(config.Config{
		Model:         "gpt-6-sol",
		MaxIterations: 2,
		OpenAIAPIKey:  "test-key",
		HomeDir:       t.TempDir(),
		Network:       "deny",
	}, root, trust.WorkspaceTrusted, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Store.AddMessages(t.Context(), "chat", gogent.NewUserMessage("keep me")); err != nil {
		t.Fatal(err)
	}
	askNames := toolNames(t, session.registries[ModeAsk])
	planNames := toolNames(t, session.registries[ModePlan])
	agentNames := toolNames(t, session.registries[ModeAgent])
	for _, name := range []string{"read_file", "grep", "find", "web_search"} {
		if !askNames[name] || !planNames[name] || !agentNames[name] {
			t.Fatalf("missing %s ask=%v plan=%v agent=%v", name, askNames, planNames, agentNames)
		}
	}
	for _, name := range []string{"edit_file", "shell", "delegate", "write_plan"} {
		if askNames[name] {
			t.Fatalf("ask has %s", name)
		}
	}
	if !planNames["write_plan"] || planNames["edit_file"] || planNames["shell"] || planNames["delegate"] {
		t.Fatalf("plan tools = %v", planNames)
	}
	if !agentNames["edit_file"] || !agentNames["shell"] || !agentNames["delegate"] || agentNames["write_plan"] {
		t.Fatalf("agent tools = %v", agentNames)
	}

	if err := session.SetMode(ModePlan); err != nil {
		t.Fatal(err)
	}
	if session.Mode != ModePlan || session.Registry != session.registries[ModePlan] {
		t.Fatal("plan mode did not switch registry")
	}
	if !strings.Contains(session.Model.SystemPrompt(), prompt.ModePrefix("plan")) {
		t.Fatal("plan prefix missing")
	}
	if !strings.Contains(session.Model.SystemPrompt(), ".gitignore") {
		t.Fatal("plan prompt missing gitignore note")
	}
	messages, err := session.Store.Load(t.Context(), "chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Content != "keep me" {
		t.Fatalf("transcript = %+v", messages)
	}
}

func TestParseModeCommand(t *testing.T) {
	mode, command, ok := ParseModeCommand("/mode plan")
	if mode != ModePlan || !command || !ok {
		t.Fatalf("parse = %s %v %v", mode, command, ok)
	}
	if _, command, ok = ParseModeCommand("/mode sideways"); !command || ok {
		t.Fatal("unknown /mode should be a failed command")
	}
	if _, command, _ = ParseModeCommand("hello"); command {
		t.Fatal("plain text is not a command")
	}
}

func toolNames(t *testing.T, registry *gogent.ToolRegistry) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, tool := range registry.Tools() {
		names[tool.Name()] = true
	}
	return names
}

type namedTool struct {
	name string
}

func (t namedTool) Name() string                { return t.name }
func (t namedTool) Description() string         { return "custom" }
func (t namedTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t namedTool) RequiresApproval(context.Context, json.RawMessage) (gogent.ApprovalDecision, error) {
	return gogent.ApprovalDecision{}, nil
}
func (t namedTool) Execute(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func TestExtraToolStaysOnItsMode(t *testing.T) {
	root, err := workspace.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Model:         "gpt-4o-mini",
		MaxIterations: 2,
		OpenAIAPIKey:  "test-key",
		HomeDir:       t.TempDir(),
		Network:       "deny",
	}
	session, err := New(cfg, root, trust.WorkspaceTrusted, map[Mode][]gogent.Tool{
		ModeAsk: {namedTool{name: "forecast"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !toolNames(t, session.registries[ModeAsk])["forecast"] {
		t.Fatal("ask registry missing forecast")
	}
	if toolNames(t, session.registries[ModeAgent])["forecast"] || toolNames(t, session.registries[ModePlan])["forecast"] {
		t.Fatal("forecast leaked onto another mode")
	}
	if _, err := New(cfg, root, trust.WorkspaceTrusted, map[Mode][]gogent.Tool{
		ModeAgent: {namedTool{name: "read_file"}},
	}); err == nil {
		t.Fatal("duplicate built-in name was accepted")
	}
}
