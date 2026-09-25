package gopi

import (
	"testing"
	"time"

	"github.com/cgund98/gopi/internal/session"
	"github.com/cgund98/gopi/internal/workspace"
)

func TestLatestSessionPicksNewestForWorkspace(t *testing.T) {
	home := t.TempDir()
	store, err := session.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	workRoot, err := workspace.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	otherRoot, err := workspace.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	work := workRoot.Path
	other := otherRoot.Path
	for _, file := range []session.File{
		{ID: "old", Workspace: work, Updated: older},
		{ID: "other", Workspace: other, Updated: older.Add(time.Hour)},
		{ID: "new", Workspace: work, Updated: older.Add(2 * time.Hour)},
	} {
		if err := store.Save(file); err != nil {
			t.Fatal(err)
		}
	}
	got, err := latestSession(home, "")
	if err != nil || got.ID != "new" {
		t.Fatalf("latest = %+v err = %v", got, err)
	}
	got, err = latestSession(home, work)
	if err != nil || got.ID != "new" {
		t.Fatalf("workspace latest = %+v err = %v", got, err)
	}
}
