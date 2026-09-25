package app

import (
	"context"
	"fmt"
	"strings"

	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/inmemory"
	"github.com/cgund98/gogent/openai"
	"github.com/cgund98/gopi/internal/config"
	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/prompt"
	gopisecrets "github.com/cgund98/gopi/internal/secrets"
	"github.com/cgund98/gopi/internal/tools"
	"github.com/cgund98/gopi/internal/trust"
	"github.com/cgund98/gopi/internal/workspace"
)

// Mode is the active interaction mode. It selects a tool registry and a prompt prefix.
type Mode string

const (
	ModeAgent Mode = "agent"
	ModeAsk   Mode = "ask"
	ModePlan  Mode = "plan"
)

// Session is one workspace chat wired to a gogent agent.
type Session struct {
	Agent     *gogent.Agent
	Model     *openai.Model
	Store     gogent.MessageStore
	Registry  *gogent.ToolRegistry
	Events    *gogent.ChannelBroadcaster
	Root      workspace.Root
	Workspace trust.Workspace
	Config    config.Config
	Edit      *tools.EditFile
	WritePlan *tools.WritePlan
	Mode      Mode

	registries map[Mode]*gogent.ToolRegistry
	client     openaisdk.Client
	basePrompt string
}

// New builds the agent, registry, and in-memory transcript for one run.
func New(cfg config.Config, root workspace.Root, workspaceTrust trust.Workspace) (*Session, error) {
	if cfg.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}

	rules, err := policy.Build(root.Path, cfg.HomeDir, "")
	if err != nil {
		return nil, fmt.Errorf("build path policy: %w", err)
	}
	edit := &tools.EditFile{Root: root, Workspace: workspaceTrust, Rules: rules}
	plan := &tools.WritePlan{Root: root, Workspace: workspaceTrust}
	read := []gogent.Tool{
		&tools.ReadFile{Root: root, Rules: rules},
		&tools.Grep{Root: root, Rules: rules},
		&tools.Find{Root: root, Rules: rules},
	}
	shell := &tools.Shell{Root: root, HomeDir: cfg.HomeDir, Network: cfg.Network, AllowHosts: cfg.AllowHosts, DenyHosts: cfg.DenyHosts}
	search := &tools.WebSearch{Endpoint: cfg.SearchEndpoint, APIKey: cfg.Secrets["search_api_key"]}
	redact := gopisecrets.NewRedactor(cfg.Secrets).Apply

	text, err := prompt.Assemble(promptOptions(cfg, root.Path, workspaceTrust))
	if err != nil {
		return nil, fmt.Errorf("assemble prompt: %w", err)
	}
	client := openaisdk.NewClient(option.WithAPIKey(cfg.OpenAIAPIKey))
	session := &Session{
		Root:       root,
		Workspace:  workspaceTrust,
		Config:     cfg,
		Edit:       edit,
		WritePlan:  plan,
		Mode:       ModeAgent,
		registries: map[Mode]*gogent.ToolRegistry{},
		client:     client,
		basePrompt: text,
	}
	delegate := &tools.Delegate{
		Root:       root,
		Rules:      rules,
		HomeDir:    cfg.HomeDir,
		Network:    cfg.Network,
		AllowHosts: cfg.AllowHosts,
		DenyHosts:  cfg.DenyHosts,
		Redact:     redact,
		NewModel: func(registry *gogent.ToolRegistry) (gogent.Model, error) {
			return openai.NewChat(&session.client, registry).
				WithModel(cfg.Model).
				WithSystemPrompt(session.basePrompt).
				Build()
		},
	}
	agentTools := append(append([]gogent.Tool{}, read...), shell, edit, delegate, search)
	askTools := append(append([]gogent.Tool{}, read...), search)
	planTools := append(append([]gogent.Tool{}, read...), plan, search)
	for mode, list := range map[Mode][]gogent.Tool{
		ModeAgent: agentTools,
		ModeAsk:   askTools,
		ModePlan:  planTools,
	} {
		registry, err := registerTools(list, redact)
		if err != nil {
			return nil, err
		}
		session.registries[mode] = registry
	}

	events := gogent.NewChannelBroadcaster()
	store := inmemory.NewMessageStore()
	session.Events = events
	session.Store = store
	if err := session.SetMode(ModeAgent); err != nil {
		return nil, err
	}
	return session, nil
}

