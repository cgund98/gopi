package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cgund98/gogent"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	"github.com/cgund98/gopi/internal/app"
	"github.com/cgund98/gopi/internal/session"
	"github.com/cgund98/gopi/internal/tools"
	"github.com/cgund98/gopi/internal/toolview"
)

type chatModel struct {
	ctx      context.Context
	chatID   string
	agent    *gogent.Agent
	store    gogent.MessageStore
	registry *gogent.ToolRegistry
	events   *gogent.ChannelBroadcaster
	// renderers are custom tool renderers, already wrapped by safeRenderers.
	renderers map[string]toolview.Renderer
	subagent  *tools.DelegateProgress

	messages         []gogent.Message
	toolCards        []toolCardView
	pendingApprovals []gogent.PendingToolCall
	input            textarea.Model
	transcriptVP     viewport.Model
	approvalList     list.Model
	activeToolCallID string
	selectedTool     int
	followChatEnd    bool
	busy             bool
	workStarted      time.Time
	turnWork         map[string]time.Duration
	spinner          spinner.Model
	status           string
	mouseOff         bool
	err              error
	modelName        string
	workspacePath    string
	systemPrompt     string
	mode             app.Mode
	switchMode       func(app.Mode) error
	setModel         func(string) error
	summarize        func(context.Context, string) (gogent.Message, error)
	prepareBuild     func() error
	tasks            *tools.TaskList
	taskEpoch        int
	taskSeed         []tools.Task
	modelOverrides   map[string]string
	completeOpen     bool
	completeIndex    int
	completeItems    []completion
	runCancel        context.CancelFunc
	planOpen         bool
	planPath         string
	planBody         string
	planVP           viewport.Model
	plansOpen        bool
	planRows         []string
	planCursor       int
	planScroll       int
	planConfirm      bool
	planListErr      string
	seenPlans        map[string]bool
	sessions         *session.Store
	readGrants       *tools.ReadGrants
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
	sessionScroll    int
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
	ti := textarea.New()
	ti.Placeholder = "Ask anything…"
	ti.Prompt = "> "
	ti.ShowLineNumbers = false
	ti.CharLimit = 100000
	ti.MaxHeight = maxComposerLines
	ti.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter"))
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ti.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ti.FocusedStyle.Prompt = lipgloss.NewStyle()
	ti.BlurredStyle.Prompt = lipgloss.NewStyle()
	ti.SetHeight(1)
	ti.SetWidth(60)
	ti.Focus()

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
	m.workStarted = time.Now()
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
		textarea.Blink,
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
			_ = m.refreshFromStore()
			m.finishWork()
		} else {
			m.afterStoreRefresh()
			if errors.Is(msg.Err, context.Canceled) {
				m.err = nil
				m.status = "Interrupted"
			}
		}
		return m, m.persistSession()

	case compactDoneMsg:
		m.busy = false
		if msg.Err == nil && msg.Summary.ID != "" && !m.workStarted.IsZero() {
			if m.turnWork == nil {
				m.turnWork = map[string]time.Duration{}
			}
			m.turnWork[msg.Summary.ID] = time.Since(m.workStarted)
		}
		m.workStarted = time.Time{}
		if msg.Err != nil {
			m.err = msg.Err
			m.status = msg.Err.Error()
			return m, nil
		}
		if err := m.store.DeleteAllMessages(m.ctx, m.chatID); err != nil {
			m.err = err
			m.status = err.Error()
			return m, nil
		}
		if err := m.store.AddMessages(m.ctx, m.chatID, msg.Summary); err != nil {
			m.err = err
			m.status = err.Error()
			return m, nil
		}
		if len(msg.Keep) > 0 {
			if err := m.store.AddMessages(m.ctx, m.chatID, msg.Keep...); err != nil {
				m.err = err
				m.status = err.Error()
				return m, nil
			}
		}
		m.err = nil
		m.status = "Compacted earlier turns"
		m.afterStoreRefresh()
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
		if !m.busy && !m.inApprovalMode() && (msg.String() == "esc" || msg.String() == "ctrl+c") {
			if m.input.Value() != "" {
				m.input.SetValue("")
				m.completeOpen = false
				return m, nil
			}
			if msg.String() == "esc" {
				return m, tea.Quit
			}
		}
		if m.handleGlobalKeys(msg) {
			return m, tea.Quit
		}
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m, m.handleWheel(msg)

	default:
		if m.composerOpen() && isShiftEnter(msg) {
			return m.insertNewline()
		}
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

