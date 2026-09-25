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
	Command      string   `json:"command" jsonschema:"description=Shell command to run inside the sandbox"`
	Cwd          string   `json:"cwd,omitempty" jsonschema:"description=Working directory relative to the workspace"`
	Profile      string   `json:"profile,omitempty" jsonschema:"description=sandbox, extra_paths when read_paths or write_paths is set, or unsandboxed to run without Seatbelt after the user approves"`
	SecretNames  []string `json:"secret_names,omitempty" jsonschema:"-"`
	ReadPaths    []string `json:"read_paths,omitempty" jsonschema:"description=Extra files or directories to read. The user must approve the call."`
	WritePaths   []string `json:"write_paths,omitempty" jsonschema:"description=Extra files or directories to write. The user must approve the call."`
	NetworkHosts []string `json:"network_hosts,omitempty" jsonschema:"description=Extra hosts this command may reach through the proxy. The user must approve the call."`
	Network      string   `json:"network,omitempty" jsonschema:"description=Set to unrestricted to allow outbound network. The user must approve the call."`
}

// Shell runs a command under the default macOS sandbox profile.
type Shell struct {
	Root       workspace.Root
	HomeDir    string
	Network    string
	AllowHosts []string
	DenyHosts  []string
	Secrets    map[string]string
}

func (t *Shell) Name() string { return "shell" }

func (t *Shell) Description() string {
	return "Run a command in the workspace sandbox. Network is denied unless the user configured an allowlist. If the result says the sandbox blocked a file, call shell again with read_paths or write_paths. If it says network is denied, call shell again with network_hosts or network set to unrestricted. Set profile to unsandboxed only when the command must run without Seatbelt. Those calls ask the user for approval and do not run until they approve. The user chooses any secret env names on the approval card."
}

func (t *Shell) Parameters() json.RawMessage { return schemaFor(new(shellArgs)) }

