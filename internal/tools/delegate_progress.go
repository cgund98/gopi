package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/cgund98/gogent"
)

// DelegateProgress is what the UI shows while a subagent runs. It is safe to read
// from another goroutine.
type DelegateProgress struct {
	mu      sync.Mutex
	running bool
	status  DelegateStatus
}

// DelegateStatus is one snapshot of a running subagent.
type DelegateStatus struct {
	Task      string
	Started   time.Time
	ToolCalls int
	// Last describes the most recent tool call, for example "grep resume".
	Last string
}

// Snapshot returns the running subagent's status. ok is false when none is running.
func (p *DelegateProgress) Snapshot() (status DelegateStatus, ok bool) {
	if p == nil {
		return DelegateStatus{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status, p.running
}

func (p *DelegateProgress) start(task string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = true
	p.status = DelegateStatus{Task: task, Started: time.Now()}
}

func (p *DelegateProgress) finish() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running = false
	p.status = DelegateStatus{}
}

func (p *DelegateProgress) toolCalled(name string, args json.RawMessage) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status.ToolCalls++
	p.status.Last = activityLabel(name, args)
}

// progressTool records each child tool call before it runs.
type progressTool struct {
	gogent.Tool
	progress *DelegateProgress
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
