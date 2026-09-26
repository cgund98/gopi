package tui

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

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
	showUsage bool,
	thoughts map[string]time.Duration,
) string {
	return renderTranscriptProgress(messages, cards, selectedTool, width, showUsage, nil, thoughts)
}

func renderTranscriptProgress(
	messages []gogent.Message,
	cards []toolCardView,
	selectedTool int,
	width int,
	showUsage bool,
	progress func(done, total int),
	thoughts map[string]time.Duration,
) string {
	if len(messages) == 0 {
		return helpStyle.Render("Send a message to get started.")
	}

	toolResults := indexToolResults(messages)
	renderedResults := make(map[string]struct{})

	var b strings.Builder
	first := true
	var previousRole gogent.MessageRole

	total := len(messages)
	done := 0
	usageAt := -1
	if showUsage {
		usageAt = lastUsageIndex(messages)
	}
	for i, message := range messages {
		done++
		if progress != nil {
			progress(done, total)
		}
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
					if idx >= 0 && hideToolResult(cards[idx]) {
						continue
					}
					if text := renderToolResultMessage(messages, cards, toolCall.ID, result, width); text != "" {
						b.WriteByte('\n')
						b.WriteString(text)
					}
				}
			}
			if line := turnSummaryLine(showUsage, i == usageAt, message, thoughts); line != "" {
				b.WriteByte('\n')
				b.WriteString(toolDimStyle.Render(line))
			}
			previousRole = gogent.MessageRoleAssistant

		case gogent.MessageRoleTool:
			if _, ok := renderedResults[message.ToolCallID]; ok {
				continue
			}
			text := renderToolResultMessage(messages, cards, message.ToolCallID, message, width)
			if text == "" {
				continue
			}
			writeTranscriptGap(&b, &first)
			b.WriteString(text)
			previousRole = gogent.MessageRoleTool
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func turnSummaryLine(showFooter, isUsage bool, message gogent.Message, worked map[string]time.Duration) string {
	if !showFooter {
		return ""
	}
	usage := ""
	if isUsage {
		usage = formatTurnUsage(message.Usage)
	}
	workedLine := ""
	if worked != nil && message.ID != "" {
		if d, ok := worked[message.ID]; ok && d >= 0 {
			workedLine = "Worked for " + formatThought(d)
		}
	}
	if usage == "" {
		return workedLine
	}
	if workedLine == "" {
		return usage
	}
	return usage + "  " + workedLine
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

func renderToolResultMessage(messages []gogent.Message, cards []toolCardView, toolCallID string, message gogent.Message, width int) string {
	if card, ok := toolCardByID(cards, toolCallID); ok && card.ToolName == "tasks" && !strings.Contains(message.Content, `"error"`) {
		return renderFinishedTaskLines(messages, cards, toolCallID, width)
	}
	if card, ok := toolCardByID(cards, toolCallID); ok {
		if hideToolResult(card) {
			return ""
		}
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
	return wrapWidth(line, maxWidth)
}

func wrapWidth(line string, maxWidth int) []string {
	if maxWidth < 1 {
		maxWidth = 1
	}
	if lipgloss.Width(line) <= maxWidth {
		return []string{line}
	}
	var parts []string
	var current strings.Builder
	width := 0
	for _, r := range line {
		rw := lipgloss.Width(string(r))
		if rw > maxWidth {
			rw = maxWidth
		}
		if width > 0 && width+rw > maxWidth {
			parts = append(parts, current.String())
			current.Reset()
			width = 0
		}
		current.WriteRune(r)
		width += rw
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func wrapBlock(text string, width int) string {
	if width < 8 {
		width = 8
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, wrapWidth(line, width)...)
	}
	return strings.Join(out, "\n")
}

func wrapStyled(text string, style lipgloss.Style, width int) string {
	var lines []string
	for _, line := range strings.Split(wrapBlock(text, width), "\n") {
		lines = append(lines, style.Render(line))
	}
	return strings.Join(lines, "\n")
}

func lastUsageIndex(messages []gogent.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == gogent.MessageRoleAssistant && !messages[i].Usage.Empty() {
			return i
		}
	}
	return -1
}

func syncTranscriptViewport(
	vp *viewport.Model,
	messages []gogent.Message,
	cards []toolCardView,
	selectedTool int,
	followEnd bool,
	showUsage bool,
	thoughts map[string]time.Duration,
) {
	atBottom := vp.AtBottom()
	offset := vp.YOffset

	vp.SetContent(renderTranscript(messages, cards, selectedTool, vp.Width, showUsage, thoughts))

	if followEnd || atBottom {
		vp.GotoBottom()
	} else {
		vp.SetYOffset(offset)
	}
}
