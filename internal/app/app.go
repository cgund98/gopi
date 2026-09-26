package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/inmemory"
	"github.com/cgund98/gogent/openai"

	"github.com/cgund98/gopi/internal/config"
	"github.com/cgund98/gopi/internal/models"
	"github.com/cgund98/gopi/internal/policy"
	"github.com/cgund98/gopi/internal/prompt"
	gopisecrets "github.com/cgund98/gopi/internal/secrets"
	"github.com/cgund98/gopi/internal/tools"
	"github.com/cgund98/gopi/internal/toolview"
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
	Tasks     *tools.TaskList
	Grants    *tools.ReadGrants
	Mode      Mode
	// Subagent reports a running delegate call.
	Subagent *tools.DelegateProgress
	// Renderers holds custom tools that implement toolview.Renderer, by name, across all modes.
	Renderers map[string]toolview.Renderer
	// Redact removes secret values from text shown in the UI.
	Redact func(string) string

	registries map[Mode]*gogent.ToolRegistry
	extra      map[Mode][]gogent.Tool
	models     *modelFactory
	overrides  map[Mode]string
	efforts    map[Mode]string
	active     string
	basePrompt string
}

// New builds the agent, registry, and in-memory transcript for one run.
// extra adds tools to a mode. A name that matches a built-in tool is an error.
// Extra tools are not registered on the delegate child.
func New(cfg config.Config, root workspace.Root, workspaceTrust trust.Workspace, extra map[Mode][]gogent.Tool) (*Session, error) {
	secretFiles := make([]string, 0, len(cfg.SecretFiles))
	for _, path := range cfg.SecretFiles {
		secretFiles = append(secretFiles, path)
	}
	sort.Strings(secretFiles)
	rules, err := policy.Build(root.Path, cfg.HomeDir, "", secretFiles...)
	if err != nil {
		return nil, fmt.Errorf("build path policy: %w", err)
	}
	edit := &tools.EditFile{Root: root, Workspace: workspaceTrust, Rules: rules}
	plan := &tools.WritePlan{Root: root, Workspace: workspaceTrust}
	grants := &tools.ReadGrants{}
	for _, dir := range prompt.SkillRoots(promptOptions(cfg, root.Path, workspaceTrust)) {
		grants.Add(dir)
	}
	read := []gogent.Tool{
		&tools.ReadFile{Root: root, Rules: rules, Grants: grants},
		&tools.Grep{Root: root, Rules: rules, Grants: grants},
		&tools.Find{Root: root, Rules: rules, Grants: grants},
		&tools.GrantRead{Root: root, Grants: grants},
	}
	shell := &tools.Shell{Root: root, HomeDir: cfg.HomeDir, Network: cfg.Network, AllowHosts: cfg.AllowHosts, DenyHosts: cfg.DenyHosts, Secrets: cfg.Secrets, Grants: grants, SecretFiles: secretFiles, HostOnly: cfg.HostOnly}
	search := &tools.WebSearch{Endpoint: cfg.SearchEndpoint, APIKey: cfg.Secrets[gopisecrets.SearchAPIKey]}
	fetch := &tools.WebFetch{}
	taskList := tools.NewTaskList(root)
	tasks := &tools.Tasks{List: taskList}
	redact := gopisecrets.NewRedactor(cfg.Secrets).Apply

	text, err := prompt.Assemble(promptOptions(cfg, root.Path, workspaceTrust))
	if err != nil {
		return nil, fmt.Errorf("assemble prompt: %w", err)
	}
	session := &Session{
		Root:       root,
		Workspace:  workspaceTrust,
		Config:     cfg,
		Edit:       edit,
		WritePlan:  plan,
		Tasks:      taskList,
		Grants:     grants,
		Mode:       ModeAgent,
		Subagent:   &tools.DelegateProgress{},
		Renderers:  collectRenderers(extra),
		Redact:     redact,
		registries: map[Mode]*gogent.ToolRegistry{},
		extra:      extra,
		models:     newModelFactory(cfg),
		overrides:  map[Mode]string{},
		efforts:    map[Mode]string{},
		basePrompt: text,
	}
	delegate := &tools.Delegate{
		Root:        root,
		Rules:       rules,
		HomeDir:     cfg.HomeDir,
		Network:     cfg.Network,
		AllowHosts:  cfg.AllowHosts,
		DenyHosts:   cfg.DenyHosts,
		SecretFiles: secretFiles,
		Redact:      redact,
		Grants:      grants,
		Progress:    session.Subagent,
		NewModel: func(registry *gogent.ToolRegistry) (gogent.Model, error) {
			return session.models.New(session.active, registry, session.basePrompt, session.effortFor(session.Mode))
		},
	}
	agentTools := append(append([]gogent.Tool{}, read...), shell, edit, delegate, search, fetch, tasks, &tools.UpdatePlan{Inner: plan})
	askTools := append(append([]gogent.Tool{}, read...), search, fetch)
	planTools := append(append([]gogent.Tool{}, read...), plan, search, fetch)
	agentTools = append(agentTools, extra[ModeAgent]...)
	askTools = append(askTools, extra[ModeAsk]...)
	planTools = append(planTools, extra[ModePlan]...)
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

// ExtraTools returns tools added for each mode so a rebuilt session keeps them.
func (s *Session) ExtraTools() map[Mode][]gogent.Tool {
	return s.extra
}

// collectRenderers must run on the unwrapped tools: WrapRedacting hides the interface.
func collectRenderers(extra map[Mode][]gogent.Tool) map[string]toolview.Renderer {
	out := map[string]toolview.Renderer{}
	for _, list := range extra {
		for _, tool := range list {
			if renderer, ok := tool.(toolview.Renderer); ok {
				out[tool.Name()] = renderer
			}
		}
	}
	return out
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

// ActiveModel is the model name of the current turn.
func (s *Session) ActiveModel() string {
	return s.active
}

// ActiveEffort is the effort in effect for the active mode and model.
func (s *Session) ActiveEffort() string {
	return s.effortFor(s.Mode)
}

// ModelOverrides returns the per-mode names chosen with /model.
func (s *Session) ModelOverrides() map[string]string {
	if len(s.overrides) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.overrides))
	for mode, name := range s.overrides {
		out[string(mode)] = name
	}
	return out
}

