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

func TestGrantReadLetsLaterReadSkipApproval(t *testing.T) {
	root := openTemp(t)
	outside := t.TempDir()
	note := filepath.Join(outside, "note.txt")
	if err := os.WriteFile(note, []byte("session-body\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, ".env"), []byte("floor-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	resolved, _, err := root.Canonical(outside)
	if err != nil {
		t.Fatal(err)
	}
	grants := &ReadGrants{}
	grant := &GrantRead{Root: root, Grants: grants}
	raw := json.RawMessage(`{"path":` + mustJSON(t, resolved) + `}`)
	decision, err := grant.RequiresApproval(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Required || decision.Reason != resolved {
		t.Fatalf("decision = %#v", decision)
	}
	if grants.Covers(resolved) {
		t.Fatal("stored the path before approval")
	}
	if _, err := grant.Execute(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	again, err := grant.RequiresApproval(context.Background(), raw)
	if err != nil || again.Required {
		t.Fatalf("second decision = %#v err = %v", again, err)
	}

	readTool := &ReadFile{Root: root, Rules: rules, Grants: grants}
	noteRaw := json.RawMessage(`{"path":` + mustJSON(t, note) + `}`)
	decision, err = readTool.RequiresApproval(context.Background(), noteRaw)
	if err != nil || decision.Required {
		t.Fatalf("read decision = %#v err = %v", decision, err)
	}
	body, err := readTool.Execute(context.Background(), noteRaw)
	if err != nil || !strings.Contains(string(body), "session-body") {
		t.Fatalf("read = %s err = %v", body, err)
	}

	envRaw := json.RawMessage(`{"path":` + mustJSON(t, filepath.Join(outside, ".env")) + `}`)
	decision, err = readTool.RequiresApproval(context.Background(), envRaw)
	if err != nil || !decision.Required || !strings.Contains(decision.Reason, "Protected path") {
		t.Fatalf("protected decision = %#v err = %v", decision, err)
	}

	grepTool := &Grep{Root: root, Rules: rules, Grants: grants}
	found, err := grepTool.Execute(context.Background(), json.RawMessage(`{"pattern":"session-body","path":`+mustJSON(t, resolved)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Matches []struct {
			Path string `json:"path"`
			Text string `json:"text"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(found, &payload); err != nil {
		t.Fatal(err)
	}
	sawNote := false
	for _, match := range payload.Matches {
		if strings.HasSuffix(match.Path, ".env") || strings.Contains(match.Text, "floor-secret") {
			t.Fatalf("floor file opened: %s", found)
		}
		if strings.HasSuffix(match.Path, "note.txt") {
			sawNote = true
		}
	}
	if !sawNote {
		t.Fatalf("grep = %s", found)
	}

	model := &scriptModel{steps: []gogent.Message{
		gogent.NewAssistantMessageWithToolCalls("", []gogent.ToolCall{{
			ID:       "call_read",
			ToolName: "read_file",
			Args:     noteRaw,
		}}),
		gogent.NewAssistantMessage("finished"),
	}}
	delegate := &Delegate{
		Root:  root,
		Rules: rules,
		NewModel: func(*gogent.ToolRegistry) (gogent.Model, error) {
			return model, nil
		},
	}
	result, err := delegate.Execute(context.Background(), json.RawMessage(`{"task":"read the note"}`))
	if err != nil {
		t.Fatal(err)
	}
	var delegated struct {
		Answer string   `json:"answer"`
		Denied []string `json:"denied"`
	}
	if err := json.Unmarshal(result, &delegated); err != nil {
		t.Fatal(err)
	}
	if delegated.Answer != "finished" || len(delegated.Denied) != 1 || !strings.Contains(delegated.Denied[0], "outside the workspace") || strings.Contains(string(result), "session-body") {
		t.Fatalf("delegate = %s", result)
	}
}
