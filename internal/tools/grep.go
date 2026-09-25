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
	"github.com/cgund98/gopi/internal/workspace"
)

const maxGrepMatches = 50

type grepArgs struct {
	Pattern string `json:"pattern" jsonschema:"description=Substring to search for"`
	Path    string `json:"path,omitempty" jsonschema:"description=File or directory relative to the workspace root"`
}

type grepMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Grep searches file contents inside the workspace.
type Grep struct {
	Root workspace.Root
}

func (t *Grep) Name() string { return "grep" }

func (t *Grep) Description() string {
	return "Search for a substring in workspace files. Paths outside the workspace are refused."
}

func (t *Grep) Parameters() json.RawMessage { return schemaFor(new(grepArgs)) }

func (t *Grep) RequiresApproval() bool { return false }

func (t *Grep) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var args grepArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	if args.Pattern == "" {
		return nil, fmt.Errorf("pattern is required")
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
	return json.Marshal(map[string]any{
		"matches":   matches,
		"truncated": len(matches) >= maxGrepMatches,
	})
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
