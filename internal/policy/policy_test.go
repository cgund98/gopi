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
	rules, err := BuildWith(Options{Workspace: root, GopiHome: home, Binary: "/usr/local/bin/gopi", RespectGitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	envPattern := patternMatching(t, rules.DenyRead, filepath.Join(root, ".env"))
	if !envPattern.MatchString(filepath.Join(root, ".ENV")) {
		t.Fatal("floor does not cover .ENV")
	}
	patternMatching(t, rules.DenyRead, filepath.Join(root, "debug.log"))
	if _, ok := rules.MatchRead(filepath.Join(root, ".gopi", "config.toml")); !ok {
		t.Fatal(".gopi is not on the floor")
	}
}

func TestWorkspacePlansAreNotProtected(t *testing.T) {
	root := t.TempDir()
	rules, err := Build(root, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	plans := filepath.Join(root, ".gopi", "plans")

	// The workspace's own plans read and write like any other workspace file.
	if id, ok := rules.MatchRead(filepath.Join(plans, "ship.md")); ok {
		t.Fatalf("plan read denied by %q, want no rule", id)
	}
	if id, ok := rules.MatchWrite(filepath.Join(plans, "ship.md")); ok {
		t.Fatalf("plan write denied by %q, want no rule", id)
	}
	if !rules.Opens(filepath.Join(root, ".gopi")) {
		t.Fatal("a walker must be able to descend into .gopi to reach plans")
	}

	// The waiver reaches no further than the .gopi rule it was carved from.
	for _, name := range []string{".env", ".env.local", "server.pem", "id_rsa", "credentials.json"} {
		path := filepath.Join(plans, name)
		if _, ok := rules.MatchRead(path); !ok {
			t.Fatalf("%s inside plans is readable, want the floor to win", name)
		}
		if _, ok := rules.MatchWrite(path); !ok {
			t.Fatalf("%s inside plans is writable, want the floor to win", name)
		}
	}

	// The rest of .gopi, and any nested .gopi, stay protected. A nested file
	// inside plans is ordinary workspace content and stays open.
	if id, ok := rules.MatchRead(filepath.Join(plans, "nested", "notes.md")); ok {
		t.Fatalf("a file under plans denied by %q, want no rule", id)
	}
	for _, rel := range [][]string{
		{".gopi", "config.toml"},
		{".gopi", "secrets.toml"},
		{"scratch", "demo", ".gopi", "plans", "ship.md"},
	} {
		path := filepath.Join(append([]string{root}, rel...)...)
		if _, ok := rules.MatchRead(path); !ok {
			t.Fatalf("%s is readable, want it protected", filepath.Join(rel...))
		}
	}
}

func TestOpenCountsOnlyTheWorkspacePlans(t *testing.T) {
	root := t.TempDir()
	rules, err := Build(root, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, ".gopi", "plans")
	if len(rules.Open) != 1 || rules.Open[0] != want {
		t.Fatalf("open = %v, want [%s]", rules.Open, want)
	}
	if rules.Opens(filepath.Join(root, "scratch", "demo", ".gopi")) {
		t.Fatal("a nested .gopi must not be walked for the workspace plans")
	}
	if rules.Opens(filepath.Join(root, "scratch", "demo", ".gopi", "plans")) {
		t.Fatal("a nested .gopi/plans must stay protected")
	}
}

func TestRepoGitignoreIsNotReadByDefault(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("scratch\n*.log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte("vendor\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := Build(root, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := rules.MatchRead(filepath.Join(root, "scratch", "demo", "cache")); ok {
		t.Fatalf("scratch denied by %q, want no rule from .gitignore", id)
	}
	if id, ok := rules.MatchRead(filepath.Join(root, "debug.log")); ok {
		t.Fatalf("debug.log denied by %q, want no rule from .gitignore", id)
	}
	if id, ok := rules.MatchRead(filepath.Join(root, "vendor", "lib.go")); ok {
		t.Fatalf("vendor denied by %q, want no rule from .git/info/exclude", id)
	}
	// The floor is unaffected.
	if _, ok := rules.MatchRead(filepath.Join(root, ".env")); !ok {
		t.Fatal(".env is not on the floor")
	}
}

func TestRespectGitignoreAddsRepoRules(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte("vendor\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := BuildWith(Options{Workspace: root, GopiHome: t.TempDir(), RespectGitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{filepath.Join("scratch", "demo"), "vendor"} {
		path := filepath.Join(root, rel)
		if _, ok := rules.MatchRead(path); !ok {
			t.Fatalf("%s is readable, want a repo ignore rule", rel)
		}
		if _, ok := rules.MatchWrite(path); !ok {
			t.Fatalf("%s is writable, want a repo ignore rule", rel)
		}
	}
}

func TestGopiIgnoreFileIsAlwaysApplied(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "ignore"), []byte("scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := Build(root, home, "")
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := rules.MatchRead(filepath.Join(root, "scratch", "demo")); !ok || id != "scratch" {
		t.Fatalf("read rule = %q %v, want the global ignore rule", id, ok)
	}
}

func TestSecretFilesAreProtected(t *testing.T) {
	token := filepath.Join(t.TempDir(), "gcal_token.json")
	rules, err := Build(t.TempDir(), t.TempDir(), "", token)
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := rules.MatchRead(token); !ok || id != "secret "+token {
		t.Fatalf("read rule = %q %v", id, ok)
	}
	if _, ok := rules.MatchWrite(token); !ok {
		t.Fatal("secret file is writable")
	}
	if _, ok := rules.MatchRead(token + ".bak"); ok {
		t.Fatal("rule matched a sibling file")
	}
	patternMatching(t, rules.DenyRead, token)
}

func TestUntranslatableIgnoreFailsClosed(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "ignore"), []byte("foo[0-9]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(t.TempDir(), home, ""); err == nil {
		t.Fatal("expected untranslatable pattern in the global ignore file to fail")
	}
}

func TestUntranslatableRepoIgnoreFailsClosedWhenRespected(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("foo[0-9]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(root, t.TempDir(), ""); err != nil {
		t.Fatalf("default Build() should not read .gitignore, got %v", err)
	}
	if _, err := BuildWith(Options{Workspace: root, GopiHome: t.TempDir(), RespectGitignore: true}); err == nil {
		t.Fatal("expected untranslatable .gitignore pattern to fail when respected")
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
