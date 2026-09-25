package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cgund98/gogent"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	"github.com/cgund98/gopi/internal/app"
	"github.com/cgund98/gopi/internal/session"
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
	spinner          spinner.Model
	status           string
	err              error
	modelName        string
	workspacePath    string
	systemPrompt     string
	mode             app.Mode
	switchMode       func(app.Mode) error
	prepareBuild     func() error
	runCancel        context.CancelFunc
	planOpen         bool
	planPath         string
	planBody         string
	planVP           viewport.Model
	plansOpen        bool
	planRows         []string
	planCursor       int
	planConfirm      bool
	planListErr      string
	seenPlans        map[string]bool
	sessions         *session.Store
	sessionTitle     string
	chatTitle        func(context.Context, string, string) (string, error)
	sessionsOpen     bool
	sessionLoading   bool
	sessionStatus    string
	sessionEvents    <-chan tea.Msg
	loadSession      func(*session.File) tea.Cmd
	applyLoaded      func(sessionLoadedMsg)
	sessionsHome     string
	sessionRows      []session.File
	sessionCursor    int
	sessionErr       string
	sessionConfirm   bool
	review           []session.ReviewEntry
	reviewOpen       bool
	reviewCursor     int
	reviewHunk       int
	reviewPane       int
	reviewScroll     int
	reviewErr        string
	forgetEdit       func(string)
	secretNames      []string
	secretSelected   map[string]map[string]bool

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

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))

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
		busy:         false,
		spinner:      sp,
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

func (m *chatModel) startRun(fn func(context.Context) error) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	if m.runCancel != nil {
		m.runCancel()
	}
	m.runCancel = cancel
	m.busy = true
	m.err = nil
	m.status = ""
	return tea.Batch(
		runAgent(func() error {
			return fn(ctx)
		}),
		m.spinner.Tick,
	)
}

