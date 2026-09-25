package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/cgund98/gopi"
)

func main() {
	workspaceFlag := flag.String("workspace", "", "workspace directory (default: current directory)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var opts []gopi.Option
	if *workspaceFlag != "" {
		opts = append(opts, gopi.WithWorkspace(*workspaceFlag))
	}
	if err := gopi.Run(ctx, opts...); err != nil {
		fmt.Fprintf(os.Stderr, "gopi: %v\n", err)
		os.Exit(1)
	}
}
