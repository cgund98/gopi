package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gopi/internal/app"
	gopisecrets "github.com/cgund98/gopi/internal/secrets"
	sess "github.com/cgund98/gopi/internal/session"
	"github.com/cgund98/gopi/internal/trust"
	"github.com/cgund98/gopi/internal/workspace"
)

// Run starts the terminal UI. An unknown workspace asks for trust before the chat.
func Run(ctx context.Context, session *app.Session, decisions *trust.Store) error {
	model := newProgram(ctx, session, decisions)
	program := tea.NewProgram(model, tea.WithAltScreen())
	_, err := program.Run()
	return err
}

type phase int

const (
	phaseTrust phase = iota
	phaseChat
)

type programModel struct {
	phase     phase
	session   *app.Session
	decisions *trust.Store
	chat      *chatModel
	width     int
	height    int
	haveSize  bool
	err       error
}

func newProgram(ctx context.Context, session *app.Session, decisions *trust.Store) *programModel {
	model := &programModel{
		session:   session,
		decisions: decisions,
		chat:      newChatModel(ctx, session.Agent, session.Store, session.Registry, session.Events),
	}
	model.bindChat(session)
	if store, err := sess.Open(session.Config.HomeDir); err == nil {
		model.chat.sessions = store
	}
	model.chat.chatTitle = func(ctx context.Context, userText, assistantText string) (string, error) {
		return model.session.ChatTitle(ctx, userText, assistantText)
	}
	model.chat.loadSession = model.loadSession
	model.chat.applyLoaded = model.applyLoaded
	if session.Workspace == trust.WorkspaceUnknown {
		model.phase = phaseTrust
		return model
	}
	model.phase = phaseChat
	return model
}

func (m *programModel) Init() tea.Cmd {
	if m.phase == phaseChat {
		return m.chat.Init()
	}
	return nil
}

func (m *programModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
		m.haveSize = true
	}
	if m.phase == phaseTrust {
		return m.updateTrust(msg)
	}
	next, cmd := m.chat.Update(msg)
	m.chat = next.(*chatModel)
	return m, cmd
}

func (m *programModel) updateTrust(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "y", "Y":
		return m, m.chooseTrust(true)
	case "n", "N":
		return m, m.chooseTrust(false)
	default:
		return m, nil
	}
}

func (m *programModel) bindChat(session *app.Session) {
	m.session = session
	m.chat.agent = session.Agent
	m.chat.store = session.Store
	m.chat.registry = session.Registry
	m.chat.events = session.Events
	m.chat.modelName = session.Config.Model
	m.chat.workspacePath = session.Root.Path
	m.chat.mode = session.Mode
	m.chat.input.Prompt = modePrompt(session.Mode)
	m.chat.switchMode = func(mode app.Mode) error {
		if err := m.session.SetMode(mode); err != nil {
			return err
		}
		m.chat.agent = m.session.Agent
		m.chat.registry = m.session.Registry
		if m.session.Model != nil {
			m.chat.systemPrompt = m.session.Model.SystemPrompt()
		}
		return nil
	}
	if session.Model != nil {
		m.chat.systemPrompt = session.Model.SystemPrompt()
	}
	m.chat.secretNames = gopisecrets.OfferNames(session.Config.Secrets)
}

func awaitSession(events <-chan tea.Msg) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-events
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *programModel) loadSession(file *sess.File) tea.Cmd {
	width := m.chat.transcriptVP.Width
	if width < 20 {
		width = m.chat.width
	}
	events := make(chan tea.Msg)
	m.chat.sessionEvents = events
	m.chat.sessionStatus = "Opening session"
	go m.runLoad(events, file, width)
	return awaitSession(events)
}

func (m *programModel) runLoad(events chan tea.Msg, file *sess.File, width int) {
	defer close(events)
	report := func(text string) {
		events <- sessionProgressMsg{Text: text}
	}
	render := func(messages []gogent.Message, registry *gogent.ToolRegistry) string {
		report("Preparing markdown")
		prepareMarkdown(width)
		cards := buildToolCards(messages, registry)
		return renderTranscriptProgress(messages, cards, -1, width, func(done, total int) {
			report(fmt.Sprintf("Rendering message %d of %d", done, total))
		})
	}
	if file == nil && m.chat.sessionsHome == m.session.Root.Path {
		events <- sessionLoadedMsg{Fresh: true, Same: true, Body: helpStyle.Render("Send a message to get started.")}
		return
	}
	if file != nil && file.Workspace == m.session.Root.Path {
		saved := *file
		report("Restoring messages")
		body := render(saved.Messages, m.session.Registry)
		events <- sessionLoadedMsg{Same: true, File: saved, Body: body}
		return
	}
	cfg := m.session.Config
	lookup := m.decisions.Lookup
	var saved sess.File
	fresh := file == nil
	workspacePath := m.chat.sessionsHome
	if file != nil {
		saved = *file
		workspacePath = saved.Workspace
	}
	report("Opening workspace")
	root, err := workspace.Open(workspacePath)
	if err != nil {
		events <- sessionLoadedMsg{Err: err}
		return
	}
	report("Building session")
	next, err := app.New(cfg, root, lookup(root.Path), m.session.ExtraTools())
	if err != nil {
		events <- sessionLoadedMsg{Err: err}
		return
	}
	if fresh {
		events <- sessionLoadedMsg{Next: next, Fresh: true, Body: helpStyle.Render("Send a message to get started.")}
		return
	}
	report("Restoring messages")
	if err := loadSavedChat(next, saved); err != nil {
		events <- sessionLoadedMsg{Err: err}
		return
	}
	messages, err := next.Store.Load(context.Background(), saved.ID)
	if err != nil {
		events <- sessionLoadedMsg{Err: err}
		return
	}
	body := render(messages, next.Registry)
	events <- sessionLoadedMsg{Next: next, File: saved, Body: body}
}

