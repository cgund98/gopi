package policy

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FloorGlobs are compiled into gopi and cannot be turned off.
var FloorGlobs = []string{
	"**/.env",
	"**/.env.*",
	"**/.gopi",
	"**/*.pem",
	"**/*.key",
	"**/id_rsa",
	"**/id_ed25519",
	"**/credentials.json",
	"**/secrets.json",
}

// Rules is the deny set for one sandboxed command.
type Rules struct {
	DenyRead  []string
	DenyWrite []string
	// Open lists directories carved out of the deny set. See openPaths.
	Open  []string
	read  []compiledRule
	write []compiledRule
}

type compiledRule struct {
	id string
	re *regexp.Regexp
}

// Options configures BuildWith.
type Options struct {
	// Workspace is the trusted workspace root.
	Workspace string
	// GopiHome is the gopi configuration directory, which holds the ignore file.
	GopiHome string
	// Binary is the running gopi executable, which stays write-protected.
	Binary string
	// RespectGitignore adds the repository's .gitignore and .git/info/exclude to
	// the deny set. It is off by default; see Build.
	RespectGitignore bool
	// Protected are files secrets.toml references, denied for reads and writes.
	Protected []string
}

// Build unions the security floor, ~/.gopi/ignore, and the paths secrets.toml
// references. protected adds files that secrets.toml references, denied for
// reads and writes. A pattern that cannot be translated fails closed.
//
// Repository ignore files are left out. A .gitignore says what git tracks, not
// what the agent may read, so a build output, a cache, or a scratch directory
// listed there would otherwise need an approval on every visit. Use BuildWith
// with RespectGitignore to add those rules back.
func Build(workspace, gopiHome, binaryPath string, protected ...string) (Rules, error) {
	return BuildWith(Options{
		Workspace: workspace,
		GopiHome:  gopiHome,
		Binary:    binaryPath,
		Protected: protected,
	})
}

// BuildWith builds the deny set for one sandboxed command from opts.
// A pattern that cannot be translated fails closed.
func BuildWith(opts Options) (Rules, error) {
	var rules Rules
	for _, path := range opts.Protected {
		if err := rules.add("secret "+path, "^"+foldLiteral(path)+"$", true, true); err != nil {
			return Rules{}, err
		}
	}
	for _, glob := range FloorGlobs {
		pattern, err := globToRegex(glob, "")
		if err != nil {
			return Rules{}, err
		}
		if err := rules.add(glob, pattern, true, true); err != nil {
			return Rules{}, err
		}
	}
	for _, dir := range homeProtectedDirs() {
		pattern := "^" + foldLiteral(dir) + "(/.*)?$"
		if err := rules.add(dir, pattern, true, true); err != nil {
			return Rules{}, err
		}
	}
	for _, path := range writeProtected(opts.Workspace, opts.Binary) {
		pattern := "^" + foldLiteral(path) + "(/.*)?$"
		if err := rules.add(path, pattern, false, true); err != nil {
			return Rules{}, err
		}
	}

	sources := []string{filepath.Join(opts.GopiHome, "ignore")}
	if opts.RespectGitignore {
		sources = append(sources,
			filepath.Join(opts.Workspace, ".gitignore"),
			filepath.Join(opts.Workspace, ".git", "info", "exclude"),
		)
	}
	for _, source := range sources {
		lines, err := readIgnore(source)
		if err != nil {
			return Rules{}, err
		}
		for _, line := range lines {
			pattern, skip, err := translateIgnoreLine(line, opts.Workspace)
			if err != nil {
				return Rules{}, fmt.Errorf("ignore %s: %w", source, err)
			}
			if skip || pattern == "" {
				continue
			}
			if err := rules.add(trimmedIgnoreID(line), pattern, true, true); err != nil {
				return Rules{}, err
			}
		}
	}
	rules.Open = openPaths(opts.Workspace)
	return rules, nil
}

// openPaths are the directories carved out of the deny set.
//
// A .gopi directory is gopi's own state, but the plans inside a workspace are
// agent-authored markdown, not configuration or a secret. Treating them as
// protected put an approval card in front of every read, so the workspace's own
// .gopi/plans is read and written like any other workspace file. A nested .gopi
// belongs to some other project and stays protected.
func openPaths(workspace string) []string {
	if workspace == "" {
		return nil
	}
	return []string{filepath.Join(workspace, ".gopi", "plans")}
}

func trimmedIgnoreID(line string) string {
	id := strings.TrimSpace(line)
	if id == "" {
		return "ignore"
	}
	return id
}

func (r *Rules) add(id, pattern string, read, write bool) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("compile rule %s: %w", id, err)
	}
	compiled := compiledRule{id: id, re: re}
	if read {
		r.DenyRead = append(r.DenyRead, pattern)
		r.read = append(r.read, compiled)
	}
	if write {
		r.DenyWrite = append(r.DenyWrite, pattern)
		r.write = append(r.write, compiled)
	}
	return nil
}

