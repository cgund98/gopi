package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/cgund98/gogent"
)

const maxSessions = 50

// File is one saved chat under ~/.gopi/sessions.
type File struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Workspace string            `json:"workspace"`
	Mode      string            `json:"mode"`
	Updated   time.Time         `json:"updated"`
	Messages  []gogent.Message  `json:"messages"`
	Review    []ReviewEntry     `json:"review,omitempty"`
	Models    map[string]string `json:"models,omitempty"`
}

// ReviewEntry is one path this session changed with edit_file.
// Baseline is a unique file name under the session directory, not the workspace path.
type ReviewEntry struct {
	Path     string   `json:"path"`
	Baseline string   `json:"baseline,omitempty"`
	Created  bool     `json:"created,omitempty"`
	Approved []string `json:"approved,omitempty"`
	Hunks    []string `json:"hunks,omitempty"`
}

// Store reads and writes session files.
type Store struct {
	mu  sync.Mutex
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(file)
}

func (s *Store) saveLocked(file File) error {
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
	return s.pruneLocked(file.ID)
}

// Load reads one session file.
func (s *Store) Load(id string) (File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(id)
}

func (s *Store) loadLocked(id string) (File, error) {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked()
}

func (s *Store) listLocked() ([]File, error) {
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
		file, err := s.loadLocked(id)
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

func (s *Store) pruneLocked(keepID string) error {
	files, err := s.listLocked()
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
		if err := s.removeLocked(files[i].ID); err != nil {
			return err
		}
		excess--
	}
	return nil
}

// Delete removes one session file. A missing file is not an error.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		return fmt.Errorf("session id is required")
	}
	return s.removeLocked(id)
}

func (s *Store) removeLocked(id string) error {
	if err := os.Remove(s.path(id)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove session: %w", err)
	}
	if err := os.RemoveAll(s.baselineDir(id)); err != nil {
		return fmt.Errorf("remove baselines: %w", err)
	}
	return nil
}

// NoteEdit records the first pre-write copy for a path and clears hunk decisions on a later edit.
func (s *Store) NoteEdit(id string, path string, first, created bool, before []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" || path == "" {
		return fmt.Errorf("session id and path are required")
	}
	file, err := s.loadOrCreate(id)
	if err != nil {
		return err
	}
	index := reviewIndex(file.Review, path)
	if !first {
		if index < 0 {
			return nil
		}
		file.Review[index].Approved = nil
		return s.saveLocked(file)
	}
	entry := ReviewEntry{Path: path, Created: created}
	if index >= 0 {
		entry = file.Review[index]
		entry.Approved = nil
	}
	if !created && entry.Baseline == "" {
		name := baselineName(path)
		if err := s.writeBaseline(id, name, before); err != nil {
			return err
		}
		entry.Baseline = name
	}
	if index >= 0 {
		file.Review[index] = entry
	} else {
		file.Review = append(file.Review, entry)
	}
	return s.saveLocked(file)
}

// ReadBaseline returns the pre-write bytes for one review entry.
func (s *Store) ReadBaseline(id string, entry ReviewEntry) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.Created || entry.Baseline == "" {
		return nil, nil
	}
	body, err := os.ReadFile(filepath.Join(s.baselineDir(id), entry.Baseline))
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	return body, nil
}

// DropReview removes one path and its baseline file.
func (s *Store) DropReview(id, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadLocked(id)
	if err != nil {
		return err
	}
	kept := file.Review[:0]
	for _, entry := range file.Review {
		if entry.Path == path {
			if entry.Baseline != "" {
				_ = os.Remove(filepath.Join(s.baselineDir(id), entry.Baseline))
			}
			continue
		}
		kept = append(kept, entry)
	}
	file.Review = kept
	return s.saveLocked(file)
}

// SaveReview writes hunk decisions for one session without dropping the transcript.
func (s *Store) SaveReview(id string, review []ReviewEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadLocked(id)
	if err != nil {
		return err
	}
	file.Review = review
	return s.saveLocked(file)
}

func (s *Store) writeBaseline(id, name string, body []byte) error {
	dir := s.baselineDir(id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create baseline directory: %w", err)
	}
	path := filepath.Join(dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename baseline: %w", err)
	}
	return nil
}

func (s *Store) baselineDir(id string) string {
	return filepath.Join(s.dir, id)
}

func baselineName(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])
}

func reviewIndex(entries []ReviewEntry, path string) int {
	for i, entry := range entries {
		if entry.Path == path {
			return i
		}
	}
	return -1
}

func (s *Store) loadOrCreate(id string) (File, error) {
	if _, err := os.Stat(s.path(id)); err != nil {
		if os.IsNotExist(err) {
			return File{ID: id}, nil
		}
		return File{}, err
	}
	return s.loadLocked(id)
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}