func registerTools(list []gogent.Tool, redact func(string) string) (*gogent.ToolRegistry, error) {
	registry := gogent.NewToolRegistry()
	for _, tool := range list {
		if err := registry.RegisterTool(tools.WrapRedacting(tool, redact)); err != nil {
			return nil, fmt.Errorf("register tool %s: %w", tool.Name(), err)
		}
	}
	return registry, nil
}

// SetMode switches the registry and prompt prefix. The transcript stays on the same store.
func (s *Session) SetMode(mode Mode) error {
	switch mode {
	case ModeAgent, ModeAsk, ModePlan:
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
	registry := s.registries[mode]
	model, err := openai.NewChat(&s.client, registry).
		WithModel(s.Config.Model).
		WithSystemPrompt(prompt.WithMode(s.basePrompt, string(mode))).
		Build()
	if err != nil {
		return fmt.Errorf("build model: %w", err)
	}
	if s.Store == nil {
		s.Store = inmemory.NewMessageStore()
	}
	if s.Events == nil {
		s.Events = gogent.NewChannelBroadcaster()
	}
	s.Mode = mode
	s.Registry = registry
	s.Model = model
	s.Agent = gogent.NewAgent(s.Store, s.Events, model, registry, s.Config.MaxIterations)
	return nil
}

// ChatTitle asks the model for a short title and sends no tools.
func (s *Session) ChatTitle(ctx context.Context, userText, assistantText string) (string, error) {
	model, err := openai.NewChat(&s.client, gogent.NewToolRegistry()).
		WithModel(s.Config.Model).
		WithSystemPrompt("Reply with a short chat title of at most 6 words and nothing else.").
		Build()
	if err != nil {
		return "", err
	}
	message, err := model.GenerateResponse(ctx, []gogent.Message{
		gogent.NewUserMessage("User:\n" + userText + "\n\nAssistant:\n" + assistantText),
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(message.Content), nil
}

// ParseModeCommand reports whether text is a mode command and which mode it names.
// command is true for /agent, /ask, /plan, and /mode. ok is false when /mode has no known target.
func ParseModeCommand(text string) (mode Mode, command bool, ok bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", false, false
	}
	switch fields[0] {
	case "/agent", "/ask", "/plan":
		if len(fields) != 1 {
			return "", false, false
		}
		return Mode(strings.TrimPrefix(fields[0], "/")), true, true
	case "/mode":
		if len(fields) != 2 {
			return "", true, false
		}
		switch Mode(fields[1]) {
		case ModeAgent, ModeAsk, ModePlan:
			return Mode(fields[1]), true, true
		default:
			return "", true, false
		}
	default:
		return "", false, false
	}
}

// RefreshPrompt rebuilds the system prompt after the workspace trust decision changes.
func (s *Session) RefreshPrompt() error {
	text, err := prompt.Assemble(promptOptions(s.Config, s.Root.Path, s.Workspace))
	if err != nil {
		return err
	}
	s.basePrompt = text
	if s.Model == nil {
		return nil
	}
	s.Model.SetSystemPrompt(prompt.WithMode(text, string(s.Mode)))
	return nil
}

func promptOptions(cfg config.Config, workspace string, workspaceTrust trust.Workspace) prompt.Options {
	return prompt.Options{
		Base:          prompt.Builtin(),
		HomeDir:       cfg.HomeDir,
		Workspace:     workspace,
		Trusted:       workspaceTrust == trust.WorkspaceTrusted,
		UserPrompt:    cfg.UserPrompt,
		MaxBytes:      cfg.ProjectDocMaxBytes,
		SkillDirs:     cfg.SkillDirs,
		FallbackFiles: cfg.FallbackFiles,
	}
}