func (t *Shell) RequiresApproval(_ context.Context, raw json.RawMessage) (gogent.ApprovalDecision, error) {
	var args shellArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return gogent.ApprovalDecision{}, fmt.Errorf("parse arguments: %w", err)
	}
	if args.Profile != sandbox.ProfileUnsandboxed && len(args.ReadPaths) == 0 && len(args.WritePaths) == 0 && len(args.NetworkHosts) == 0 && args.Network == "" {
		return gogent.ApprovalDecision{}, nil
	}
	var reasons []string
	if args.Profile == sandbox.ProfileUnsandboxed {
		reasons = append(reasons, "Profile: sandbox -> unsandboxed")
	}
	if len(args.ReadPaths) > 0 || len(args.WritePaths) > 0 {
		reads, writes, err := t.canonicalGrants(args)
		if err != nil {
			return gogent.ApprovalDecision{}, nil
		}
		reasons = append(reasons, grantReason(reads, writes))
	}
	if args.Network == sandbox.NetworkUnrestricted {
		reasons = append(reasons, "Network: deny -> unrestricted")
	} else if len(args.NetworkHosts) > 0 {
		reasons = append(reasons, "Network: "+strings.Join(args.NetworkHosts, ", "))
	}
	if len(reasons) == 0 {
		return gogent.ApprovalDecision{}, nil
	}
	return gogent.ApprovalDecision{Required: true, Reason: strings.Join(reasons, "; ")}, nil
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
	if args.Profile != "" && args.Profile != sandbox.ProfileSandbox && args.Profile != "extra_paths" && args.Profile != sandbox.ProfileUnsandboxed {
		t.audit(command, args.Cwd, sandbox.ProfileSandbox, "denied", -1, time.Since(started))
		return accessDenied(args.Profile, "only the sandbox profile is available"), nil
	}
	if args.Profile == "extra_paths" && len(args.ReadPaths) == 0 && len(args.WritePaths) == 0 {
		t.audit(command, args.Cwd, sandbox.ProfileSandbox, "denied", -1, time.Since(started))
		return accessDenied(args.Profile, "name read_paths or write_paths"), nil
	}
	if args.Network != "" && args.Network != sandbox.NetworkUnrestricted {
		t.audit(command, args.Cwd, sandbox.ProfileSandbox, "denied", -1, time.Since(started))
		return accessDenied(args.Network, "network must be unrestricted"), nil
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
	if runtime.GOOS != "darwin" && args.Profile != sandbox.ProfileUnsandboxed {
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

	env := sandbox.ScrubbedEnv(tmp)
	injected := injectedSecretNames(t.Secrets, args.SecretNames)
	env = append(env, injectedEnv(t.Secrets, injected)...)
	network := sandbox.NetworkDeny
	var ports []int
	if args.Network == sandbox.NetworkUnrestricted {
		network = sandbox.NetworkUnrestricted
	} else if t.Network == sandbox.NetworkAllowlist || len(args.NetworkHosts) > 0 {
		proxy, err := sandbox.ListenProxy(policy.NetworkPolicy{
			Allow: append(append([]string{}, t.AllowHosts...), args.NetworkHosts...),
			Deny:  t.DenyHosts,
		})
		if err != nil {
			return nil, fmt.Errorf("start network proxy: %w", err)
		}
		defer proxy.Close()
		network = sandbox.NetworkAllowlist
		ports = []int{proxy.HTTPPort(), proxy.SOCKSPort()}
		env = sandbox.WithProxyEnv(env, proxy.HTTPPort(), proxy.SOCKSPort())
	}
	profile := sandbox.Profile{
		Name:        sandbox.ProfileSandbox,
		ReadRoots:   []string{t.Root.Path},
		WriteRoots:  []string{t.Root.Path, tmp},
		Home:        homeDir(),
		DenyRead:    rules.DenyRead,
		DenyWrite:   rules.DenyWrite,
		ExtraReads:  reads,
		ExtraWrites: writes,
		Network:     network,
		ProxyPorts:  ports,
		Env:         env,
		Timeout:     sandbox.DefaultTimeout,
		OutputLimit: sandbox.DefaultOutputLimit,
		WorkDir:     cwd,
		Argv:        []string{"/bin/sh", "-c", command},
	}
	profileName := sandbox.ProfileSandbox
	if args.Profile == sandbox.ProfileUnsandboxed {
		profile.Name = sandbox.ProfileUnsandboxed
		profileName = sandbox.ProfileUnsandboxed
	} else if len(reads) > 0 || len(writes) > 0 {
		profileName = "extra_paths"
	}
	result, err := launchCommand(ctx, profile)
	if err != nil {
		t.audit(command, cwd, profileName, "failed", result.ExitCode, time.Since(started), injected)
		return nil, err
	}
	t.audit(command, cwd, profileName, "ran", result.ExitCode, time.Since(started), injected)
	payload := map[string]any{
		"exit_code": result.ExitCode,
		"stdout":    result.Stdout,
		"stderr":    result.Stderr,
		"truncated": result.Truncated,
	}
	if result.ExitCode != 0 {
		var notes []string
		if len(reads) == 0 && len(writes) == 0 {
			if hint, paths := fileElevation(result.Stdout, result.Stderr); hint != "" {
				notes = append(notes, hint)
				if len(paths) > 0 {
					payload["blocked_paths"] = paths
				}
			}
		}
		if args.Network != sandbox.NetworkUnrestricted && len(args.NetworkHosts) == 0 {
			if hint, hosts := networkElevation(result.Stdout, result.Stderr); hint != "" {
				notes = append(notes, hint)
				if len(hosts) > 0 {
					payload["blocked_hosts"] = hosts
				}
			}
		}
		if len(notes) > 0 {
			payload["message"] = strings.Join(notes, " ")
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

const networkElevationHint = "Network is denied. Call shell again with the same command and put each host in network_hosts, or set network to unrestricted. That call asks the user for approval and does not run until they approve."

var networkHostPattern = regexp.MustCompile(`(?i)\b([a-z0-9-]+(?:\.[a-z0-9-]+)+)\b`)

func networkElevation(stdout, stderr string) (string, []string) {
	text := stdout + "\n" + stderr
	lower := strings.ToLower(text)
	denied := strings.Contains(lower, "could not resolve") || strings.Contains(lower, "network is unreachable") ||
		((strings.Contains(lower, "operation not permitted") || strings.Contains(lower, "permission denied")) &&
			(strings.Contains(lower, "connect") || strings.Contains(lower, "socket") || strings.Contains(lower, "network")))
	if !denied {
		return "", nil
	}
	var hosts []string
	seen := map[string]bool{}
	for _, match := range networkHostPattern.FindAllString(lower, -1) {
		if seen[match] || !strings.Contains(match, ".") {
			continue
		}
		seen[match] = true
		hosts = append(hosts, match)
	}
	return networkElevationHint, hosts
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

func (t *Shell) audit(command, cwd, profileName, decision string, exitCode int, duration time.Duration, secrets ...[]string) {
	if t.HomeDir == "" {
		return
	}
	names := ""
	if len(secrets) > 0 && len(secrets[0]) > 0 {
		names = " secrets=" + strings.Join(secrets[0], ",")
	}
	line := fmt.Sprintf("%s tool=shell command=%q cwd=%q profile=%s decision=%s exit=%d duration=%s%s\n",
		time.Now().UTC().Format(time.RFC3339), command, cwd, profileName, decision, exitCode, duration.Round(time.Millisecond), names)
	f, err := os.OpenFile(t.HomeDir+"/audit.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(line)
}

var launchCommand = sandbox.Launch

func injectedSecretNames(secrets map[string]string, requested []string) []string {
	var names []string
	seen := map[string]bool{}
	for _, name := range requested {
		if seen[name] || strings.ContainsAny(name, "=\n\r") {
			continue
		}
		seen[name] = true
		switch name {
		case "openai_api_key", "search_api_key":
			continue
		}
		if _, ok := secrets[name]; !ok {
			continue
		}
		names = append(names, name)
	}
	return names
}

func injectedEnv(secrets map[string]string, names []string) []string {
	env := make([]string, 0, len(names))
	for _, name := range names {
		env = append(env, name+"="+secrets[name])
	}
	return env
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
