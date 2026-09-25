package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
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
	if strings.Contains(string(tool.Parameters()), "secret_names") {
		t.Fatal("secret_names is visible to the model")
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
	if !containsOutput(raw, "ok") {
		t.Fatalf("result = %s", raw)
	}
}

func TestSeatbeltFailureDoesNotRetryUnsandboxed(t *testing.T) {
	root := openTemp(t)
	tool := &Shell{Root: root, HomeDir: t.TempDir()}
	var names []string
	orig := launchCommand
	launchCommand = func(_ context.Context, profile sandbox.Profile) (sandbox.Result, error) {
		names = append(names, profile.Name)
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
}

func TestSecretInjectionStaysOutOfResultAndAudit(t *testing.T) {
	root := openTemp(t)
	home := t.TempDir()
	const secret = "token-value-xyz"
	tool := &Shell{Root: root, HomeDir: home, Secrets: map[string]string{"DEPLOY_TOKEN": secret, "openai_api_key": "sk-host"}}
	orig := launchCommand
	launchCommand = func(_ context.Context, profile sandbox.Profile) (sandbox.Result, error) {
		value := ""
		for _, entry := range profile.Env {
			if strings.HasPrefix(entry, "DEPLOY_TOKEN=") {
				value = strings.TrimPrefix(entry, "DEPLOY_TOKEN=")
			}
			if strings.Contains(entry, "sk-host") {
				t.Errorf("host key injected: %s", entry)
			}
		}
		return sandbox.Result{Stdout: value}, nil
	}
	defer func() { launchCommand = orig }()
	raw, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"printf x","profile":"unsandboxed","secret_names":["DEPLOY_TOKEN","openai_api_key"]}`))
	if err != nil {
		t.Fatal(err)
	}
	wrapped := WrapRedacting(tool, func(text string) string {
		return strings.ReplaceAll(text, secret, "[redacted]")
	})
	redacted, err := wrapped.Execute(context.Background(), json.RawMessage(`{"command":"printf x","profile":"unsandboxed","secret_names":["DEPLOY_TOKEN"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(redacted), secret) {
		t.Fatalf("result leaked secret: %s", redacted)
	}
	if !strings.Contains(string(raw), secret) {
		t.Fatal("child environment did not receive the secret")
	}
	audit, err := os.ReadFile(filepath.Join(home, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(audit), secret) || !strings.Contains(string(audit), "secrets=DEPLOY_TOKEN") {
		t.Fatalf("audit = %s", audit)
	}
}
