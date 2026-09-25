package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/workspace"
)

const maxDeniedPaths = 8

func readGrantDecision(root workspace.Root, paths []string) (gogent.ApprovalDecision, error) {
	if len(paths) == 0 {
		return gogent.ApprovalDecision{}, nil
	}
	resolved, err := canonicalPaths(root, paths)
	if err != nil {
		return gogent.ApprovalDecision{}, nil
	}
	return gogent.ApprovalDecision{Required: true, Reason: grantReason(resolved, nil)}, nil
}

func canonicalPaths(root workspace.Root, paths []string) ([]string, error) {
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		canon, _, err := root.Canonical(path)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, canon)
	}
	return resolved, nil
}

func coversGrant(path string, grants []string) bool {
	for _, grant := range grants {
		if path == grant || strings.HasPrefix(path, grant+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// grantCoversRead reports whether an approved path lets this file be read.
// A directory grant opens files that share its rule. A floor file under that
// directory stays denied until that file is granted on its own.
func grantCoversRead(path string, rules policy.Rules, grants []string) bool {
	rule, protected := rules.MatchRead(path)
	if !protected {
		return true
	}
	for _, grant := range grants {
		if path != grant && !strings.HasPrefix(path, grant+string(os.PathSeparator)) {
			continue
		}
		grantRule, grantProtected := rules.MatchRead(grant)
		if grantProtected && grantRule == rule {
			return true
		}
	}
	return false
}

// grantOpens reports whether this directory can be walked. A grant of the
// directory itself opens it, and so does a grant of a file inside it.
func grantOpens(path string, grants []string) bool {
	if coversGrant(path, grants) {
		return true
	}
	prefix := path + string(os.PathSeparator)
	for _, grant := range grants {
		if strings.HasPrefix(grant, prefix) {
			return true
		}
	}
	return false
}

// searchRoots is the walk start for grep and find. An empty path walks the
// workspace, plus any path outside it approved on this call. A path outside
// the workspace is allowed when this call or a session grant covers it.
func searchRoots(root workspace.Root, path string, grants, session []string) ([]string, error) {
	var roots []string
	if path == "" {
		roots = []string{root.Path}
	} else {
		resolved, outside, err := root.Canonical(path)
		if err != nil {
			return nil, err
		}
		if outside && !coversGrant(resolved, grants) && !coversGrant(resolved, session) {
			return nil, fmt.Errorf("path %s is outside the workspace; call grant_read or pass it in read_paths", path)
		}
		roots = []string{resolved}
	}
	for _, grant := range grants {
		if coversGrant(grant, roots) {
			continue
		}
		_, outside, err := root.Canonical(grant)
		if err != nil {
			return nil, err
		}
		if outside {
			roots = append(roots, grant)
		}
	}
	return roots, nil
}

func appendDenied(denied *[]map[string]string, root, path, rule string) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	*denied = append(*denied, map[string]string{
		"error":   "access_denied",
		"path":    filepath.ToSlash(rel),
		"message": "protected path " + rule,
	})
}

func trimDenied(denied []map[string]string) []map[string]string {
	if len(denied) <= maxDeniedPaths {
		return denied
	}
	extra := len(denied) - maxDeniedPaths
	trimmed := append([]map[string]string{}, denied[:maxDeniedPaths]...)
	return append(trimmed, map[string]string{
		"error":   "access_denied",
		"message": fmt.Sprintf("%d more protected paths omitted", extra),
	})
}

func elevationRetry(tool string, blocked []string) (string, []string) {
	if len(blocked) == 0 {
		return "", nil
	}
	return fmt.Sprintf("The sandbox blocked file access. Call %s again with the same arguments and put each blocked path in read_paths. That call asks the user for approval and does not run until they approve.", tool), blocked
}
