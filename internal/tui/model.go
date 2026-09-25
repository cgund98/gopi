package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cgund98/gogent"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

type chatModel struct {
	ctx      context.Context
	chatID   string
	agent    *gogent.Agent
	store    gogent.MessageStore
	registry *gogent.ToolRegistry
	events   *gogent.ChannelBroadcaster

	messages         []gogent.Message
	toolCards        []toolCardView
	pendingApprovals []gogent.PendingToolCall
	input            textinput.Model
	transcriptVP     viewport.Model
	approvalList     list.Model
	activeToolCallID string
	selectedTool     int
	followChatEnd    bool
	busy             bool
	status           string
	err              error
	modelName        string
	workspacePath    string

	width  int
	height int
}

type chatEventMsg struct {
	Event gogent.ChatEvent
}

type agentFinishedMsg struct {
	Err error
}

func newChatModel(
	ctx context.Context,
	agent *gogent.Agent,
	store gogent.MessageStore,
	registry *gogent.ToolRegistry,
	events *gogent.ChannelBroadcaster,
) *chatModel {
	ti := textinput.New()
	ti.Placeholder = "Ask anything…"
	ti.Prompt = "> "
	ti.Focus()
	ti.CharLimit = 2000
	ti.Width = 60

	vp := viewport.New(80, 20)
	vp.MouseWheelEnabled = false
	vp.SetContent(helpStyle.Render("Send a message to get started."))

	return &chatModel{
		ctx:          ctx,
		chatID:       uuid.New().String(),
		agent:        agent,
		store:        store,
		registry:     registry,
		events:       events,
		input:        ti,
		transcriptVP: vp,
		approvalList: newApprovalList(40, 4),
	}
}

func listenForChatEvent(ch <-chan gogent.ChatEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return nil
		}
		return chatEventMsg{Event: event}
	}
}

func runAgent(fn func() error) tea.Cmd {
	return func() tea.Msg {
		return agentFinishedMsg{Err: fn()}
	}
}

func (m *chatModel) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		listenForChatEvent(m.events.Channel()),
	)
}

func (m *chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.applyLayout()
		m.refreshTranscript(false)
		return m, nil

	case chatEventMsg:
		m.afterStoreRefresh()
		return m, listenForChatEvent(m.events.Channel())

	case agentFinishedMsg:
		m.busy = false
		if msg.Err != nil {
			m.err = msg.Err
			m.status = msg.Err.Error()
		} else {
			m.afterStoreRefresh()
		}
		return m, nil

	case tea.KeyMsg:
		if m.handleGlobalKeys(msg) {
			return m, tea.Quit
		}
		return m.handleKey(msg)

	case tea.MouseMsg:
		if m.inApprovalMode() {
			return m, nil
		}
		var cmd tea.Cmd
		m.transcriptVP, cmd = m.transcriptVP.Update(msg)
		m.followChatEnd = m.transcriptVP.AtBottom()
		return m, cmd

	default:
		if m.inApprovalMode() {
			var cmd tea.Cmd
			m.approvalList, cmd = m.approvalList.Update(msg)
			return m, cmd
		}
		if !m.busy {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		return m, nil
	}
}

func (m *chatModel) afterStoreRefresh() {
	if err := m.refreshFromStore(); err != nil {
		m.err = err
		m.status = "Failed to refresh chat"
		return
	}

	m.err = nil
	m.applyLayout()
	m.syncApprovalFocus()
	m.refreshTranscript(m.followChatEnd || m.busy)

	if m.busy {
		return
	}

	if m.inApprovalMode() {
		m.input.Blur()
		m.status = fmt.Sprintf("Approval required — tool %d of %d", m.approvalQueueIndex()+1, len(m.pendingApprovals))
		return
	}

	m.input.Focus()
	if len(m.messages) == 0 {
		m.status = ""
		return
	}
	m.status = ""
}

func (m *chatModel) handleGlobalKeys(msg tea.KeyMsg) (quit bool) {
	switch msg.String() {
	case "ctrl+c", "q":
		return true
	}
	return false
}

