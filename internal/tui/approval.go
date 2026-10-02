package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/cgund98/gogent"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cgund98/gopi/internal/toolview"
)

const (
	choiceApprove = "Approve"
	choiceReject  = "Reject"
)

const approvalHelpText = "↑/↓ select · enter confirm · y/n reply · tab scroll"

type approvalChoiceItem struct {
	label string
}

func (i approvalChoiceItem) FilterValue() string { return i.label }

func (i approvalChoiceItem) Title() string { return i.label }

func (i approvalChoiceItem) Description() string { return "" }

func newApprovalList(width, height int) list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false

	l := list.New(
		[]list.Item{
			approvalChoiceItem{label: choiceApprove},
			approvalChoiceItem{label: choiceReject},
		},
		delegate,
		width,
		height,
	)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	l.SetShowPagination(false)
	l.DisableQuitKeybindings()
	return l
}

func (m *chatModel) inApprovalMode() bool {
	return !m.busy && len(m.pendingApprovals) > 0
}

func (m *chatModel) approvalQueueIndex() int {
	for i, pending := range m.pendingApprovals {
		if pending.ToolCallID == m.activeToolCallID {
			return i
		}
	}
	return 0
}

func (m *chatModel) syncApprovalFocus() {
	if len(m.pendingApprovals) == 0 {
		m.activeToolCallID = ""
		m.selectedTool = -1
		m.approvalFocusHistory = false
		return
	}

	// The approval prompt does not repeat the tool headline, so when a new
	// approval becomes active bring the transcript's card into view. It is the
	// one place the command is rendered.
	previous := m.activeToolCallID
	defer func() {
		if m.activeToolCallID != previous {
			m.followChatEnd = true
		}
	}()

	if len(m.pendingApprovals) == 1 {
		m.activeToolCallID = m.pendingApprovals[0].ToolCallID
		m.selectedTool = toolCardIndexByPending(m.toolCards, m.pendingApprovals[0])
		m.refreshApprovalChoices()
		return
	}

	for _, pending := range m.pendingApprovals {
		if pending.ToolCallID == m.activeToolCallID {
			m.selectedTool = toolCardIndexByPending(m.toolCards, pending)
			m.refreshApprovalChoices()
			return
		}
	}

	m.activeToolCallID = m.pendingApprovals[0].ToolCallID
	m.selectedTool = toolCardIndexByPending(m.toolCards, m.pendingApprovals[0])
	m.refreshApprovalChoices()
}

func (m *chatModel) refreshApprovalChoices() {
	items := []list.Item{
		approvalChoiceItem{label: choiceApprove},
		approvalChoiceItem{label: choiceReject},
	}
	m.approvalList.SetItems(items)
}

func (m *chatModel) currentPendingApproval() (gogent.PendingToolCall, bool) {
	for _, pending := range m.pendingApprovals {
		if pending.ToolCallID == m.activeToolCallID {
			return pending, true
		}
	}
	if len(m.pendingApprovals) == 0 {
		return gogent.PendingToolCall{}, false
	}
	return m.pendingApprovals[0], true
}

func (m *chatModel) resolveSubmissionPending() (gogent.PendingToolCall, bool) {
	if err := m.refreshFromStore(); err != nil {
		return gogent.PendingToolCall{}, false
	}
	if len(m.pendingApprovals) == 0 {
		return gogent.PendingToolCall{}, false
	}
	for _, pending := range m.pendingApprovals {
		if pending.ToolCallID == m.activeToolCallID {
			return pending, true
		}
	}
	return m.pendingApprovals[0], true
}

// renderApprovalPrompt renders the part of an approval the transcript does not
// already show: why the call paused, and the extra access it is asking for. The
// tool headline, its body, and an edit diff stay in the transcript, which renders
// them once.
func renderApprovalPrompt(pending gogent.PendingToolCall, renderer toolview.Renderer, width int) string {
	card := toolCardView{ToolName: pending.ToolName, Args: pending.Args, Renderer: renderer}
	var b strings.Builder
	if pending.Reason != "" {
		b.WriteString(wrapStyled(pending.Reason, statusStyle, width))
		b.WriteByte('\n')
	}
	if body := renderApprovalBody(card, width); body != "" {
		b.WriteString(body)
		b.WriteByte('\n')
	}
	return b.String()
}

// approvalMinTranscript is the rows kept for the chat history while an approval
// is pending. The rest of the window goes to the approval block.
const approvalMinTranscript = 4

