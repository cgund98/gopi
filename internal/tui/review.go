package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cgund98/gopi/internal/review"
	"github.com/cgund98/gopi/internal/session"
)

func (m *chatModel) openReview() {
	m.reloadReview()
	m.reviewOpen = true
	m.reviewCursor = 0
	m.reviewHunk = 0
	m.reviewPane = reviewPaneTree
	m.reviewScroll = 0
	m.reviewErr = ""
	if m.pendingReviewCount() == 0 {
		m.reviewErr = "No file edits to review"
	}
}

func (m *chatModel) reloadReview() {
	if m.sessions == nil || m.chatID == "" {
		return
	}
	file, err := m.sessions.Load(m.chatID)
	if err != nil {
		return
	}
	m.review = file.Review
}

func (m *chatModel) handleReviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.reviewOpen = false
		return m, nil
	case "tab":
		if m.reviewPane == reviewPaneTree {
			m.reviewPane = reviewPaneFile
			m.reviewScroll = -1
		} else {
			m.reviewPane = reviewPaneTree
		}
		return m, nil
	case "up":
		if m.reviewPane == reviewPaneFile {
			m.scrollReview(-1)
			return m, nil
		}
		if m.reviewCursor > 0 {
			m.reviewCursor--
			m.reviewHunk = 0
			m.reviewScroll = -1
		}
		return m, nil
	case "down":
		if m.reviewPane == reviewPaneFile {
			m.scrollReview(1)
			return m, nil
		}
		files := m.pendingReviewFiles()
		if m.reviewCursor+1 < len(files) {
			m.reviewCursor++
			m.reviewHunk = 0
			m.reviewScroll = -1
		}
		return m, nil
	case "shift+up", "shift+down":
		if m.reviewPane == reviewPaneFile {
			delta, _ := fastScroll(msg)
			m.scrollReview(delta)
		}
		return m, nil
	case "pgup":
		if m.reviewPane == reviewPaneFile {
			m.scrollReview(-(m.reviewPage()))
		}
		return m, nil
	case "pgdown":
		if m.reviewPane == reviewPaneFile {
			m.scrollReview(m.reviewPage())
		}
		return m, nil
	case "n":
		m.stepHunk(1)
		return m, nil
	case "p":
		m.stepHunk(-1)
		return m, nil
	case "a":
		m.decideHunk(true)
		return m, nil
	case "x":
		m.decideHunk(false)
		return m, nil
	case "A":
		m.decideFile(true)
		return m, nil
	case "X":
		m.decideFile(false)
		return m, nil
	default:
		return m, nil
	}
}

const (
	reviewPaneTree = 0
	reviewPaneFile = 1
)

func (m *chatModel) scrollReview(delta int) {
	m.reviewScroll += delta
	if m.reviewScroll < 0 {
		m.reviewScroll = 0
	}
}

func (m *chatModel) reviewPage() int {
	page := m.height - 8
	if page < 1 {
		return 1
	}
	return page
}

func (m *chatModel) stepHunk(delta int) {
	_, hunks := m.currentReview()
	if len(hunks) == 0 {
		return
	}
	next := m.reviewHunk + delta
	if next < 0 || next >= len(hunks) {
		return
	}
	m.reviewHunk = next
	m.reviewScroll = -1
}

func (m *chatModel) decideHunk(approve bool) {
	entry, hunks := m.currentReview()
	if entry.Path == "" || m.reviewHunk < 0 || m.reviewHunk >= len(hunks) {
		return
	}
	hunk := hunks[m.reviewHunk]
	if approve {
		if !containsString(entry.Approved, hunk.ID) {
			entry.Approved = append(entry.Approved, hunk.ID)
		}
	} else if err := m.rejectHunk(entry, hunk); err != nil {
		m.reviewErr = err.Error()
		return
	}
	m.finishDecision(entry)
}

func (m *chatModel) decideFile(approve bool) {
	entry, hunks := m.currentReview()
	if entry.Path == "" || len(hunks) == 0 {
		return
	}
	if approve {
		for _, hunk := range hunks {
			if !containsString(entry.Approved, hunk.ID) {
				entry.Approved = append(entry.Approved, hunk.ID)
			}
		}
	} else if err := m.rejectFile(entry, hunks); err != nil {
		m.reviewErr = err.Error()
		return
	}
	m.finishDecision(entry)
}