func (m *chatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.inApprovalMode() {
		return m.handleApprovalKey(msg)
	}

	if m.busy {
		return m, nil
	}

	switch msg.String() {
	case "up", "down", "pgup", "pgdown", "home", "end":
		var cmd tea.Cmd
		m.transcriptVP, cmd = m.transcriptVP.Update(msg)
		m.followChatEnd = m.transcriptVP.AtBottom()
		return m, cmd
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.input.SetValue("")
		m.busy = true
		m.followChatEnd = true
		m.err = nil
		return m, runAgent(func() error {
			return m.agent.RunWithUserInput(m.ctx, m.chatID, text)
		})
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *chatModel) refreshFromStore() error {
	messages, err := m.store.Load(m.ctx, m.chatID)
	if err != nil {
		return err
	}
	m.messages = messages
	m.toolCards = buildToolCards(messages, m.registry)

	m.pendingApprovals = buildPendingApprovals(m.messages, m.registry)
	return nil
}

func (m *chatModel) refreshTranscript(followEnd bool) {
	selected := -1
	if m.inApprovalMode() {
		selected = m.selectedTool
	}
	syncTranscriptViewport(&m.transcriptVP, m.messages, m.toolCards, selected, followEnd)
}

const footerBufferLines = 1

func (m *chatModel) footerLines() int {
	footer := footerBufferLines + 2
	if m.inApprovalMode() {
		footer += 12
	} else if m.status != "" || m.err != nil {
		footer += 2
	} else {
		footer++
	}
	return footer
}

func (m *chatModel) applyLayout() {
	innerW := m.width
	if innerW < 20 {
		innerW = 20
	}

	footer := m.footerLines()
	transcriptH := m.height - footer
	if transcriptH < 4 {
		transcriptH = 4
	}

	m.transcriptVP.Width = innerW
	m.transcriptVP.Height = transcriptH

	if m.inApprovalMode() {
		m.approvalList.SetWidth(innerW)
		m.approvalList.SetHeight(4)
	} else {
		m.input.Width = innerW - len(m.input.Prompt)
		if m.input.Width < 10 {
			m.input.Width = 10
		}
	}
}

func (m *chatModel) View() string {
	if m.width == 0 {
		return "Loading…"
	}

	m.applyLayout()

	var b strings.Builder
	b.WriteString(m.transcriptVP.View())
	b.WriteByte('\n')
	for range footerBufferLines {
		b.WriteByte('\n')
	}

	if m.inApprovalMode() {
		b.WriteString(renderDivider(m.width))
		b.WriteByte('\n')
		if pending, ok := m.currentPendingApproval(); ok {
			b.WriteString(renderApprovalPrompt(pending, m.width))
			b.WriteByte('\n')
		}
		b.WriteString(m.approvalList.View())
		b.WriteByte('\n')
		b.WriteString(helpStyle.Render(approvalHelpText))
	} else if m.busy {
		b.WriteString(promptStyle.Render("> "))
		b.WriteString(statusStyle.Render(m.input.Value() + " …"))
	} else {
		b.WriteString(m.input.View())
	}

	if m.status != "" || m.err != nil {
		b.WriteByte('\n')
		if m.err != nil {
			b.WriteString(errStyle.Render(m.status))
		} else {
			b.WriteString(statusStyle.Render(m.status))
		}
	}

	b.WriteByte('\n')
	b.WriteString(renderDivider(m.width))
	b.WriteByte('\n')
	b.WriteString(renderStatusSuffix(m.modelName, m.workspacePath, m.busy, m.width))

	return b.String()
}

func renderDivider(width int) string {
	if width < 1 {
		width = 40
	}
	return dividerStyle.Render(strings.Repeat("─", width))
}

func renderStatusSuffix(model, workspace string, thinking bool, width int) string {
	if model == "" {
		model = "model"
	}
	left := displayWorkspace(workspace)
	right := model
	if thinking {
		right += " | thinking"
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		room := width - lipgloss.Width(right) - 1
		left = truncateWidth(left, room)
		gap = width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if gap < 1 {
		gap = 1
	}
	return statusStyle.Render(left + strings.Repeat(" ", gap) + right)
}

func displayWorkspace(path string) string {
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	prefix := home + string(os.PathSeparator)
	if strings.HasPrefix(path, prefix) {
		return "~" + string(os.PathSeparator) + path[len(prefix):]
	}
	return path
}
