package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const maxDiffLines = 12
const readPreviewLines = 8
const shellPreviewLines = 5

func toolHeadline(card toolCardView) string {
	switch card.ToolName {
	case "edit_file":
		path, old, _ := editArgs(card.Args)
		if path == "" {
			return "edit"
		}
		if old == "" {
			return "create " + path
		}
		return "edit " + path
	case "read_file":
		if path := stringArg(card.Args, "path"); path != "" {
			return "read " + path
		}
		return "read"
	case "grep":
		return searchHeadline("grep", card.Args)
	case "find":
		return searchHeadline("find", card.Args)
	case "shell":
		if command := stringArg(card.Args, "command"); command != "" {
			return "shell " + command
		}
		return "shell"
	default:
		return card.ToolName
	}
}

func renderToolBody(card toolCardView, width int) string {
	if card.ToolName != "edit_file" {
		return ""
	}
	path, old, newText := editArgs(card.Args)
	return renderEditDiff(path, old, newText, width)
}

func hideToolResult(card toolCardView) bool {
	if card.ToolName != "edit_file" {
		return false
	}
	return !strings.Contains(card.Result, `"error"`)
}

func renderFriendlyResult(card toolCardView, content string, width int) string {
	var probe struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(content), &probe); err == nil && probe.Error != "" {
		return ""
	}
	switch card.ToolName {
	case "read_file":
		var payload struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(content), &payload); err != nil {
			return ""
		}
		if strings.TrimSpace(payload.Content) == "" {
			return toolResultStyle.Render("  (empty)")
		}
		preview, hidden := previewLines(payload.Content, readPreviewLines)
		body := indentBlock(preview, width)
		if hidden > 0 {
			body += "\n" + toolDimStyle.Render(fmt.Sprintf("  … %d more lines", hidden))
		}
		return toolResultStyle.Render(body)
	case "grep":
		var payload struct {
			Matches []struct {
				Path string `json:"path"`
				Line int    `json:"line"`
				Text string `json:"text"`
			} `json:"matches"`
		}
		if err := json.Unmarshal([]byte(content), &payload); err != nil {
			return ""
		}
		if len(payload.Matches) == 0 {
			return toolResultStyle.Render("  no matches")
		}
		var lines []string
		for _, match := range payload.Matches {
			lines = append(lines, fmt.Sprintf("%s:%d: %s", match.Path, match.Line, match.Text))
		}
		return toolResultStyle.Render(indentBlock(strings.Join(lines, "\n"), width))
	case "find":
		var payload struct {
			Files []string `json:"files"`
		}
		if err := json.Unmarshal([]byte(content), &payload); err != nil {
			return ""
		}
		if len(payload.Files) == 0 {
			return toolResultStyle.Render("  no files")
		}
		shown := payload.Files
		hidden := 0
		if len(shown) > readPreviewLines {
			hidden = len(shown) - readPreviewLines
			shown = shown[:readPreviewLines]
		}
		body := indentBlock(strings.Join(shown, "\n"), width)
		if hidden > 0 {
			body += "\n" + toolDimStyle.Render(fmt.Sprintf("  … %d more files", hidden))
		}
		return toolResultStyle.Render(body)
	case "shell":
		var payload struct {
			Stdout    string `json:"stdout"`
			Stderr    string `json:"stderr"`
			ExitCode  int    `json:"exit_code"`
			Truncated bool   `json:"truncated"`
		}
		if err := json.Unmarshal([]byte(content), &payload); err != nil {
			return ""
		}
		return renderShellOutput(payload.Stdout, payload.Stderr, payload.ExitCode, payload.Truncated, width)
	default:
		return ""
	}
}

