package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cgund98/gopi/internal/sandbox"
)

func TestShellRejectsWiderProfileAndOutsideCwd(t *testing.T) {
	root := openTemp(t)
	tool := &Shell{Root: root, HomeDir: t.TempDir()}
	denied, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo hi","profile":"workspace_network"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	if err := json.Unmarshal(denied, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] != "access_denied" {
		t.Fatalf("payload = %#v", payload)
	}

	outside, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo hi","cwd":"../outside"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(outside, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] != "access_denied" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestShellExtraPathsRequireApproval(t *testing.T) {
	root := openTemp(t)
	if err := os.WriteFile(filepath.Join(root.Path, ".env"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := &Shell{Root: root, HomeDir: t.TempDir()}
	decision, err := tool.RequiresApproval(context.Background(), json.RawMessage(`{"command":"cat .env","read_paths":[".env"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Required || !strings.Contains(decision.Reason, "Elevated file access") || !strings.Contains(decision.Reason, ".env") {
		t.Fatalf("decision = %#v", decision)
	}
	plain, err := tool.RequiresApproval(context.Background(), json.RawMessage(`{"command":"echo hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Required {
		t.Fatalf("plain shell should not pause: %#v", plain)
	}
}

func TestShellNetworkRequiresApproval(t *testing.T) {
	root := openTemp(t)
	tool := &Shell{Root: root, HomeDir: t.TempDir()}
	hosts, err := tool.RequiresApproval(context.Background(), json.RawMessage(`{"command":"curl https://example.com","network_hosts":["example.com"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !hosts.Required || !strings.Contains(hosts.Reason, "example.com") {
		t.Fatalf("hosts = %#v", hosts)
	}
	open, err := tool.RequiresApproval(context.Background(), json.RawMessage(`{"command":"curl https://example.com","network":"unrestricted"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !open.Required || !strings.Contains(open.Reason, "deny -> unrestricted") {
		t.Fatalf("unrestricted = %#v", open)
	}
	hint, blocked := networkElevation("", "nc: connect: Operation not permitted\n")
	if !strings.Contains(hint, "network_hosts") {
		t.Fatalf("hint = %q hosts = %#v", hint, blocked)
	}
}

func TestShellFileDenialAsksToRetryElevated(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandboxed shell runs on macOS")
	}
	root := openTemp(t)
	if err := os.WriteFile(filepath.Join(root.Path, ".env"), []byte("super-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := &Shell{Root: root, HomeDir: t.TempDir()}
	raw := runShell(t, tool, `cat .env`)
	var payload struct {
		Message      string   `json:"message"`
		BlockedPaths []string `json:"blocked_paths"`
		Stdout       string   `json:"stdout"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload.Message, "read_paths or write_paths") {
		t.Fatalf("result = %s", raw)
	}
	if payload.Stdout != "" && strings.Contains(payload.Stdout, "super-secret") {
		t.Fatalf("sandbox read .env: %s", raw)
	}
	found := false
	for _, path := range payload.BlockedPaths {
		if path == ".env" {
			found = true
		}
	}
	if !found {
		t.Fatalf("blocked paths = %#v body = %s", payload.BlockedPaths, raw)
	}
}

func TestFileElevationIgnoresNetworkDenial(t *testing.T) {
	hint, paths := fileElevation("", "nc: connect: Operation not permitted\n")
	if hint != "" || paths != nil {
		t.Fatalf("hint = %q paths = %#v", hint, paths)
	}
}

func TestShellReturnsExitCode(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandboxed shell runs on macOS")
	}
	root := openTemp(t)
	tool := &Shell{Root: root, HomeDir: t.TempDir()}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"exit 3"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ExitCode int `json:"exit_code"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ExitCode != 3 {
		t.Fatalf("exit = %d body = %s", payload.ExitCode, result)
	}
}

func TestShellEscape(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandboxed shell runs on macOS")
	}
	root := openTemp(t)
	secret := []byte("super-secret")
	if err := os.WriteFile(filepath.Join(root.Path, ".env"), secret, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root.Path, ".git", "hooks"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root.Path, "escape")); err != nil {
		t.Fatal(err)
	}

	tool := &Shell{Root: root, HomeDir: t.TempDir()}
	readEnv := runShell(t, tool, `cat .env`)
	if containsOutput(readEnv, "super-secret") {
		t.Fatalf("sandbox read .env: %s", readEnv)
	}
	hook := runShell(t, tool, `echo pwned > .git/hooks/pre-commit`)
	if _, err := os.Stat(filepath.Join(root.Path, ".git", "hooks", "pre-commit")); err == nil {
		t.Fatalf("sandbox wrote a hook: %s", hook)
	}
	linked := runShell(t, tool, `cat escape`)
	if containsOutput(linked, "outside") {
		t.Fatalf("sandbox followed symlink: %s", linked)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
			accepted <- struct{}{}
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	_ = runShell(t, tool, fmt.Sprintf("nc -G 1 -z 127.0.0.1 %d", port))
	select {
	case <-accepted:
		t.Fatal("sandbox connected to a local listener")
	case <-time.After(500 * time.Millisecond):
	}
}

func runShell(t *testing.T, tool *Shell, command string) json.RawMessage {
	t.Helper()
	body, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Execute(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func containsOutput(raw json.RawMessage, needle string) bool {
	var payload struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}
	return strings.Contains(payload.Stdout, needle) || strings.Contains(payload.Stderr, needle)
}

func TestUnsandboxedRequiresApprovalAndSkipsSeatbelt(t *testing.T) {
	root := openTemp(t)
	home := t.TempDir()
	tool := &Shell{Root: root, HomeDir: home}
	decision, err := tool.RequiresApproval(context.Background(), json.RawMessage(`{"command":"echo hi","profile":"unsandboxed"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Required || !strings.Contains(decision.Reason, "Profile: sandbox -> unsandboxed") {
		t.Fatalf("decision = %#v", decision)
	}
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	t.Setenv("GOPI_PARENT_MARKER", "parent-secret")
	var saw sandbox.Profile
	orig := launchCommand
	launchCommand = func(_ context.Context, profile sandbox.Profile) (sandbox.Result, error) {
		saw = profile
		return sandbox.Result{Stdout: "ok\n"}, nil
	}
	defer func() { launchCommand = orig }()
	raw, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo hi","profile":"unsandboxed"}`))
	if err != nil {
		t.Fatal(err)
	}
	if saw.Name != sandbox.ProfileUnsandboxed {
		t.Fatalf("profile = %#v", saw)
	}
	if len(saw.Argv) == 0 || saw.Argv[0] == "/usr/bin/sandbox-exec" {
		t.Fatalf("argv = %#v", saw.Argv)
	}
	joined := strings.Join(saw.Env, "\n")
	if strings.Contains(joined, "SSH_AUTH_SOCK") || strings.Contains(joined, "parent-secret") {
		t.Fatalf("env = %s", joined)
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(joined, "HOME="+userHome) {
		t.Fatalf("env = %s", joined)
	}
	if !containsOutput(raw, "ok") {
		t.Fatalf("result = %s", raw)
	}
}

func TestShellIgnoresStaleSecretNamesArgument(t *testing.T) {
	root := openTemp(t)
	tool := &Shell{Root: root, HomeDir: t.TempDir()}
	var env []string
	orig := launchCommand
	launchCommand = func(_ context.Context, profile sandbox.Profile) (sandbox.Result, error) {
		env = profile.Env
		return sandbox.Result{Stdout: "ok\n"}, nil
	}
	defer func() { launchCommand = orig }()
	raw, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"printf x","profile":"unsandboxed","secret_names":["DEPLOY_TOKEN","DEEPSEEK_API_KEY"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !containsOutput(raw, "ok") {
		t.Fatalf("result = %s", raw)
	}
	for _, entry := range env {
		if strings.HasPrefix(entry, "DEPLOY_TOKEN=") || strings.HasPrefix(entry, "DEEPSEEK_API_KEY=") {
			t.Fatalf("stale secret_names injected a value: %s", entry)
		}
	}
}

func TestSeatbeltFailureDoesNotRetryUnsandboxed(t *testing.T) {
	root := openTemp(t)
	tool := &Shell{Root: root, HomeDir: t.TempDir()}
	var names []string
	orig := launchCommand
	var sandboxedEnv string
	launchCommand = func(_ context.Context, profile sandbox.Profile) (sandbox.Result, error) {
		names = append(names, profile.Name)
		sandboxedEnv = strings.Join(profile.Env, "\n")
		return sandbox.Result{}, fmt.Errorf("sandbox-exec failed")
	}
	defer func() { launchCommand = orig }()
	raw, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo hi"}`))
	if runtime.GOOS != "darwin" {
		if err != nil || len(names) != 0 {
			t.Fatalf("err = %v launches = %#v", err, names)
		}
		var payload map[string]string
		if jsonErr := json.Unmarshal(raw, &payload); jsonErr != nil {
			t.Fatal(jsonErr)
		}
		if payload["error"] != "access_denied" {
			t.Fatalf("payload = %#v", payload)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), "sandbox-exec failed") {
		t.Fatalf("err = %v", err)
	}
	if len(names) != 1 || names[0] != sandbox.ProfileSandbox {
		t.Fatalf("launches = %#v", names)
	}
	if strings.Contains(sandboxedEnv, "HOME=") {
		t.Fatalf("sandboxed env = %s", sandboxedEnv)
	}
}

func TestShellProfileDeniesSecretFiles(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandboxed shell is macOS only")
	}
	token := filepath.Join(t.TempDir(), "gcal_token.json")
	tool := &Shell{Root: openTemp(t), HomeDir: t.TempDir(), SecretFiles: []string{token}}
	var saw sandbox.Profile
	orig := launchCommand
	launchCommand = func(_ context.Context, profile sandbox.Profile) (sandbox.Result, error) {
		saw = profile
		return sandbox.Result{Stdout: "ok\n"}, nil
	}
	defer func() { launchCommand = orig }()
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo hi"}`)); err != nil {
		t.Fatal(err)
	}
	for _, pattern := range saw.DenyRead {
		if regexp.MustCompile(pattern).MatchString(token) {
			return
		}
	}
	t.Fatalf("deny-read list misses %s: %#v", token, saw.DenyRead)
}

func TestShellProfileIncludesSessionGrant(t *testing.T) {
	root := openTemp(t)
	outside := t.TempDir()
	grants := &ReadGrants{}
	grants.Add(outside)
	tool := &Shell{Root: root, HomeDir: t.TempDir(), Grants: grants}
	reads := sessionReads(grants, false)
	if len(reads) != 1 || reads[0] != outside {
		t.Fatalf("reads = %#v", reads)
	}
	if sessionReads(grants, true) != nil {
		t.Fatal("unsandboxed profile included session reads")
	}
	body, err := sandbox.SeatbeltProfile(sandbox.Profile{
		SessionReads: reads,
		DenyRead:     []string{"^(.*/)?\\.[eE][nN][vV](/.*)?$"},
		ExtraReads:   []string{filepath.Join(outside, ".env")},
		Network:      sandbox.NetworkDeny,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, outside) {
		t.Fatalf("profile missing grant:\n%s", body)
	}
	if runtime.GOOS != "darwin" {
		return
	}
	var saw sandbox.Profile
	orig := launchCommand
	launchCommand = func(_ context.Context, profile sandbox.Profile) (sandbox.Result, error) {
		saw = profile
		return sandbox.Result{Stdout: "ok\n"}, nil
	}
	defer func() { launchCommand = orig }()
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo hi"}`)); err != nil {
		t.Fatal(err)
	}
	if len(saw.SessionReads) != 1 || saw.SessionReads[0] != outside {
		t.Fatalf("profile reads = %#v", saw.SessionReads)
	}
}
