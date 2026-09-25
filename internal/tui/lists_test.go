package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cgund98/gopi/internal/session"
)

func TestSessionListFollowsCursor(t *testing.T) {
	rows := make([]session.File, 8)
	for i := range rows {
		rows[i] = session.File{Title: fmt.Sprintf("Chat %d", i), Workspace: "/work", Updated: time.Now().UTC()}
	}
	m := &chatModel{
		height:      10,
		width:       40,
		sessionRows: rows,
	}

	view := m.renderSessions()
	if !strings.Contains(view, "Sessions (8)") || !strings.Contains(view, "New session") || !strings.Contains(view, "Chat 1") || strings.Contains(view, "Chat 7") || !strings.Contains(view, statusStyle.Render("  /work")) {
		t.Fatalf("top of list = %q", view)
	}

	m.sessionCursor = 8
	view = m.renderSessions()
	if strings.Contains(view, "New session") || !strings.Contains(view, "Chat 7") || !strings.Contains(view, "Chat 5") {
		t.Fatalf("scrolled list = %q", view)
	}

	m.sessionCursor = 7
	view = m.renderSessions()
	if strings.Contains(view, "New session") || !strings.Contains(view, "Chat 5") || !strings.Contains(view, "Chat 6") {
		t.Fatalf("window jumped = %q", view)
	}
}

func TestPlanListFollowsCursor(t *testing.T) {
	rows := make([]string, 12)
	for i := range rows {
		rows[i] = fmt.Sprintf(".gopi/plans/plan-%02d.md", i)
	}
	m := &chatModel{
		height:   8,
		width:    40,
		planRows: rows,
	}

	view := m.renderPlans()
	if !strings.Contains(view, "Plans (12)") || !strings.Contains(view, "plan-00.md") || strings.Contains(view, "plan-11.md") {
		t.Fatalf("top of list = %q", view)
	}

	m.planCursor = 11
	view = m.renderPlans()
	if strings.Contains(view, "plan-00.md") || !strings.Contains(view, "plan-11.md") || !strings.Contains(view, "plan-08.md") {
		t.Fatalf("scrolled list = %q", view)
	}
}
