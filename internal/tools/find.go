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
	"github.com/cgund98/gopi/internal/workspace"
)

const maxFindFiles = 50

type findArgs struct {
	Pattern   string   `json:"pattern,omitempty" jsonschema:"description=Substring matched against the workspace-relative file path. Omit to list files."`
	Path      string   `json:"path,omitempty" jsonschema:"description=File or directory relative to the workspace root"`
	ReadPaths []string `json:"read_paths,omitempty" jsonschema:"description=Protected paths, or files and directories outside the workspace, to include. The user must approve the call."`
}

// Find lists workspace files whose paths contain a substring.
type Find struct {
	Root   workspace.Root
	Rules  policy.Rules
	Grants *ReadGrants
}

func (t *Find) Name() string { return "find" }

func (t *Find) Description() string {
	return "List workspace files by path substring. Omit the pattern to list files. A directory granted with grant_read is listed the same way. Protected files are omitted. If the result says the sandbox blocked a file or a directory, call find again with that path in read_paths. read_paths also accepts a directory outside the workspace. That call asks the user for approval and does not run until they approve."
}

func (t *Find) Parameters() json.RawMessage { return schemaFor(new(findArgs)) }

func (t *Find) RequiresApproval(_ context.Context, raw json.RawMessage) (gogent.ApprovalDecision, error) {
	var args findArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return gogent.ApprovalDecision{}, fmt.Errorf("parse arguments: %w", err)
	}
	return readGrantDecision(t.Root, args.ReadPaths)
}

func (t *Find) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var args findArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	grants, err := canonicalPaths(t.Root, args.ReadPaths)
	if err != nil {
		return accessDenied(args.Path, err.Error()), nil
	}
	starts, err := searchRoots(t.Root, args.Path, grants, t.Grants.List())
	if err != nil {
		return accessDenied(args.Path, err.Error()), nil
	}

	var files []string
	var denied []map[string]string
	for _, start := range starts {
		err = filepath.WalkDir(start, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == ".git" {
					return filepath.SkipDir
				}
				if rule, ok := t.Rules.MatchRead(path); ok && !grantOpens(path, grants) {
					appendDenied(&denied, t.Root.Path, path, rule)
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(t.Root.Path, path)
			if err != nil {
				rel = path
			}
			rel = filepath.ToSlash(rel)
			if rule, ok := t.Rules.MatchRead(path); ok && !grantCoversRead(path, t.Rules, grants) {
				appendDenied(&denied, t.Root.Path, path, rule)
				return nil
			}
			if len(files) >= maxFindFiles {
				return errStopWalk
			}
			if args.Pattern != "" && !strings.Contains(rel, args.Pattern) {
				return nil
			}
			files = append(files, rel)
			return nil
		})
		if err == errStopWalk {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list workspace files: %w", err)
		}
	}
	denied = trimDenied(denied)
	payload := map[string]any{
		"files":     files,
		"denied":    denied,
		"truncated": len(files) >= maxFindFiles,
	}
	if hint, paths := elevationRetry("find", deniedPaths(denied)); hint != "" {
		payload["message"] = hint
		payload["blocked_paths"] = paths
	}
	return json.Marshal(payload)
}

var _ gogent.Tool = (*Find)(nil)
