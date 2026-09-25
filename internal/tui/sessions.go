package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gopi/internal/app"
	"github.com/cgund98/gopi/internal/session"
)

const helpText = `/agent, /ask, /plan, and /mode <name> switch the session mode
/sessions opens the saved-chat list
/help shows this list`

type sessionSavedMsg struct {
	Title string
	Err   error
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
	return func() tea.Msg {
		messages, err := store.Load(ctx, id)
		if err != nil || len(messages) == 0 {
			return nil
		}
		title := ""
		if existing, err := sessions.Load(id); err == nil {
			title = existing.Title
		}
		if title == "" {
			title = generateTitle(ctx, titleFn, messages)
		}
		err = sessions.Save(session.File{
			ID:        id,
			Title:     title,
			Workspace: workspace,
			Mode:      mode,
			Updated:   time.Now().UTC(),
			Messages:  messages,
		})
		return sessionSavedMsg{Title: title, Err: err}
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
	var b strings.Builder
	b.WriteString(planTitleStyle.Render("Sessions"))
	b.WriteString("\n\n")
	b.WriteString(sessionLine(m.sessionCursor == 0, "New session", m.sessionsHome))
	for i, file := range m.sessionRows {
		title := file.Title
		if title == "" {
			title = "Untitled"
		}
		when := file.Updated.Local().Format("2006-01-02 15:04")
		b.WriteString("\n")
		b.WriteString(sessionLine(m.sessionCursor == i+1, title+"  "+when, file.Workspace))
	}
	if m.sessionErr != "" {
		b.WriteString("\n\n")
		b.WriteString(errStyle.Render(m.sessionErr))
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

func sessionLine(selected bool, title, detail string) string {
	line := fmt.Sprintf("%s\n  %s", title, detail)
	if selected {
		return agentModeStyle.Render(line)
	}
	return line
}
