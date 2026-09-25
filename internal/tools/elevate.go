package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cgund98/gogent"

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
