package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/workspace"
)

const (
	maxGrepMatches = 50
	// maxGrepLineRunes caps one match's text. Minified files and build output can
	// put megabytes on a single line.
	maxGrepLineRunes = 300
	// maxGrepBytes caps the match text in one result so it stays well inside a
	// model's context window.
	maxGrepBytes = 32 << 10
	// binarySniffBytes is how much of a file is checked for a NUL byte.
	binarySniffBytes = 8 << 10
)

type grepArgs struct {
	Pattern   string   `json:"pattern" jsonschema:"description=Substring to search for"`
	Path      string   `json:"path,omitempty" jsonschema:"description=File or directory relative to the workspace root"`
	ReadPaths []string `json:"read_paths,omitempty" jsonschema:"description=Protected paths, or files and directories outside the workspace, to include. The user must approve the call."`
}

type grepMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// Grep searches file contents inside the workspace.
type Grep struct {
	Root   workspace.Root
	Rules  policy.Rules
	Grants *ReadGrants
}

func (t *Grep) Name() string { return "grep" }

func (t *Grep) Description() string {
	return "Search for a substring in workspace files. Binary files are skipped, long lines are cut to the text around the match, and results stop at 50 matches or 32 KB, so narrow the path or pattern when truncated is true. A directory granted with grant_read is searched the same way. Protected files are omitted. If the result says the sandbox blocked a file or a directory, call grep again with that path in read_paths. read_paths also accepts a directory outside the workspace. That call asks the user for approval and does not run until they approve."
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
	starts, err := searchRoots(t.Root, args.Path, grants, t.Grants.List())
	if err != nil {
		return accessDenied(args.Path, err.Error()), nil
	}

	var matches []grepMatch
	var denied []map[string]string
	textBytes := 0
	truncated := false
	binary := 0
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
			if rule, ok := t.Rules.MatchRead(path); ok && !grantCoversRead(path, t.Rules, grants) {
				appendDenied(&denied, t.Root.Path, path, rule)
				return nil
			}
			if len(matches) >= maxGrepMatches || textBytes >= maxGrepBytes {
				truncated = true
				return errStopWalk
			}
			found, isBinary, err := searchFile(t.Root.Path, path, args.Pattern, maxGrepMatches-len(matches))
			if isBinary {
				binary++
			}
			if err != nil {
				return nil
			}
			for _, match := range found {
				if textBytes+len(match.Text) > maxGrepBytes {
					truncated = true
					return errStopWalk
				}
				textBytes += len(match.Text)
				matches = append(matches, match)
			}
			return nil
		})
		if err == errStopWalk {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("search workspace: %w", err)
		}
	}
	denied = trimDenied(denied)
	payload := map[string]any{
		"matches":   matches,
		"denied":    denied,
		"truncated": truncated || len(matches) >= maxGrepMatches,
	}
	if binary > 0 {
		payload["binary_files_skipped"] = binary
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

// searchFile returns up to remaining matches. A file with a NUL byte near the start
// is treated as binary and not searched.
func searchFile(root, path, pattern string, remaining int) (matches []grepMatch, binary bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()

	reader := bufio.NewReaderSize(file, 64*1024)
	head, _ := reader.Peek(binarySniffBytes)
	if bytes.IndexByte(head, 0) >= 0 {
		return nil, true, nil
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if strings.Contains(line, pattern) {
			matches = append(matches, grepMatch{Path: rel, Line: lineNo, Text: clipLine(line, pattern, maxGrepLineRunes)})
			if len(matches) >= remaining {
				break
			}
		}
	}
	if errors.Is(scanner.Err(), bufio.ErrTooLong) {
		return matches, false, nil
	}
	return matches, false, scanner.Err()
}

// clipLine keeps about limit runes of line centred on the first match, marking cut
// ends with an ellipsis.
func clipLine(line, pattern string, limit int) string {
	if utf8.RuneCountInString(line) <= limit {
		return line
	}
	runes := []rune(line)
	at := 0
	if index := strings.Index(line, pattern); index >= 0 {
		at = utf8.RuneCountInString(line[:index])
	}
	patternRunes := utf8.RuneCountInString(pattern)
	start := at - (limit-patternRunes)/2
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(runes) {
		end = len(runes)
		start = max(0, end-limit)
	}
	out := string(runes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}

var _ gogent.Tool = (*Grep)(nil)
