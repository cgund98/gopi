package tools

import (
	"bufio"
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

const maxGrepMatches = 50

type grepArgs struct {
	Pattern   string   `json:"pattern" jsonschema:"description=Substring to search for"`
	Path      string   `json:"path,omitempty" jsonschema:"description=File or directory relative to the workspace root"`
	ReadPaths []string `json:"read_paths,omitempty" jsonschema:"description=Protected files or directories to include. The user must approve the call."`
}

type grepMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Grep searches file contents inside the workspace.
type Grep struct {
	Root  workspace.Root
	Rules policy.Rules
}

func (t *Grep) Name() string { return "grep" }

func (t *Grep) Description() string {
	return "Search for a substring in workspace files. Protected files are omitted. If the result says the sandbox blocked a file, call grep again with that path in read_paths. That call asks the user for approval and does not run until they approve."
}

func (t *Grep) Parameters() json.RawMessage { return schemaFor(new(grepArgs)) }

func (t *Grep) RequiresApproval(_ context.Context, raw json.RawMessage) (gogent.ApprovalDecision, error) {
	var args grepArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return gogent.ApprovalDecision{}, fmt.Errorf("parse arguments: %w", err)
	}
	return readGrantDecision(t.Root, args.ReadPaths)
}

func (t *Grep) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var args grepArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	if args.Pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	grants, err := canonicalPaths(t.Root, args.ReadPaths)
	if err != nil {
		return accessDenied(args.Path, err.Error()), nil
	}
	start := t.Root.Path
	if args.Path != "" {
		resolved, err := t.Root.Resolve(args.Path)
		if err != nil {
			return accessDenied(args.Path, err.Error()), nil
		}
		start = resolved
	}

	var matches []grepMatch
	var denied []map[string]string
	err = filepath.WalkDir(start, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if rule, ok := t.Rules.MatchRead(path); ok && !coversGrant(path, grants) {
			rel, relErr := filepath.Rel(t.Root.Path, path)
			if relErr != nil {
				rel = path
			}
			denied = append(denied, map[string]string{
				"error":   "access_denied",
				"path":    filepath.ToSlash(rel),
				"message": "protected path " + rule,
			})
			return nil
		}
		if len(matches) >= maxGrepMatches {
			return errStopWalk
		}
		found, err := searchFile(t.Root.Path, path, args.Pattern, maxGrepMatches-len(matches))
		if err != nil {
			return nil
		}
		matches = append(matches, found...)
		return nil
	})
	if err != nil && err != errStopWalk {
		return nil, fmt.Errorf("search workspace: %w", err)
	}
	payload := map[string]any{
		"matches":   matches,
		"denied":    denied,
		"truncated": len(matches) >= maxGrepMatches,
	}
	blocked := deniedPaths(denied)
	if hint, paths := elevationRetry("grep", blocked); hint != "" {
		payload["message"] = hint
		payload["blocked_paths"] = paths
	}
	return json.Marshal(payload)
}

func deniedPaths(denied []map[string]string) []string {
	paths := make([]string, 0, len(denied))
	for _, item := range denied {
		if path := item["path"]; path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

var errStopWalk = fmt.Errorf("match cap reached")

func searchFile(root, path, pattern string, remaining int) (matches []grepMatch, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()

	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if strings.Contains(line, pattern) {
			matches = append(matches, grepMatch{Path: rel, Line: lineNo, Text: line})
			if len(matches) >= remaining {
				break
			}
		}
	}
	return matches, scanner.Err()
}

var _ gogent.Tool = (*Grep)(nil)
