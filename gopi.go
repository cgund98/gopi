// Package gopi starts the coding agent. The cmd/gopi binary calls Run with no
// extra tools. Another program can pass WithTool to add a tool to a mode.
package gopi

import (
	"context"
	"fmt"
	"os"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/app"
	"github.com/cgund98/gopi/internal/config"
	"github.com/cgund98/gopi/internal/session"
	"github.com/cgund98/gopi/internal/toolview"
	"github.com/cgund98/gopi/internal/trust"
	"github.com/cgund98/gopi/internal/tui"
	"github.com/cgund98/gopi/internal/workspace"
)

// Mode selects which tool registry receives a custom tool.
type Mode = app.Mode

const (
	ModeAgent Mode = app.ModeAgent
	ModeAsk   Mode = app.ModeAsk
	ModePlan  Mode = app.ModePlan
)

// ToolRenderer is optional. A tool passed to WithTool or WithToolFactory that also
// implements it controls its headline, approval body, and result panel. An empty
// headline or a zero ToolView keeps the default rendering.
type ToolRenderer = toolview.Renderer

// ToolView is plain text that gopi frames, styles, and wraps.
type ToolView = toolview.View

// ToolField is one labeled row in a ToolView.
type ToolField = toolview.Field

// Option changes how Run starts the session.
type Option func(*options)

type options struct {
	workspace string
	resume    bool
	tools     map[Mode][]gogent.Tool
	factories []toolFactory
}

type toolFactory struct {
	mode  Mode
	build func(ToolEnv) (gogent.Tool, error)
}

// ToolEnv is what a tool factory may read while Run builds the tool.
type ToolEnv struct {
	// Workspace is the canonical workspace root.
	Workspace string

	secrets map[string]string
	files   map[string]string
}

// Secret returns a value from ~/.gopi/secrets.toml, or the contents of the file
// it references. A missing name is an error.
func (e ToolEnv) Secret(name string) (string, error) {
	value, ok := e.secrets[name]
	if !ok {
		return "", fmt.Errorf("secret %s is missing from ~/.gopi/secrets.toml", name)
	}
	return value, nil
}

// SecretPath returns the file a secret references, for a tool that must write the
// file back (for example a refreshed OAuth token). A string secret is an error.
func (e ToolEnv) SecretPath(name string) (string, error) {
	path, ok := e.files[name]
	if !ok {
		if _, exists := e.secrets[name]; exists {
			return "", fmt.Errorf("secret %s is a string, not a { file = \"path\" } reference", name)
		}
		return "", fmt.Errorf("secret %s is missing from ~/.gopi/secrets.toml", name)
	}
	return path, nil
}

// WithToolFactory adds a tool built at startup from a ToolEnv. Use it when the
// tool needs secrets from ~/.gopi/secrets.toml. An error from factory stops Run.
// Like WithTool, the tool is added to one mode and not to the delegate child.
func WithToolFactory(mode Mode, factory func(ToolEnv) (gogent.Tool, error)) Option {
	return func(o *options) {
		o.factories = append(o.factories, toolFactory{mode: mode, build: factory})
	}
}

// buildTools runs the factories and returns every custom tool by mode.
func buildTools(cfg config.Config, workspacePath string, o options) (map[Mode][]gogent.Tool, error) {
	out := map[Mode][]gogent.Tool{}
	for mode, list := range o.tools {
		out[mode] = append(out[mode], list...)
	}
	env := ToolEnv{Workspace: workspacePath, secrets: cfg.Secrets, files: cfg.SecretFiles}
	for _, factory := range o.factories {
		tool, err := factory.build(env)
		if err != nil {
			return nil, fmt.Errorf("build %s tool: %w", factory.mode, err)
		}
		if tool == nil {
			return nil, fmt.Errorf("build %s tool: factory returned no tool", factory.mode)
		}
		out[factory.mode] = append(out[factory.mode], tool)
	}
	return out, nil
}

// WithWorkspace uses dir instead of the current directory.
func WithWorkspace(dir string) Option {
	return func(o *options) {
		o.workspace = dir
	}
}

// WithResume opens the newest saved chat instead of an empty one.
// WithWorkspace limits that choice to chats for that directory.
func WithResume() Option {
	return func(o *options) {
		o.resume = true
	}
}

// WithTool adds tool to one mode. Pass it again to add the same tool to another mode.
// The tool is not registered on the delegate child. A built-in name is rejected.
func WithTool(mode Mode, tool gogent.Tool) Option {
	return func(o *options) {
		if o.tools == nil {
			o.tools = map[Mode][]gogent.Tool{}
		}
		o.tools[mode] = append(o.tools[mode], tool)
	}
}

// Run loads ~/.gopi, opens the workspace, and starts the terminal UI.
func Run(ctx context.Context, opts ...Option) error {
	var options options
	for _, opt := range opts {
		opt(&options)
	}

	home, err := config.HomeDir()
	if err != nil {
		return err
	}
	cfg, err := config.Load(home)
	if err != nil {
		return err
	}

	dir := options.workspace
	if dir == "" && !options.resume {
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("current directory: %w", err)
		}
	}
	var resume *session.File
	if options.resume {
		picked, err := latestSession(home, dir)
		if err != nil {
			return err
		}
		resume = &picked
		dir = picked.Workspace
	}
	root, err := workspace.Open(dir)
	if err != nil {
		return err
	}

	decisions, err := trust.Open(home)
	if err != nil {
		return err
	}

	custom, err := buildTools(cfg, root.Path, options)
	if err != nil {
		return err
	}

	session, err := app.New(cfg, root, decisions.Lookup(root.Path), custom)
	if err != nil {
		return err
	}
	return tui.Run(ctx, session, decisions, resume)
}

func latestSession(home, workspace string) (session.File, error) {
	store, err := session.Open(home)
	if err != nil {
		return session.File{}, err
	}
	files, err := store.List()
	if err != nil {
		return session.File{}, err
	}
	root := ""
	if workspace != "" {
		opened, err := workspaceOpen(workspace)
		if err != nil {
			return session.File{}, err
		}
		root = opened
	}
	for _, file := range files {
		if root == "" || file.Workspace == root {
			return file, nil
		}
	}
	if root != "" {
		return session.File{}, fmt.Errorf("no saved session for %s", root)
	}
	return session.File{}, fmt.Errorf("no saved session")
}

func workspaceOpen(path string) (string, error) {
	root, err := workspace.Open(path)
	if err != nil {
		return "", err
	}
	return root.Path, nil
}