func renderShellOutput(stdout, stderr string, exitCode int, truncated bool, width int) string {
	var combined strings.Builder
	if strings.TrimSpace(stdout) != "" {
		combined.WriteString(strings.TrimRight(stdout, "\n"))
	}
	if strings.TrimSpace(stderr) != "" {
		if combined.Len() > 0 {
			combined.WriteByte('\n')
		}
		combined.WriteString(strings.TrimRight(stderr, "\n"))
	}
	preview, hidden := previewLines(combined.String(), shellPreviewLines)
	var lines []string
	if strings.TrimSpace(preview) != "" {
		lines = strings.Split(preview, "\n")
	}
	if hidden > 0 {
		lines = append(lines, fmt.Sprintf("… %d more lines", hidden))
	} else if truncated {
		lines = append(lines, "… truncated")
	}
	if exitCode != 0 {
		lines = append(lines, fmt.Sprintf("exit %d", exitCode))
	}
	if len(lines) == 0 {
		lines = []string{"(no output)"}
	}

	frameWidth := width - 2
	if frameWidth < 28 {
		frameWidth = 28
	}
	inner := frameWidth - 2
	textWidth := inner - diffFrameStyle.GetHorizontalPadding()
	if textWidth < 1 {
		textWidth = 1
	}
	var body []string
	for _, line := range lines {
		body = append(body, toolDimStyle.Render(truncateWidth(line, textWidth)))
	}
	return diffFrameStyle.Width(inner).Render(strings.Join(body, "\n"))
}

func renderApprovalBody(card toolCardView, width int) string {
	switch card.ToolName {
	case "edit_file":
		return renderToolBody(card, width)
	case "read_file", "grep", "find", "shell":
		return renderPathGrants(card.Args, width)
	default:
		return renderToolArgsBlock(card.Args, width)
	}
}

func renderPathGrants(args json.RawMessage, width int) string {
	var lines []string
	for _, path := range stringListArg(args, "read_paths") {
		lines = append(lines, "read "+path)
	}
	for _, path := range stringListArg(args, "write_paths") {
		lines = append(lines, "write "+path)
	}
	for _, host := range stringListArg(args, "network_hosts") {
		lines = append(lines, "network "+host)
	}
	if stringArg(args, "network") == "unrestricted" {
		lines = append(lines, "network unrestricted")
	}
	if len(lines) == 0 {
		return ""
	}
	bodyWidth := width - 2
	if bodyWidth < 20 {
		bodyWidth = 20
	}
	var out []string
	for _, line := range lines {
		out = append(out, toolDimStyle.Render("  "+truncateWidth(line, bodyWidth)))
	}
	return strings.Join(out, "\n")
}

func searchHeadline(name string, args json.RawMessage) string {
	pattern := stringArg(args, "pattern")
	path := stringArg(args, "path")
	if pattern == "" && path == "" {
		return name
	}
	if pattern == "" {
		return name + " in " + path
	}
	if path == "" {
		return name + " " + pattern
	}
	return name + " " + pattern + " in " + path
}

func indentBlock(text string, width int) string {
	bodyWidth := width - 2
	if bodyWidth < 20 {
		bodyWidth = 20
	}
	var out []string
	for _, line := range strings.Split(wrapText(text, bodyWidth), "\n") {
		out = append(out, "  "+line)
	}
	return strings.Join(out, "\n")
}

func previewLines(text string, limit int) (string, int) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if limit <= 0 || len(lines) <= limit {
		return text, 0
	}
	return strings.Join(lines[:limit], "\n"), len(lines) - limit
}

func renderToolError(content string, width int) string {
	var payload struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil || payload.Error == "" {
		return ""
	}
	text := payload.Message
	if payload.Path != "" {
		if text != "" {
			text = payload.Path + ": " + text
		} else {
			text = payload.Path
		}
	}
	label := humanError(payload.Error)
	if text == "" {
		text = label
	} else if label != "" {
		text = label + ". " + text
	}
	return errStyle.Render(indentBlock(text, width))
}

func humanError(code string) string {
	switch code {
	case "access_denied":
		return "Access denied"
	case "execution_failed":
		return "Failed"
	case "rejected":
		return "Rejected"
	case "not_found":
		return "Not found"
	default:
		return strings.ReplaceAll(code, "_", " ")
	}
}