// MatchRead reports the rule id when path is unreadable without approval.
func (r Rules) MatchRead(path string) (string, bool) {
	return r.match(r.read, path)
}

// MatchWrite reports the rule id when path is unwritable without approval.
func (r Rules) MatchWrite(path string) (string, bool) {
	return r.match(r.write, path)
}

// Opens reports whether an open directory is dir itself or sits under it, so a
// walker can descend into an otherwise denied directory to reach it.
func (r Rules) Opens(dir string) bool {
	for _, open := range r.Open {
		if open == dir || strings.HasPrefix(open, dir+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// match returns the first rule that denies path, skipping rules waived by an
// open directory. Every match is considered, not just the first, so a waiver
// cannot shadow a different protection: opening .gopi/plans lifts the .gopi
// rule, but a .env or a *.pem inside those plans is still caught by its own.
func (r Rules) match(rules []compiledRule, path string) (string, bool) {
	for _, rule := range rules {
		if !rule.re.MatchString(path) {
			continue
		}
		if r.waived(rule, path) {
			continue
		}
		return rule.id, true
	}
	return "", false
}

// waived reports whether an open directory lifts this rule for path. The path
// must sit inside the open directory, and that directory must be caught by the
// same rule, so the waiver reaches no further than the rule it was carved from.
func (r Rules) waived(rule compiledRule, path string) bool {
	for _, open := range r.Open {
		if path != open && !strings.HasPrefix(path, open+string(os.PathSeparator)) {
			continue
		}
		if rule.re.MatchString(open) {
			return true
		}
	}
	return false
}

func homeProtectedDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	names := []string{".ssh", ".aws", ".kube", ".gnupg", ".gopi", filepath.Join("Library", "Keychains")}
	dirs := make([]string, 0, len(names))
	for _, name := range names {
		dirs = append(dirs, filepath.Join(home, name))
	}
	return dirs
}

func writeProtected(workspace, binaryPath string) []string {
	paths := []string{
		filepath.Join(workspace, ".git", "config"),
		filepath.Join(workspace, ".git", "hooks"),
		filepath.Join(workspace, ".git", "info", "attributes"),
		filepath.Join(workspace, ".gopi"),
		filepath.Join(workspace, ".gitignore"),
		filepath.Join(workspace, ".git", "info", "exclude"),
	}
	if binaryPath != "" {
		paths = append(paths, binaryPath)
	}
	return paths
}

func readIgnore(path string) (lines []string, err error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err = scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func translateIgnoreLine(line, workspace string) (pattern string, skip bool, err error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", true, nil
	}
	negated := strings.HasPrefix(trimmed, "!")
	body := trimmed
	if negated {
		body = strings.TrimSpace(strings.TrimPrefix(trimmed, "!"))
	}
	if strings.ContainsAny(body, "[]\\") {
		return "", false, fmt.Errorf("pattern %q cannot be translated", line)
	}
	if negated {
		if coversFloor(body) {
			return "", true, nil
		}
		return "", false, fmt.Errorf("pattern %q cannot be translated", line)
	}
	pattern, err = globToRegex(body, workspace)
	return pattern, false, err
}

func coversFloor(pattern string) bool {
	base := strings.Trim(pattern, "/")
	for _, floor := range FloorGlobs {
		floorBase := strings.TrimPrefix(floor, "**/")
		if base == floorBase || base == floor || strings.HasSuffix(floor, "/"+base) {
			return true
		}
	}
	return strings.HasPrefix(base, ".env")
}

func globToRegex(glob, root string) (string, error) {
	anchored := strings.HasPrefix(glob, "/")
	glob = strings.TrimPrefix(glob, "/")
	glob = strings.TrimSuffix(glob, "/")
	if strings.HasPrefix(glob, "**/") {
		glob = strings.TrimPrefix(glob, "**/")
		anchored = false
	}
	if glob == "" {
		return "", fmt.Errorf("empty ignore pattern")
	}
	anywhere := !anchored && !strings.Contains(glob, "/")
	var b strings.Builder
	b.WriteString("^")
	if root != "" {
		b.WriteString(regexp.QuoteMeta(root))
		b.WriteString("/")
		if anywhere {
			b.WriteString("(.*/)?")
		}
	} else {
		b.WriteString("(.*/)?")
	}
	for i := 0; i < len(glob); i++ {
		switch glob[i] {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				b.WriteString(".*")
				i++
				if i+1 < len(glob) && glob[i+1] == '/' {
					i++
				}
				continue
			}
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(foldLiteral(string(glob[i])))
		}
	}
	b.WriteString("(/.*)?$")
	return b.String(), nil
}

func foldLiteral(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			fmt.Fprintf(&b, "[%c%c]", r, r-32)
		case r >= 'A' && r <= 'Z':
			fmt.Fprintf(&b, "[%c%c]", r+32, r)
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return b.String()
}
