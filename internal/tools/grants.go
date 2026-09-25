package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/workspace"
)

// ReadGrants is the set of paths this chat may read outside the workspace.
// Protected paths stay denied. Writes are not included.
type ReadGrants struct {
	mu    sync.Mutex
	paths []string
}

// Add stores a canonical path. A path already covered is left as it is.
func (g *ReadGrants) Add(path string) {
	if g == nil || path == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if coversGrant(path, g.paths) {
		return
	}
	g.paths = append(g.paths, path)
}

// List returns a copy of the granted paths.
func (g *ReadGrants) List() []string {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, len(g.paths))
	copy(out, g.paths)
	return out
}

// Replace sets the granted paths. An empty list clears the chat.
func (g *ReadGrants) Replace(paths []string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.paths = append([]string(nil), paths...)
}

// Covers reports whether path is a granted path or a file under one.
func (g *ReadGrants) Covers(path string) bool {
	return coversGrant(path, g.List())
}

type grantReadArgs struct {
	Path string `json:"path" jsonschema:"description=File or directory outside the workspace to read for the rest of this chat."`
}

// GrantRead asks once to read a path for the rest of the chat.
type GrantRead struct {
	Root   workspace.Root
	Grants *ReadGrants
}

func (t *GrantRead) Name() string { return "grant_read" }

func (t *GrantRead) Description() string {
	return "Ask to read a file or directory outside the workspace for the rest of this chat. Call this when later reads, searches, or shell commands will need that directory more than once. One-off files still use read_paths. Writes stay on write_paths and still ask every time. Protected paths stay denied. A path already granted does not ask again."
}

func (t *GrantRead) Parameters() json.RawMessage { return schemaFor(new(grantReadArgs)) }

func (t *GrantRead) RequiresApproval(_ context.Context, raw json.RawMessage) (gogent.ApprovalDecision, error) {
	resolved, err := t.canonical(raw)
	if err != nil || t.Grants.Covers(resolved) {
		return gogent.ApprovalDecision{}, nil
	}
	return gogent.ApprovalDecision{Required: true, Reason: resolved}, nil
}

func (t *GrantRead) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	resolved, err := t.canonical(raw)
	if err != nil {
		return accessDenied(grantPath(raw), err.Error()), nil
	}
	t.Grants.Add(resolved)
	return json.Marshal(map[string]any{"path": resolved, "granted": true})
}

func (t *GrantRead) canonical(raw json.RawMessage) (string, error) {
	path := grantPath(raw)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	resolved, _, err := t.Root.Canonical(path)
	return resolved, err
}

func grantPath(raw json.RawMessage) string {
	var args grantReadArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return ""
	}
	return args.Path
}

var _ gogent.Tool = (*GrantRead)(nil)
