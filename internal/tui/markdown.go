package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

var (
	markdownRenderer *glamour.TermRenderer
	markdownWidth    int
	markdownMu       sync.Mutex
)

func renderMarkdown(content string, width int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	bodyWidth := markdownBodyWidth(width)
	rendered, err := renderMarkdownWithGlamour(content, bodyWidth)
	if err != nil {
		return wrapText(content, width)
	}
	return strings.TrimRight(rendered, "\n")
}

func markdownBodyWidth(width int) int {
	bodyWidth := width - 2
	if bodyWidth < 20 {
		return 20
	}
	return bodyWidth
}

func renderMarkdownWithGlamour(content string, width int) (string, error) {
	markdownMu.Lock()
	defer markdownMu.Unlock()

	if markdownRenderer == nil || markdownWidth != width {
		renderer, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return "", err
		}
		markdownRenderer = renderer
		markdownWidth = width
	}

	return markdownRenderer.Render(content)
}
