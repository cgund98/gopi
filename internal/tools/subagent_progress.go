package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/cgund98/gogent"
)

// SubagentProgress is what the UI shows while a child agent runs. It is safe to
// read from another goroutine.
type SubagentProgress struct {
	mu      sync.Mutex
	running bool
	status  SubagentStatus
}

// SubagentStatus is one snapshot of a running child agent.
type SubagentStatus struct {
	// Kind is "subagent" for delegate and "explore" for the explore tool.
	Kind  string
	Task  string
	Query string
	// Started is when the child began.
	Started time.Time
	// ToolCalls counts every tool call the child made.
	ToolCalls int
	// Searches counts the child's grep and find calls.
	Searches int
	// Last describes the most recent tool call, for example "grep resume".
	Last string
}

// Snapshot returns the running child's status. ok is false when none is running.
func (p *SubagentProgress) Snapshot() (status SubagentStatus, ok bool) {
	if p == nil {
		return SubagentStatus{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status, p.running
}

func (p *SubagentProgress) begin(kind, task string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = true
	p.status = SubagentStatus{Kind: kind, Task: task, Started: time.Now()}
}

func (p *SubagentProgress) finish() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = false
	p.status = SubagentStatus{}
}

func (p *SubagentProgress) toolCalled(name string, args json.RawMessage) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.ToolCalls++
	if name == "grep" || name == "find" {
		p.status.Searches++
	}
	if query := searchQuery(args); query != "" {
		p.status.Query = query
	}
	p.status.Last = activityLabel(name, args)
}

func (p *SubagentProgress) start(task string) {
	p.begin("subagent", task)
}

// progressTool records each child tool call before it runs.
type progressTool struct {
	gogent.Tool
	progress *SubagentProgress
}

func (t progressTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	t.progress.toolCalled(t.Name(), args)
	return t.Tool.Execute(ctx, args)
}

func activityLabel(name string, raw json.RawMessage) string {
	var args struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
		Command string `json:"command"`
	}
	_ = json.Unmarshal(raw, &args)
	var label string
	switch name {
	case "read_file":
		label = "read " + args.Path
	case "grep":
		label = "grep " + args.Pattern
	case "find":
		label = "find " + firstNonEmptyString(args.Pattern, args.Path)
	case "shell":
		label = "$ " + args.Command
	default:
		label = name
	}
	return strings.Join(strings.Fields(label), " ")
}

// searchQuery is the pattern a grep or find call searched for, if any.
func searchQuery(raw json.RawMessage) string {
	var args struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(args.Pattern), " ")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
