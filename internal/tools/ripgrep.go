package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/sandbox"
	"github.com/cgund98/gopi/internal/workspace"
)

const (
	// EngineAuto uses ripgrep when it is available and falls back to the walker.
	EngineAuto = "auto"
	// EngineRipgrep requires ripgrep; a missing binary is an error.
	EngineRipgrep = "ripgrep"
	// EngineBuiltin always uses the Go walker.
	EngineBuiltin = "builtin"

	// BackendRipgrep and BackendBuiltin are the values reported in a grep result.
	BackendRipgrep = "ripgrep"
	BackendBuiltin = "builtin"

	// ripgrepOutputLimit caps the raw JSON ripgrep may emit. It has to be well
	// above sandbox.DefaultOutputLimit so a record is not cut in half.
	ripgrepOutputLimit = 4 << 20
)

// ResolveEngine validates a configured engine name and reports the backend that
// will actually run. A path of "" means ripgrep is unavailable.
func ResolveEngine(engine, rgPath string) (string, error) {
	switch engine {
	case EngineAuto, "":
		if rgPath == "" {
			return BackendBuiltin, nil
		}
		return BackendRipgrep, nil
	case EngineRipgrep:
		if rgPath == "" {
			return "", fmt.Errorf("search.engine is %q but no ripgrep binary was found; set search.ripgrep_path or use \"auto\"", EngineRipgrep)
		}
		return BackendRipgrep, nil
	case EngineBuiltin:
		return BackendBuiltin, nil
	default:
		return "", fmt.Errorf("search.engine %q is not one of %q, %q, %q", engine, EngineAuto, EngineRipgrep, EngineBuiltin)
	}
}

// LookupRG resolves the ripgrep binary once at startup. A blank configured path
// looks for rg on PATH. A configured path that does not resolve is an error only
// when the caller asked for ripgrep by name; otherwise it is reported as absent
// so the caller can fall back.
func LookupRG(configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		path, err := exec.LookPath("rg")
		if err != nil {
			return "", nil
		}
		return path, nil
	}
	if filepath.IsAbs(configured) {
		info, err := os.Stat(configured)
		if err != nil {
			return "", fmt.Errorf("search.ripgrep_path %q: %w", configured, err)
		}
		if info.IsDir() || info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("search.ripgrep_path %q is not executable", configured)
		}
		return configured, nil
	}
	path, err := exec.LookPath(configured)
	if err != nil {
		return "", fmt.Errorf("search.ripgrep_path %q: %w", configured, err)
	}
	return path, nil
}

// SearchEngine runs ripgrep under the same Seatbelt profile a shell command gets,
// with the workspace as its only read root.
type SearchEngine struct {
	// RGPath is the resolved ripgrep binary. Empty means ripgrep is unavailable.
	RGPath string
	// HomeDir is ~/.gopi, which holds the ignore file the policy reads.
	HomeDir string
	// SecretFiles are the files secrets.toml references; the sandbox denies them.
	SecretFiles []string
	// RespectGitignore adds the repository's ignore files to the deny set.
	RespectGitignore bool
}

type searchRequest struct {
	Root workspace.Root
	// Starts are the absolute directories ripgrep should walk.
	Starts []string
	// Pattern is the expression to match.
	Pattern string
	// Regex selects a regular expression instead of a literal substring.
	Regex bool
	// Grants are this chat's session read grants, used to re-check a match.
	Grants []string
	// Rules is the path policy, reapplied to every match as a second gate.
	Rules policy.Rules
}

type searchResult struct {
	Matches   []grepMatch
	Denied    []map[string]string
	Binary    int
	Truncated bool
}

