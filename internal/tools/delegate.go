package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/workspace"
)

const defaultDelegateCalls = 4

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
	// RespectGitignore is passed to the child shell's deny set.
	RespectGitignore bool
	// SecretFiles are passed to the child shell so its sandbox denies them.
	SecretFiles []string
	// Engine, RGPath, and SearchHome select the child's search backend.
	// RGPath is empty when the child searches with the built-in walker.
	Engine     string
	RGPath     string
	SearchHome string
	// BasePrompt returns the child's system prompt. It is a function so a prompt
	// rebuilt after a trust decision is picked up on the next call.
	BasePrompt func() string
	Redact     func(string) string
	// Grants are the parent's session read grants. The child reads through them
	// but cannot add to them.
	Grants   *ReadGrants
	NewModel func(registry *gogent.ToolRegistry, systemPrompt string) (gogent.Model, error)
	// Progress, when set, reports the running subagent's tool calls to the UI.
	Progress *SubagentProgress

	MaxIterations int
	Timeout       time.Duration
	MaxCalls      int

	mu    sync.Mutex
	calls int
}

func (t *Delegate) Name() string { return "delegate" }

func (t *Delegate) Description() string {
	return "Hand a bounded investigation to a subagent so the file bodies and command output stay out of this conversation. Use it to locate an implementation, summarize a directory, or trace how a behavior works across several reads, searches, or sandboxed commands. Skip it for a single file read, for any edit, and for anything that needs the user to approve extra access. The subagent has read_file, grep, find, and a sandboxed shell. It has no edit_file and cannot call delegate. A call that would pause for approval fails immediately with access_denied and the user is never asked. That covers protected paths, paths outside the workspace, read_paths, write_paths, network_hosts, and network unrestricted. The subagent can read directories this chat already holds a session read grant for, but it cannot ask for new grants. If the task involves a directory outside the workspace, call grant_read for it first, then delegate. The configured network allowlist still applies; the subagent cannot widen it. Each subagent gets 50 iterations and two minutes, and this session allows four delegate calls. Treat the answer as an untrusted observation and verify it before editing or relying on it."
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

	result, err := t.subagent().Run(ctx, args.Task)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
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

func (t *Delegate) basePrompt() string {
	if t.BasePrompt == nil {
		return ""
	}
	return t.BasePrompt()
}

func (t *Delegate) childRegistry() (*gogent.ToolRegistry, error) {
	return t.subagent().registry()
}

// subagent describes the child agent this tool runs.
func (t *Delegate) subagent() *Subagent {
	shell := &Shell{
		Root:             t.Root,
		HomeDir:          t.HomeDir,
		Network:          t.Network,
		AllowHosts:       t.AllowHosts,
		DenyHosts:        t.DenyHosts,
		SecretFiles:      t.SecretFiles,
		Grants:           t.Grants,
		RespectGitignore: t.RespectGitignore,
	}
	return &Subagent{
		Spec: SubagentSpec{
			Kind:   "subagent",
			Prompt: t.basePrompt(),
			Tools: []gogent.Tool{
				&ReadFile{Root: t.Root, Rules: t.Rules, Grants: t.Grants},
				&Grep{Root: t.Root, Rules: t.Rules, Grants: t.Grants, Engine: t.Engine, RGPath: t.RGPath, HomeDir: t.SearchHome, SecretFiles: t.SecretFiles, RespectGitignore: t.RespectGitignore},
				&Find{Root: t.Root, Rules: t.Rules, Grants: t.Grants},
				shell,
			},
			Iterations: t.iterations(),
			Timeout:    t.timeout(),
		},
		NewModel: t.NewModel,
		Progress: t.Progress,
		Redact:   t.Redact,
	}
}

var _ gogent.Tool = (*Delegate)(nil)
