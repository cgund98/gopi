package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/cgund98/gopi/internal/app"
	"github.com/cgund98/gopi/internal/config"
	"github.com/cgund98/gopi/internal/trust"
	"github.com/cgund98/gopi/internal/tui"
	"github.com/cgund98/gopi/internal/workspace"
)

func main() {
	workspaceFlag := flag.String("workspace", "", "workspace directory (default: current directory)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, *workspaceFlag); err != nil {
		fmt.Fprintf(os.Stderr, "gopi: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, workspaceFlag string) error {
	home, err := config.HomeDir()
	if err != nil {
		return err
	}
	cfg, err := config.Load(home)
	if err != nil {
		return err
	}

	dir := workspaceFlag
	if dir == "" {
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("current directory: %w", err)
		}
	}
	root, err := workspace.Open(dir)
	if err != nil {
		return err
	}

	decisions, err := trust.Open(home)
	if err != nil {
		return err
	}

	session, err := app.New(cfg, root, decisions.Lookup(root.Path))
	if err != nil {
		return err
	}
	return tui.Run(ctx, session, decisions)
}
