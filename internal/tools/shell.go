package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/sandbox"
	"github.com/cgund98/gopi/internal/workspace"
)

type shellArgs struct {
	Command    string   `json:"command" jsonschema:"description=Shell command to run inside the sandbox"`
	Cwd        string   `json:"cwd,omitempty" jsonschema:"description=Working directory relative to the workspace"`
	Profile    string   `json:"profile,omitempty" jsonschema:"description=sandbox, or extra_paths when read_paths or write_paths is set"`
	ReadPaths  []string `json:"read_paths,omitempty" jsonschema:"description=Extra files or directories to read. The user must approve the call."`
	WritePaths []string `json:"write_paths,omitempty" jsonschema:"description=Extra files or directories to write. The user must approve the call."`
}

// Shell runs a command under the default macOS sandbox profile.
type Shell struct {
	Root    workspace.Root
	HomeDir string
}

func (t *Shell) Name() string { return "shell" }

func (t *Shell) Description() string {
	return "Run a command in the workspace sandbox. Network is denied. If the result says the sandbox blocked a file, call shell again with the same command and set read_paths or write_paths to the blocked path. That call asks the user for approval and does not run until they approve."
}

func (t *Shell) Parameters() json.RawMessage { return schemaFor(new(shellArgs)) }

func (t *Shell) RequiresApproval(_ context.Context, raw json.RawMessage) (gogent.ApprovalDecision, error) {
	var args shellArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return gogent.ApprovalDecision{}, fmt.Errorf("parse arguments: %w", err)
	}
	if len(args.ReadPaths) == 0 && len(args.WritePaths) == 0 {
		return gogent.ApprovalDecision{}, nil
	}
	reads, writes, err := t.canonicalGrants(args)
	if err != nil {
		return gogent.ApprovalDecision{}, nil
	}
	return gogent.ApprovalDecision{Required: true, Reason: grantReason(reads, writes)}, nil
}

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
	if args.Profile != "" && args.Profile != sandbox.ProfileSandbox && args.Profile != "extra_paths" {
		t.audit(command, args.Cwd, sandbox.ProfileSandbox, "denied", -1, time.Since(started))
		return accessDenied(args.Profile, "only the sandbox profile is available"), nil
	}
	if args.Profile == "extra_paths" && len(args.ReadPaths) == 0 && len(args.WritePaths) == 0 {
		t.audit(command, args.Cwd, sandbox.ProfileSandbox, "denied", -1, time.Since(started))
		return accessDenied(args.Profile, "name read_paths or write_paths"), nil
	}
	reads, writes, err := t.canonicalGrants(args)
	if err != nil {
		t.audit(command, args.Cwd, sandbox.ProfileSandbox, "denied", -1, time.Since(started))
		return accessDenied(args.Cwd, err.Error()), nil
	}
	cwd := t.Root.Path
	if args.Cwd != "" {
		resolved, err := t.Root.Resolve(args.Cwd)
		if err != nil {
			t.audit(command, args.Cwd, sandbox.ProfileSandbox, "denied", -1, time.Since(started))
			return accessDenied(args.Cwd, err.Error()), nil
		}
		cwd = resolved
	}
	if runtime.GOOS != "darwin" {
		t.audit(command, cwd, sandbox.ProfileSandbox, "denied", -1, time.Since(started))
		return accessDenied(cwd, "sandboxed shell is only available on macOS"), nil
	}

	rules, err := policy.Build(t.Root.Path, t.HomeDir, executablePath())
	if err != nil {
		t.audit(command, cwd, sandbox.ProfileSandbox, "denied", -1, time.Since(started))
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
		ExtraReads:  reads,
		ExtraWrites: writes,
		Network:     sandbox.NetworkDeny,
		Env:         sandbox.ScrubbedEnv(tmp),
		Timeout:     sandbox.DefaultTimeout,
		OutputLimit: sandbox.DefaultOutputLimit,
		WorkDir:     cwd,
		Argv:        []string{"/bin/sh", "-c", command},
	}
	profileName := sandbox.ProfileSandbox
	if len(reads) > 0 || len(writes) > 0 {
		profileName = "extra_paths"
	}
	result, err := sandbox.Launch(ctx, profile)
	if err != nil {
		t.audit(command, cwd, profileName, "failed", result.ExitCode, time.Since(started))
		return nil, err
	}
	t.audit(command, cwd, profileName, "ran", result.ExitCode, time.Since(started))
	payload := map[string]any{
		"exit_code": result.ExitCode,
		"stdout":    result.Stdout,
		"stderr":    result.Stderr,
		"truncated": result.Truncated,
	}
	if result.ExitCode != 0 && profileName == sandbox.ProfileSandbox {
		if hint, paths := fileElevation(result.Stdout, result.Stderr); hint != "" {
			payload["message"] = hint
			if len(paths) > 0 {
				payload["blocked_paths"] = paths
			}
		}
	}
	return json.Marshal(payload)
}

func (t *Shell) canonicalGrants(args shellArgs) (reads, writes []string, err error) {
	for _, path := range args.ReadPaths {
		resolved, _, err := t.Root.Canonical(path)
		if err != nil {
			return nil, nil, err
		}
		reads = append(reads, resolved)
	}
	for _, path := range args.WritePaths {
		resolved, _, err := t.Root.Canonical(path)
		if err != nil {
			return nil, nil, err
		}
		writes = append(writes, resolved)
	}
	return reads, writes, nil
}

const fileElevationHint = "The sandbox blocked file access. Call shell again with the same command and put each blocked path in read_paths or write_paths. That call asks the user for approval and does not run until they approve."

var fileDenialLine = regexp.MustCompile(`(?i)(?:^|:\s)([^\s:]+):\s*(?:operation not permitted|permission denied)\s*$`)

func fileElevation(stdout, stderr string) (string, []string) {
	var paths []string
	seen := map[string]bool{}
	blocked := false
	for _, line := range strings.Split(stdout+"\n"+stderr, "\n") {
		if !strings.Contains(strings.ToLower(line), "operation not permitted") && !strings.Contains(strings.ToLower(line), "permission denied") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "connect") || strings.Contains(lower, "socket") || strings.Contains(lower, "getaddrinfo") || strings.Contains(lower, "network") {
			continue
		}
		blocked = true
		match := fileDenialLine.FindStringSubmatch(strings.TrimSpace(line))
		if len(match) < 2 || seen[match[1]] {
			continue
		}
		seen[match[1]] = true
		paths = append(paths, match[1])
	}
	if !blocked {
		return "", nil
	}
	return fileElevationHint, paths
}

func grantReason(reads, writes []string) string {
	var parts []string
	if len(reads) > 0 {
		parts = append(parts, "read "+strings.Join(reads, ", "))
	}
	if len(writes) > 0 {
		parts = append(parts, "write "+strings.Join(writes, ", "))
	}
	return "Elevated file access: " + strings.Join(parts, "; ")
}

func (t *Shell) audit(command, cwd, profileName, decision string, exitCode int, duration time.Duration) {
	if t.HomeDir == "" {
		return
	}
	line := fmt.Sprintf("%s tool=shell command=%q cwd=%q profile=%s decision=%s exit=%d duration=%s\n",
		time.Now().UTC().Format(time.RFC3339), command, cwd, profileName, decision, exitCode, duration.Round(time.Millisecond))
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
