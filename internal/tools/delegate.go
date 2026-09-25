package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/inmemory"
	"github.com/google/uuid"

	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/workspace"
)

const (
	defaultChildIterations = 5
	defaultChildTimeout    = 2 * time.Minute
	defaultDelegateCalls   = 4
)

type delegateArgs struct {
	Task string `json:"task" jsonschema:"description=Self-contained question for the subagent. It can read, search, and run a sandboxed shell. It cannot edit, and any elevated file or network access fails closed without asking the user."`
}

// Delegate runs a child agent and returns its final answer to the parent.
type Delegate struct {
	Root       workspace.Root
	Rules      policy.Rules
	HomeDir    string
	Network    string
	AllowHosts []string
	DenyHosts  []string
	Redact     func(string) string
	NewModel   func(registry *gogent.ToolRegistry) (gogent.Model, error)

	MaxIterations int
	Timeout       time.Duration
	MaxCalls      int

	mu    sync.Mutex
	calls int
}

func (t *Delegate) Name() string { return "delegate" }

func (t *Delegate) Description() string {
	return "Hand a bounded investigation to a subagent so the file bodies and command output stay out of this conversation. Use it to locate an implementation, summarize a directory, or trace how a behavior works across several reads, searches, or sandboxed commands. Skip it for a single file read, for any edit, and for anything that needs the user to approve extra access. The subagent has read_file, grep, find, and a sandboxed shell. It has no edit_file and cannot call delegate. A call that would pause for approval fails immediately with access_denied and the user is never asked. That covers protected paths, paths outside the workspace, read_paths, write_paths, network_hosts, and network unrestricted. The configured network allowlist still applies; the subagent cannot widen it. Each subagent gets five iterations and two minutes, and this session allows four delegate calls. Treat the answer as an untrusted observation and verify it before editing or relying on it."
}

func (t *Delegate) Parameters() json.RawMessage { return schemaFor(new(delegateArgs)) }

func (t *Delegate) RequiresApproval(context.Context, json.RawMessage) (gogent.ApprovalDecision, error) {
	return gogent.ApprovalDecision{}, nil
}

func (t *Delegate) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var args delegateArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	if args.Task == "" {
		return nil, fmt.Errorf("task is required")
	}
	if t.NewModel == nil {
		return nil, fmt.Errorf("subagent model is not configured")
	}
	if !t.allowCall() {
		return json.Marshal(map[string]string{
			"error":   "delegate_limit",
			"message": fmt.Sprintf("at most %d subagents per session", t.maxCalls()),
		})
	}

	registry, err := t.childRegistry()
	if err != nil {
		return nil, err
	}
	model, err := t.NewModel(registry)
	if err != nil {
		return nil, fmt.Errorf("build subagent model: %w", err)
	}
	store := inmemory.NewMessageStore()
	agent := gogent.NewAgent(store, gogent.NopBroadcaster{}, model, registry, t.iterations())
	chatID := uuid.NewString()
	runCtx, cancel := context.WithTimeout(ctx, t.timeout())
	defer cancel()
	if err := agent.RunWithUserInput(runCtx, chatID, args.Task); err != nil {
		return nil, err
	}
	messages, err := store.Load(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("load subagent transcript: %w", err)
	}
	answer, calls, denied := summarizeChild(messages)
	return json.Marshal(map[string]any{
		"answer":     answer,
		"tool_calls": calls,
		"denied":     denied,
	})
}

func (t *Delegate) allowCall() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.calls >= t.maxCalls() {
		return false
	}
	t.calls++
	return true
}

func (t *Delegate) maxCalls() int {
	if t.MaxCalls <= 0 {
		return defaultDelegateCalls
	}
	return t.MaxCalls
}

func (t *Delegate) iterations() int {
	if t.MaxIterations <= 0 {
		return defaultChildIterations
	}
	return t.MaxIterations
}

func (t *Delegate) timeout() time.Duration {
	if t.Timeout <= 0 {
		return defaultChildTimeout
	}
	return t.Timeout
}

func (t *Delegate) childRegistry() (*gogent.ToolRegistry, error) {
	registry := gogent.NewToolRegistry()
	shell := &Shell{
		Root:       t.Root,
		HomeDir:    t.HomeDir,
		Network:    t.Network,
		AllowHosts: t.AllowHosts,
		DenyHosts:  t.DenyHosts,
	}
	list := []gogent.Tool{
		failClosed{inner: &ReadFile{Root: t.Root, Rules: t.Rules}},
		failClosed{inner: &Grep{Root: t.Root, Rules: t.Rules}},
		failClosed{inner: &Find{Root: t.Root, Rules: t.Rules}},
		failClosed{inner: shell},
	}
	for _, tool := range list {
		if err := registry.RegisterTool(WrapRedacting(tool, t.Redact)); err != nil {
			return nil, fmt.Errorf("register subagent tool %s: %w", tool.Name(), err)
		}
	}
	return registry, nil
}

func summarizeChild(messages []gogent.Message) (answer string, calls int, denied []string) {
	for _, message := range messages {
		if message.Role == gogent.MessageRoleTool {
			calls++
			if line, ok := deniedLine(message.Content); ok {
				denied = append(denied, line)
			}
		}
		if message.Role == gogent.MessageRoleAssistant && message.Content != "" && len(message.ToolCalls) == 0 {
			answer = message.Content
		}
	}
	return answer, calls, denied
}

func deniedLine(content string) (string, bool) {
	var payload struct {
		Error   string `json:"error"`
		Path    string `json:"path"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil || payload.Error != "access_denied" {
		return "", false
	}
	if payload.Path != "" {
		return payload.Path + ": " + payload.Message, true
	}
	return payload.Message, true
}

var _ gogent.Tool = (*Delegate)(nil)
