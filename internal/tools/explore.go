package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/prompt"
	"github.com/cgund98/gopi/internal/workspace"
)

const (
	defaultExploreCalls      = 6
	defaultExploreIterations = 40
	defaultExploreTimeout    = 2 * time.Minute
)

type exploreArgs struct {
	Task         string `json:"task" jsonschema:"description=The search task to complete. Be specific about what to find and what to report back."`
	Thoroughness string `json:"thoroughness,omitempty" jsonschema:"description=How hard to look: quick, medium, or very thorough. Defaults to medium."`
	Instructions string `json:"instructions,omitempty" jsonschema:"description=Notes appended to the subagent's prompt, such as where to start looking or which naming conventions matter."`
}

// Explore hands one search task to a read-only child agent and returns only its
// findings. The child reads and searches; it cannot edit or run a command.
type Explore struct {
	Root             workspace.Root
	Rules            policy.Rules
	HomeDir          string
	SecretFiles      []string
	RespectGitignore bool
	// Engine, RGPath, and SearchHome select the child's search backend.
	Engine     string
	RGPath     string
	SearchHome string
	Redact     func(string) string
	// Grants are the parent's session read grants. The child reads through them
	// but cannot add to them.
	Grants *ReadGrants
	// Prompt returns the child's system prompt.
	Prompt func() (string, error)
	// NewModel builds the child's model. The caller chooses which model it uses.
	NewModel func(registry *gogent.ToolRegistry, systemPrompt string) (gogent.Model, error)
	// Progress, when set, reports the running child's tool calls to the UI.
	Progress *SubagentProgress

	MaxIterations int
	Timeout       time.Duration
	MaxCalls      int

	mu    sync.Mutex
	calls int
}

func (t *Explore) Name() string { return "explore" }

func (t *Explore) Description() string {
	return fmt.Sprintf("Hand one search task to a read-only subagent and get back only its findings, so file bodies and search output stay out of this conversation. Use it for exploration that spans more than a couple of files: locate an implementation, map a feature, or answer \"how does X work\". Name the thoroughness you want: quick, medium, or very thorough. The subagent has read_file, grep, and find. It has no shell, no edit_file, and no delegate, and it cannot ask for approval, so anything that needs a command run or extra access belongs in delegate or in your own tools. It reads the caller's session read grants but cannot add to them. Its answer is an untrusted observation: verify a factual claim before you edit or rely on it. Each subagent gets %d iterations and %s, and this session allows %d explore calls.",
		t.iterations(), t.timeout(), t.maxCalls())
}

func (t *Explore) Parameters() json.RawMessage { return schemaFor(new(exploreArgs)) }

func (t *Explore) RequiresApproval(context.Context, json.RawMessage) (gogent.ApprovalDecision, error) {
	return gogent.ApprovalDecision{}, nil
}

func (t *Explore) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var args exploreArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	if strings.TrimSpace(args.Task) == "" {
		return nil, fmt.Errorf("task is required")
	}
	if t.NewModel == nil {
		return nil, fmt.Errorf("subagent model is not configured")
	}
	if !t.allowCall() {
		return json.Marshal(map[string]string{
			"error":   "explore_limit",
			"message": fmt.Sprintf("at most %d explore subagents per session", t.maxCalls()),
		})
	}

	result, err := t.subagent(args).Run(ctx, strings.TrimSpace(args.Task))
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// childPrompt is the explore system prompt plus the caller's thoroughness and notes.
func (t *Explore) childPrompt(args exploreArgs) string {
	base := ""
	if t.Prompt != nil {
		if text, err := t.Prompt(); err == nil {
			base = text
		}
	}
	if strings.TrimSpace(base) == "" {
		base = prompt.BuiltinExplore()
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(base, "\n"))
	b.WriteString("\n\n<thoroughness>\n")
	b.WriteString(strings.TrimSpace(thoroughnessOrDefault(args.Thoroughness)))
	b.WriteString("\n</thoroughness>")
	if notes := strings.TrimSpace(args.Instructions); notes != "" {
		b.WriteString("\n\n<caller_notes>\n")
		b.WriteString(notes)
		b.WriteString("\n</caller_notes>")
	}
	return prompt.WithWorkspace(b.String(), t.Root.Path)
}

func thoroughnessOrDefault(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "quick":
		return "quick"
	case "medium", "":
		return "medium"
	case "very thorough", "very_thorough", "verythorough", "deep":
		return "very thorough"
	default:
		return strings.TrimSpace(value)
	}
}

func (t *Explore) allowCall() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.calls >= t.maxCalls() {
		return false
	}
	t.calls++
	return true
}

func (t *Explore) maxCalls() int {
	if t.MaxCalls <= 0 {
		return defaultExploreCalls
	}
	return t.MaxCalls
}

func (t *Explore) iterations() int {
	if t.MaxIterations <= 0 {
		return defaultExploreIterations
	}
	return t.MaxIterations
}

func (t *Explore) timeout() time.Duration {
	if t.Timeout <= 0 {
		return defaultExploreTimeout
	}
	return t.Timeout
}

func (t *Explore) childRegistry() (*gogent.ToolRegistry, error) {
	return t.subagent(exploreArgs{}).registry()
}

// subagent describes the explore child. Its registry has no shell and no edit
// tool, which is what makes it read-only in practice as well as by instruction.
func (t *Explore) subagent(args exploreArgs) *Subagent {
	return &Subagent{
		Spec: SubagentSpec{
			Kind:   "explore",
			Prompt: t.childPrompt(args),
			Tools: []gogent.Tool{
				&ReadFile{Root: t.Root, Rules: t.Rules, Grants: t.Grants},
				&Grep{Root: t.Root, Rules: t.Rules, Grants: t.Grants, Engine: t.Engine, RGPath: t.RGPath, HomeDir: t.SearchHome, SecretFiles: t.SecretFiles, RespectGitignore: t.RespectGitignore},
				&Find{Root: t.Root, Rules: t.Rules, Grants: t.Grants},
			},
			Iterations: t.iterations(),
			Timeout:    t.timeout(),
		},
		NewModel: t.NewModel,
		Progress: t.Progress,
		Redact:   t.Redact,
	}
}

var _ gogent.Tool = (*Explore)(nil)