// finishWork records one duration for the turn, including tool calls, on the
// assistant message that shows token usage.
func (m *chatModel) finishWork() {
	if m.busy || m.workStarted.IsZero() {
		return
	}
	id := workMessageID(m.messages)
	if id != "" {
		if m.turnWork == nil {
			m.turnWork = map[string]time.Duration{}
		}
		m.turnWork[id] = time.Since(m.workStarted)
	}
	m.workStarted = time.Time{}
}

func workMessageID(messages []gogent.Message) string {
	if i := lastUsageIndex(messages); i >= 0 && messages[i].ID != "" {
		return messages[i].ID
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == gogent.MessageRoleAssistant && messages[i].ID != "" {
			return messages[i].ID
		}
	}
	return ""
}

func (m *chatModel) thinkingLabel() string {
	if m.workStarted.IsZero() {
		return ""
	}
	return formatThought(time.Since(m.workStarted))
}

// subagentLabel reads like "Subagent 42s · 3 tool calls · grep resume".
func subagentLabel(status tools.DelegateStatus, now time.Time) string {
	parts := []string{"Subagent " + formatThought(now.Sub(status.Started))}
	switch status.ToolCalls {
	case 0:
		parts = append(parts, "starting")
	case 1:
		parts = append(parts, "1 tool call")
	default:
		parts = append(parts, fmt.Sprintf("%d tool calls", status.ToolCalls))
	}
	if status.Last != "" {
		parts = append(parts, status.Last)
	}
	return strings.Join(parts, " · ")
}

func formatThought(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int(d.Seconds())
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	mins := secs / 60
	secs %= 60
	if secs == 0 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dm %ds", mins, secs)
}

func (m *chatModel) afterStoreRefresh() {
	if err := m.refreshFromStore(); err != nil {
		m.err = err
		m.status = "Failed to refresh chat"
		return
	}
	m.finishWork()

	m.err = nil
	m.syncTasks()
	m.noticeWrittenPlans()
	m.applyLayout()
	m.syncApprovalFocus()
	m.refreshTranscript(m.followChatEnd)
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

const fastScrollLines = 10

func historyScrollKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "up", "down", "pgup", "pgdown", "home", "end", "shift+up", "shift+down":
		return true
	default:
		return false
	}
}

// fastScroll reports the line delta for shift+up and shift+down.
func fastScroll(msg tea.KeyMsg) (int, bool) {
	switch msg.String() {
	case "shift+up":
		return -fastScrollLines, true
	case "shift+down":
		return fastScrollLines, true
	default:
		return 0, false
	}
}

func scrollViewport(vp *viewport.Model, msg tea.KeyMsg) tea.Cmd {
	if delta, ok := fastScroll(msg); ok {
		scrollLines(vp, delta)
		return nil
	}
	var cmd tea.Cmd
	*vp, cmd = vp.Update(msg)
	return cmd
}

const wheelLines = 3

// handleWheel scrolls whichever view is open. Other mouse events are ignored.
func (m *chatModel) handleWheel(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress {
		return nil
	}
	delta := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		delta = -wheelLines
	case tea.MouseButtonWheelDown:
		delta = wheelLines
	default:
		return nil
	}
	switch {
	case m.reviewOpen:
		if m.reviewPane == reviewPaneFile {
			m.scrollReview(delta)
		}
	case m.planOpen:
		scrollLines(&m.planVP, delta)
	case m.sessionsOpen, m.plansOpen, m.inApprovalMode():
	default:
		scrollLines(&m.transcriptVP, delta)
		m.followChatEnd = m.transcriptVP.AtBottom()
	}
	return nil
}

