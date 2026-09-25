package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cgund98/gopi/internal/review"
)

func TestHighlightLinesKeepsTextAndLineCount(t *testing.T) {
	source := "package main\n\n/* a\n   b */\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	lines := highlightLines("main.go", source)
	want := strings.Split(source, "\n")
	if len(lines) < len(want)-1 {
		t.Fatalf("got %d lines, want %d", len(lines), len(want))
	}
	for i, line := range want[:len(want)-1] {
		if got := spansText(lines[i]); got != line {
			t.Fatalf("line %d = %q, want %q", i, got, line)
		}
	}
	if len(lines[0]) < 2 {
		t.Fatalf("package line was not split into tokens: %+v", lines[0])
	}
	if comment := lines[3]; len(comment) != 1 || comment[0].style.GetForeground() != lines[2][0].style.GetForeground() {
		t.Fatalf("second comment line is not styled as a comment: %+v", comment)
	}
}

func TestHighlightLinesSkipsUnknownAndLargeFiles(t *testing.T) {
	if highlightLines("notes.unknownext", "hello") != nil {
		t.Fatal("unknown file type was highlighted")
	}
	if highlightLines("big.go", strings.Repeat("x", maxHighlightBytes+1)) != nil {
		t.Fatal("large file was highlighted")
	}
}

func TestWrapSpansKeepsStylesAcrossBreaks(t *testing.T) {
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	blue := lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	lines := wrapSpans([]codeSpan{{text: "abcd", style: red}, {text: "efgh", style: blue}}, 3)
	var got []string
	for _, line := range lines {
		got = append(got, spansText(line))
	}
	if strings.Join(got, "|") != "abc|def|gh" {
		t.Fatalf("wrapped = %q", got)
	}
	if len(lines[1]) != 2 || lines[1][0].text != "d" || lines[1][1].text != "ef" {
		t.Fatalf("middle line spans = %+v", lines[1])
	}
}

func TestExpandSpanTabsCountsAcrossSpans(t *testing.T) {
	spans := expandSpanTabs([]codeSpan{{text: "ab"}, {text: "\tc"}})
	if got := spansText(spans); got != "ab  c" {
		t.Fatalf("expanded = %q", got)
	}
}

func TestReviewRowsAreHighlightedAndTinted(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)

	current := "package main\n\nfunc added() {}\n"
	hunks := []review.Hunk{{ID: "h1", NewStart: 2, Old: []string{"func removed() {}"}, New: []string{"func added() {}"}}}
	rows, focus := renderFileWithDiff("main.go", current, hunks, "h1", 60)
	if focus != 2 {
		t.Fatalf("focus = %d", focus)
	}
	var deleted, added string
	for _, row := range rows {
		switch plain := stripANSI(row); {
		case strings.Contains(plain, "- func removed"):
			deleted = row
		case strings.Contains(plain, "+ func added"):
			added = row
		}
	}
	if deleted == "" || added == "" {
		t.Fatalf("rows = %q", rows)
	}
	if !strings.Contains(added, "48;2;") || !strings.Contains(deleted, "48;2;") {
		t.Fatalf("changed rows have no background tint:\n%q\n%q", added, deleted)
	}
	if got := lipgloss.Width(added); got != 60 {
		t.Fatalf("added row width = %d, want the full 60 columns", got)
	}
	again, _ := renderFileWithDiff("main.go", current, hunks, "h1", 60)
	if &again[0] != &rows[0] {
		t.Fatal("unchanged review was rendered again instead of reused")
	}
}
