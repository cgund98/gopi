package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cgund98/gogent"
)

func TestSaveSkipsUntilCalledAndPrunesOldest(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(store.dir); err != nil || len(entries) != 0 {
		t.Fatalf("dir entries = %v err = %v", entries, err)
	}

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := 0; i < maxSessions+1; i++ {
		id := string(rune('a'+i/26)) + string(rune('a'+i%26))
		err := store.Save(File{
			ID:        id,
			Title:     id,
			Workspace: "/work",
			Mode:      "agent",
			Updated:   now.Add(time.Duration(i) * time.Minute),
			Messages:  []gogent.Message{gogent.NewUserMessage("hello")},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	files, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != maxSessions {
		t.Fatalf("kept = %d", len(files))
	}
	if files[0].Updated.Before(files[len(files)-1].Updated) {
		t.Fatal("list is not newest first")
	}
	oldestID := string(rune('a'+0)) + string(rune('a'+0))
	if _, err := os.Stat(filepath.Join(store.dir, oldestID+".json")); !os.IsNotExist(err) {
		t.Fatal("oldest session was kept")
	}
}

func TestSaveUpdatesSameFile(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := File{ID: "chat", Title: "first", Workspace: "/work", Mode: "ask", Updated: time.Now().UTC()}
	if err := store.Save(file); err != nil {
		t.Fatal(err)
	}
	file.Title = "second"
	file.Messages = []gogent.Message{gogent.NewUserMessage("again")}
	if err := store.Save(file); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load("chat")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Title != "second" || len(loaded.Messages) != 1 {
		t.Fatalf("loaded = %+v", loaded)
	}
	info, err := os.Stat(store.path("chat"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestDeleteRemovesSessionFile(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(File{ID: "gone", Title: "Gone", Workspace: "/work", Updated: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("gone"); err == nil {
		t.Fatal("deleted session still loads")
	}
}
