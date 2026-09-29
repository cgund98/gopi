package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Root is a canonical workspace directory.
type Root struct {
	Path string
}

// Open canonicalizes a workspace path. A leading ~ or ~/ expands to the home
// directory; see expandHome.
func Open(path string) (Root, error) {
	expanded, err := expandHome(path)
	if err != nil {
		return Root{}, err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return Root{}, fmt.Errorf("absolute workspace path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Root{}, fmt.Errorf("resolve workspace path: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return Root{}, fmt.Errorf("stat workspace: %w", err)
	}
	if !info.IsDir() {
		return Root{}, fmt.Errorf("workspace %s is not a directory", resolved)
	}
	return Root{Path: resolved}, nil
}

// Resolve canonicalizes candidate and reports whether it stays inside the workspace.
// A symlink whose target leaves the workspace is outside.
func (r Root) Resolve(candidate string) (string, error) {
	resolved, outside, err := r.Canonical(candidate)
	if err != nil {
		return "", err
	}
	if outside {
		return "", fmt.Errorf("path %s is outside the workspace", candidate)
	}
	return resolved, nil
}

// Canonical resolves candidate without refusing paths that leave the workspace.
// A leading ~ or ~/ expands to the home directory; see expandHome.
func (r Root) Canonical(candidate string) (resolved string, outside bool, err error) {
	if candidate == "" {
		return "", false, fmt.Errorf("path is empty")
	}
	expanded, err := expandHome(candidate)
	if err != nil {
		return "", false, err
	}
	joined := expanded
	if !filepath.IsAbs(joined) {
		joined = filepath.Join(r.Path, expanded)
	}
	resolved, err = resolveExistingPrefix(joined)
	if err != nil {
		return "", false, err
	}
	return resolved, !inside(r.Path, resolved), nil
}

// expandHome replaces a leading ~ or ~/ with the home directory, the way a shell
// does, so a user can type /allowpath ~/code and gopi --workspace ~/code.
//
// Only those two forms expand. ~someone and a ~ in the middle of a path are
// ordinary characters, and a relative ~path is still joined to the workspace, so
// a directory actually named ~ is untouched.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand ~: %w", err)
	}
	if home == "" {
		return "", fmt.Errorf("expand ~: the home directory is unknown")
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}

func resolveExistingPrefix(path string) (string, error) {
	cleaned := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		return resolved, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}

	var missing []string
	current := cleaned
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("resolve %s: no existing parent", path)
		}
		missing = append([]string{filepath.Base(current)}, missing...)
		if _, err := os.Lstat(parent); err == nil {
			resolvedParent, err := filepath.EvalSymlinks(parent)
			if err != nil {
				return "", fmt.Errorf("resolve %s: %w", parent, err)
			}
			return filepath.Join(append([]string{resolvedParent}, missing...)...), nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolve %s: %w", parent, err)
		}
		current = parent
	}
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
