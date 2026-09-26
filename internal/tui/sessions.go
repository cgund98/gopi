package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cgund98/gogent"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cgund98/gopi/internal/app"
	"github.com/cgund98/gopi/internal/session"
)

const helpText = `/agent, /ask, /plan, and /mode <name> switch the session mode
/model <name> sets the model for the active mode
/compact summarizes earlier turns
/mouse [on|off] toggles mouse capture for text selection
/sessions opens the saved-chat list
/plans opens saved plans
/review walks file edits from this chat
/help shows this list`

type sessionSavedMsg struct {
	Title  string
	Review []session.ReviewEntry
	Err    error
}

type sessionProgressMsg struct {
	Text string
}

type sessionLoadedMsg struct {
	Next  *app.Session
	File  session.File
	Fresh bool
	Same  bool
	Body  string
	Err   error
}

func (m *chatModel) persistSession() tea.Cmd {
	if m.sessions == nil {
		return nil
	}
	id := m.chatID
	workspace := m.workspacePath
	mode := string(m.mode)
	store := m.store
	sessions := m.sessions
	titleFn := m.chatTitle
	ctx := m.ctx
	models := m.modelOverrides
	var grants []string
	haveGrants := m.readGrants != nil
	if haveGrants {
		grants = m.readGrants.List()
	}
	return func() tea.Msg {
		messages, err := store.Load(ctx, id)
		if err != nil || len(messages) == 0 {
			return nil
		}
		title := ""
		var review []session.ReviewEntry
		var savedGrants []string
		if existing, err := sessions.Load(id); err == nil {
			title = existing.Title
			review = existing.Review
			savedGrants = existing.ReadGrants
		}
		if title == "" {
			title = generateTitle(ctx, titleFn, messages)
		}
		if !haveGrants {
			grants = savedGrants
		}
		err = sessions.Save(session.File{
			ID:         id,
			Title:      title,
			Workspace:  workspace,
			Mode:       mode,
			Updated:    time.Now().UTC(),
			Messages:   messages,
			Review:     review,
			Models:     models,
			ReadGrants: grants,
		})
		return sessionSavedMsg{Title: title, Review: review, Err: err}
	}
}

func generateTitle(ctx context.Context, titleFn func(context.Context, string, string) (string, error), messages []gogent.Message) string {
	user, assistant := firstExchange(messages)
	if titleFn != nil && user != "" {
		title, err := titleFn(ctx, user, assistant)
		if err == nil {
			title = strings.TrimSpace(title)
			if title != "" {
				return title
			}
		}
	}
	return fallbackTitle(messages)
}

func firstExchange(messages []gogent.Message) (string, string) {
	var user, assistant string
	for _, message := range messages {
		if user == "" && message.Role == gogent.MessageRoleUser {
			user = message.Content
		}
		if message.Role == gogent.MessageRoleAssistant && strings.TrimSpace(message.Content) != "" {
			assistant = message.Content
		}
	}
	return user, assistant
}

func fallbackTitle(messages []gogent.Message) string {
	for _, message := range messages {
		if message.Role != gogent.MessageRoleUser {
			continue
		}
		line := strings.TrimSpace(strings.Split(message.Content, "\n")[0])
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > 60 {
			return string(runes[:60])
		}
		return line
	}
	return "Chat"
}

func (m *chatModel) openSessions() tea.Cmd {
	m.sessionsOpen = true
	m.sessionCursor = 0
	m.sessionScroll = 0
	m.sessionsHome = m.workspacePath
	m.sessionErr = ""
	m.sessionConfirm = false
	if m.sessions == nil {
		m.sessionRows = nil
		m.sessionErr = "Sessions are unavailable"
		return nil
	}
	files, err := m.sessions.List()
	if err != nil {
		m.sessionErr = err.Error()
		m.sessionRows = nil
		return nil
	}
	m.sessionRows = files
	return nil
}

func (m *chatModel) handleSessionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.sessionConfirm {
			m.sessionConfirm = false
			return m, nil
		}
		m.sessionsOpen = false
		return m, nil
	case "up":
		if m.sessionCursor > 0 {
			m.sessionCursor--
		}
		m.sessionConfirm = false
		return m, nil
	case "down":
		if m.sessionCursor < len(m.sessionRows) {
			m.sessionCursor++
		}
		m.sessionConfirm = false
		return m, nil
	case "x":
		m.deleteSelectedSession()
		return m, nil
	case "enter":
		return m, m.chooseSession()
	default:
		return m, nil
	}
}

