package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cgund98/gogent"
)

type taskUpdate struct {
	ID      string `json:"id" jsonschema:"description=Id of an existing task."`
	Content string `json:"content,omitempty" jsonschema:"description=Replacement text. Omit to leave the text unchanged."`
	Status  string `json:"status,omitempty" jsonschema:"description=pending, in_progress, completed, or cancelled."`
}

type tasksArgs struct {
	Clear  bool         `json:"clear,omitempty" jsonschema:"description=Drop every current task. Combine with add to replace the list with a new batch."`
	Remove []string     `json:"remove,omitempty" jsonschema:"description=Ids to drop. Ids not listed stay."`
	Update []taskUpdate `json:"update,omitempty" jsonschema:"description=Change the status or content of existing ids. Other ids stay."`
	Add    []Task       `json:"add,omitempty" jsonschema:"description=Tasks to append. Each needs an id and content."`
}

// Tasks patches the session checklist. Agent mode registers it. Ask, Plan, and a delegate child do not.
type Tasks struct {
	List *TaskList
}

func (t *Tasks) Name() string { return "tasks" }

func (t *Tasks) Description() string {
	return "Patch the session checklist. clear drops every item. add appends new items, and an id that already exists is updated instead of rejected. update changes the status or content of existing ids. remove drops ids. A call can do more than one of these, applied in that order. Ids the call does not mention stay. clear plus add replaces the list. At most one item is in progress. If a list is already loaded, do not add those ids again. Leave unchanged ids out of the call."
}

func (t *Tasks) Parameters() json.RawMessage { return schemaFor(new(tasksArgs)) }

func (t *Tasks) RequiresApproval(context.Context, json.RawMessage) (gogent.ApprovalDecision, error) {
	return gogent.ApprovalDecision{}, nil
}

func (t *Tasks) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var args tasksArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	if !args.Clear && len(args.Remove) == 0 && len(args.Update) == 0 && len(args.Add) == 0 {
		return nil, fmt.Errorf("add, update, remove, or clear a task")
	}
	next, err := patchTasks(t.snapshot(), args)
	if err != nil {
		return nil, err
	}
	if t.List != nil {
		if err := t.List.apply(next); err != nil {
			return nil, err
		}
	}
	return TaskResultJSON(next)
}

func (t *Tasks) snapshot() []Task {
	if t == nil || t.List == nil {
		return nil
	}
	return t.List.snapshot()
}

func patchTasks(items []Task, args tasksArgs) ([]Task, error) {
	next := cloneTasks(items)
	if args.Clear {
		next = nil
	}
	for _, id := range args.Remove {
		idx := taskIndex(next, id)
		if idx < 0 {
			return nil, fmt.Errorf("unknown task %s", id)
		}
		next = append(next[:idx], next[idx+1:]...)
	}
	for _, update := range args.Update {
		idx := taskIndex(next, update.ID)
		if idx < 0 {
			return nil, fmt.Errorf("unknown task %s", update.ID)
		}
		if update.Content == "" && update.Status == "" {
			return nil, fmt.Errorf("update %s needs content or status", update.ID)
		}
		if update.Content != "" {
			next[idx].Content = update.Content
		}
		if update.Status != "" {
			next[idx].Status = update.Status
		}
	}
	for _, item := range args.Add {
		idx := taskIndex(next, item.ID)
		if idx >= 0 {
			if item.Content != "" {
				next[idx].Content = item.Content
			}
			if item.Status != "" {
				next[idx].Status = item.Status
			}
			continue
		}
		next = append(next, item)
	}
	return normalizeTasks(next)
}

func taskIndex(items []Task, id string) int {
	for i, item := range items {
		if item.ID == id {
			return i
		}
	}
	return -1
}

var _ gogent.Tool = (*Tasks)(nil)
