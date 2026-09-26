package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
	Root   workspace.Root
	Rules  policy.Rules
	Grants *ReadGrants
}

func (t *ReadFile) Name() string { return "read_file" }

func (t *ReadFile) Description() string {
	return "Read a file. The result gives start_line, end_line, and total_lines for the returned content. When truncated is true, call again with offset set to next_offset to read the rest; lines already returned do not need to be read again. Paths outside the workspace and protected paths require approval. A path covered by grant_read does not ask again unless it is protected."
}

func (t *ReadFile) Parameters() json.RawMessage { return schemaFor(new(readFileArgs)) }

func (t *ReadFile) RequiresApproval(_ context.Context, raw json.RawMessage) (gogent.ApprovalDecision, error) {
	resolved, outside, err := t.locate(raw)
	if err != nil {
		return gogent.ApprovalDecision{}, nil
	}
	if rule, ok := t.Rules.MatchRead(resolved); ok {
		return gogent.ApprovalDecision{Required: true, Reason: "Protected path " + rule + ": " + displayPath(raw)}, nil
	}
	if outside && !t.Grants.Covers(resolved) {
		return gogent.ApprovalDecision{Required: true, Reason: "Path is outside the workspace: " + displayPath(raw)}, nil
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
	window := readWindow(string(body), args.Offset, args.Limit)
	payload := map[string]any{
		"path":        args.Path,
		"content":     window.text,
		"start_line":  window.start,
		"end_line":    window.end,
		"total_lines": window.total,
	}
	if window.next > 0 {
		payload["truncated"] = true
		payload["next_offset"] = window.next
	}
	return json.Marshal(payload)
}

type lineWindow struct {
	text  string
	start int
	end   int
	total int
	next  int
}

// readWindow returns whole lines from offset, up to limit lines and
// maxReadBytes. next is the 1-based line to pass as offset to continue,
// or 0 when the window reaches the end of the file or the requested limit.
func readWindow(text string, offset, limit int) lineWindow {
	lines := splitKeep(text)
	window := lineWindow{total: len(lines)}
	start := 0
	if offset > 1 {
		start = offset - 1
	}
	if start >= len(lines) {
		return window
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	var b strings.Builder
	i := start
	for ; i < end; i++ {
		line := lines[i]
		if b.Len()+len(line) > maxReadBytes {
			if b.Len() == 0 {
				b.WriteString(line[:maxReadBytes])
				i++
			}
			break
		}
		b.WriteString(line)
	}
	window.text = b.String()
	window.start = start + 1
	window.end = i
	if i < end {
		window.next = i + 1
	}
	return window
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
