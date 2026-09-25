package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cgund98/gogent"
)

const maxSessions = 50

// File is one saved chat under ~/.gopi/sessions.
type File struct {
	ID        string           `json:"id"`
	Title     string           `json:"title"`
	Workspace string           `json:"workspace"`
	Mode      string           `json:"mode"`
	Updated   time.Time        `json:"updated"`
	Messages  []gogent.Message `json:"messages"`
}

// Store reads and writes session files.
type Store struct {
	dir string
}

// Open uses <home>/sessions.
func Open(homeDir string) (*Store, error) {
	dir := filepath.Join(homeDir, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create sessions directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Save writes the chat and drops the oldest files past the cap.
// The open chat is never deleted.
func (s *Store) Save(file File) error {
	if file.ID == "" {
		return fmt.Errorf("session id is required")
	}
	if file.Updated.IsZero() {
		file.Updated = time.Now().UTC()
	}
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	body = append(body, '\n')
	path := s.path(file.ID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write session: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename session: %w", err)
	}
	return s.prune(file.ID)
}

// Load reads one session file.
func (s *Store) Load(id string) (File, error) {
	body, err := os.ReadFile(s.path(id))
	if err != nil {
		return File{}, fmt.Errorf("read session: %w", err)
	}
	var file File
	if err := json.Unmarshal(body, &file); err != nil {
		return File{}, fmt.Errorf("decode session: %w", err)
	}
	return file, nil
}

// List returns sessions newest first. A missing or unreadable file is skipped.
func (s *Store) List() ([]File, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	files := make([]File, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-len(".json")]
		file, err := s.Load(id)
		if err != nil {
			continue
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Updated.After(files[j].Updated)
	})
	return files, nil
}

func (s *Store) prune(keepID string) error {
	files, err := s.List()
	if err != nil {
		return err
	}
	if len(files) <= maxSessions {
		return nil
	}
	excess := len(files) - maxSessions
	for i := len(files) - 1; i >= 0 && excess > 0; i-- {
		if files[i].ID == keepID {
			continue
		}
		if err := os.Remove(s.path(files[i].ID)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove session: %w", err)
		}
		excess--
	}
	return nil
}

// Delete removes one session file. A missing file is not an error.
func (s *Store) Delete(id string) error {
	if id == "" {
		return fmt.Errorf("session id is required")
	}
	if err := os.Remove(s.path(id)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove session: %w", err)
	}
	return nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}
