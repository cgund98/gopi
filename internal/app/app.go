package app

import (
	"fmt"

	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/inmemory"
	"github.com/cgund98/gogent/openai"
	"github.com/cgund98/gopi/internal/config"
	"github.com/cgund98/gopi/internal/prompt"
	"github.com/cgund98/gopi/internal/tools"
	"github.com/cgund98/gopi/internal/trust"
	"github.com/cgund98/gopi/internal/workspace"
)

// Session is one workspace chat wired to a gogent agent.
type Session struct {
	Agent     *gogent.Agent
	Store     gogent.MessageStore
	Registry  *gogent.ToolRegistry
	Events    *gogent.ChannelBroadcaster
	Root      workspace.Root
	Workspace trust.Workspace
	Config    config.Config
	Edit      *tools.EditFile
}

// New builds the agent, registry, and in-memory transcript for one run.
func New(cfg config.Config, root workspace.Root, workspaceTrust trust.Workspace) (*Session, error) {
	if cfg.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}

	registry := gogent.NewToolRegistry()
	edit := &tools.EditFile{Root: root, Workspace: workspaceTrust}
	fileTools := []gogent.Tool{
		&tools.ReadFile{Root: root},
		&tools.Grep{Root: root},
		&tools.Find{Root: root},
		edit,
	}
	for _, tool := range fileTools {
		if err := registry.RegisterTool(tool); err != nil {
			return nil, fmt.Errorf("register tool %s: %w", tool.Name(), err)
		}
	}

	client := openaisdk.NewClient(option.WithAPIKey(cfg.OpenAIAPIKey))
	model, err := openai.NewChat(&client, registry).
		WithModel(cfg.Model).
		WithSystemPrompt(prompt.WithWorkspace(cfg.SystemPrompt, root.Path)).
		Build()
	if err != nil {
		return nil, fmt.Errorf("build model: %w", err)
	}

	events := gogent.NewChannelBroadcaster()
	store := inmemory.NewMessageStore()
	agent := gogent.NewAgent(store, events, model, registry, cfg.MaxIterations)
	return &Session{
		Agent:     agent,
		Store:     store,
		Registry:  registry,
		Events:    events,
		Root:      root,
		Workspace: workspaceTrust,
		Config:    cfg,
		Edit:      edit,
	}, nil
}