// approvalPrompt renders the approval detail, capped to maxLines so a long reason
// or path list cannot push the approval block past the window. The remaining
// lines are shown as a count; the call itself is in the transcript.
func (m *chatModel) approvalPrompt(pending gogent.PendingToolCall, maxLines int) string {
	out := renderApprovalPrompt(pending, m.renderers[pending.ToolName], m.width)
	if maxLines < 1 {
		maxLines = 1
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) <= maxLines {
		return out
	}
	hidden := len(lines) - (maxLines - 1)
	lines = lines[:maxLines-1]
	lines = append(lines, toolDimStyle.Render(fmt.Sprintf("… %d more lines", hidden)))
	return strings.Join(lines, "\n") + "\n"
}

// approvalSpace splits the rows the approval block may use. The block is a
// divider, the approval detail, the choices list, and a help line. It is sized
// so the whole frame still fits the window and the history keeps a few rows: the
// help line goes first, then the list and the detail shrink toward one row each.
func (m *chatModel) approvalSpace() (promptRows, listRows int, showHelp bool) {
	budget := m.height - 3 - approvalMinTranscript // buffer, status line, history
	if budget < 3 {
		budget = 3
	}
	showHelp = budget >= 4
	overhead := 1 // divider
	if showHelp {
		overhead++
	}
	listRows = budget - overhead - 1 // leave the prompt at least one row
	if listRows > approvalListHeight {
		listRows = approvalListHeight
	}
	if listRows < 1 {
		listRows = 1
	}
	promptRows = budget - overhead - listRows
	if promptRows < 1 {
		promptRows = 1
	}
	return promptRows, listRows, showHelp
}

// approvalBlock is the divider, the approval detail, the choices, and the help
// line, built one entry per drawn line. View draws it and footerLines measures
// it, so the two always agree.
func (m *chatModel) approvalBlock() string {
	promptRows, _, showHelp := m.approvalSpace()
	lines := []string{renderDivider(m.width)}
	if pending, ok := m.currentPendingApproval(); ok {
		prompt := strings.TrimSuffix(m.approvalPrompt(pending, promptRows), "\n")
		lines = append(lines, strings.Split(prompt, "\n")...)
	}
	lines = append(lines, strings.Split(m.approvalList.View(), "\n")...)
	if showHelp {
		lines = append(lines, helpStyle.Render(truncateWidth(approvalHelpText, m.width)))
	}
	return strings.Join(lines, "\n")
}

// approvalBlockLines is the height of approvalBlock(). footerLines reserves
// exactly this many rows.
func (m *chatModel) approvalBlockLines() int {
	if !m.inApprovalMode() {
		return 0
	}
	return len(strings.Split(m.approvalBlock(), "\n"))
}

func (m *chatModel) handleApprovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The approval always wins on these keys, whichever pane has focus, so a
	// reply is never lost while the history is focused.
	switch msg.String() {
	case "enter":
		item, ok := m.approvalList.SelectedItem().(approvalChoiceItem)
		if !ok {
			return m, nil
		}
		return m, m.submitApprovalChoice(item.label)
	case "y", "Y":
		return m, m.submitApprovalChoice(choiceApprove)
	case "n", "N", "r", "R":
		return m, m.submitApprovalChoice(choiceReject)
	case "tab", "shift+tab":
		m.approvalFocusHistory = !m.approvalFocusHistory
		return m, nil
	}

	if m.approvalFocusHistory {
		if historyScrollKey(msg) {
			return m.scrollHistory(msg)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.approvalList, cmd = m.approvalList.Update(msg)
	return m, cmd
}

func (m *chatModel) submitApprovalChoice(choice string) tea.Cmd {
	pending, ok := m.resolveSubmissionPending()
	if !ok {
		return nil
	}

	m.activeToolCallID = pending.ToolCallID
	messageID := pending.MessageID
	toolCallID := pending.ToolCallID

	m.busy = true
	m.err = nil
	switch choice {
	case choiceApprove:
		m.status = "Running tool…"
		return m.startRun(func(ctx context.Context) error {
			return m.agent.ApproveToolCall(ctx, m.chatID, messageID, toolCallID)
		})
	case choiceReject:
		m.status = "Rejecting…"
		return m.startRun(func(ctx context.Context) error {
			return m.agent.RejectToolCall(ctx, m.chatID, messageID, toolCallID)
		})
	default:
		m.busy = false
		return nil
	}
}
