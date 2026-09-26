package tui

import (
	"hash/fnv"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

// maxHighlightBytes skips highlighting for large files such as generated code or
// lock files, where tokenizing would stall the review view.
const maxHighlightBytes = 512 << 10

const highlightCacheSize = 64

// codeSpan is a run of text in one syntax color.
type codeSpan struct {
	text  string
	style lipgloss.Style
}

var highlightTheme = styles.Get("github-dark")

var (
	tokenStylesMu sync.Mutex
	tokenStyles   = map[chroma.TokenType]lipgloss.Style{}

	highlightsMu sync.Mutex
	highlights   = map[uint64][][]codeSpan{}
)

// highlightLines splits text into lines of colored spans. It returns nil when the
// file type is unknown or the text is too large, and callers draw plain text.
// Tokenizing the whole text keeps multi-line comments and strings correct.
func highlightLines(path, text string) [][]codeSpan {
	if text == "" || len(text) > maxHighlightBytes {
		return nil
	}
	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		return nil
	}
	key := highlightKey(path, text)
	highlightsMu.Lock()
	cached, ok := highlights[key]
	highlightsMu.Unlock()
	if ok {
		return cached
	}

	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, text)
	if err != nil {
		return nil
	}
	lines := [][]codeSpan{nil}
	for token := iterator(); token != chroma.EOF; token = iterator() {
		for i, part := range strings.Split(token.Value, "\n") {
			if i > 0 {
				lines = append(lines, nil)
			}
			if part != "" {
				last := len(lines) - 1
				lines[last] = append(lines[last], codeSpan{text: part, style: tokenStyle(token.Type)})
			}
		}
	}

	highlightsMu.Lock()
	if len(highlights) >= highlightCacheSize {
		highlights = map[uint64][][]codeSpan{}
	}
	highlights[key] = lines
	highlightsMu.Unlock()
	return lines
}

func highlightKey(parts ...string) uint64 {
	h := fnv.New64a()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

func tokenStyle(tokenType chroma.TokenType) lipgloss.Style {
	tokenStylesMu.Lock()
	defer tokenStylesMu.Unlock()
	if style, ok := tokenStyles[tokenType]; ok {
		return style
	}
	entry := highlightTheme.Get(tokenType)
	style := toolResultStyle
	foreground := entry.Colour //nolint:misspell // chroma's field name
	if foreground.IsSet() {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(foreground.String()))
	}
	if entry.Bold == chroma.Yes {
		style = style.Bold(true)
	}
	if entry.Italic == chroma.Yes {
		style = style.Italic(true)
	}
	tokenStyles[tokenType] = style
	return style
}

func spansText(spans []codeSpan) string {
	var b strings.Builder
	for _, span := range spans {
		b.WriteString(span.text)
	}
	return b.String()
}

// expandSpanTabs replaces tabs with spaces to the next tab stop, counting columns
// across span boundaries.
func expandSpanTabs(spans []codeSpan) []codeSpan {
	out := make([]codeSpan, 0, len(spans))
	col := 0
	for _, span := range spans {
		var b strings.Builder
		for _, r := range span.text {
			if r == '\t' {
				spaces := reviewTabWidth - col%reviewTabWidth
				b.WriteString(strings.Repeat(" ", spaces))
				col += spaces
				continue
			}
			b.WriteRune(r)
			col += lipgloss.Width(string(r))
		}
		out = append(out, codeSpan{text: b.String(), style: span.style})
	}
	return out
}

// wrapSpans hard-wraps spans at width columns, like wrapWidth. It always returns
// at least one line.
func wrapSpans(spans []codeSpan, width int) [][]codeSpan {
	if width < 1 {
		width = 1
	}
	var lines [][]codeSpan
	var current []codeSpan
	col := 0
	for _, span := range spans {
		var b strings.Builder
		for _, r := range span.text {
			rw := lipgloss.Width(string(r))
			if rw > width {
				rw = width
			}
			if col > 0 && col+rw > width {
				if b.Len() > 0 {
					current = append(current, codeSpan{text: b.String(), style: span.style})
					b.Reset()
				}
				lines = append(lines, current)
				current = nil
				col = 0
			}
			b.WriteRune(r)
			col += rw
		}
		if b.Len() > 0 {
			current = append(current, codeSpan{text: b.String(), style: span.style})
		}
	}
	return append(lines, current)
}
