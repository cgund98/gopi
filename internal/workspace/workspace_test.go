package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// homeTemp points HOME at a fresh temp dir and returns its canonical form, since
// the platform temp dir is itself a symlink on macOS.
func homeTemp(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestCanonicalExpandsHome(t *testing.T) {
	home := homeTemp(t)
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ in, want string }{
		{"~", home},
		{"~/", home},
		{"~/code", filepath.Join(home, "code")},
		{"~/code/app", filepath.Join(home, "code", "app")},
		{"~/code/../notes.md", filepath.Join(home, "notes.md")},
	} {
		got, outside, err := root.Canonical(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%s = %s, want %s", tc.in, got, tc.want)
		}
		// Opening a path under the home directory always leaves the workspace and
		// so still needs a grant.
		if !outside {
			t.Fatalf("%s must resolve outside the workspace", tc.in)
		}
	}
}

func TestCanonicalLeavesOtherTildePathsAlone(t *testing.T) {
	homeTemp(t)
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// ~someone, a relative ~path, and a mid-path ~ are ordinary characters, so
	// they stay inside the workspace rather than reaching the home directory.
	for _, in := range []string{"~other/code", "~backup", "a/~/b", "src/~x.go"} {
		got, outside, err := root.Canonical(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if outside {
			t.Fatalf("%s resolved outside the workspace: %s", in, got)
		}
		if !strings.HasPrefix(got, root.Path+string(filepath.Separator)) {
			t.Fatalf("%s = %s, want it under %s", in, got, root.Path)
		}
	}
}

func TestCanonicalErrorsWhenHomeIsUnknown(t *testing.T) {
	t.Setenv("HOME", "")
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := root.Canonical("~/code"); err == nil {
		t.Fatal("an unknown home directory must be an error, not a relative path")
	}
	// A path that does not use ~ is unaffected.
	if _, _, err := root.Canonical("code"); err != nil {
		t.Fatalf("plain relative path: %v", err)
	}
}

func TestOpenExpandsHome(t *testing.T) {
	home := homeTemp(t)
	dir := filepath.Join(home, "code", "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	root, err := Open("~/code/app")
	if err != nil {
		t.Fatal(err)
	}
	if root.Path != want {
		t.Fatalf("root = %s, want %s", root.Path, want)
	}
	// A trailing slash and a bare ~ resolve too.
	if bare, err := Open("~"); err != nil || bare.Path != home {
		t.Fatalf("Open(~) = %v err = %v", bare, err)
	}
}

func TestResolveRefusesExpandedHomeSibling(t *testing.T) {
	home := homeTemp(t)
	if err := os.MkdirAll(filepath.Join(home, "elsewhere"), 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Resolve still refuses an outside path after expansion, so the ~ shortcut
	// cannot be used to slip past the workspace check.
	if _, err := root.Resolve("~/elsewhere"); err == nil {
		t.Fatal("Resolve must refuse a path outside the workspace")
	}
}
