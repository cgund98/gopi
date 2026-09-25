package trust

import (
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(dir, "repo")
	if got := store.Lookup(workspace); got != WorkspaceUnknown {
		t.Fatalf("lookup = %v", got)
	}
	if err := store.Set(workspace, true); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Lookup(workspace); got != WorkspaceTrusted {
		t.Fatalf("lookup = %v", got)
	}
	if err := reopened.Set(workspace, false); err != nil {
		t.Fatal(err)
	}
	if got := reopened.Lookup(workspace); got != WorkspaceUntrusted {
		t.Fatalf("lookup = %v", got)
	}
}
