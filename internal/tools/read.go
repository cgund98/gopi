package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/workspace"
)

type readFileArgs struct {
	Path   string `json:"path" jsonschema:"description=Path relative to the workspace root"`
	Offset int    `json:"offset,omitempty" jsonschema:"description=1-based line to start from"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=Maximum number of lines to return"`
}

// ReadFile reads a text file inside the workspace.
type ReadFile struct {
	Root  workspace.Root
	Rules policy.Rules
}

func (t *ReadFile) Name() string { return "read_file" }

func (t *ReadFile) Description() string {
	return "Read a file. Paths outside the workspace and protected paths require approval."
}

func (t *ReadFile) Parameters() json.RawMessage { return schemaFor(new(readFileArgs)) }

func (t *ReadFile) RequiresApproval(_ context.Context, raw json.RawMessage) (gogent.ApprovalDecision, error) {
	resolved, outside, err := t.locate(raw)
	if err != nil {
		return gogent.ApprovalDecision{}, nil
	}
	if outside {
		reason := "Path is outside the workspace: " + displayPath(raw)
		if rule, ok := t.Rules.MatchRead(resolved); ok {
			reason = "Protected path " + rule + ": " + displayPath(raw)
		}
		return gogent.ApprovalDecision{Required: true, Reason: reason}, nil
	}
	if rule, ok := t.Rules.MatchRead(resolved); ok {
		return gogent.ApprovalDecision{Required: true, Reason: "Protected path " + rule + ": " + displayPath(raw)}, nil
	}
	return gogent.ApprovalDecision{}, nil
}

func (t *ReadFile) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var args readFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	resolved, _, err := t.Root.Canonical(args.Path)
	if err != nil {
		return accessDenied(args.Path, err.Error()), nil
	}
	body, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	text := sliceLines(string(body), args.Offset, args.Limit)
	if len(text) > maxReadBytes {
		text = text[:maxReadBytes]
	}
	return json.Marshal(map[string]any{
		"path":    args.Path,
		"content": text,
	})
}

func (t *ReadFile) locate(raw json.RawMessage) (resolved string, outside bool, err error) {
	var args readFileArgs
	if err = json.Unmarshal(raw, &args); err != nil {
		return "", false, fmt.Errorf("parse arguments: %w", err)
	}
	return t.Root.Canonical(args.Path)
}

func displayPath(raw json.RawMessage) string {
	var args readFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return ""
	}
	return args.Path
}

func sliceLines(text string, offset, limit int) string {
	if offset <= 1 && limit <= 0 {
		return text
	}
	lines := splitKeep(text)
	start := 0
	if offset > 1 {
		start = offset - 1
	}
	if start > len(lines) {
		return ""
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	out := ""
	for i := start; i < end; i++ {
		out += lines[i]
	}
	return out
}

func splitKeep(text string) []string {
	if text == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

var _ gogent.Tool = (*ReadFile)(nil)
