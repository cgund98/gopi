package tui

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/cgund98/gogent"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

const toolResultIndent = "  "

func indexToolResults(messages []gogent.Message) map[string]gogent.Message {
	results := make(map[string]gogent.Message)
	for _, message := range messages {
		if message.Role == gogent.MessageRoleTool && message.ToolCallID != "" {
			results[message.ToolCallID] = message
		}
	}
	return results
}

func renderTranscript(
	messages []gogent.Message,
	cards []toolCardView,
	selectedTool int,
	width int,
) string {
	if len(messages) == 0 {
		return helpStyle.Render("Send a message to get started.")
	}

	toolResults := indexToolResults(messages)
	renderedResults := make(map[string]struct{})

	var b strings.Builder
	first := true
	var previousRole gogent.MessageRole

	for _, message := range messages {
		switch message.Role {
		case gogent.MessageRoleUser:
			writeTranscriptGap(&b, &first)
			b.WriteString(renderUserMessage(message, width))
			previousRole = gogent.MessageRoleUser

		case gogent.MessageRoleAssistant:
			if message.Content == "" && !message.HasToolCalls() {
				continue
			}

			showLabel := !continuesAssistantTurn(previousRole)
			writeTranscriptGap(&b, &first)
			if message.Content != "" {
				if showLabel {
					b.WriteString(renderAssistantMessage(message, width))
				} else {
					b.WriteString(renderAssistantContent(message, width))
				}
				first = false
			} else if showLabel {
				b.WriteString(assistantPrefixStyle.Render("assistant"))
				first = false
			}

			for i, toolCall := range message.ToolCalls {
				if i == 0 && (message.Content != "" || showLabel) {
					b.WriteString("\n\n")
					first = false
				} else if i > 0 {
					b.WriteByte('\n')
				}
				idx := toolCardIndex(cards, toolCall.ID)
				if idx >= 0 {
					highlight := selectedTool >= 0 && idx == selectedTool && cards[idx].CanReview
					b.WriteString(renderInlineToolBlock(cards[idx], highlight, width))
				}
				if result, ok := toolResults[toolCall.ID]; ok {
					renderedResults[toolCall.ID] = struct{}{}
					if idx < 0 || !hideToolResult(cards[idx]) {
						b.WriteByte('\n')
						b.WriteString(renderToolResultMessage(cards, toolCall.ID, result, width))
					}
				}
			}
			previousRole = gogent.MessageRoleAssistant

		case gogent.MessageRoleTool:
			if _, ok := renderedResults[message.ToolCallID]; ok {
				continue
			}
			writeTranscriptGap(&b, &first)
			b.WriteString(renderToolResultMessage(cards, message.ToolCallID, message, width))
			previousRole = gogent.MessageRoleTool
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func continuesAssistantTurn(role gogent.MessageRole) bool {
	return role == gogent.MessageRoleAssistant || role == gogent.MessageRoleTool
}

// writeTranscriptGap inserts a blank line between distinct turns (e.g. user ↔ assistant).
func writeTranscriptGap(b *strings.Builder, first *bool) {
	if !*first {
		b.WriteString("\n\n")
	}
	*first = false
}

func renderUserMessage(message gogent.Message, width int) string {
	return userPrefixStyle.Render("you") + "\n" + renderMarkdown(message.Content, width)
}

func renderAssistantMessage(message gogent.Message, width int) string {
	return assistantPrefixStyle.Render("assistant") + "\n" + renderAssistantContent(message, width)
}

func renderAssistantContent(message gogent.Message, width int) string {
	return renderMarkdown(message.Content, width)
}

func renderToolResultMessage(cards []toolCardView, toolCallID string, message gogent.Message, width int) string {
	if card, ok := toolCardByID(cards, toolCallID); ok {
		if friendly := renderFriendlyResult(card, message.Content, width); friendly != "" {
			return friendly
		}
	}
	if rendered := renderToolError(message.Content, width); rendered != "" {
		return rendered
	}
	content := formatJSONContent(message.Content)
	if content == "" {
		return toolResultStyle.Render(toolResultIndent + "(empty)")
	}

	indentWidth := lipgloss.Width(toolResultIndent)
	bodyWidth := width - indentWidth
	if bodyWidth < 20 {
		bodyWidth = 20
	}

	hang := strings.Repeat(" ", indentWidth)
	lines := strings.Split(wrapText(content, bodyWidth), "\n")

	var parts []string
	for i, line := range lines {
		if i == 0 {
			parts = append(parts, toolResultStyle.Render(toolResultIndent)+line)
			continue
		}
		parts = append(parts, toolResultStyle.Render(hang)+line)
	}
	return strings.Join(parts, "\n")
}

func formatJSONContent(content string) string {
	if content == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(content), "", "  "); err != nil {
		return content
	}
	return buf.String()
}

func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}
	maxWidth := width - 2
	if maxWidth < 20 {
		maxWidth = 20
	}

	lines := strings.Split(text, "\n")
	var out []string
	for _, line := range lines {
		out = append(out, breakLine(line, maxWidth)...)
	}
	return strings.Join(out, "\n")
}

func breakLine(line string, maxWidth int) []string {
	if len(line) <= maxWidth {
		return []string{line}
	}
	var parts []string
	for len(line) > maxWidth {
		parts = append(parts, line[:maxWidth])
		line = line[maxWidth:]
	}
	if line != "" {
		parts = append(parts, line)
	}
	return parts
}

func syncTranscriptViewport(
	vp *viewport.Model,
	messages []gogent.Message,
	cards []toolCardView,
	selectedTool int,
	followEnd bool,
) {
	atBottom := vp.AtBottom()
	offset := vp.YOffset

	vp.SetContent(renderTranscript(messages, cards, selectedTool, vp.Width))

	if followEnd || atBottom {
		vp.GotoBottom()
	} else {
		vp.SetYOffset(offset)
	}
}
