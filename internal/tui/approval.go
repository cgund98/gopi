package tui

import (
	"strings"

	"github.com/cgund98/gogent"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	choiceApprove = "Approve"
	choiceReject  = "Reject"
)

const approvalHelpText = "↑/↓ select · enter confirm · y approve · n reject"

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
		return
	}

	if len(m.pendingApprovals) == 1 {
		m.activeToolCallID = m.pendingApprovals[0].ToolCallID
		m.selectedTool = toolCardIndexByPending(m.toolCards, m.pendingApprovals[0])
		return
	}

	for _, pending := range m.pendingApprovals {
		if pending.ToolCallID == m.activeToolCallID {
			m.selectedTool = toolCardIndexByPending(m.toolCards, pending)
			return
		}
	}

	m.activeToolCallID = m.pendingApprovals[0].ToolCallID
	m.selectedTool = toolCardIndexByPending(m.toolCards, m.pendingApprovals[0])
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

func renderApprovalPrompt(pending gogent.PendingToolCall, width int) string {
	var b strings.Builder
	b.WriteString(renderToolLine(pending.ToolName, toolCardPending, true))
	b.WriteString("\n\n")
	b.WriteString(renderToolArgsBlock(pending.Args, width))
	b.WriteByte('\n')
	return b.String()
}

func (m *chatModel) handleApprovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	default:
		var cmd tea.Cmd
		m.approvalList, cmd = m.approvalList.Update(msg)
		return m, cmd
	}
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
		return runAgent(func() error {
			return m.agent.ApproveToolCall(m.ctx, m.chatID, messageID, toolCallID)
		})
	case choiceReject:
		m.status = "Rejecting…"
		return runAgent(func() error {
			return m.agent.RejectToolCall(m.ctx, m.chatID, messageID, toolCallID)
		})
	default:
		m.busy = false
		return nil
	}
}
