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

// Open canonicalizes a workspace path.
func Open(path string) (Root, error) {
	abs, err := filepath.Abs(path)
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
	if candidate == "" {
		return "", fmt.Errorf("path is empty")
	}
	joined := candidate
	if !filepath.IsAbs(joined) {
		joined = filepath.Join(r.Path, candidate)
	}
	resolved, err := resolveExistingPrefix(joined)
	if err != nil {
		return "", err
	}
	if !inside(r.Path, resolved) {
		return "", fmt.Errorf("path %s is outside the workspace", candidate)
	}
	return resolved, nil
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
