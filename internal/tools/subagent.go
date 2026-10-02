package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/inmemory"
	"github.com/google/uuid"
)

const (
	defaultChildIterations = 50
	defaultChildTimeout    = 2 * time.Minute
)

// SubagentSpec is the fixed configuration of one child agent.
type SubagentSpec struct {
	// Kind names the child for the UI: "subagent" or "explore".
	Kind string
	// Prompt is the child's system prompt. The task is appended to it.
	Prompt string
	// Tools are the child's tools, before the wrappers the runner adds.
	Tools []gogent.Tool
	// Iterations caps the child's model turns.
	Iterations int
	// Timeout caps the child's wall-clock time.
	Timeout time.Duration
}

// SubagentResult is the single message a child returns to its parent.
type SubagentResult struct {
	Kind      string   `json:"kind"`
	Answer    string   `json:"answer"`
	ToolCalls int      `json:"tool_calls"`
	Denied    []string `json:"denied"`
}

// Subagent runs one child agent and returns only its final answer.
// The child cannot edit, cannot pause for approval, and cannot widen any limit.
type Subagent struct {
	Spec SubagentSpec
	// NewModel builds the child's model with the given system prompt.
	NewModel func(registry *gogent.ToolRegistry, systemPrompt string) (gogent.Model, error)
	// Progress, when set, reports the running child's tool calls to the UI.
	Progress *SubagentProgress
	// Redact removes secret values from the child's context and result.
	Redact func(string) string
}

// Run starts the child, waits for it, and summarizes its transcript.
func (s *Subagent) Run(ctx context.Context, task string) (SubagentResult, error) {
	if s.NewModel == nil {
		return SubagentResult{}, fmt.Errorf("subagent model is not configured")
	}
	registry, err := s.registry()
	if err != nil {
		return SubagentResult{}, err
	}
	s.Progress.begin(s.kind(), task)
	defer s.Progress.finish()
	model, err := s.NewModel(registry, s.Spec.Prompt)
	if err != nil {
		return SubagentResult{}, fmt.Errorf("build subagent model: %w", err)
	}
	store := inmemory.NewMessageStore()
	agent := gogent.NewAgent(store, gogent.NopBroadcaster{}, model, registry, s.iterations())
	chatID := uuid.NewString()
	runCtx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()
	if err := agent.RunWithUserInput(runCtx, chatID, task); err != nil {
		return SubagentResult{}, err
	}
	messages, err := store.Load(ctx, chatID)
	if err != nil {
		return SubagentResult{}, fmt.Errorf("load subagent transcript: %w", err)
	}
	answer, calls, denied := summarizeChild(messages)
	return SubagentResult{Kind: s.kind(), Answer: answer, ToolCalls: calls, Denied: denied}, nil
}

// registry builds the child's registry with the wrappers every child gets: a call
// that would need approval fails closed instead, secrets are redacted, and each
// call is reported to the UI.
func (s *Subagent) registry() (*gogent.ToolRegistry, error) {
	registry := gogent.NewToolRegistry()
	for _, tool := range s.Spec.Tools {
		wrapped := progressTool{Tool: WrapRedacting(failClosed{inner: tool}, s.Redact), progress: s.Progress}
		if err := registry.RegisterTool(wrapped); err != nil {
			return nil, fmt.Errorf("register subagent tool %s: %w", tool.Name(), err)
		}
	}
	return registry, nil
}

func (s *Subagent) kind() string {
	if s.Spec.Kind == "" {
		return "subagent"
	}
	return s.Spec.Kind
}

func (s *Subagent) iterations() int {
	if s.Spec.Iterations <= 0 {
		return defaultChildIterations
	}
	return s.Spec.Iterations
}

func (s *Subagent) timeout() time.Duration {
	if s.Spec.Timeout <= 0 {
		return defaultChildTimeout
	}
	return s.Spec.Timeout
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
