package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/cgund98/gopi"
)

const usage = `gopi [flags] [workspace]

Start the coding agent in a workspace.

  workspace             directory to work in (default: current directory)

Flags:
`

func main() {
	resumeFlag := flag.Bool("resume", false, "open the newest saved session")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	workspace := flag.Arg(0)
	if flag.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "gopi: at most one workspace directory")
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var opts []gopi.Option
	if workspace != "" {
		opts = append(opts, gopi.WithWorkspace(workspace))
	}
	if *resumeFlag {
		opts = append(opts, gopi.WithResume())
	}
	if err := gopi.Run(ctx, opts...); err != nil {
		fmt.Fprintf(os.Stderr, "gopi: %v\n", err)
		os.Exit(1)
	}
}