func (m *chatModel) finishDecision(entry session.ReviewEntry) {
	m.reviewErr = ""
	if len(m.trackedHunks(entry)) == 0 {
		if err := m.dropReview(entry.Path); err != nil {
			m.reviewErr = err.Error()
			return
		}
	} else if err := m.storeReview(entry); err != nil {
		m.reviewErr = err.Error()
		return
	}
	files := m.pendingReviewFiles()
	if len(files) == 0 {
		m.reviewOpen = false
		m.reviewCursor = 0
		m.reviewHunk = 0
		return
	}
	if m.reviewCursor >= len(files) {
		m.reviewCursor = len(files) - 1
		m.reviewHunk = 0
	}
	if _, hunks := m.currentReview(); m.reviewHunk >= len(hunks) {
		m.reviewHunk = 0
	}
}

func (m *chatModel) rejectHunk(entry session.ReviewEntry, hunk review.Hunk) error {
	path := m.reviewDiskPath(entry.Path)
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	next, err := review.Reject(string(body), hunk)
	if err != nil {
		return err
	}
	if next == "" && entry.Created {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, []byte(next), 0o644)
}

// rejectFile applies rejects bottom-up so earlier hunks keep their line positions,
// and writes nothing if any hunk no longer matches.
func (m *chatModel) rejectFile(entry session.ReviewEntry, hunks []review.Hunk) error {
	path := m.reviewDiskPath(entry.Path)
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	ordered := append([]review.Hunk(nil), hunks...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].NewStart > ordered[j].NewStart })
	next := string(body)
	for _, hunk := range ordered {
		if next, err = review.Reject(next, hunk); err != nil {
			return err
		}
	}
	if next == "" && entry.Created {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, []byte(next), 0o644)
}

func (m *chatModel) stampReviewHunks(path string) {
	if m.sessions == nil || m.chatID == "" {
		return
	}
	file, err := m.sessions.Load(m.chatID)
	if err != nil {
		return
	}
	index := -1
	for i, entry := range file.Review {
		if entry.Path == path {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	entry := file.Review[index]
	var ids []string
	for _, hunk := range m.hunksFor(entry) {
		ids = append(ids, hunk.ID)
	}
	if len(ids) == 0 {
		_ = m.dropReview(path)
		return
	}
	entry.Hunks = ids
	entry.Approved = nil
	file.Review[index] = entry
	_ = m.sessions.SaveReview(m.chatID, file.Review)
}

func (m *chatModel) dropReview(path string) error {
	kept := make([]session.ReviewEntry, 0, len(m.review))
	for _, entry := range m.review {
		if entry.Path != path {
			kept = append(kept, entry)
		}
	}
	m.review = kept
	if m.forgetEdit != nil {
		m.forgetEdit(path)
	}
	if m.sessions == nil {
		return nil
	}
	return m.sessions.DropReview(m.chatID, path)
}

func (m *chatModel) storeReview(entry session.ReviewEntry) error {
	for i := range m.review {
		if m.review[i].Path == entry.Path {
			m.review[i] = entry
		}
	}
	if m.sessions == nil {
		return nil
	}
	return m.sessions.SaveReview(m.chatID, m.review)
}

func (m *chatModel) pendingReviewCount() int {
	return len(m.pendingReviewFiles())
}

func (m *chatModel) pendingReviewFiles() []session.ReviewEntry {
	var pending []session.ReviewEntry
	for _, entry := range m.review {
		if m.undecidedHunks(entry) > 0 {
			pending = append(pending, entry)
		}
	}
	return sortReviewEntries(pending)
}

func (m *chatModel) undecidedHunks(entry session.ReviewEntry) int {
	return len(m.trackedHunks(entry))
}

func (m *chatModel) trackedHunks(entry session.ReviewEntry) []review.Hunk {
	var hunks []review.Hunk
	for _, hunk := range m.hunksFor(entry) {
		if len(entry.Hunks) > 0 && !containsString(entry.Hunks, hunk.ID) {
			continue
		}
		if containsString(entry.Approved, hunk.ID) {
			continue
		}
		hunks = append(hunks, hunk)
	}
	return hunks
}

func (m *chatModel) currentReview() (session.ReviewEntry, []review.Hunk) {
	files := m.pendingReviewFiles()
	if len(files) == 0 || m.reviewCursor < 0 || m.reviewCursor >= len(files) {
		return session.ReviewEntry{}, nil
	}
	entry := files[m.reviewCursor]
	return entry, m.trackedHunks(entry)
}

func (m *chatModel) hunksFor(entry session.ReviewEntry) []review.Hunk {
	baseline := ""
	if m.sessions != nil && entry.Baseline != "" {
		body, err := m.sessions.ReadBaseline(m.chatID, entry)
		if err == nil {
			baseline = string(body)
		}
	}
	current := ""
	body, err := os.ReadFile(m.reviewDiskPath(entry.Path))
	if err == nil {
		current = string(body)
	}
	return review.Diff(baseline, current)
}

func (m *chatModel) reviewDiskPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(m.workspacePath, path)
}

func (m *chatModel) renderReview() string {
	width := m.width
	if width < 40 {
		width = 40
	}
	leftW := width / 3
	if leftW < 20 {
		leftW = 20
	}
	if leftW > 36 {
		leftW = 36
	}
	rightW := width - leftW
	if rightW < 20 {
		rightW = 20
	}
	bodyH := m.height - 2
	if m.reviewErr != "" {
		bodyH -= len(strings.Split(wrapBlock(m.reviewErr, m.width), "\n"))
	}
	if bodyH < 8 {
		bodyH = 8
	}
	files := m.pendingReviewFiles()
	panel := lipgloss.NewStyle().Height(bodyH)
	left := panel.Width(leftW).Render(m.renderReviewTree(files, leftW, bodyH))
	right := panel.Width(rightW).Render(m.renderReviewFile(rightW, bodyH))
	var b strings.Builder
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, right))
	if m.reviewErr != "" {
		b.WriteByte('\n')
		b.WriteString(wrapStyled(m.reviewErr, errStyle, m.width))
	}
	b.WriteByte('\n')
	help := "up down file · tab viewer · n p hunk · a x hunk · A X file · esc back"
	if m.reviewPane == reviewPaneFile {
		help = "↑↓ scroll · shift fast · tab tree · n p hunk · a x hunk · A X file · esc"
	}
	b.WriteString(reviewHelpLine(help, width))
	return b.String()
}