func (m *chatModel) cancelRun() {
	if m.runCancel == nil {
		return
	}
	m.runCancel()
	m.status = "Canceling…"
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
		if m.sessionLoading {
			return m, nil
		}
		m.refreshTranscript(false)
		if m.planOpen {
			m.renderPlan()
		}
		return m, nil

	case spinner.TickMsg:
		if !m.busy && !m.sessionLoading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case chatEventMsg:
		if m.sessionLoading {
			return m, listenForChatEvent(m.events.Channel())
		}
		m.afterStoreRefresh()
		return m, listenForChatEvent(m.events.Channel())

	case sessionProgressMsg:
		if m.sessionLoading {
			m.sessionStatus = msg.Text
		}
		return m, awaitSession(m.sessionEvents)

	case sessionLoadedMsg:
		m.sessionLoading = false
		m.sessionStatus = ""
		if msg.Err != nil {
			m.sessionsOpen = true
			m.sessionErr = msg.Err.Error()
			return m, nil
		}
		if m.applyLoaded != nil {
			m.applyLoaded(msg)
		}
		return m, listenForChatEvent(m.events.Channel())

	case agentFinishedMsg:
		m.busy = false
		if m.runCancel != nil {
			m.runCancel()
			m.runCancel = nil
		}
		if msg.Err != nil && !errors.Is(msg.Err, context.Canceled) {
			m.err = msg.Err
			m.status = msg.Err.Error()
		} else {
			m.afterStoreRefresh()
			if errors.Is(msg.Err, context.Canceled) {
				m.err = nil
				m.status = "Interrupted"
			}
		}
		return m, m.persistSession()

	case sessionSavedMsg:
		if msg.Title != "" {
			m.sessionTitle = msg.Title
		}
		m.review = msg.Review
		if msg.Err != nil {
			m.status = msg.Err.Error()
		}
		return m, nil

	case tea.KeyMsg:
		if m.sessionLoading {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}
		if m.sessionsOpen {
			return m.handleSessionKey(msg)
		}
		if m.reviewOpen {
			return m.handleReviewKey(msg)
		}
		if m.plansOpen {
			return m.handlePlansKey(msg)
		}
		if m.planOpen {
			return m.handlePlanKey(msg)
		}
		if m.busy && (msg.String() == "esc" || msg.String() == "ctrl+c") {
			m.cancelRun()
			return m, nil
		}
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
	m.noticeWrittenPlans()
	m.applyLayout()
	m.syncApprovalFocus()
	m.refreshTranscript(m.followChatEnd || m.busy)
	if m.planOpen {
		m.input.Blur()
		return
	}

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
	case "ctrl+c":
		return true
	case "q":
		return !m.input.Focused()
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
		if text == "/help" {
			m.input.SetValue("")
			m.status = helpText
			return m, nil
		}
		if m.status == helpText {
			m.status = ""
		}
		if text == "/sessions" {
			m.input.SetValue("")
			return m, m.openSessions()
		}
		if text == "/review" {
			m.input.SetValue("")
			m.openReview()
			return m, nil
		}
		if text == "/plans" {
			m.input.SetValue("")
			m.openPlans()
			return m, nil
		}
		if mode, command, ok := app.ParseModeCommand(text); command {
			m.input.SetValue("")
			if m.busy || m.inApprovalMode() {
				m.status = "Finish the current turn before switching modes"
				return m, nil
			}
			if !ok {
				m.status = "Unknown mode. Use /agent, /ask, /plan, or /mode <name>"
				return m, nil
			}
			if m.switchMode == nil {
				m.status = "Mode switch is unavailable"
				return m, nil
			}
			if err := m.switchMode(mode); err != nil {
				m.err = err
				m.status = err.Error()
				return m, nil
			}
			m.mode = mode
			m.input.Prompt = modePrompt(mode)
			m.status = ""
			return m, nil
		}
		m.input.SetValue("")
		m.followChatEnd = true
		m.err = nil
		return m, m.startRun(func(ctx context.Context) error {
			return m.agent.RunWithUserInput(ctx, m.chatID, text)
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
	if m.pendingReviewCount() > 0 {
		footer += 2
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
	if m.sessionLoading {
		label := m.sessionStatus
		if label == "" {
			label = "Opening session"
		}
		return m.spinner.View() + statusStyle.Render(" "+label)
	}
	if m.width == 0 {
		return "Loading…"
	}
	if m.sessionsOpen {
		return m.renderSessions()
	}
	if m.reviewOpen {
		return m.renderReview()
	}
	if m.plansOpen {
		return m.renderPlans()
	}
	if m.planOpen {
		return m.planView()
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
		b.WriteString(renderBusyLine(modePrompt(m.mode)+m.spinner.View()+statusStyle.Render(" Thinking"), helpStyle.Render("esc cancel"), m.width))
	} else {
		b.WriteString(m.input.View())
	}

	if count := m.pendingReviewCount(); count > 0 {
		b.WriteByte('\n')
		label := "Review pending · 1 file"
		if count != 1 {
			label = fmt.Sprintf("Review pending · %d files", count)
		}
		b.WriteString(statusStyle.Render(label))
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
	b.WriteString(renderStatusSuffix(m.modelName, m.workspacePath, contextPercent(m.systemPrompt, m.messages, m.input.Value()), m.busy, m.width))

	return b.String()
}

func renderDivider(width int) string {
	if width < 1 {
		width = 40
	}
	return dividerStyle.Render(strings.Repeat("─", width))
}

func renderBusyLine(left, right string, width int) string {
	if width < 1 {
		width = 40
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func renderStatusSuffix(model, workspace string, contextPct int, thinking bool, width int) string {
	if model == "" {
		model = "model"
	}
	left := displayWorkspace(workspace)
	right := fmt.Sprintf("%s | %d%%", model, contextPct)
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

func modePrompt(mode app.Mode) string {
	label := string(mode)
	if label == "" {
		label = string(app.ModeAgent)
		mode = app.ModeAgent
	}
	var style lipgloss.Style
	switch mode {
	case app.ModeAsk:
		style = askModeStyle
	case app.ModePlan:
		style = planModeStyle
	default:
		style = agentModeStyle
	}
	return style.Render(label) + promptStyle.Render(" > ")
}

const contextWindowTokens = 128000

func contextPercent(system string, messages []gogent.Message, draft string) int {
	chars := len(system) + len(draft)
	for _, message := range messages {
		chars += len(message.Content)
		for _, call := range message.ToolCalls {
			chars += len(call.ToolName) + len(call.Args) + len(call.Result)
		}
	}
	tokens := chars / 4
	percent := tokens * 100 / contextWindowTokens
	if percent > 100 {
		return 100
	}
	return percent
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