func scrollLines(vp *viewport.Model, delta int) {
	if delta < 0 {
		vp.ScrollUp(-delta)
	} else {
		vp.ScrollDown(delta)
	}
}

func (m *chatModel) scrollHistory(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd := scrollViewport(&m.transcriptVP, msg)
	m.followChatEnd = m.transcriptVP.AtBottom()
	return m, cmd
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

	if m.busy && historyScrollKey(msg) {
		return m.scrollHistory(msg)
	}
	if m.busy {
		return m, nil
	}

	if msg.Paste {
		text := strings.ReplaceAll(string(msg.Runes), "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
		m.input.InsertString(text)
		m.syncComplete()
		return m, nil
	}

	switch msg.String() {
	case "up", "down", "pgup", "pgdown", "home", "end":
		if m.completeOpen && (msg.String() == "up" || msg.String() == "down") {
			delta := 1
			if msg.String() == "up" {
				delta = -1
			}
			m.moveComplete(delta)
			return m, nil
		}
		if msg.String() == "up" && m.input.LineCount() > 1 && m.input.Line() > 0 {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		if msg.String() == "down" && m.input.LineCount() > 1 && m.input.Line() < m.input.LineCount()-1 {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		return m.scrollHistory(msg)
	case "shift+up", "shift+down":
		return m.scrollHistory(msg)
	case "shift+enter", "alt+enter":
		return m.insertNewline()
	case "tab":
		if m.completeOpen {
			m.acceptComplete()
			return m, nil
		}
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		m.completeOpen = false
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
			m.completeOpen = false
			m.openPlans()
			return m, nil
		}
		if text == "/compact" {
			m.input.SetValue("")
			m.completeOpen = false
			return m, m.compact()
		}
		if text == "/mouse" || strings.HasPrefix(text, "/mouse ") {
			m.input.SetValue("")
			m.completeOpen = false
			return m, m.handleMouse(text)
		}
		if strings.HasPrefix(text, "/model") {
			m.input.SetValue("")
			m.completeOpen = false
			m.handleModel(text)
			return m, nil
		}
		if strings.HasPrefix(text, "/allowpath ") {
			m.input.SetValue("")
			m.completeOpen = false
			m.handleAllowPath(text)
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
	m.syncComplete()
	return m, cmd
}

func (m *chatModel) refreshFromStore() error {
	messages, err := m.store.Load(m.ctx, m.chatID)
	if err != nil {
		return err
	}
	m.messages = messages
	m.toolCards = buildToolCards(messages, m.registry, m.renderers)

	m.pendingApprovals = buildPendingApprovals(m.messages, m.registry)
	return nil
}

func (m *chatModel) refreshTranscript(followEnd bool) {
	selected := -1
	if m.inApprovalMode() {
		selected = m.selectedTool
	}
	syncTranscriptViewport(&m.transcriptVP, m.messages, m.toolCards, selected, followEnd, !m.busy && !m.inApprovalMode(), m.turnWork)
}

const footerBufferLines = 1

func (m *chatModel) footerLines() int {
	footer := footerBufferLines + 2
	if m.inApprovalMode() {
		footer += 12
	} else if m.status != "" || m.err != nil {
		n := len(strings.Split(wrapBlock(m.status, m.width), "\n"))
		if n < 1 {
			n = 1
		}
		footer += 1 + n
	} else {
		footer++
		footer += m.inputExtraLines()
	}
	if m.pendingReviewCount() > 0 {
		footer += 2
	}
	footer += m.completeLines()
	footer += m.taskPanelHeight()
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
		m.syncComposer(innerW)
	}
}

const maxComposerLines = 8

func (m *chatModel) composerOpen() bool {
	return !m.sessionLoading && !m.busy && !m.inApprovalMode() && !m.planOpen && !m.plansOpen && !m.reviewOpen && !m.sessionsOpen
}

func (m *chatModel) insertNewline() (tea.Model, tea.Cmd) {
	m.input.InsertString("\n")
	m.syncComplete()
	return m, nil
}

// Terminals report Shift+Enter as alt+enter, or as a Kitty or xterm CSI sequence.
func isShiftEnter(msg tea.Msg) bool {
	text, ok := msg.(interface{ String() string })
	if !ok {
		return false
	}
	switch text.String() {
	case "shift+enter", "alt+enter", shiftEnterKitty, shiftEnterModifyOther:
		return true
	default:
		return false
	}
}

var (
	shiftEnterKitty       = csiReport("13;2u")
	shiftEnterModifyOther = csiReport("27;2;13~")
)

func csiReport(body string) string {
	return fmt.Sprintf("?CSI%+v?", []byte(body))
}

func (m *chatModel) inputExtraLines() int {
	n := m.input.LineCount()
	if n < 1 {
		n = 1
	}
	if n > maxComposerLines {
		n = maxComposerLines
	}
	return n - 1
}

func (m *chatModel) syncComposer(width int) {
	prompt := modePrompt(m.mode)
	m.input.Prompt = prompt
	promptWidth := lipgloss.Width(prompt)
	if promptWidth < 1 {
		promptWidth = 1
	}
	m.input.SetPromptFunc(promptWidth, func(lineIdx int) string {
		if lineIdx == 0 {
			return prompt
		}
		return strings.Repeat(" ", promptWidth)
	})
	if width < promptWidth+10 {
		width = promptWidth + 10
	}
	m.input.SetWidth(width)
	height := m.input.LineCount()
	if height < 1 {
		height = 1
	}
	if height > maxComposerLines {
		height = maxComposerLines
	}
	m.input.SetHeight(height)
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
	if panel := renderTaskPanel(m.visibleTasks(), m.width); panel != "" {
		b.WriteString(panel)
		b.WriteByte('\n')
	}

	if m.inApprovalMode() {
		b.WriteString(renderDivider(m.width))
		b.WriteByte('\n')
		if pending, ok := m.currentPendingApproval(); ok {
			b.WriteString(renderApprovalPrompt(pending, m.renderers[pending.ToolName], m.width))
			b.WriteByte('\n')
		}
		b.WriteString(m.approvalList.View())
		b.WriteByte('\n')
		b.WriteString(helpStyle.Render(approvalHelpText))
	} else if m.busy {
		label := " Thinking"
		if thought := m.thinkingLabel(); thought != "" {
			label += " " + thought
		}
		if status, ok := m.subagent.Snapshot(); ok {
			label = " " + subagentLabel(status, time.Now())
		}
		label = truncateWidth(label, m.width-lipgloss.Width(modePrompt(m.mode))-lipgloss.Width(m.spinner.View())-lipgloss.Width("esc cancel")-1)
		b.WriteString(renderBusyLine(modePrompt(m.mode)+m.spinner.View()+statusStyle.Render(label), helpStyle.Render("esc cancel"), m.width))
	} else {
		if menu := m.renderComplete(); menu != "" {
			b.WriteString(menu)
			b.WriteByte('\n')
		}
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
		style := statusStyle
		if m.err != nil {
			style = errStyle
		}
		b.WriteString(wrapStyled(m.status, style, m.width))
	}

	b.WriteByte('\n')
	b.WriteString(renderDivider(m.width))
	b.WriteByte('\n')
	usage := ""
	if !m.busy && !m.inApprovalMode() {
		usage = formatUsageStatus(m.modelName, m.messages)
	}
	b.WriteString(renderStatusSuffix(m.modelName, m.workspacePath, usage, contextPercent(m.modelName, m.systemPrompt, m.messages, m.input.Value()), m.busy, m.thinkingLabel(), m.width))

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

func renderStatusSuffix(model, workspace, usage string, contextPct int, thinking bool, thought string, width int) string {
	if model == "" {
		model = "model"
	}
	left := displayWorkspace(workspace)
	right := fmt.Sprintf("%s | %d%%", model, contextPct)
	if usage != "" {
		right += " | " + usage
	}
	if thinking {
		right += " | thinking"
		if thought != "" {
			right += " " + thought
		}
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
