package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gopi/internal/workspace"
)

const maxFindFiles = 50

type findArgs struct {
	Pattern string `json:"pattern,omitempty" jsonschema:"description=Substring matched against the workspace-relative file path. Omit to list files."`
	Path    string `json:"path,omitempty" jsonschema:"description=File or directory relative to the workspace root"`
}

// Find lists workspace files whose paths contain a substring.
type Find struct {
	Root workspace.Root
}

func (t *Find) Name() string { return "find" }

func (t *Find) Description() string {
	return "List workspace files by path substring. Omit the pattern to list files. Paths outside the workspace are refused."
}

func (t *Find) Parameters() json.RawMessage { return schemaFor(new(findArgs)) }

func (t *Find) RequiresApproval() bool { return false }

func (t *Find) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var args findArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	start := t.Root.Path
	if args.Path != "" {
		resolved, err := t.Root.Resolve(args.Path)
		if err != nil {
			return accessDenied(args.Path, err.Error()), nil
		}
		start = resolved
	}

	var files []string
	err := filepath.WalkDir(start, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= maxFindFiles {
			return errStopWalk
		}
		rel, err := filepath.Rel(t.Root.Path, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if args.Pattern != "" && !strings.Contains(rel, args.Pattern) {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil && err != errStopWalk {
		return nil, fmt.Errorf("list workspace files: %w", err)
	}
	return json.Marshal(map[string]any{
		"files":     files,
		"truncated": len(files) >= maxFindFiles,
	})
}

var _ gogent.Tool = (*Find)(nil)