// reviewHelpLine keeps the help on one row. A wrapped row would push the
// file header off the top of the screen.
func reviewHelpLine(help string, width int) string {
	const label = "Review Mode"
	if lipgloss.Width(help)+1+lipgloss.Width(label) <= width {
		return renderBusyLine(helpStyle.Render(help), helpStyle.Render(label), width)
	}
	return helpStyle.Render(truncateWidth(help, width))
}

func (m *chatModel) renderReviewFile(width, height int) string {
	entry, hunks := m.currentReview()
	if entry.Path == "" {
		return "No file edits to review"
	}
	added, deleted := 0, 0
	for _, hunk := range hunks {
		added += len(hunk.New)
		deleted += len(hunk.Old)
	}
	focusID := ""
	if m.reviewHunk >= 0 && m.reviewHunk < len(hunks) {
		focusID = hunks[m.reviewHunk].ID
	}
	body, err := os.ReadFile(m.reviewDiskPath(entry.Path))
	current := ""
	if err == nil {
		current = string(body)
	}
	rows, focusRow := renderFileWithDiff(entry.Path, current, hunks, focusID, width)
	var fileLines []string
	focus := 0
	for i, row := range rows {
		if i == focusRow {
			focus = len(fileLines)
		}
		fileLines = append(fileLines, strings.Split(row, "\n")...)
	}
	counts := diffAddStyle.Render(fmt.Sprintf("+%d", added)) + " " + diffDelStyle.Render(fmt.Sprintf("−%d", deleted))
	nameWidth := width - lipgloss.Width(counts) - 1
	if nameWidth < 1 {
		nameWidth = 1
	}
	nameParts := wrapWidth(entry.Path, nameWidth)
	if len(nameParts) == 0 {
		nameParts = []string{""}
	}
	viewH := height - len(nameParts)
	if viewH < 1 {
		viewH = 1
	}
	if len(fileLines) > viewH {
		start := focus
		maxStart := len(fileLines) - viewH
		if m.reviewPane == reviewPaneFile && m.reviewScroll >= 0 {
			start = m.reviewScroll
		}
		if start > maxStart {
			start = maxStart
		}
		if start < 0 {
			start = 0
		}
		if m.reviewPane == reviewPaneFile {
			m.reviewScroll = start
		}
		fileLines = fileLines[start : start+viewH]
	} else if m.reviewPane == reviewPaneFile {
		m.reviewScroll = 0
	}
	name := m.reviewPaneTitle(nameParts[0], m.reviewPane == reviewPaneFile)
	gap := width - lipgloss.Width(name) - lipgloss.Width(counts)
	if gap < 1 {
		gap = 1
	}
	header := name + strings.Repeat(" ", gap) + counts
	for _, part := range nameParts[1:] {
		header += "\n" + m.reviewPaneTitle(part, m.reviewPane == reviewPaneFile)
	}
	return header + "\n" + strings.Join(fileLines, "\n")
}

func (m *chatModel) reviewPaneTitle(label string, active bool) string {
	if active {
		return toolSelectedStyle.Render(label)
	}
	return toolDimStyle.Render(label)
}

