package tui

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cgund98/gogent"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	choiceApprove = "Approve"
	choiceReject  = "Reject"
)

const approvalHelpText = "↑/↓ select · space toggle secret · enter confirm · y approve · n reject"

type approvalChoiceItem struct {
	label  string
	secret bool
	on     bool
}

func (i approvalChoiceItem) FilterValue() string { return i.label }

func (i approvalChoiceItem) Title() string {
	if !i.secret {
		return i.label
	}
	mark := " "
	if i.on {
		mark = "x"
	}
	return "[" + mark + "] " + i.label
}

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
	var items []list.Item
	if pending, ok := m.currentPendingApproval(); ok && pending.ToolName == "shell" {
		selected := m.secretSelected[pending.ToolCallID]
		for _, name := range m.secretNames {
			items = append(items, approvalChoiceItem{label: name, secret: true, on: selected[name]})
		}
	}
	items = append(items,
		approvalChoiceItem{label: choiceApprove},
		approvalChoiceItem{label: choiceReject},
	)
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

func renderApprovalPrompt(pending gogent.PendingToolCall, width int) string {
	card := toolCardView{ToolName: pending.ToolName, Args: pending.Args}
	var b strings.Builder
	b.WriteString(renderToolLine(toolHeadline(card), toolCardPending, true, width))
	b.WriteString("\n\n")
	if pending.Reason != "" {
		b.WriteString(statusStyle.Render(pending.Reason))
		b.WriteByte('\n')
	}
	if body := renderApprovalBody(card, width); body != "" {
		b.WriteString(body)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m *chatModel) handleApprovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case " ":
		m.toggleSelectedSecret()
		return m, nil
	case "enter":
		item, ok := m.approvalList.SelectedItem().(approvalChoiceItem)
		if !ok {
			return m, nil
		}
		if item.secret {
			m.toggleSelectedSecret()
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
		names := m.selectedSecretNames(toolCallID)
		return m.startRun(func(ctx context.Context) error {
			if err := m.writeSecretNames(ctx, messageID, toolCallID, names); err != nil {
				return err
			}
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

func (m *chatModel) toggleSelectedSecret() {
	item, ok := m.approvalList.SelectedItem().(approvalChoiceItem)
	if !ok || !item.secret {
		return
	}
	pending, ok := m.currentPendingApproval()
	if !ok {
		return
	}
	if m.secretSelected == nil {
		m.secretSelected = map[string]map[string]bool{}
	}
	if m.secretSelected[pending.ToolCallID] == nil {
		m.secretSelected[pending.ToolCallID] = map[string]bool{}
	}
	m.secretSelected[pending.ToolCallID][item.label] = !m.secretSelected[pending.ToolCallID][item.label]
	index := m.approvalList.Index()
	m.refreshApprovalChoices()
	m.approvalList.Select(index)
}

func (m *chatModel) selectedSecretNames(toolCallID string) []string {
	selected := m.secretSelected[toolCallID]
	var names []string
	for _, name := range m.secretNames {
		if selected[name] {
			names = append(names, name)
		}
	}
	return names
}

func (m *chatModel) writeSecretNames(ctx context.Context, messageID, toolCallID string, names []string) error {
	message, err := m.store.GetMessage(ctx, m.chatID, messageID)
	if err != nil {
		return err
	}
	updated := false
	for i := range message.ToolCalls {
		call := &message.ToolCalls[i]
		if call.ID != toolCallID || call.ToolName != "shell" {
			continue
		}
		args := map[string]any{}
		if len(call.Args) > 0 {
			if err := json.Unmarshal(call.Args, &args); err != nil {
				return err
			}
		}
		if len(names) == 0 {
			delete(args, "secret_names")
		} else {
			args["secret_names"] = names
		}
		raw, err := json.Marshal(args)
		if err != nil {
			return err
		}
		call.Args = raw
		updated = true
	}
	if !updated {
		return nil
	}
	return m.store.UpdateMessage(ctx, m.chatID, message.ID, message)
}
