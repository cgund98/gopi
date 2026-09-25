package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgund98/gopi/internal/trust"
)

func TestWritePlanCreatesAndUpdates(t *testing.T) {
	root := openTemp(t)
	tool := &WritePlan{Root: root, Workspace: trust.WorkspaceTrusted}
	decision, err := tool.RequiresApproval(context.Background(), json.RawMessage(`{"plan_name":"Ship It","body":"# plan"}`))
	if err != nil || decision.Required {
		t.Fatalf("decision = %+v err = %v", decision, err)
	}
	created, err := tool.Execute(context.Background(), json.RawMessage(`{"plan_name":"Ship It","body":"# plan\n"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Path   string `json:"path"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(created, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "created" || !strings.HasPrefix(payload.Path, ".gopi/plans/ship-it-") || !strings.HasSuffix(payload.Path, ".md") {
		t.Fatalf("created = %+v", payload)
	}
	if _, err := os.Stat(filepath.Join(root.Path, ".gitignore")); !os.IsNotExist(err) {
		t.Fatal("gitignore should not be created")
	}

	updated, err := tool.Execute(context.Background(), json.RawMessage(`{"plan_name":"ignored","path":"`+payload.Path+`","body":"# revised\n"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "updated" {
		t.Fatalf("updated = %+v", payload)
	}
	body, err := os.ReadFile(filepath.Join(root.Path, filepath.FromSlash(payload.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "# revised\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestWritePlanUpdatesGitignoreAndRefusesUntrusted(t *testing.T) {
	root := openTemp(t)
	if err := os.WriteFile(filepath.Join(root.Path, ".gitignore"), []byte("tmp/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := &WritePlan{Root: root, Workspace: trust.WorkspaceTrusted}
	decision, err := tool.RequiresApproval(context.Background(), json.RawMessage(`{"plan_name":"notes","body":"hi"}`))
	if err != nil || decision.Required {
		t.Fatalf("decision = %+v err = %v", decision, err)
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"plan_name":"notes","body":"hi"}`)); err != nil {
		t.Fatal(err)
	}
	ignore, err := os.ReadFile(filepath.Join(root.Path, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "\n.gopi/plans\n") {
		t.Fatalf("gitignore = %q", ignore)
	}

	denied, err := (&WritePlan{Root: root, Workspace: trust.WorkspaceUntrusted}).Execute(context.Background(), json.RawMessage(`{"plan_name":"notes","body":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(denied), "access_denied") {
		t.Fatalf("denied = %s", denied)
	}
	outside, err := tool.Execute(context.Background(), json.RawMessage(`{"plan_name":"notes","path":"../secret.md","body":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(outside), "access_denied") {
		t.Fatalf("outside = %s", outside)
	}
}
