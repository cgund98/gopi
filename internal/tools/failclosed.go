package tools

import (
	"context"
	"encoding/json"

	"github.com/cgund98/gogent"
)

// failClosed runs a tool only when it would not pause for approval.
// A subagent cannot raise an approval card.
type failClosed struct {
	inner gogent.Tool
}

func (t failClosed) Name() string                { return t.inner.Name() }
func (t failClosed) Description() string         { return t.inner.Description() }
func (t failClosed) Parameters() json.RawMessage { return t.inner.Parameters() }

func (t failClosed) RequiresApproval(context.Context, json.RawMessage) (gogent.ApprovalDecision, error) {
	return gogent.ApprovalDecision{}, nil
}

func (t failClosed) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	decision, err := t.inner.RequiresApproval(ctx, args)
	if err != nil {
		return accessDenied(argPath(args), err.Error()), nil
	}
	if decision.Required {
		message := decision.Reason
		if message == "" {
			message = "approval is not available to a subagent"
		}
		return accessDenied(argPath(args), message), nil
	}
	return t.inner.Execute(ctx, args)
}

func argPath(args json.RawMessage) string {
	var payload struct {
		Path    string `json:"path"`
		Command string `json:"command"`
		Network string `json:"network"`
	}
	if err := json.Unmarshal(args, &payload); err != nil {
		return ""
	}
	if payload.Path != "" {
		return payload.Path
	}
	if payload.Network != "" {
		return payload.Network
	}
	return payload.Command
}
