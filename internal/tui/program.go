package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cgund98/gopi/internal/app"
	"github.com/cgund98/gopi/internal/trust"
)

// Run starts the terminal UI. An unknown workspace asks for trust before the chat.
func Run(ctx context.Context, session *app.Session, decisions *trust.Store) error {
	model := newProgram(ctx, session, decisions)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
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
	model.chat.modelName = session.Config.Model
	model.chat.workspacePath = session.Root.Path
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

func (m *programModel) chooseTrust(trusted bool) tea.Cmd {
	if err := m.decisions.Set(m.session.Root.Path, trusted); err != nil {
		m.err = err
		return nil
	}
	if trusted {
		m.session.Workspace = trust.WorkspaceTrusted
		m.session.Edit.Workspace = trust.WorkspaceTrusted
	} else {
		m.session.Workspace = trust.WorkspaceUntrusted
		m.session.Edit.Workspace = trust.WorkspaceUntrusted
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
