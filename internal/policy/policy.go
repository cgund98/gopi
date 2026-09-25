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
}

// Build unions the security floor, ~/.gopi/ignore, and repo gitignore files.
// A pattern that cannot be translated fails closed.
func Build(workspace, gopiHome, binaryPath string) (Rules, error) {
	var rules Rules
	for _, glob := range FloorGlobs {
		pattern, err := globToRegex(glob, "")
		if err != nil {
			return Rules{}, err
		}
		rules.DenyRead = append(rules.DenyRead, pattern)
		rules.DenyWrite = append(rules.DenyWrite, pattern)
	}
	for _, dir := range homeProtectedDirs() {
		pattern := "^" + foldLiteral(dir) + "(/.*)?$"
		rules.DenyRead = append(rules.DenyRead, pattern)
		rules.DenyWrite = append(rules.DenyWrite, pattern)
	}
	for _, path := range writeProtected(workspace, binaryPath) {
		pattern := "^" + foldLiteral(path) + "(/.*)?$"
		rules.DenyWrite = append(rules.DenyWrite, pattern)
	}

	sources := []string{
		filepath.Join(gopiHome, "ignore"),
		filepath.Join(workspace, ".gitignore"),
		filepath.Join(workspace, ".git", "info", "exclude"),
	}
	for _, source := range sources {
		lines, err := readIgnore(source)
		if err != nil {
			return Rules{}, err
		}
		for _, line := range lines {
			pattern, skip, err := translateIgnoreLine(line, workspace)
			if err != nil {
				return Rules{}, fmt.Errorf("ignore %s: %w", source, err)
			}
			if skip || pattern == "" {
				continue
			}
			rules.DenyRead = append(rules.DenyRead, pattern)
			rules.DenyWrite = append(rules.DenyWrite, pattern)
		}
	}
	return rules, nil
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
