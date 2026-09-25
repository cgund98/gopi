package prompt

import "strings"
import "testing"

func TestBuiltinFollowsPiShape(t *testing.T) {
	body := Builtin()
	for _, want := range []string{"expert coding assistant", "<tools>", "<rules>", "Be concise in your responses", "read_file", "edit_file", "delegate", "web_search", "untrusted observation", "~/.gopi/config.toml", "sandbox.network.allow", "~/.gopi/skills", "skill_dirs", ".gopi/skills"} {
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
