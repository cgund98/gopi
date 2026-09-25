package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/trust"
	"github.com/cgund98/gopi/internal/workspace"
)

type editFileArgs struct {
	Path string `json:"path" jsonschema:"description=Path relative to the workspace root"`
	Old  string `json:"old" jsonschema:"description=Exact text to replace. Empty creates a new file."`
	New  string `json:"new" jsonschema:"description=Replacement text"`
}

// EditNote is one successful edit_file write.
// First is set only for the first write of that path. Before is the pre-write
// body, and Created is set when the file did not exist.
type EditNote struct {
	Path    string
	First   bool
	Created bool
	Before  []byte
}

// EditFile replaces an exact snippet, or creates a file when old is empty.
type EditFile struct {
	Root      workspace.Root
	Workspace trust.Workspace
	Rules     policy.Rules
	OnWrite   func(EditNote)

	seen map[string]bool
}

// Reset forgets which paths already have a baseline. A new chat uses it.
func (t *EditFile) Reset() {
	t.seen = map[string]bool{}
}

// Forget lets the next edit of path capture a new baseline.
func (t *EditFile) Forget(path string) {
	delete(t.seen, path)
}

// Seed marks paths that already have a baseline so a later edit keeps it.
func (t *EditFile) Seed(paths []string) {
	if t.seen == nil {
		t.seen = map[string]bool{}
	}
	for _, path := range paths {
		t.seen[path] = true
	}
}

func (t *EditFile) Name() string { return "edit_file" }

func (t *EditFile) Description() string {
	return "Replace an exact text snippet in a workspace file, or create a file when old is empty. Protected paths and paths outside the workspace ask for approval. Refused when the workspace is untrusted."
}

func (t *EditFile) Parameters() json.RawMessage { return schemaFor(new(editFileArgs)) }

func (t *EditFile) RequiresApproval(_ context.Context, raw json.RawMessage) (gogent.ApprovalDecision, error) {
	if t.Workspace != trust.WorkspaceTrusted {
		return gogent.ApprovalDecision{}, nil
	}
	resolved, outside, err := t.locate(raw)
	if err != nil {
		return gogent.ApprovalDecision{}, nil
	}
	if rule, ok := t.Rules.MatchWrite(resolved); ok {
		return gogent.ApprovalDecision{Required: true, Reason: "Protected path " + rule + ": " + editDisplayPath(raw)}, nil
	}
	if outside {
		return gogent.ApprovalDecision{Required: true, Reason: grantReason(nil, []string{resolved})}, nil
	}
	return gogent.ApprovalDecision{}, nil
}

func (t *EditFile) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var args editFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	if t.Workspace != trust.WorkspaceTrusted {
		return accessDenied(args.Path, "workspace is not trusted; edits are refused"), nil
	}
	resolved, _, err := t.Root.Canonical(args.Path)
	if err != nil {
		return accessDenied(args.Path, err.Error()), nil
	}

	existing, err := os.ReadFile(resolved)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read file: %w", err)
	}
	created := os.IsNotExist(err)
	var next string
	switch {
	case created:
		if args.Old != "" {
			return nil, fmt.Errorf("file does not exist")
		}
		next = args.New
	case args.Old == "":
		next = args.New
	default:
		count := strings.Count(string(existing), args.Old)
		if count == 0 {
			return nil, fmt.Errorf("old text was not found")
		}
		if count > 1 {
			return nil, fmt.Errorf("old text matched %d times", count)
		}
		next = strings.Replace(string(existing), args.Old, args.New, 1)
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}
	if err := os.WriteFile(resolved, []byte(next), 0o644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}
	t.noteWrite(args.Path, created, existing)
	return json.Marshal(map[string]string{
		"path":   args.Path,
		"status": "updated",
	})
}

func (t *EditFile) locate(raw json.RawMessage) (resolved string, outside bool, err error) {
	var args editFileArgs
	if err = json.Unmarshal(raw, &args); err != nil {
		return "", false, fmt.Errorf("parse arguments: %w", err)
	}
	return t.Root.Canonical(args.Path)
}

func (t *EditFile) noteWrite(path string, created bool, before []byte) {
	if t.seen == nil {
		t.seen = map[string]bool{}
	}
	first := !t.seen[path]
	t.seen[path] = true
	if t.OnWrite == nil {
		return
	}
	note := EditNote{Path: path, First: first, Created: created}
	if first && !created {
		note.Before = append([]byte(nil), before...)
	}
	t.OnWrite(note)
}

func editDisplayPath(raw json.RawMessage) string {
	var args editFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return ""
	}
	return args.Path
}

var _ gogent.Tool = (*EditFile)(nil)
