package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgund98/gopi/internal/trust"
)

func TestWritePlanStoresTodos(t *testing.T) {
	root := openTemp(t)
	tool := &WritePlan{Root: root, Workspace: trust.WorkspaceTrusted}
	created, err := tool.Execute(t.Context(), json.RawMessage(`{"plan_name":"Ship","body":"# Ship\n\nDo it.\n","todos":[{"id":"fetch-tool","content":"Add web_fetch","status":"pending"},{"id":"wire","content":"Register it"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(created, &payload); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root.Path, filepath.FromSlash(payload.Path)))
	if err != nil {
		t.Fatal(err)
	}
	items, prose, err := SplitPlan(string(body))
	if err != nil {
		t.Fatal(err)
	}
	if prose != "# Ship\n\nDo it.\n" || len(items) != 2 || items[0].ID != "fetch-tool" || items[1].Status != taskPending {
		t.Fatalf("items=%#v prose=%q", items, prose)
	}
}

func TestTasksPatchClearAndSyncPlan(t *testing.T) {
	root := openTemp(t)
	planTool := &WritePlan{Root: root, Workspace: trust.WorkspaceTrusted}
	created, err := planTool.Execute(t.Context(), json.RawMessage(`{"plan_name":"Ship","body":"# Ship\n","todos":[{"id":"a","content":"First"},{"id":"b","content":"Second"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(created, &payload); err != nil {
		t.Fatal(err)
	}
	list := NewTaskList(root)
	if err := list.Set([]Task{{ID: "a", Content: "First", Status: taskPending}, {ID: "b", Content: "Second", Status: taskPending}}, payload.Path); err != nil {
		t.Fatal(err)
	}
	tool := &Tasks{List: list}

	added, err := tool.Execute(t.Context(), json.RawMessage(`{"add":[{"id":"c","content":"Third"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	items, ok := ParseTaskResult(string(added))
	if !ok || len(items) != 3 || items[2].ID != "c" {
		t.Fatalf("add = %s", added)
	}

	patched, err := tool.Execute(t.Context(), json.RawMessage(`{"update":[{"id":"a","status":"completed"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	items, ok = ParseTaskResult(string(patched))
	if !ok || items[0].Status != taskCompleted || items[1].Status != taskPending || items[2].ID != "c" {
		t.Fatalf("patch = %#v", items)
	}

	if _, err := tool.Execute(t.Context(), json.RawMessage(`{"update":[{"id":"b","status":"in_progress"},{"id":"c","status":"in_progress"}]}`)); err == nil || !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("error = %v", err)
	}
	if got := list.Items(); len(got) != 3 || got[1].Status != taskPending {
		t.Fatalf("rejected update changed the list: %#v", got)
	}

	cleared, err := tool.Execute(t.Context(), json.RawMessage(`{"clear":true,"add":[{"id":"d","content":"Only"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	items, ok = ParseTaskResult(string(cleared))
	if !ok || len(items) != 1 || items[0].ID != "d" {
		t.Fatalf("clear add = %#v", items)
	}

	if _, err := tool.Execute(t.Context(), json.RawMessage(`{"clear":true}`)); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root.Path, filepath.FromSlash(payload.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "# Ship\n" {
		t.Fatalf("plan = %q", body)
	}
	saved, prose, err := SplitPlan(string(body))
	if err != nil || len(saved) != 0 || prose != "# Ship\n" {
		t.Fatalf("saved=%#v prose=%q err=%v", saved, prose, err)
	}
}

func TestTasksAddExistingIDUpdatesIt(t *testing.T) {
	tool := &Tasks{List: NewTaskList(openTemp(t))}
	if _, err := tool.Execute(t.Context(), json.RawMessage(`{"add":[{"id":"1","content":"Run go mod init"}]}`)); err != nil {
		t.Fatal(err)
	}
	updated, err := tool.Execute(t.Context(), json.RawMessage(`{"add":[{"id":"1","content":"Run go mod init","status":"in_progress"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	items, ok := ParseTaskResult(string(updated))
	if !ok || len(items) != 1 || items[0].Status != taskInProgress {
		t.Fatalf("items = %#v", items)
	}
}

func TestTaskChangesReportsNewFinishesOnce(t *testing.T) {
	prev := []Task{{ID: "a", Content: "First", Status: taskPending}, {ID: "b", Content: "Second", Status: taskInProgress}}
	next := []Task{{ID: "a", Content: "First", Status: taskCompleted}, {ID: "b", Content: "Second", Status: taskInProgress}}
	changes := TaskChanges(prev, next)
	if len(changes) != 1 || changes[0].Kind != "done" || changes[0].Content != "First" {
		t.Fatalf("changes = %#v", changes)
	}
	again := TaskChanges(next, next)
	if len(again) != 0 {
		t.Fatalf("repeat = %#v", again)
	}
	removed := TaskChanges(next, []Task{{ID: "a", Content: "First", Status: taskCompleted}})
	if len(removed) != 1 || removed[0].Kind != "removed" || removed[0].Content != "Second" {
		t.Fatalf("removed = %#v", removed)
	}
}