func toolCardByID(cards []toolCardView, toolCallID string) (toolCardView, bool) {
	idx := toolCardIndex(cards, toolCallID)
	if idx < 0 {
		return toolCardView{}, false
	}
	return cards[idx], true
}

func renderEditDiff(path, old, newText string, width int) string {
	rows := append(diffRows(old, "-", diffDelStyle), diffRows(newText, "+", diffAddStyle)...)
	if path == "" && len(rows) == 0 {
		return ""
	}
	if path == "" {
		path = "edit"
	}

	frameWidth := width - 2
	if frameWidth < 28 {
		frameWidth = 28
	}
	inner := frameWidth - 2
	textWidth := inner - diffFrameStyle.GetHorizontalPadding()
	if textWidth < 1 {
		textWidth = 1
	}
	gutterWidth := len(fmt.Sprintf("%d", len(rows)))
	if gutterWidth < 2 {
		gutterWidth = 2
	}

	var body []string
	body = append(body, toolSuccessStyle.Bold(true).Render(truncateWidth(path, textWidth)))
	body = append(body, dividerStyle.Render(strings.Repeat("─", textWidth)))
	if len(rows) == 0 {
		body = append(body, toolDimStyle.Render("(empty)"))
	}
	for i, row := range rows {
		body = append(body, renderDiffRow(i+1, gutterWidth, row, textWidth))
	}
	return diffFrameStyle.Width(inner).Render(strings.Join(body, "\n"))
}

type diffRow struct {
	sign  string
	text  string
	style lipgloss.Style
}

func diffRows(text, sign string, style lipgloss.Style) []diffRow {
	if text == "" {
		return nil
	}
	raw := strings.Split(strings.TrimRight(text, "\n"), "\n")
	truncated := false
	if len(raw) > maxDiffLines {
		raw = raw[:maxDiffLines]
		truncated = true
	}
	rows := make([]diffRow, 0, len(raw))
	for _, line := range raw {
		rows = append(rows, diffRow{sign: sign, text: line, style: style})
	}
	if truncated {
		rows = append(rows, diffRow{sign: " ", text: "…", style: toolDimStyle})
	}
	return rows
}

func renderDiffRow(lineNo, gutterWidth int, row diffRow, inner int) string {
	gutter := toolDimStyle.Render(fmt.Sprintf("%*d ", gutterWidth, lineNo))
	prefix := row.style.Render(row.sign + " ")
	used := gutterWidth + 1 + 2
	textWidth := inner - used
	if textWidth < 8 {
		textWidth = 8
	}
	parts := breakLine(row.text, textWidth)
	if len(parts) == 0 {
		parts = []string{""}
	}
	var out []string
	for i, part := range parts {
		if i == 0 {
			out = append(out, gutter+prefix+row.style.Render(part))
			continue
		}
		out = append(out, toolDimStyle.Render(strings.Repeat(" ", gutterWidth+1))+row.style.Render("  "+part))
	}
	return strings.Join(out, "\n")
}

func editArgs(raw json.RawMessage) (path, old, newText string) {
	var args struct {
		Path string `json:"path"`
		Old  string `json:"old"`
		New  string `json:"new"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", "", ""
	}
	return args.Path, args.Old, args.New
}

func truncateWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	ellipsis := "…"
	budget := width - lipgloss.Width(ellipsis)
	if budget <= 0 {
		return ellipsis
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > budget {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	b.WriteString(ellipsis)
	return b.String()
}

func stringArg(raw json.RawMessage, key string) string {
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return ""
	}
	value, _ := args[key].(string)
	return value
}

func stringListArg(raw json.RawMessage, key string) []string {
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil
	}
	items, _ := args[key].([]any)
	paths := make([]string, 0, len(items))
	for _, item := range items {
		path, ok := item.(string)
		if ok && path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}
