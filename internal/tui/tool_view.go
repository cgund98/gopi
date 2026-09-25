package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	"github.com/cgund98/gopi/internal/toolview"
)

// safeRenderers wraps each custom renderer so its output is redacted, stripped of
// terminal escapes, and a panic falls back to the default rendering.
func safeRenderers(renderers map[string]toolview.Renderer, redact func(string) string) map[string]toolview.Renderer {
	if len(renderers) == 0 {
		return nil
	}
	out := make(map[string]toolview.Renderer, len(renderers))
	for name, renderer := range renderers {
		out[name] = safeRenderer{inner: renderer, redact: redact}
	}
	return out
}

type safeRenderer struct {
	inner  toolview.Renderer
	redact func(string) string
}

func (r safeRenderer) Headline(args json.RawMessage) (headline string) {
	defer func() {
		if recover() != nil {
			headline = ""
		}
	}()
	return strings.Join(strings.Fields(r.clean(r.inner.Headline(args))), " ")
}

func (r safeRenderer) RenderApproval(args json.RawMessage) (view toolview.View) {
	defer func() {
		if recover() != nil {
			view = toolview.View{}
		}
	}()
	return r.cleanView(r.inner.RenderApproval(args))
}

func (r safeRenderer) RenderResult(args, result json.RawMessage) (view toolview.View) {
	defer func() {
		if recover() != nil {
			view = toolview.View{}
		}
	}()
	return r.cleanView(r.inner.RenderResult(args, result))
}

func (r safeRenderer) cleanView(v toolview.View) toolview.View {
	out := toolview.View{Hide: v.Hide, Markdown: r.clean(v.Markdown)}
	for _, field := range v.Fields {
		out.Fields = append(out.Fields, toolview.Field{Label: r.clean(field.Label), Value: r.clean(field.Value)})
	}
	for _, line := range v.Lines {
		out.Lines = append(out.Lines, r.clean(line))
	}
	return out
}

func (r safeRenderer) clean(s string) string {
	if r.redact != nil {
		s = r.redact(s)
	}
	return stripControl(s)
}

var escapeSequence = regexp.MustCompile(`\x1b(\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|.)`)

// stripControl removes terminal escape sequences and control characters other than
// newline, and turns tabs into spaces.
func stripControl(s string) string {
	s = escapeSequence.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, s)
}

// renderToolView draws a custom view in the output frame. Fields come first, then
// lines, then markdown, capped at maxDiffLines rows.
func renderToolView(v toolview.View, width int) string {
	if v.Hide || v.IsZero() {
		return ""
	}
	inner, textWidth := outputFrameMetrics(width)
	var rows []string

	labelWidth := 0
	for _, field := range v.Fields {
		if w := lipgloss.Width(field.Label); w > labelWidth {
			labelWidth = w
		}
	}
	if limit := textWidth / 3; labelWidth > limit {
		labelWidth = limit
	}
	valueWidth := textWidth - labelWidth - 2
	if valueWidth < 1 {
		valueWidth = 1
	}
	for _, field := range v.Fields {
		label := truncateWidth(field.Label, labelWidth)
		label += strings.Repeat(" ", labelWidth-lipgloss.Width(label))
		for i, part := range wrapWidth(strings.ReplaceAll(field.Value, "\n", " "), valueWidth) {
			if i == 0 {
				rows = append(rows, toolDimStyle.Render(label)+"  "+part)
				continue
			}
			rows = append(rows, strings.Repeat(" ", labelWidth+2)+part)
		}
	}

	for _, line := range v.Lines {
		for _, text := range strings.Split(line, "\n") {
			for _, part := range wrapWidth(text, textWidth) {
				rows = append(rows, toolDimStyle.Render(part))
			}
		}
	}

	if markdown := strings.TrimSpace(v.Markdown); markdown != "" {
		if len(rows) > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, strings.Split(renderMarkdown(markdown, textWidth), "\n")...)
	}

	if hidden := len(rows) - maxDiffLines; hidden > 0 {
		rows = append(rows[:maxDiffLines], toolDimStyle.Render(fmt.Sprintf("… %d more lines", hidden)))
	}
	return diffFrameStyle.Width(inner).Render(strings.Join(rows, "\n"))
}

// customResult returns the renderer's view of a completed, non-error result.
func customResult(card toolCardView, content string) (toolview.View, bool) {
	if card.Renderer == nil || card.State != toolCardCompleted || resultError(content) {
		return toolview.View{}, false
	}
	view := card.Renderer.RenderResult(card.Args, json.RawMessage(content))
	return view, !view.IsZero()
}