// SetModelOverrides restores names chosen in an earlier session.
// A name no longer in the catalog is dropped so the mode falls back to config.
func (s *Session) SetModelOverrides(overrides map[string]string) {
	s.overrides = map[Mode]string{}
	for mode, name := range overrides {
		if _, _, err := models.Parse(name); err != nil {
			continue
		}
		s.overrides[Mode(mode)] = name
	}
}

// EffortOverrides returns the per-mode effort values chosen with /effort.
func (s *Session) EffortOverrides() map[string]string {
	if len(s.efforts) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.efforts))
	for mode, effort := range s.efforts {
		out[string(mode)] = effort
	}
	return out
}

// SetEffortOverrides restores effort values chosen in an earlier session.
func (s *Session) SetEffortOverrides(overrides map[string]string) {
	s.efforts = map[Mode]string{}
	for mode, effort := range overrides {
		s.efforts[Mode(mode)] = effort
	}
}

// SetMode switches the registry, prompt prefix, and that mode's model.
func (s *Session) SetMode(mode Mode) error {
	switch mode {
	case ModeAgent, ModeAsk, ModePlan:
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
	return s.apply(mode, s.modelFor(mode))
}

// SetModel sets the model used by the active mode for the rest of this session.
func (s *Session) SetModel(name string) error {
	if _, _, err := models.Parse(name); err != nil {
		return err
	}
	if s.overrides == nil {
		s.overrides = map[Mode]string{}
	}
	s.overrides[s.Mode] = name
	return s.apply(s.Mode, name)
}

// SetEffort sets the effort used by the active mode for the rest of this session.
func (s *Session) SetEffort(effort string) error {
	if s.efforts == nil {
		s.efforts = map[Mode]string{}
	}
	s.efforts[s.Mode] = effort
	return s.apply(s.Mode, s.active)
}

// SetBuild selects the agent registry and the build model for one plan turn.
func (s *Session) SetBuild() error {
	name := s.Config.BuildModel
	if name == "" {
		name = s.modelFor(ModeAgent)
	}
	return s.apply(ModeAgent, name)
}

func (s *Session) modelFor(mode Mode) string {
	if name := s.overrides[mode]; name != "" {
		return name
	}
	return s.Config.ModelFor(string(mode))
}

func (s *Session) effortFor(mode Mode) string {
	if effort := s.efforts[mode]; effort != "" {
		return effort
	}
	return s.Config.EffortFor(string(mode))
}

func (s *Session) apply(mode Mode, name string) error {
	registry := s.registries[mode]
	model, err := s.models.New(name, registry, prompt.WithMode(s.basePrompt, string(mode)), s.effortFor(mode))
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
	s.active = name
	s.Registry = registry
	s.Model = model
	s.Agent = gogent.NewAgent(s.Store, s.Events, model, registry, s.Config.MaxIterations)
	return nil
}

// ChatTitle asks the model for a short title and sends no tools.
func (s *Session) ChatTitle(ctx context.Context, userText, assistantText string) (string, error) {
	model, err := s.models.New(s.active, gogent.NewToolRegistry(), "Reply with a short chat title of at most 6 words and nothing else.", s.effortFor(s.Mode))
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

// Summarize asks the active model for a summary of earlier turns. The reply carries that turn's usage.
func (s *Session) Summarize(ctx context.Context, transcript string) (gogent.Message, error) {
	model, err := s.models.New(s.active, gogent.NewToolRegistry(), "Summarize the earlier conversation. Keep decisions, file paths, and unfinished work. Reply with the summary only.", s.effortFor(s.Mode))
	if err != nil {
		return gogent.Message{}, err
	}
	message, err := model.GenerateResponse(ctx, []gogent.Message{
		gogent.NewUserMessage(transcript),
	})
	if err != nil {
		return gogent.Message{}, err
	}
	message.Content = "Summary of earlier turns:\n" + strings.TrimSpace(message.Content)
	return message, nil
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