func (m *chatModel) deleteSelectedSession() {
	if m.sessions == nil || m.sessionCursor == 0 {
		m.sessionConfirm = false
		m.sessionErr = "Select a saved chat to delete"
		return
	}
	index := m.sessionCursor - 1
	if index >= len(m.sessionRows) {
		m.sessionConfirm = false
		return
	}
	if !m.sessionConfirm {
		m.sessionConfirm = true
		m.sessionErr = ""
		return
	}
	file := m.sessionRows[index]
	if err := m.sessions.Delete(file.ID); err != nil {
		m.sessionConfirm = false
		m.sessionErr = err.Error()
		return
	}
	m.sessionRows = append(m.sessionRows[:index], m.sessionRows[index+1:]...)
	if m.sessionCursor > len(m.sessionRows) {
		m.sessionCursor = len(m.sessionRows)
	}
	m.sessionConfirm = false
	m.sessionErr = ""
}

func (m *chatModel) chooseSession() tea.Cmd {
	if m.loadSession == nil {
		m.sessionErr = "Sessions are unavailable"
		return nil
	}
	var file *session.File
	if m.sessionCursor > 0 {
		index := m.sessionCursor - 1
		if index >= len(m.sessionRows) {
			return nil
		}
		copied := m.sessionRows[index]
		file = &copied
	}
	m.sessionsOpen = false
	m.sessionLoading = true
	m.sessionErr = ""
	return tea.Batch(m.spinner.Tick, m.loadSession(file))
}

func (m *chatModel) renderSessions() string {
	count := len(m.sessionRows) + 1
	visible := listCapacity(m.height, listChrome(m.width, m.sessionErr), 2)
	m.sessionScroll = fitListOffset(m.sessionScroll, m.sessionCursor, count, visible)
	start, end := 0, count
	if visible > 0 {
		start = m.sessionScroll
		end = start + visible
		if end > count {
			end = count
		}
	}

	var b strings.Builder
	b.WriteString(planTitleStyle.Render(fmt.Sprintf("Sessions (%d)", len(m.sessionRows))))
	b.WriteString("\n\n")
	for index := start; index < end; index++ {
		if index > start {
			b.WriteByte('\n')
		}
		if index == 0 {
			b.WriteString(sessionLine(m.sessionCursor == 0, "New session", m.sessionsHome))
			continue
		}
		file := m.sessionRows[index-1]
		title := file.Title
		if title == "" {
			title = "Untitled"
		}
		when := file.Updated.Local().Format("2006-01-02 15:04")
		b.WriteString(sessionLine(m.sessionCursor == index, title+"  "+when, file.Workspace))
	}
	if m.sessionErr != "" {
		b.WriteString("\n\n")
		b.WriteString(wrapStyled(m.sessionErr, errStyle, m.width))
	}
	b.WriteString("\n\n")
	if m.sessionConfirm && m.sessionCursor > 0 && m.sessionCursor-1 < len(m.sessionRows) {
		title := m.sessionRows[m.sessionCursor-1].Title
		if title == "" {
			title = "Untitled"
		}
		b.WriteString(helpStyle.Render("x delete " + title + " · esc cancel"))
	} else {
		b.WriteString(helpStyle.Render("enter open · x delete · esc back"))
	}
	return b.String()
}

// listChrome is the title, the gap under it, the gap above the help line, and the help line.
// An error adds one blank line plus its wrapped height.
func listChrome(width int, errText string) int {
	chrome := 4
	if strings.TrimSpace(errText) == "" {
		return chrome
	}
	return chrome + 1 + len(strings.Split(wrapBlock(errText, width), "\n"))
}

// listCapacity is how many items fit. Zero means the height is unknown, so the caller shows every row.
func listCapacity(height, chrome, itemLines int) int {
	if height <= 0 || itemLines < 1 {
		return 0
	}
	room := height - chrome
	if room < itemLines {
		return 1
	}
	return room / itemLines
}

// fitListOffset moves the window just enough to keep the cursor on screen.
func fitListOffset(offset, cursor, count, visible int) int {
	if count <= 0 || visible <= 0 || visible >= count {
		return 0
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= count {
		cursor = count - 1
	}
	if offset > cursor {
		offset = cursor
	}
	if offset+visible <= cursor {
		offset = cursor - visible + 1
	}
	if offset < 0 {
		offset = 0
	}
	if maxOffset := count - visible; offset > maxOffset {
		offset = maxOffset
	}
	return offset
}

func sessionLine(selected bool, title, detail string) string {
	if selected {
		title = agentModeStyle.Render(title)
	}
	return title + "\n" + statusStyle.Render("  "+detail)
}
