package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/policy"
)

func TestExploreDescriptionCoversToolsetAndLimits(t *testing.T) {
	text := (&Explore{}).Description()
	for _, want := range []string{
		"quick, medium, or very thorough",
		"read_file, grep, and find",
		"no shell",
		"cannot ask for approval",
		"untrusted observation",
		"6 explore calls",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("description missing %q: %s", want, text)
		}
	}
}

func TestExploreRequiresApprovalNeverPauses(t *testing.T) {
	decision, err := (&Explore{}).RequiresApproval(context.Background(), json.RawMessage(`{"task":"find it"}`))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Required {
		t.Fatalf("explore should fail closed instead of pausing: %+v", decision)
	}
}

func TestExploreRejectsAnEmptyTask(t *testing.T) {
	if _, err := (&Explore{}).Execute(context.Background(), json.RawMessage(`{"task":"  "}`)); err == nil {
		t.Fatal("an empty task should fail")
	}
}

func TestExploreChildRegistryIsReadOnly(t *testing.T) {
	root := openTemp(t)
	rules, err := policy.Build(root.Path, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := (&Explore{Root: root, Rules: rules}).childRegistry()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, registered := range registry.Tools() {
		names[registered.Name()] = true
	}
	for _, name := range []string{"read_file", "grep", "find"} {
		if !names[name] {
			t.Fatalf("missing %s in %#v", name, names)
		}
	}
	if names["shell"] || names["edit_file"] || names["delegate"] || names["explore"] || names["grant_read"] {
		t.Fatalf("explore child tools = %#v", names)
	}
}

func TestExploreChildPromptCarriesThoroughnessAndNotes(t *testing.T) {
	root := openTemp(t)
	tool := &Explore{Root: root, Prompt: func() (string, error) { return "You are a search specialist.", nil }}

	plain := tool.childPrompt(exploreArgs{Task: "find the parser"})
	if !strings.HasPrefix(plain, "You are a search specialist.") {
		t.Fatalf("prompt = %q", plain)
	}
	if !strings.Contains(plain, "<thoroughness>\nmedium\n</thoroughness>") {
		t.Fatalf("default thoroughness = %q", plain)
	}
	if strings.Contains(plain, "<caller_notes>") {
		t.Fatalf("notes block should be omitted when empty: %q", plain)
	}
	if !strings.Contains(plain, "<cwd>\n"+root.Path+"\n</cwd>") {
		t.Fatalf("prompt is missing the workspace: %q", plain)
	}

	noted := tool.childPrompt(exploreArgs{Task: "find the parser", Thoroughness: "very thorough", Instructions: "start in internal"})
	if !strings.Contains(noted, "<thoroughness>\nvery thorough\n</thoroughness>") {
		t.Fatalf("thoroughness = %q", noted)
	}
	if !strings.Contains(noted, "<caller_notes>\nstart in internal\n</caller_notes>") {
		t.Fatalf("notes = %q", noted)
	}
}

func TestExploreChildPromptFallsBackWhenTheOverrideIsEmpty(t *testing.T) {
	tool := &Explore{Root: openTemp(t), Prompt: func() (string, error) { return "", nil }}
	if got := tool.childPrompt(exploreArgs{Task: "find it"}); !strings.Contains(got, "file search specialist") {
		t.Fatalf("prompt = %q", got)
	}
}

func TestExploreResultShapeAndLimit(t *testing.T) {
	root := openTemp(t)
	model := &scriptModel{steps: []gogent.Message{gogent.NewAssistantMessage("the parser lives in parser.go")}}
	tool := &Explore{
		Root:     root,
		MaxCalls: 1,
		NewModel: func(*gogent.ToolRegistry, string) (gogent.Model, error) { return model, nil },
	}
	raw, err := tool.Execute(context.Background(), json.RawMessage(`{"task":"find the parser","thoroughness":"quick"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Kind      string   `json:"kind"`
		Answer    string   `json:"answer"`
		ToolCalls int      `json:"tool_calls"`
		Denied    []string `json:"denied"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Kind != "explore" || payload.Answer != "the parser lives in parser.go" {
		t.Fatalf("payload = %+v", payload)
	}

	second, err := tool.Execute(context.Background(), json.RawMessage(`{"task":"again"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(second), "explore_limit") {
		t.Fatalf("second call = %s", second)
	}
}

func TestExploreReportsProgressAsExplore(t *testing.T) {
	root := openTemp(t)
	progress := &SubagentProgress{}
	model := &scriptModel{steps: []gogent.Message{gogent.NewAssistantMessage("done")}}
	tool := &Explore{
		Root:     root,
		Progress: progress,
		NewModel: func(*gogent.ToolRegistry, string) (gogent.Model, error) { return model, nil },
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"task":"look around"}`)); err != nil {
		t.Fatal(err)
	}
	// finish clears the status, so the kind has to be checked while it runs.
	progress.begin("explore", "look around")
	status, ok := progress.Snapshot()
	if !ok || status.Kind != "explore" || status.Task != "look around" {
		t.Fatalf("status = %+v running = %v", status, ok)
	}
	progress.finish()
}

func TestSubagentProgressCountsSearches(t *testing.T) {
	progress := &SubagentProgress{}
	progress.begin("explore", "look")
	progress.toolCalled("read_file", json.RawMessage(`{"path":"main.go"}`))
	progress.toolCalled("grep", json.RawMessage(`{"pattern":"resume"}`))
	progress.toolCalled("find", json.RawMessage(`{"pattern":".go"}`))
	status, ok := progress.Snapshot()
	if !ok {
		t.Fatal("progress should be running")
	}
	if status.ToolCalls != 3 || status.Searches != 2 || status.Query != ".go" {
		t.Fatalf("status = %+v", status)
	}
	if status.Last != "find .go" {
		t.Fatalf("last = %q", status.Last)
	}
}
