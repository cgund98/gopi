package prompt

import (
	"strings"
	"testing"
)

func TestBuiltinFollowsPiShape(t *testing.T) {
	body := Builtin()
	for _, want := range []string{"expert coding assistant", "<tools>", "<rules>", "what you are about to do", "batch-add the steps", "Be concise in your responses", "read_file", "edit_file", "delegate", "web_search", "web_fetch", "tasks", "untrusted observation", "~/.gopi/config.toml", "sandbox.network.allow", "~/.gopi/skills", "skill_dirs", ".gopi/skills", "login keychain", "already has HOME", "Keep shell commands simple", "more than two or three", "dodge the sandbox"} {
		if !strings.Contains(body, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

func TestWithWorkspace(t *testing.T) {
	got := WithWorkspace("hello", "/tmp/repo")
	if !strings.Contains(got, "<cwd>\n/tmp/repo\n</cwd>") {
		t.Fatalf("prompt = %q", got)
	}
}
