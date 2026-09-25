package tui

import (
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

var (
	markdownRenderer *glamour.TermRenderer
	markdownWidth    int
	markdownCache    = map[string]string{}
	markdownMu       sync.Mutex
)

func renderMarkdown(content string, width int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	bodyWidth := markdownBodyWidth(width)
	key := content + "\x00" + strconv.Itoa(bodyWidth)
	markdownMu.Lock()
	if rendered, ok := markdownCache[key]; ok {
		markdownMu.Unlock()
		return rendered
	}
	markdownMu.Unlock()

	rendered, err := renderMarkdownWithGlamour(content, bodyWidth)
	if err != nil {
		return wrapText(content, width)
	}
	rendered = strings.TrimRight(rendered, "\n")
	markdownMu.Lock()
	if len(markdownCache) > 256 {
		markdownCache = map[string]string{}
	}
	markdownCache[key] = rendered
	markdownMu.Unlock()
	return rendered
}

func prepareMarkdown(width int) {
	width = markdownBodyWidth(width)
	markdownMu.Lock()
	defer markdownMu.Unlock()
	if markdownRenderer != nil && markdownWidth == width {
		return
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return
	}
	markdownRenderer = renderer
	markdownWidth = width
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
			glamour.WithStandardStyle("dark"),
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
