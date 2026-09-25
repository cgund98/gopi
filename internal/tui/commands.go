package tui

import (
	"strings"
	"time"

	"github.com/cgund98/gogent"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cgund98/gopi/internal/models"
)

type compactDoneMsg struct {
	Summary gogent.Message
	Keep    []gogent.Message
	Err     error
}

func (m *chatModel) handleModel(text string) {
	fields := strings.Fields(text)
	if len(fields) == 1 {
		m.status = "Models: " + strings.Join(models.Names(), ", ")
		return
	}
	if len(fields) != 2 {
		m.status = "Usage: /model <name>"
		return
	}
	if m.busy || m.inApprovalMode() {
		m.status = "Finish the current turn before changing the model"
		return
	}
	if m.setModel == nil {
		m.status = "Model switch is unavailable"
		return
	}
	if err := m.setModel(fields[1]); err != nil {
		m.err = err
		m.status = err.Error()
		return
	}
	m.err = nil
	m.status = "Model set to " + fields[1]
}

func (m *chatModel) handleMouse(text string) tea.Cmd {
	fields := strings.Fields(text)
	off := !m.mouseOff
	if len(fields) == 2 && (fields[1] == "on" || fields[1] == "off") {
		off = fields[1] == "off"
	} else if len(fields) != 1 {
		m.status = "Usage: /mouse [on|off]"
		return nil
	}
	m.mouseOff = off
	if off {
		m.status = "Mouse off: the terminal selects text; scroll with the keyboard"
		return tea.DisableMouse
	}
	m.status = "Mouse on: the wheel scrolls gopi; hold Option (iTerm) or Shift to select"
	return tea.EnableMouseCellMotion
}

func (m *chatModel) compact() tea.Cmd {
	if m.busy || m.inApprovalMode() {
		m.status = "Finish the current turn before compacting"
		return nil
	}
	index := -1
	for i, message := range m.messages {
		if message.Role == gogent.MessageRoleUser {
			index = i
		}
	}
	if index <= 0 {
		m.status = "Nothing to compact"
		return nil
	}
	older := m.messages[:index]
	keep := append([]gogent.Message(nil), m.messages[index:]...)
	var text strings.Builder
	for _, message := range older {
		text.WriteString(string(message.Role))
		text.WriteString(": ")
		text.WriteString(message.Content)
		text.WriteByte('\n')
	}
	if m.summarize == nil {
		m.status = "Compact is unavailable"
		return nil
	}
	summarize := m.summarize
	body := text.String()
	ctx := m.ctx
	m.busy = true
	m.status = ""
	m.err = nil
	m.workStarted = time.Now()
	return tea.Batch(func() tea.Msg {
		summary, err := summarize(ctx, body)
		return compactDoneMsg{Summary: summary, Keep: keep, Err: err}
	}, m.spinner.Tick)
}
