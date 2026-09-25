package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectChainIsRootFirstAndOverrideReplaces(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "pkg")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "AGENTS.md"), "root rules")
	writeFile(t, filepath.Join(nested, "AGENTS.md"), "ignored")
	writeFile(t, filepath.Join(nested, "AGENTS.override.md"), "nested rules")

	got, err := Assemble(Options{Base: "base", Workspace: nested, Trusted: true, MaxBytes: 1000})
	if err != nil {
		t.Fatal(err)
	}
	rootAt := strings.Index(got, "root rules")
	nestedAt := strings.Index(got, "nested rules")
	if rootAt < 0 || nestedAt < 0 || rootAt > nestedAt || strings.Contains(got, "ignored") {
		t.Fatalf("prompt = %q", got)
	}
}

func TestUntrustedOmitsProjectInstructions(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	writeFile(t, filepath.Join(home, "AGENTS.md"), "user rules")
	writeFile(t, filepath.Join(workspace, "AGENTS.md"), "project rules")
	writeSkill(t, filepath.Join(home, "skills", "lint"), "lint", "user skill")
	writeSkill(t, filepath.Join(workspace, ".gopi", "skills", "ship"), "ship", "project skill")

	got, err := Assemble(Options{Base: "base", HomeDir: home, Workspace: workspace, Trusted: false, MaxBytes: 8000})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "user rules") || !strings.Contains(got, "user skill") {
		t.Fatalf("missing user instructions: %q", got)
	}
	if strings.Contains(got, "project rules") || strings.Contains(got, "project skill") {
		t.Fatalf("untrusted prompt included project files: %q", got)
	}
}

func TestChainKeepsTailWhenOverBudget(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "AGENTS.md"), "HEAD "+strings.Repeat("x", 200)+" TAIL")
	got, err := Assemble(Options{Base: "base", Workspace: workspace, Trusted: true, MaxBytes: 80})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, truncationMarker) || !strings.Contains(got, "TAIL") || strings.Contains(got, "HEAD") {
		t.Fatalf("prompt = %q", got)
	}
}

func TestSkillWithoutNameIsOmitted(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "skills", "blank", "SKILL.md"), "---\ndescription: missing name\n---\nbody\n")
	got, err := Assemble(Options{Base: "base", HomeDir: home, MaxBytes: 4000})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "missing name") || strings.Contains(got, "<skills>") {
		t.Fatalf("prompt = %q", got)
	}
}

func TestSkillDirAndProjectReplacement(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	registry := t.TempDir()
	writeSkill(t, filepath.Join(registry, "lint"), "lint", "from registry")
	writeSkill(t, filepath.Join(workspace, ".gopi", "skills", "lint"), "lint", "from project")

	got, err := Assemble(Options{
		Base:      "base",
		HomeDir:   home,
		Workspace: workspace,
		Trusted:   true,
		SkillDirs: []string{registry},
		MaxBytes:  8000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "from project") || strings.Contains(got, "from registry") {
		t.Fatalf("prompt = %q", got)
	}
	if !strings.Contains(got, filepath.Join(workspace, ".gopi", "skills", "lint", "SKILL.md")) {
		t.Fatalf("prompt = %q", got)
	}
}

func TestSystemPromptIsAppended(t *testing.T) {
	got, err := Assemble(Options{Base: "builtin rules", UserPrompt: "custom voice", Workspace: "/tmp/repo", MaxBytes: 4000})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(got, "builtin rules") < 0 || strings.Index(got, "builtin rules") > strings.Index(got, "custom voice") {
		t.Fatalf("prompt = %q", got)
	}
}

func writeSkill(t *testing.T, dir, name, description string) {
	t.Helper()
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\nbody\n"
	writeFile(t, filepath.Join(dir, "SKILL.md"), body)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
