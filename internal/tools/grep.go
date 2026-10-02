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
	"regexp"
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
	Regex     bool     `json:"regex,omitempty" jsonschema:"description=Match pattern as a regular expression instead of a literal substring."`
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
	// Engine is auto, ripgrep, or builtin. RGPath is the resolved ripgrep
	// binary, and is empty when ripgrep is unavailable.
	Engine string
	RGPath string
	// HomeDir is ~/.gopi, and SecretFiles are the files the sandbox denies.
	HomeDir          string
	SecretFiles      []string
	RespectGitignore bool
}

func (t *Grep) Name() string { return "grep" }

func (t *Grep) Description() string {
	engine := "Search runs on a built-in walker."
	if backend, err := ResolveEngine(t.Engine, t.RGPath); err == nil && backend == BackendRipgrep {
		engine = "Search is backed by ripgrep, which finds files in large trees quickly."
	}
	return engine + " The pattern is a literal substring unless regex is true. Binary files are skipped, long lines are cut to the text around the match, and results stop at 50 matches or 32 KB, so narrow the path or pattern when truncated is true. The result reports which backend ran in backend. A directory granted with grant_read is searched the same way. Protected files are omitted. If the result says the sandbox blocked a file or a directory, call grep again with that path in read_paths. read_paths also accepts a directory outside the workspace. That call asks the user for approval and does not run until they approve."
}

func (t *Grep) Parameters() json.RawMessage { return schemaFor(new(grepArgs)) }

func (t *Grep) RequiresApproval(_ context.Context, raw json.RawMessage) (gogent.ApprovalDecision, error) {
	var args grepArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return gogent.ApprovalDecision{}, fmt.Errorf("parse arguments: %w", err)
	}
	return readGrantDecision(t.Root, args.ReadPaths)
}

func (t *Grep) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var args grepArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	if args.Pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	if args.Regex {
		if _, err := regexp.Compile(args.Pattern); err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
	}
	grants, err := canonicalPaths(t.Root, args.ReadPaths)
	if err != nil {
		return accessDenied(args.Path, err.Error()), nil
	}
	opened := append(t.Grants.List(), grants...)
	starts, err := searchRoots(t.Root, args.Path, grants, t.Grants.List())
	if err != nil {
		return accessDenied(args.Path, err.Error()), nil
	}

	backend, engineErr := ResolveEngine(t.Engine, t.RGPath)
	if engineErr != nil {
		return nil, engineErr
	}
	if backend == BackendRipgrep {
		payload, fallback, err := t.searchWithRipgrep(ctx, starts, args)
		if err != nil {
			return nil, err
		}
		if fallback == "" {
			return payload, nil
		}
		// The engine is optional unless it was named explicitly, so a launch
		// failure falls back to the walker and says why.
		result, err := t.searchWithWalker(starts, args, opened)
		if err != nil {
			return nil, err
		}
		result["backend"] = BackendBuiltin
		result["fallback_reason"] = fallback
		return marshalPayload(result)
	}
	result, err := t.searchWithWalker(starts, args, opened)
	if err != nil {
		return nil, err
	}
	result["backend"] = BackendBuiltin
	return marshalPayload(result)
}

// searchWithRipgrep runs the ripgrep backend. A non-empty fallback means the
// engine could not run and the caller should use the walker instead.
func (t *Grep) searchWithRipgrep(ctx context.Context, starts []string, args grepArgs) (json.RawMessage, string, error) {
	engine := &SearchEngine{RGPath: t.RGPath, HomeDir: t.HomeDir, SecretFiles: t.SecretFiles, RespectGitignore: t.RespectGitignore}
	found, err := engine.search(ctx, searchRequest{
		Root:    t.Root,
		Starts:  starts,
		Pattern: args.Pattern,
		Regex:   args.Regex,
		Grants:  t.Grants.List(),
	})
	if err != nil {
		if t.Engine == EngineRipgrep {
			return nil, "", err
		}
		return nil, err.Error(), nil
	}
	denied := trimDenied(found.Denied)
	payload := map[string]any{
		"matches":   found.Matches,
		"denied":    denied,
		"truncated": found.Truncated,
		"backend":   BackendRipgrep,
	}
	if found.Binary > 0 {
		payload["binary_files_skipped"] = found.Binary
	}
	if hint, paths := elevationRetry("grep", deniedPaths(denied)); hint != "" {
		payload["message"] = hint
		payload["blocked_paths"] = paths
	}
	raw, err := marshalPayload(payload)
	return raw, "", err
}

func (t *Grep) searchWithWalker(starts []string, args grepArgs, opened []string) (map[string]any, error) {
	var matches []grepMatch
	var denied []map[string]string
	textBytes := 0
	truncated := false
	binary := 0

	var re *regexp.Regexp
	if args.Regex {
		compiled, err := regexp.Compile(args.Pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
		re = compiled
	}
	for _, start := range starts {
		err := filepath.WalkDir(start, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == ".git" {
					return filepath.SkipDir
				}
				if rule, ok := t.Rules.MatchRead(path); ok && !grantOpens(path, t.Rules, opened) && !t.Rules.Opens(path) {
					appendDenied(&denied, t.Root.Path, path, rule)
					return filepath.SkipDir
				}
				return nil
			}
			if rule, ok := t.Rules.MatchRead(path); ok && !grantCoversRead(path, t.Rules, opened) {
				appendDenied(&denied, t.Root.Path, path, rule)
				return nil
			}
			if len(matches) >= maxGrepMatches || textBytes >= maxGrepBytes {
				truncated = true
				return errStopWalk
			}
			found, isBinary, err := searchFile(t.Root.Path, path, args.Pattern, re, maxGrepMatches-len(matches))
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
	if hint, paths := elevationRetry("grep", deniedPaths(denied)); hint != "" {
		payload["message"] = hint
		payload["blocked_paths"] = paths
	}
	return payload, nil
}

func marshalPayload(payload map[string]any) (json.RawMessage, error) {
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
// is treated as binary and not searched. re, when set, matches as a regular
// expression; otherwise the pattern is a literal substring.
func searchFile(root, path, pattern string, re *regexp.Regexp, remaining int) (matches []grepMatch, binary bool, err error) {
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
		at := matchIndex(pattern, re, line)
		if at < 0 {
			continue
		}
		matches = append(matches, grepMatch{Path: rel, Line: lineNo, Text: clipLineAt(line, at, maxGrepLineRunes)})
		if len(matches) >= remaining {
			break
		}
	}
	if errors.Is(scanner.Err(), bufio.ErrTooLong) {
		return matches, false, nil
	}
	return matches, false, scanner.Err()
}

// matchIndex reports the byte offset of the first match, or -1.
func matchIndex(pattern string, re *regexp.Regexp, line string) int {
	if re != nil {
		if found := re.FindStringIndex(line); found != nil {
			return found[0]
		}
		return -1
	}
	return strings.Index(line, pattern)
}

// clipLine keeps about limit runes of line centered on the first match, marking cut
// ends with an ellipsis.
func clipLine(line, pattern string, limit int) string {
	return clipLineAt(line, strings.Index(line, pattern), limit)
}

// clipLineAt is clipLine when the match offset is already known.
func clipLineAt(line string, index, limit int) string {
	if utf8.RuneCountInString(line) <= limit {
		return line
	}
	runes := []rune(line)
	at := 0
	if index > 0 {
		at = utf8.RuneCountInString(line[:index])
	}
	start := at - limit/2
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
