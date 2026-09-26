// Package toolview defines how a custom tool describes its calls to the terminal UI.
package toolview

import "encoding/json"

// Renderer is optional. A custom tool that also implements it controls how gopi
// draws its calls. An empty headline or a zero View keeps gopi's default.
type Renderer interface {
	// Headline replaces the tool name on the "> ..." line.
	Headline(args json.RawMessage) string
	// RenderApproval draws the body of the approval prompt, below the reason.
	RenderApproval(args json.RawMessage) View
	// RenderResult draws a completed result. Error results never reach it.
	RenderResult(args, result json.RawMessage) View
}

// View is plain text that gopi frames, styles, and wraps.
type View struct {
	// Hide shows only the headline.
	Hide bool
	// Fields are aligned "label  value" rows, drawn first.
	Fields []Field
	// Lines are drawn dim after the fields.
	Lines []string
	// Markdown is rendered last.
	Markdown string
}

// Field is one labeled row in a View.
type Field struct {
	Label string
	Value string
}

// IsZero reports a view that asks for gopi's default rendering.
func (v View) IsZero() bool {
	return !v.Hide && len(v.Fields) == 0 && len(v.Lines) == 0 && v.Markdown == ""
}
