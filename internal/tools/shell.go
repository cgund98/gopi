package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/sandbox"
	"github.com/cgund98/gopi/internal/workspace"
)

type shellArgs struct {
	Command string `json:"command" jsonschema:"description=Shell command to run inside the sandbox"`
	Cwd     string `json:"cwd,omitempty" jsonschema:"description=Working directory relative to the workspace"`
	Profile string `json:"profile,omitempty" jsonschema:"description=Must be sandbox. Wider profiles are refused"`
}

// Shell runs a command under the default macOS sandbox profile.
type Shell struct {
	Root    workspace.Root
	HomeDir string
}

func (t *Shell) Name() string { return "shell" }

func (t *Shell) Description() string {
	return "Run a command in the workspace sandbox. Network is denied and protected paths are unreadable. Wider profiles are refused."
}

func (t *Shell) Parameters() json.RawMessage { return schemaFor(new(shellArgs)) }

func (t *Shell) RequiresApproval() bool { return false }

func (t *Shell) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	started := time.Now()
	var args shellArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	command := strings.TrimSpace(args.Command)
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}
	if args.Profile != "" && args.Profile != sandbox.ProfileSandbox {
		t.audit(command, args.Cwd, "denied", -1, time.Since(started))
		return accessDenied(args.Profile, "only the sandbox profile is available"), nil
	}
	cwd := t.Root.Path
	if args.Cwd != "" {
		resolved, err := t.Root.Resolve(args.Cwd)
		if err != nil {
			t.audit(command, args.Cwd, "denied", -1, time.Since(started))
			return accessDenied(args.Cwd, err.Error()), nil
		}
		cwd = resolved
	}
	if runtime.GOOS != "darwin" {
		t.audit(command, cwd, "denied", -1, time.Since(started))
		return accessDenied(cwd, "sandboxed shell is only available on macOS"), nil
	}

	rules, err := policy.Build(t.Root.Path, t.HomeDir, executablePath())
	if err != nil {
		t.audit(command, cwd, "denied", -1, time.Since(started))
		return accessDenied(cwd, err.Error()), nil
	}
	tmp, err := sandbox.SessionTemp()
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	profile := sandbox.Profile{
		Name:        sandbox.ProfileSandbox,
		ReadRoots:   []string{t.Root.Path},
		WriteRoots:  []string{t.Root.Path, tmp},
		Home:        homeDir(),
		DenyRead:    rules.DenyRead,
		DenyWrite:   rules.DenyWrite,
		Network:     sandbox.NetworkDeny,
		Env:         sandbox.ScrubbedEnv(tmp),
		Timeout:     sandbox.DefaultTimeout,
		OutputLimit: sandbox.DefaultOutputLimit,
		WorkDir:     cwd,
		Argv:        []string{"/bin/sh", "-c", command},
	}
	result, err := sandbox.Launch(ctx, profile)
	if err != nil {
		t.audit(command, cwd, "failed", result.ExitCode, time.Since(started))
		return nil, err
	}
	t.audit(command, cwd, "ran", result.ExitCode, time.Since(started))
	return json.Marshal(map[string]any{
		"exit_code": result.ExitCode,
		"stdout":    result.Stdout,
		"stderr":    result.Stderr,
		"truncated": result.Truncated,
	})
}

func (t *Shell) audit(command, cwd, decision string, exitCode int, duration time.Duration) {
	if t.HomeDir == "" {
		return
	}
	line := fmt.Sprintf("%s tool=shell command=%q cwd=%q profile=%s decision=%s exit=%d duration=%s\n",
		time.Now().UTC().Format(time.RFC3339), command, cwd, sandbox.ProfileSandbox, decision, exitCode, duration.Round(time.Millisecond))
	f, err := os.OpenFile(t.HomeDir+"/audit.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(line)
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func executablePath() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	return path
}

var _ gogent.Tool = (*Shell)(nil)
