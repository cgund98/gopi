package policy

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestFloorSurvivesGitignoreNegation(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("!.env\n*.log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := Build(root, home, "/usr/local/bin/gopi")
	if err != nil {
		t.Fatal(err)
	}
	envPattern := patternMatching(t, rules.DenyRead, filepath.Join(root, ".env"))
	if !envPattern.MatchString(filepath.Join(root, ".ENV")) {
		t.Fatal("floor does not cover .ENV")
	}
	patternMatching(t, rules.DenyRead, filepath.Join(root, "debug.log"))
}

func TestUntranslatableIgnoreFailsClosed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("foo[0-9]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(root, t.TempDir(), ""); err == nil {
		t.Fatal("expected untranslatable pattern to fail")
	}
}

func patternMatching(t *testing.T, patterns []string, path string) *regexp.Regexp {
	t.Helper()
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatal(err)
		}
		if re.MatchString(path) {
			return re
		}
	}
	t.Fatalf("no pattern matches %s in %#v", path, patterns)
	return nil
}