func (m *chatModel) renderReviewTree(files []session.ReviewEntry, width, height int) string {
	files = sortReviewEntries(files)
	root := &reviewDir{}
	for i, entry := range files {
		root.insert(splitReviewPath(entry.Path), i)
	}
	var lines []string
	root.write(&lines, 0, width, m.reviewCursor)
	body := strings.Join(lines, "\n")
	if height > 1 {
		parts := strings.Split(body, "\n")
		if len(parts) > height-1 {
			parts = parts[:height-1]
		}
		body = strings.Join(parts, "\n")
	}
	header := m.reviewPaneTitle(strings.Join(wrapWidth("Files", width), "\n"), m.reviewPane == reviewPaneTree)
	return header + "\n" + body
}

type reviewDir struct {
	name  string
	files []reviewLeaf
	dirs  []*reviewDir
}

type reviewLeaf struct {
	name  string
	index int
}

func (d *reviewDir) insert(parts []string, index int) {
	if len(parts) == 1 {
		d.files = append(d.files, reviewLeaf{name: parts[0], index: index})
		return
	}
	for _, child := range d.dirs {
		if child.name == parts[0] {
			child.insert(parts[1:], index)
			return
		}
	}
	child := &reviewDir{name: parts[0]}
	d.dirs = append(d.dirs, child)
	child.insert(parts[1:], index)
}

func (d *reviewDir) write(lines *[]string, depth, width, cursor int) {
	indent := strings.Repeat("  ", depth)
	for _, file := range d.files {
		line := strings.Join(wrapWidth(indent+file.name, width), "\n")
		if file.index == cursor {
			line = agentModeStyle.Render(line)
		}
		*lines = append(*lines, line)
	}
	for i, child := range d.dirs {
		if i > 0 || len(d.files) > 0 {
			*lines = append(*lines, "")
		}
		*lines = append(*lines, toolDimStyle.Render(strings.Join(wrapWidth(indent+child.name+"/", width), "\n")))
		child.write(lines, depth+1, width, cursor)
	}
}

func splitReviewPath(path string) []string {
	path = filepath.ToSlash(path)
	return strings.Split(path, "/")
}

var (
	reviewRenderMu   sync.Mutex
	reviewRenderKey  uint64
	reviewRenderRows []string
	reviewRenderAt   int
)

// renderFileWithDiff draws the whole file with each hunk's old lines above its new
// lines. The review view redraws on every key, so the last result is reused while
// the file, hunks, focus, and width are unchanged.
func renderFileWithDiff(path, current string, hunks []review.Hunk, focusID string, width int) ([]string, int) {
	keyParts := []string{path, current, focusID, fmt.Sprint(width)}
	for _, hunk := range hunks {
		keyParts = append(keyParts, hunk.ID, fmt.Sprint(hunk.NewStart), strings.Join(hunk.Old, "\n"), strings.Join(hunk.New, "\n"))
	}
	key := highlightKey(keyParts...)
	reviewRenderMu.Lock()
	if key == reviewRenderKey && reviewRenderRows != nil {
		rows, focus := reviewRenderRows, reviewRenderAt
		reviewRenderMu.Unlock()
		return rows, focus
	}
	reviewRenderMu.Unlock()

	rows, focus := buildFileWithDiff(path, current, hunks, focusID, width)
	reviewRenderMu.Lock()
	reviewRenderKey, reviewRenderRows, reviewRenderAt = key, rows, focus
	reviewRenderMu.Unlock()
	return rows, focus
}

type reviewRowKind int

const (
	reviewRowContext reviewRowKind = iota
	reviewRowDeleted
	reviewRowAdded
)

