package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplorePromptUsesTheOverrideFile(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "explore.md"), []byte("Custom explorer.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ExplorePrompt(home)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != "Custom explorer." {
		t.Fatalf("prompt = %q", got)
	}
}

func TestExplorePromptFallsBackToBuiltin(t *testing.T) {
	home := t.TempDir()
	got, err := ExplorePrompt(home)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "file search specialist") {
		t.Fatalf("prompt = %q", got)
	}
	// A blank override is the same as none, so a half-written file cannot leave
	// the child with no instructions at all.
	if err := os.WriteFile(filepath.Join(home, "explore.md"), []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = ExplorePrompt(home)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "file search specialist") {
		t.Fatalf("blank override = %q", got)
	}
}

func TestBuiltinExploreNamesItsLimits(t *testing.T) {
	text := BuiltinExplore()
	for _, want := range []string{"find", "grep", "read_file", "access_denied", "thoroughness"} {
		if !strings.Contains(text, want) {
			t.Fatalf("explore prompt missing %q", want)
		}
	}
}

func TestBuiltinMentionsExploreSubagent(t *testing.T) {
	text := Builtin()
	for _, want := range []string{"- explore:", "read-only subagent", "untrusted observation"} {
		if !strings.Contains(text, want) {
			t.Fatalf("built-in prompt missing %q", want)
		}
	}
	if !strings.Contains(text, "call explore once with a complete task") {
		t.Fatal("built-in prompt should tell the agent to prefer explore for exploration")
	}
	if !strings.Contains(text, "ripgrep") {
		t.Fatal("built-in prompt should say search is ripgrep-backed")
	}
}
