package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gopi/internal/policy"
)

func TestDelegateDescriptionCoversUseAndElevation(t *testing.T) {
	text := (&Delegate{}).Description()
	for _, want := range []string{
		"locate an implementation",
		"access_denied",
		"read_paths",
		"write_paths",
		"network_hosts",
		"unrestricted",
		"untrusted observation",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("description missing %q", want)
		}
	}
}

func TestChildRegistryOmitsEditAndDelegate(t *testing.T) {
	tool := &Delegate{Root: openTemp(t)}
	registry, err := tool.childRegistry()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, registered := range registry.Tools() {
		names[registered.Name()] = true
	}
	for _, name := range []string{"read_file", "grep", "find", "shell"} {
		if !names[name] {
			t.Fatalf("missing %s in %#v", name, names)
		}
	}
	if names["edit_file"] || names["delegate"] {
		t.Fatalf("child tools = %#v", names)
	}
}

func TestChildProtectedReadAndUnrestrictedShellFailClosed(t *testing.T) {
	root := openTemp(t)
	if err := os.WriteFile(filepath.Join(root.Path, ".env"), []byte("super-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	readTool := failClosed{inner: &ReadFile{Root: root, Rules: rules}}
	decision, err := readTool.RequiresApproval(context.Background(), json.RawMessage(`{"path":".env"}`))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Required {
		t.Fatal("child read should not pause")
	}
	denied, err := readTool.Execute(context.Background(), json.RawMessage(`{"path":".env"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(denied), "access_denied") || strings.Contains(string(denied), "super-secret") {
		t.Fatalf("read = %s", denied)
	}

	shellTool := failClosed{inner: &Shell{Root: root, HomeDir: t.TempDir()}}
	decision, err = shellTool.RequiresApproval(context.Background(), json.RawMessage(`{"command":"curl https://example.com","network":"unrestricted"}`))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Required {
		t.Fatal("child shell should not pause")
	}
	denied, err = shellTool.Execute(context.Background(), json.RawMessage(`{"command":"curl https://example.com","network":"unrestricted"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(denied), "access_denied") {
		t.Fatalf("shell = %s", denied)
	}
}

func TestDelegateReturnsAnswerWithoutProtectedBody(t *testing.T) {
	root := openTemp(t)
	if err := os.WriteFile(filepath.Join(root.Path, ".env"), []byte("super-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptModel{steps: []gogent.Message{
		gogent.NewAssistantMessageWithToolCalls("", []gogent.ToolCall{{
			ID:       "call_read",
			ToolName: "read_file",
			Args:     json.RawMessage(`{"path":".env"}`),
		}}),
		gogent.NewAssistantMessage("finished"),
	}}
	tool := &Delegate{
		Root:  root,
		Rules: rules,
		NewModel: func(*gogent.ToolRegistry) (gogent.Model, error) {
			return model, nil
		},
	}
	raw, err := tool.Execute(context.Background(), json.RawMessage(`{"task":"read the env"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Answer    string   `json:"answer"`
		ToolCalls int      `json:"tool_calls"`
		Denied    []string `json:"denied"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Answer != "finished" || payload.ToolCalls != 1 || len(payload.Denied) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
	if strings.Contains(string(raw), "super-secret") {
		t.Fatalf("result leaked the file: %s", raw)
	}
}

func TestDelegateFanoutStops(t *testing.T) {
	started := 0
	tool := &Delegate{
		Root:     openTemp(t),
		MaxCalls: 4,
		NewModel: func(*gogent.ToolRegistry) (gogent.Model, error) {
			started++
			return &scriptModel{steps: []gogent.Message{gogent.NewAssistantMessage("ok")}}, nil
		},
	}
	for i := 0; i < 4; i++ {
		if _, err := tool.Execute(context.Background(), json.RawMessage(`{"task":"look"}`)); err != nil {
			t.Fatal(err)
		}
	}
	fifth, err := tool.Execute(context.Background(), json.RawMessage(`{"task":"again"}`))
	if err != nil {
		t.Fatal(err)
	}
	if started != 4 || !strings.Contains(string(fifth), "delegate_limit") {
		t.Fatalf("started = %d fifth = %s", started, fifth)
	}
}

type scriptModel struct {
	steps []gogent.Message
	i     int
}

func (m *scriptModel) GenerateResponse(context.Context, []gogent.Message) (gogent.Message, error) {
	if m.i >= len(m.steps) {
		return gogent.NewAssistantMessage("done"), nil
	}
	message := m.steps[m.i]
	m.i++
	return message, nil
}