func buildFileWithDiff(path, current string, hunks []review.Hunk, focusID string, width int) ([]string, int) {
	currentLines := splitFileLines(current)
	currentSpans := highlightLines(path, current)
	type row struct {
		oldNo   int
		newNo   int
		kind    reviewRowKind
		spans   []codeSpan
		focused bool
	}
	var rows []row
	focus := 0
	pos := 0
	oldNo := 1
	newNo := 1
	spansAt := func(highlighted [][]codeSpan, index int, text string, kind reviewRowKind) []codeSpan {
		if index < len(highlighted) && spansText(highlighted[index]) == text {
			return highlighted[index]
		}
		return plainSpans(text, kind)
	}
	for _, hunk := range hunks {
		for pos < hunk.NewStart && pos < len(currentLines) {
			rows = append(rows, row{oldNo: oldNo, newNo: newNo, kind: reviewRowContext, spans: spansAt(currentSpans, pos, currentLines[pos], reviewRowContext)})
			oldNo++
			newNo++
			pos++
		}
		focused := hunk.ID == focusID
		if focused {
			focus = len(rows)
		}
		oldSpans := highlightLines(path, strings.Join(hunk.Old, "\n"))
		for i, line := range hunk.Old {
			rows = append(rows, row{oldNo: oldNo, kind: reviewRowDeleted, spans: spansAt(oldSpans, i, line, reviewRowDeleted), focused: focused})
			oldNo++
		}
		for _, line := range hunk.New {
			rows = append(rows, row{newNo: newNo, kind: reviewRowAdded, spans: spansAt(currentSpans, pos, line, reviewRowAdded), focused: focused})
			newNo++
			pos++
		}
	}
	for pos < len(currentLines) {
		rows = append(rows, row{oldNo: oldNo, newNo: newNo, kind: reviewRowContext, spans: spansAt(currentSpans, pos, currentLines[pos], reviewRowContext)})
		oldNo++
		newNo++
		pos++
	}
	gutter := len(fmt.Sprintf("%d", max(oldNo, newNo)))
	if gutter < 2 {
		gutter = 2
	}
	lines := make([]string, len(rows))
	for i, row := range rows {
		lines[i] = renderReviewLine(row.oldNo, row.newNo, gutter, row.kind, row.spans, row.focused, width)
	}
	return lines, focus
}

// plainSpans is the uncolored fallback when a file type has no lexer.
func plainSpans(text string, kind reviewRowKind) []codeSpan {
	style := toolResultStyle
	switch kind {
	case reviewRowDeleted:
		style = diffDelStyle
	case reviewRowAdded:
		style = diffAddStyle
	}
	return []codeSpan{{text: text, style: style}}
}

// renderReviewLine draws one row. Added and deleted rows get a background tint
// across the full width so syntax colors stay readable; the focused hunk is brighter.
func renderReviewLine(oldNo, newNo, gutter int, kind reviewRowKind, spans []codeSpan, focused bool, width int) string {
	oldGutter := strings.Repeat(" ", gutter)
	newGutter := oldGutter
	if oldNo > 0 {
		oldGutter = fmt.Sprintf("%*d", gutter, oldNo)
	}
	if newNo > 0 {
		newGutter = fmt.Sprintf("%*d", gutter, newNo)
	}
	sign, signStyle := " ", toolResultStyle
	var background lipgloss.TerminalColor
	switch kind {
	case reviewRowDeleted:
		sign, signStyle, background = "-", diffDelStyle, diffDelBackground
		if focused {
			background = diffDelFocusBackground
		}
	case reviewRowAdded:
		sign, signStyle, background = "+", diffAddStyle, diffAddBackground
		if focused {
			background = diffAddFocusBackground
		}
	}
	tint := func(style lipgloss.Style) lipgloss.Style {
		if background == nil {
			return style
		}
		style = style.Background(background)
		if focused {
			style = style.Bold(true)
		}
		return style
	}

	gutterText := oldGutter + " " + newGutter + " "
	prefix := toolDimStyle.Render(gutterText) + tint(signStyle).Render(sign+" ")
	prefixWidth := lipgloss.Width(gutterText) + 2
	textWidth := width - prefixWidth
	if textWidth < 1 {
		textWidth = 1
	}
	pad := strings.Repeat(" ", prefixWidth)
	var lines []string
	for i, part := range wrapSpans(expandSpanTabs(spans), textWidth) {
		var b strings.Builder
		if i == 0 {
			b.WriteString(prefix)
		} else {
			b.WriteString(pad)
		}
		used := 0
		for _, span := range part {
			b.WriteString(tint(span.style).Render(span.text))
			used += lipgloss.Width(span.text)
		}
		if background != nil && used < textWidth {
			b.WriteString(tint(lipgloss.NewStyle()).Render(strings.Repeat(" ", textWidth-used)))
		}
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n")
}

// reviewTabWidth is the tab stop. Width measurement counts a tab as one column,
// while the terminal draws it up to the next stop, so tabs are expanded first.
const reviewTabWidth = 4

func splitFileLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func sortReviewEntries(entries []session.ReviewEntry) []session.ReviewEntry {
	sorted := append([]session.ReviewEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool {
		return reviewPathLess(sorted[i].Path, sorted[j].Path)
	})
	return sorted
}

func reviewPathLess(a, b string) bool {
	as := splitReviewPath(a)
	bs := splitReviewPath(b)
	n := len(as)
	if len(bs) < n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		if as[i] != bs[i] {
			aFile := i == len(as)-1
			bFile := i == len(bs)-1
			if aFile != bFile {
				return aFile
			}
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