// search runs ripgrep and returns its matches, capped the same way the walker is.
func (e *SearchEngine) search(ctx context.Context, req searchRequest) (searchResult, error) {
	if e.RGPath == "" {
		return searchResult{}, fmt.Errorf("ripgrep is not available")
	}
	if strings.TrimSpace(req.Pattern) == "" {
		return searchResult{}, fmt.Errorf("pattern is required")
	}
	if len(req.Starts) == 0 {
		return searchResult{}, fmt.Errorf("no search root")
	}
	rules := req.Rules
	if len(rules.DenyRead) == 0 {
		built, err := policy.BuildWith(policy.Options{
			Workspace:        req.Root.Path,
			GopiHome:         e.HomeDir,
			Binary:           executablePath(),
			RespectGitignore: e.RespectGitignore,
			Protected:        e.SecretFiles,
		})
		if err != nil {
			return searchResult{}, fmt.Errorf("build path policy: %w", err)
		}
		rules = built
	}
	tmp, err := sandbox.SessionTemp()
	if err != nil {
		return searchResult{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	profile := sandbox.Profile{
		Name:         sandbox.ProfileSandbox,
		ReadRoots:    []string{req.Root.Path},
		WriteRoots:   []string{tmp},
		Home:         homeDir(),
		DenyRead:     rules.DenyRead,
		DenyWrite:    rules.DenyWrite,
		OpenReads:    rules.Open,
		OpenWrites:   rules.Open,
		ExtraReads:   e.extraReads(req),
		SessionReads: req.Grants,
		Network:      sandbox.NetworkDeny,
		Env:          sandbox.ScrubbedEnv(tmp),
		Timeout:      sandbox.DefaultTimeout,
		OutputLimit:  ripgrepOutputLimit,
		WorkDir:      req.Root.Path,
		Argv:         e.argv(req),
	}
	result, err := launchCommand(ctx, profile)
	if err != nil {
		return searchResult{}, err
	}
	return e.parse(req, rules, result)
}

// extraReads opens the ripgrep binary and any search root outside the workspace.
// The binary usually lives under a denied prefix such as /Users, and a wide extra
// read is written after that deny, so this is what makes it reachable.
func (e *SearchEngine) extraReads(req searchRequest) []string {
	paths := []string{filepath.Dir(e.RGPath)}
	for _, start := range req.Starts {
		if start == req.Root.Path || strings.HasPrefix(start, req.Root.Path+string(os.PathSeparator)) {
			continue
		}
		paths = append(paths, start)
	}
	return paths
}

func (e *SearchEngine) argv(req searchRequest) []string {
	argv := []string{
		e.RGPath,
		"--json",
		"--color=never",
		"--no-heading",
		"--line-number",
		// Match the walker's discovery: it skips .git and nothing else, so a
		// .gitignore must not hide a file from a search the model asked for.
		"--no-ignore",
		"--hidden",
		"--max-count", strconv.Itoa(maxGrepMatches),
	}
	if !req.Regex {
		argv = append(argv, "-F")
	}
	argv = append(argv, "-e", req.Pattern)
	return append(argv, req.Starts...)
}

// rgEvent is the subset of ripgrep's --json stream this tool reads.
type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
		} `json:"submatches"`
		Stats struct {
			BinaryFilesSearchedAndIgnored int `json:"binaryFilesSearchedAndIgnored"`
		} `json:"stats"`
	} `json:"data"`
}

