package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/cgund98/gogent"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

const planViewHelp = "esc or q back to chat"

func (m *chatModel) noticeWrittenPlans() {
	if m.seenPlans == nil {
		m.seenPlans = map[string]bool{}
	}
	for _, message := range m.messages {
		if message.Role != gogent.MessageRoleTool || message.ToolCallID == "" || m.seenPlans[message.ToolCallID] {
			continue
		}
		card, ok := toolCardByID(m.toolCards, message.ToolCallID)
		if !ok || card.ToolName != "write_plan" {
			continue
		}
		m.seenPlans[message.ToolCallID] = true
		var payload struct {
			Path  string `json:"path"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(message.Content), &payload); err != nil || payload.Error != "" || payload.Path == "" {
			continue
		}
		m.openPlan(payload.Path)
	}
}

func (m *chatModel) openPlan(rel string) {
	full := rel
	if m.workspacePath != "" && !filepath.IsAbs(rel) {
		full = filepath.Join(m.workspacePath, filepath.FromSlash(rel))
	}
	body, err := os.ReadFile(full)
	if err != nil {
		m.err = err
		m.status = err.Error()
		return
	}
	m.planOpen = true
	m.planPath = rel
	m.planBody = string(body)
	if m.planVP.Width == 0 {
		m.planVP = viewport.New(80, 20)
		m.planVP.MouseWheelEnabled = false
	}
	m.renderPlan()
	m.input.Blur()
}

func (m *chatModel) renderPlan() {
	width := m.width
	if width < 20 {
		width = 20
	}
	m.planVP.Width = width
	height := m.height - 3
	if height < 4 {
		height = 4
	}
	m.planVP.Height = height
	title := planTitleStyle.Render(m.planPath)
	body := renderMarkdown(m.planBody, width)
	m.planVP.SetContent(title + "\n\n" + body)
}

func (m *chatModel) closePlan() {
	m.planOpen = false
	m.planPath = ""
	m.planBody = ""
	if !m.busy && !m.inApprovalMode() {
		m.input.Focus()
	}
}

func (m *chatModel) handlePlanKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.closePlan()
		return m, nil
	case "up", "down", "pgup", "pgdown", "home", "end":
		var cmd tea.Cmd
		m.planVP, cmd = m.planVP.Update(msg)
		return m, cmd
	default:
		return m, nil
	}
}

func (m *chatModel) planView() string {
	m.renderPlan()
	var b strings.Builder
	b.WriteString(m.planVP.View())
	b.WriteByte('\n')
	b.WriteString(renderDivider(m.width))
	b.WriteByte('\n')
	b.WriteString(helpStyle.Render(planViewHelp))
	return b.String()
}
