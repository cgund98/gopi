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

// Option changes how Run starts the session.
type Option func(*options)

type options struct {
	workspace string
	resume    bool
	tools     map[Mode][]gogent.Tool
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

	session, err := app.New(cfg, root, decisions.Lookup(root.Path), options.tools)
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
