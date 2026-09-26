package tui

import "github.com/charmbracelet/lipgloss"

var (
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	dividerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	userPrefixStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	assistantPrefixStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("117")).
				Bold(true)

	toolSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("86")).
				Bold(true)
	toolDimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	toolSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("108"))
	toolErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	toolResultStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	diffAddStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	diffDelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))

	diffAddBackground      = lipgloss.Color("#12261a")
	diffAddFocusBackground = lipgloss.Color("#1d4029")
	diffDelBackground      = lipgloss.Color("#2b1417")
	diffDelFocusBackground = lipgloss.Color("#4a1f24")
	diffFrameStyle         = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("238")).
				Padding(0, 1)

	agentModeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	askModeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	planModeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)

	promptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)

	planTitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
)
