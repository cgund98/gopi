package tui

import (
	"fmt"
	"strconv"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/models"
)

func sessionUsage(messages []gogent.Message) gogent.Usage {
	var total gogent.Usage
	for _, message := range messages {
		if message.Usage.Empty() {
			continue
		}
		total.Input += message.Usage.Input
		total.Output += message.Usage.Output
		total.Cached += message.Usage.Cached
	}
	return total
}

func formatUsageStatus(modelName string, messages []gogent.Message) string {
	total := sessionUsage(messages)
	if total.Input == 0 && total.Output == 0 {
		return ""
	}
	text := formatTokens(total.Input) + "/" + formatTokens(total.Output)
	if dollars, ok := models.EstimateCost(modelName, total.Input, total.Output, total.Cached); ok {
		text += " " + formatDollars(dollars)
	}
	return text
}

func formatTurnUsage(usage *gogent.Usage) string {
	if usage.Empty() {
		return ""
	}
	text := formatTokens(usage.Input) + "/" + formatTokens(usage.Output)
	if usage.Cached > 0 {
		text += " " + formatTokens(usage.Cached) + " cached"
	}
	return text
}

func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n < 1000 {
		return strconv.Itoa(n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func formatDollars(dollars float64) string {
	if dollars < 0.01 {
		return fmt.Sprintf("$%.4f", dollars)
	}
	return fmt.Sprintf("$%.2f", dollars)
}

// contextPercent estimates the next request's size. The last reported input count
// covers the transcript up to that reply; everything after it (the reply itself and
// tool results) is estimated at four characters per token.
func contextPercent(modelName, system string, messages []gogent.Message, draft string) int {
	window := models.ContextWindow(modelName)
	chars := len(draft)
	tokens := 0
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Usage != nil && messages[i].Usage.Input > 0 {
			tokens = messages[i].Usage.Input
			start = i
			break
		}
	}
	if tokens == 0 {
		chars += len(system)
	}
	for _, message := range messages[start:] {
		chars += messageChars(message)
	}
	tokens += chars / 4
	if window < 1 {
		window = 1
	}
	percent := tokens * 100 / window
	if percent > 100 {
		return 100
	}
	return percent
}

// messageChars counts what is sent to the model. A tool result is sent as its tool
// message, so the copy stored on the assistant's tool call is not counted.
func messageChars(message gogent.Message) int {
	chars := len(message.Content)
	for _, call := range message.ToolCalls {
		chars += len(call.ToolName) + len(call.Args)
	}
	return chars
}