func (e *SearchEngine) parse(req searchRequest, rules policy.Rules, result sandbox.Result) (searchResult, error) {
	req.Rules = rules
	var out searchResult
	// A sandbox denial is an expected outcome, not a failure: ripgrep exits 2
	// when it cannot read a file, and the file it could not read is exactly what
	// a pending read grant should be able to lift.
	out.Denied = e.deniedFromStderr(req, result.Stderr)
	// Exit 1 is "no matches". Exit 2 with no denials to explain it is a real
	// error, and its stderr has to survive.
	if result.ExitCode == 2 && len(out.Denied) == 0 {
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = "ripgrep exited 2"
		}
		return searchResult{}, fmt.Errorf("ripgrep: %s", message)
	}

	textBytes := 0
	scanner := bufio.NewScanner(strings.NewReader(result.Stdout))
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var event rgEvent
		// A trailing partial record, or any line ripgrep did not emit as JSON,
		// is skipped rather than failing the whole search.
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		switch event.Type {
		case "summary":
			out.Binary = event.Data.Stats.BinaryFilesSearchedAndIgnored
		case "match":
			if len(out.Matches) >= maxGrepMatches || textBytes >= maxGrepBytes {
				out.Truncated = true
				continue
			}
			match, rule, ok := e.match(req, event)
			if !ok {
				if rule != "" {
					out.Denied = append(out.Denied, denyLine(relativePath(req.Root.Path, matchAbsolute(req, event)), rule))
				}
				continue
			}
			if textBytes+len(match.Text) > maxGrepBytes {
				out.Truncated = true
				continue
			}
			textBytes += len(match.Text)
			out.Matches = append(out.Matches, match)
		}
	}
	out.Truncated = out.Truncated || result.Truncated || len(out.Matches) >= maxGrepMatches
	return out, nil
}

// matchAbsolute is the on-disk path of the file a match belongs to.
func matchAbsolute(req searchRequest, event rgEvent) string {
	if event.Data.Path.Text == "" {
		return ""
	}
	if filepath.IsAbs(event.Data.Path.Text) {
		return event.Data.Path.Text
	}
	return filepath.Join(req.Root.Path, event.Data.Path.Text)
}

// match converts one ripgrep match into a grepMatch, dropping a file the policy
// protects. rule is non-empty when the match was denied, so the caller can
// report it. The sandbox should already have stopped such a file; this is the
// second gate, and it is the one that produces a usable denial line.
func (e *SearchEngine) match(req searchRequest, event rgEvent) (match grepMatch, rule string, ok bool) {
	absolute := matchAbsolute(req, event)
	if absolute == "" {
		return grepMatch{}, "", false
	}
	if id, protected := req.Rules.MatchRead(absolute); protected {
		if !grantCoversRead(absolute, req.Rules, req.Grants) {
			return grepMatch{}, id, false
		}
	}
	text := strings.TrimRight(event.Data.Lines.Text, "\n")
	offset := -1
	if len(event.Data.Submatches) > 0 {
		offset = event.Data.Submatches[0].Start
	}
	return grepMatch{
		Path: relativePath(req.Root.Path, absolute),
		Line: event.Data.LineNumber,
		Text: clipLineAt(text, offset, maxGrepLineRunes),
	}, "", true
}

// rgDenialLine is fileDenialLine with one difference: ripgrep may append a
// parenthetical errno after the phrase, as in "Permission denied (os error 13)".
var rgDenialLine = regexp.MustCompile(`(?i)(?:^|:\s)([^\s:]+):\s*(?:operation not permitted|permission denied)\b`)

// deniedFromStderr turns a "permission denied" line from ripgrep into a denial a
// pending read grant can lift.
func (e *SearchEngine) deniedFromStderr(req searchRequest, stderr string) []map[string]string {
	var denied []map[string]string
	seen := map[string]bool{}
	for _, line := range strings.Split(stderr, "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "operation not permitted") && !strings.Contains(lower, "permission denied") {
			continue
		}
		found := rgDenialLine.FindStringSubmatch(strings.TrimSpace(line))
		if len(found) < 2 || seen[found[1]] {
			continue
		}
		seen[found[1]] = true
		absolute := found[1]
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(req.Root.Path, absolute)
		}
		denied = append(denied, map[string]string{
			"error":   "access_denied",
			"path":    relativePath(req.Root.Path, absolute),
			"message": "the sandbox denied this path",
		})
	}
	return denied
}

// denyLine is the entry a dropped match contributes to the denied list.
func denyLine(rel, rule string) map[string]string {
	return map[string]string{
		"error":   "access_denied",
		"path":    rel,
		"message": "protected path " + rule,
	}
}

func relativePath(root, absolute string) string {
	rel, err := filepath.Rel(root, absolute)
	if err != nil {
		return filepath.ToSlash(absolute)
	}
	return filepath.ToSlash(rel)
}
