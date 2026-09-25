package trust

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const fileName = "trust.json"

// Workspace is whether the user trusts a workspace enough to allow edits.
// It is not approval for an individual tool call.
type Workspace int

const (
	WorkspaceUnknown Workspace = iota
	WorkspaceUntrusted
	WorkspaceTrusted
)

// Store persists workspace trust outside the repository.
type Store struct {
	path       string
	workspaces map[string]bool
}

type file struct {
	Workspaces map[string]bool `json:"workspaces"`
}

// Open loads trust.json from the gopi home directory.
func Open(homeDir string) (*Store, error) {
	path := filepath.Join(homeDir, fileName)
	store := &Store{
		path:       path,
		workspaces: map[string]bool{},
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, fmt.Errorf("read trust file: %w", err)
	}
	var decoded file
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode trust file: %w", err)
	}
	if decoded.Workspaces != nil {
		store.workspaces = decoded.Workspaces
	}
	return store, nil
}

// Lookup reports whether a canonical workspace path is trusted.
func (s *Store) Lookup(workspace string) Workspace {
	trusted, ok := s.workspaces[workspace]
	if !ok {
		return WorkspaceUnknown
	}
	if trusted {
		return WorkspaceTrusted
	}
	return WorkspaceUntrusted
}

// Set records whether a workspace is trusted and writes the file.
func (s *Store) Set(workspace string, trusted bool) error {
	if s.workspaces == nil {
		s.workspaces = map[string]bool{}
	}
	s.workspaces[workspace] = trusted
	body, err := json.MarshalIndent(file{Workspaces: s.workspaces}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode trust file: %w", err)
	}
	body = append(body, '\n')
	if err := os.WriteFile(s.path, body, 0o600); err != nil {
		return fmt.Errorf("write trust file: %w", err)
	}
	return nil
}
