package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gopi/internal/workspace"
)

type readFileArgs struct {
	Path   string `json:"path" jsonschema:"description=Path relative to the workspace root"`
	Offset int    `json:"offset,omitempty" jsonschema:"description=1-based line to start from"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=Maximum number of lines to return"`
}

// ReadFile reads a text file inside the workspace.
type ReadFile struct {
	Root workspace.Root
}

func (t *ReadFile) Name() string { return "read_file" }

func (t *ReadFile) Description() string {
	return "Read a file inside the workspace. Paths outside the workspace are refused."
}

func (t *ReadFile) Parameters() json.RawMessage { return schemaFor(new(readFileArgs)) }

func (t *ReadFile) RequiresApproval() bool { return false }

func (t *ReadFile) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var args readFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	resolved, err := t.Root.Resolve(args.Path)
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
