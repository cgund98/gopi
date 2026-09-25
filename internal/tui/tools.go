package tui

import (
	"encoding/json"
	"strings"

	"github.com/cgund98/gogent"
	"github.com/charmbracelet/lipgloss"
)

type toolCardState string

const (
	toolCardPending   toolCardState = "pending"
	toolCardRunning   toolCardState = "running"
	toolCardCompleted toolCardState = "completed"
	toolCardRejected  toolCardState = "rejected"
	toolCardFailed    toolCardState = "failed"
)

type toolCardView struct {
	MessageID   string
	ToolCallID  string
	ToolName    string
	Description string
	Args        json.RawMessage
	State       toolCardState
	Result      string
	CanReview   bool
}

func buildToolCards(messages []gogent.Message, registry *gogent.ToolRegistry) []toolCardView {
	toolResults := make(map[string]gogent.Message)
	for _, message := range messages {
		if message.Role == gogent.MessageRoleTool && message.ToolCallID != "" {
			toolResults[message.ToolCallID] = message
		}
	}

	var cards []toolCardView
	for _, message := range messages {
		if !message.HasToolCalls() {
			continue
		}
		for _, toolCall := range message.ToolCalls {
			cards = append(cards, buildToolCard(message.ID, toolCall, toolResults[toolCall.ID], registry))
		}
	}
	return cards
}

func buildToolCard(
	messageID string,
	toolCall gogent.ToolCall,
	resultMsg gogent.Message,
	registry *gogent.ToolRegistry,
) toolCardView {
	tool := registry.GetTool(toolCall.ToolName)
	description := "No description"
	if tool != nil {
		description = tool.Description()
	}

	card := toolCardView{
		MessageID:   messageID,
		ToolCallID:  toolCall.ID,
		ToolName:    toolCall.ToolName,
		Description: description,
		Args:        toolCall.Args,
	}

	requiresApproval := toolCall.Reason != ""

	switch {
	case toolCall.IsPendingApproval() && requiresApproval:
		card.State = toolCardPending
		card.CanReview = true
	case toolCall.IsPendingApproval():
		card.State = toolCardRunning
	case toolCall.IsRejected():
		card.State = toolCardRejected
		card.Result = formatToolOutcome(resultMsg, toolCall, gogent.ToolCallRejectedContent)
	case toolCall.ExecutionStatus == gogent.ExecutionStatusFailed:
		card.State = toolCardFailed
		card.Result = formatToolOutcome(resultMsg, toolCall, string(toolCall.Result))
	case toolCall.ExecutionStatus == gogent.ExecutionStatusCompleted:
		card.State = toolCardCompleted
		card.Result = formatToolOutcome(resultMsg, toolCall, string(toolCall.Result))
	case toolCall.IsApproved():
		card.State = toolCardRunning
	default:
		card.State = toolCardPending
	}

	return card
}

func formatToolOutcome(resultMsg gogent.Message, toolCall gogent.ToolCall, fallback string) string {
	if resultMsg.Content != "" {
		return formatJSONContent(resultMsg.Content)
	}
	if len(toolCall.Result) > 0 {
		return formatJSONContent(string(toolCall.Result))
	}
	return formatJSONContent(fallback)
}

func toolCardIndex(cards []toolCardView, toolCallID string) int {
	for i, card := range cards {
		if card.ToolCallID == toolCallID {
			return i
		}
	}
	return -1
}

func renderInlineToolBlock(card toolCardView, selected bool, width int) string {
	header := renderToolLine(toolHeadline(card), card.State, selected, width)
	body := renderToolBody(card, width)
	if body == "" {
		return header
	}
	return header + "\n" + body
}

func renderToolLine(name string, state toolCardState, selected bool, width int) string {
	text := "> " + name
	maxWidth := width
	if maxWidth < 8 {
		maxWidth = 8
	}
	var style lipgloss.Style
	switch {
	case selected:
		style = toolSelectedStyle
	case state == toolCardCompleted:
		style = toolSuccessStyle
	case state == toolCardRejected || state == toolCardFailed:
		style = toolErrorStyle
	default:
		style = toolDimStyle
	}
	var out []string
	for _, part := range wrapWidth(text, maxWidth) {
		out = append(out, style.Render(part))
	}
	return strings.Join(out, "\n")
}

func renderToolArgsBlock(args json.RawMessage, width int) string {
	raw := strings.TrimSpace(string(args))
	body := formatJSONContent(raw)
	if body == "" {
		body = raw
	}
	if body == "" {
		body = "{}"
	}

	const indent = "  "
	bodyWidth := width - len(indent)
	if bodyWidth < 20 {
		bodyWidth = 20
	}

	lines := strings.Split(wrapText(body, bodyWidth), "\n")
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = toolDimStyle.Render(indent + line)
	}
	return strings.Join(out, "\n")
}

func toolCardIndexByPending(cards []toolCardView, pending gogent.PendingToolCall) int {
	return toolCardIndex(cards, pending.ToolCallID)
}

func resolvedToolCallIDs(messages []gogent.Message, assistant gogent.Message) map[string]struct{} {
	pending := make(map[string]struct{}, len(assistant.ToolCalls))
	for _, toolCall := range assistant.ToolCalls {
		pending[toolCall.ID] = struct{}{}
	}

	resolved := make(map[string]struct{}, len(pending))
	for _, message := range messages {
		if message.Role != gogent.MessageRoleTool || message.ToolCallID == "" {
			continue
		}
		if _, ok := pending[message.ToolCallID]; ok {
			resolved[message.ToolCallID] = struct{}{}
		}
	}
	return resolved
}

func findUnresolvedAssistant(messages []gogent.Message) (gogent.Message, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if !message.HasToolCalls() {
			continue
		}
		resolved := resolvedToolCallIDs(messages, message)
		for _, toolCall := range message.ToolCalls {
			if _, ok := resolved[toolCall.ID]; !ok {
				return message, true
			}
		}
	}
	return gogent.Message{}, false
}

// buildPendingApprovals returns approval-required, unresolved tool calls on the
// current assistant turn, in model tool-call order.
func buildPendingApprovals(messages []gogent.Message, _ *gogent.ToolRegistry) []gogent.PendingToolCall {
	assistant, ok := findUnresolvedAssistant(messages)
	if !ok {
		return nil
	}

	resolved := resolvedToolCallIDs(messages, assistant)
	out := make([]gogent.PendingToolCall, 0, len(assistant.ToolCalls))
	for _, toolCall := range assistant.ToolCalls {
		if _, ok := resolved[toolCall.ID]; ok {
			continue
		}
		if toolCall.Reason == "" || !toolCall.IsPendingApproval() {
			continue
		}
		out = append(out, gogent.PendingToolCall{
			MessageID:  assistant.ID,
			ToolCallID: toolCall.ID,
			ToolName:   toolCall.ToolName,
			Args:       toolCall.Args,
			Reason:     toolCall.Reason,
		})
	}
	return out
}
