package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBaselineNamesDoNotCollide(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NoteEdit("chat", "cmd/a/main.go", true, false, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := store.NoteEdit("chat", "cmd/b/main.go", true, false, []byte("b")); err != nil {
		t.Fatal(err)
	}
	if err := store.NoteEdit("chat", "cmd/a/main.go", false, false, nil); err != nil {
		t.Fatal(err)
	}
	file, err := store.Load("chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Review) != 2 {
		t.Fatalf("review = %+v", file.Review)
	}
	if file.Review[0].Baseline == "" || file.Review[0].Baseline == file.Review[1].Baseline {
		t.Fatalf("baselines = %q %q", file.Review[0].Baseline, file.Review[1].Baseline)
	}
	if filepath.Base(file.Review[0].Baseline) != file.Review[0].Baseline {
		t.Fatal("baseline name includes a path")
	}
	body, err := store.ReadBaseline("chat", file.Review[0])
	if err != nil || string(body) != "a" {
		t.Fatalf("baseline = %q err = %v", body, err)
	}
	file.Review[0].Approved = []string{"hunk"}
	if err := store.SaveReview("chat", file.Review); err != nil {
		t.Fatal(err)
	}
	if err := store.NoteEdit("chat", "cmd/a/main.go", false, false, nil); err != nil {
		t.Fatal(err)
	}
	file, err = store.Load("chat")
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Review[0].Approved) != 0 || string(mustRead(t, store, "chat", file.Review[0])) != "a" {
		t.Fatalf("later edit = %+v", file.Review[0])
	}
}

func TestDeleteRemovesBaselineDirectory(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(File{ID: "gone", Title: "Gone", Updated: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := store.NoteEdit("gone", "main.go", true, false, []byte("package main\n")); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(store.dir, "gone")
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("baseline directory was kept")
	}
}

func mustRead(t *testing.T, store *Store, id string, entry ReviewEntry) []byte {
	t.Helper()
	body, err := store.ReadBaseline(id, entry)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