func (m *programModel) applyLoaded(msg sessionLoadedMsg) {
	if msg.Fresh && msg.Same {
		m.chat.chatID = uuid.New().String()
		m.chat.sessionTitle = ""
		m.chat.messages = nil
		m.chat.toolCards = nil
		m.chat.pendingApprovals = nil
		m.chat.err = nil
		m.chat.status = ""
		m.chat.transcriptVP.SetContent(msg.Body)
		return
	}
	if msg.Same {
		if err := m.resumeHere(msg.File); err != nil {
			m.chat.sessionsOpen = true
			m.chat.sessionErr = err.Error()
			return
		}
		if msg.Body != "" {
			m.chat.transcriptVP.SetContent(msg.Body)
			m.chat.transcriptVP.GotoBottom()
		}
		return
	}
	if msg.Next == nil {
		return
	}
	m.bindChat(msg.Next)
	if msg.Fresh {
		m.chat.chatID = uuid.New().String()
		m.chat.sessionTitle = ""
	} else {
		m.chat.chatID = msg.File.ID
		m.chat.sessionTitle = msg.File.Title
	}
	m.chat.err = nil
	m.chat.status = ""
	m.chat.afterStoreRefresh()
	if msg.Body != "" {
		m.chat.transcriptVP.SetContent(msg.Body)
		m.chat.transcriptVP.GotoBottom()
	}
	if msg.Next.Workspace == trust.WorkspaceUnknown {
		m.phase = phaseTrust
	}
}

func (m *programModel) resume(file sess.File) error {
	if file.Workspace == m.session.Root.Path {
		return m.resumeHere(file)
	}
	root, err := workspace.Open(file.Workspace)
	if err != nil {
		return err
	}
	next, err := app.New(m.session.Config, root, m.decisions.Lookup(root.Path), m.session.ExtraTools())
	if err != nil {
		return err
	}
	if err := loadSavedChat(next, file); err != nil {
		return err
	}
	m.bindChat(next)
	m.chat.chatID = file.ID
	m.chat.sessionTitle = file.Title
	m.chat.err = nil
	m.chat.status = ""
	m.chat.afterStoreRefresh()
	if next.Workspace == trust.WorkspaceUnknown {
		m.phase = phaseTrust
	}
	return nil
}

func (m *programModel) resumeHere(file sess.File) error {
	if file.Mode != "" && app.Mode(file.Mode) != m.session.Mode {
		if err := m.session.SetMode(app.Mode(file.Mode)); err != nil {
			return err
		}
	}
	if err := m.session.Store.DeleteAllMessages(context.Background(), file.ID); err != nil {
		return err
	}
	if len(file.Messages) > 0 {
		if err := m.session.Store.AddMessages(context.Background(), file.ID, file.Messages...); err != nil {
			return err
		}
	}
	m.bindChat(m.session)
	m.chat.chatID = file.ID
	m.chat.sessionTitle = file.Title
	m.chat.err = nil
	m.chat.status = ""
	m.chat.afterStoreRefresh()
	return nil
}

func loadSavedChat(next *app.Session, file sess.File) error {
	if file.Mode != "" {
		if err := next.SetMode(app.Mode(file.Mode)); err != nil {
			return err
		}
	}
	if len(file.Messages) == 0 {
		return nil
	}
	return next.Store.AddMessages(context.Background(), file.ID, file.Messages...)
}

func (m *programModel) chooseTrust(trusted bool) tea.Cmd {
	if err := m.decisions.Set(m.session.Root.Path, trusted); err != nil {
		m.err = err
		return nil
	}
	if trusted {
		m.session.Workspace = trust.WorkspaceTrusted
		m.session.Edit.Workspace = trust.WorkspaceTrusted
		if m.session.WritePlan != nil {
			m.session.WritePlan.Workspace = trust.WorkspaceTrusted
		}
	} else {
		m.session.Workspace = trust.WorkspaceUntrusted
		m.session.Edit.Workspace = trust.WorkspaceUntrusted
		if m.session.WritePlan != nil {
			m.session.WritePlan.Workspace = trust.WorkspaceUntrusted
		}
	}
	if err := m.session.RefreshPrompt(); err != nil {
		m.err = err
		return nil
	}
	if m.session.Model != nil {
		m.chat.systemPrompt = m.session.Model.SystemPrompt()
	}
	m.phase = phaseChat
	cmds := []tea.Cmd{m.chat.Init()}
	if m.haveSize {
		next, cmd := m.chat.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		m.chat = next.(*chatModel)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (m *programModel) View() string {
	if m.phase == phaseChat {
		return m.chat.View()
	}
	var b strings.Builder
	b.WriteString(assistantPrefixStyle.Render("trust this workspace?"))
	b.WriteString("\n\n")
	b.WriteString(m.session.Root.Path)
	b.WriteString("\n\n")
	b.WriteString("Trusted workspaces can be edited. Untrusted workspaces stay read-only.\n")
	b.WriteString("The choice is stored in ~/.gopi, outside this repository.\n\n")
	b.WriteString(helpStyle.Render("y trust · n read-only · q quit"))
	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(errStyle.Render(m.err.Error()))
	}
	return b.String()
}
