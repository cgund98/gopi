package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cgund98/gopi/internal/app"
)

func (m *chatModel) openPlans() {
	m.plansOpen = true
	m.planCursor = 0
	m.planConfirm = false
	m.planListErr = ""
	m.planRows = m.listPlans()
}

func (m *chatModel) listPlans() []string {
	dir := filepath.Join(m.workspacePath, ".gopi", "plans")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type planFile struct {
		rel string
		mod int64
	}
	var files []planFile
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(".gopi", "plans", entry.Name()))
		files = append(files, planFile{rel: rel, mod: info.ModTime().UnixNano()})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].mod == files[j].mod {
			return files[i].rel < files[j].rel
		}
		return files[i].mod > files[j].mod
	})
	rows := make([]string, len(files))
	for i, file := range files {
		rows[i] = file.rel
	}
	return rows
}

func (m *chatModel) handlePlansKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.planConfirm {
			m.planConfirm = false
			return m, nil
		}
		m.plansOpen = false
		return m, nil
	case "up":
		if m.planCursor > 0 {
			m.planCursor--
		}
		m.planConfirm = false
		return m, nil
	case "down":
		if m.planCursor+1 < len(m.planRows) {
			m.planCursor++
		}
		m.planConfirm = false
		return m, nil
	case "x":
		m.deleteSelectedPlan()
		return m, nil
	case "enter":
		if m.planCursor < 0 || m.planCursor >= len(m.planRows) {
			return m, nil
		}
		path := m.planRows[m.planCursor]
		m.plansOpen = false
		m.planConfirm = false
		m.openPlan(path)
		return m, nil
	default:
		return m, nil
	}
}

func (m *chatModel) deleteSelectedPlan() {
	if m.planCursor < 0 || m.planCursor >= len(m.planRows) {
		m.planConfirm = false
		m.planListErr = "Select a plan to delete"
		return
	}
	if !m.planConfirm {
		m.planConfirm = true
		m.planListErr = ""
		return
	}
	rel := m.planRows[m.planCursor]
	if err := removePlanFile(m.workspacePath, rel); err != nil {
		m.planConfirm = false
		m.planListErr = err.Error()
		return
	}
	m.planRows = append(m.planRows[:m.planCursor], m.planRows[m.planCursor+1:]...)
	if m.planCursor >= len(m.planRows) && m.planCursor > 0 {
		m.planCursor--
	}
	m.planConfirm = false
	m.planListErr = ""
}

func removePlanFile(workspace, rel string) error {
	full, err := planPath(workspace, rel)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil {
		return fmt.Errorf("remove plan: %w", err)
	}
	return nil
}

func planPath(workspace, rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || strings.Contains(clean, "..") {
		return "", fmt.Errorf("path is outside .gopi/plans")
	}
	root := filepath.Join(".gopi", "plans")
	if !strings.HasPrefix(clean, root+string(filepath.Separator)) || filepath.Ext(clean) != ".md" {
		return "", fmt.Errorf("path is outside .gopi/plans")
	}
	full := filepath.Join(workspace, clean)
	parent := filepath.Join(workspace, root)
	resolved, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	parentAbs, err := filepath.Abs(parent)
	if err != nil {
		return "", err
	}
	if resolved != parentAbs && !strings.HasPrefix(resolved, parentAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path is outside .gopi/plans")
	}
	return full, nil
}

func planBuildPrompt(path string) string {
	return "Implement the plan at " + path + ". Read that file and make the changes it describes."
}

func (m *chatModel) buildPlan() tea.Cmd {
	if m.busy || m.inApprovalMode() || m.planPath == "" {
		return nil
	}
	path := m.planPath
	if m.switchMode != nil {
		if err := m.switchMode(app.ModeAgent); err != nil {
			m.status = err.Error()
			return nil
		}
		m.mode = app.ModeAgent
		m.input.Prompt = modePrompt(app.ModeAgent)
	}
	m.closePlan()
	text := planBuildPrompt(path)
	return m.startRun(func(ctx context.Context) error {
		if m.agent == nil {
			return fmt.Errorf("agent is unavailable")
		}
		return m.agent.RunWithUserInput(ctx, m.chatID, text)
	})
}

func (m *chatModel) renderPlans() string {
	var b strings.Builder
	b.WriteString(planTitleStyle.Render("Plans"))
	b.WriteString("\n\n")
	if len(m.planRows) == 0 {
		b.WriteString("No saved plans")
	}
	for i, path := range m.planRows {
		if i > 0 {
			b.WriteByte('\n')
		}
		line := path
		if i == m.planCursor {
			line = agentModeStyle.Render(line)
		}
		b.WriteString(line)
	}
	if m.planListErr != "" {
		b.WriteString("\n\n")
		b.WriteString(errStyle.Render(m.planListErr))
	}
	b.WriteString("\n\n")
	if m.planConfirm && m.planCursor >= 0 && m.planCursor < len(m.planRows) {
		b.WriteString(helpStyle.Render("x delete " + m.planRows[m.planCursor] + " · esc cancel"))
	} else {
		b.WriteString(helpStyle.Render("enter open · x delete · esc back"))
	}
	return b.String()
}
