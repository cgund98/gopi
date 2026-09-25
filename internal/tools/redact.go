package tools

import (
	"context"
	"encoding/json"

	"github.com/cgund98/gogent"
)

func WrapRedacting(inner gogent.Tool, redact func(string) string) gogent.Tool {
	if redact == nil {
		return inner
	}
	return redactingTool{inner: inner, redact: redact}
}

type redactingTool struct {
	inner  gogent.Tool
	redact func(string) string
}

func (t redactingTool) Name() string                { return t.inner.Name() }
func (t redactingTool) Description() string         { return t.inner.Description() }
func (t redactingTool) Parameters() json.RawMessage { return t.inner.Parameters() }

func (t redactingTool) RequiresApproval(ctx context.Context, args json.RawMessage) (gogent.ApprovalDecision, error) {
	return t.inner.RequiresApproval(ctx, args)
}

func (t redactingTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	result, err := t.inner.Execute(ctx, args)
	if err != nil || t.redact == nil || len(result) == 0 {
		return result, err
	}
	return json.RawMessage(t.redact(string(result))), nil
}
