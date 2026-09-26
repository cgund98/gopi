package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cgund98/gogent"

	gopisecrets "github.com/cgund98/gopi/internal/secrets"
	"github.com/cgund98/gopi/internal/toolview"
)

type fakeRenderer struct {
	headline string
	approval toolview.View
	result   toolview.View
	panics   bool
}

func (r fakeRenderer) Headline(json.RawMessage) string {
	if r.panics {
		panic("headline")
	}
	return r.headline
}

func (r fakeRenderer) RenderApproval(json.RawMessage) toolview.View {
	if r.panics {
		panic("approval")
	}
	return r.approval
}

func (r fakeRenderer) RenderResult(json.RawMessage, json.RawMessage) toolview.View {
	if r.panics {
		panic("result")
	}
	return r.result
}

func customCard(renderer toolview.Renderer, result string) toolCardView {
	return toolCardView{
		ToolName: "google_calendar",
		Args:     json.RawMessage(`{"operation":"list_events"}`),
		State:    toolCardCompleted,
		Result:   result,
		Renderer: safeRenderer{inner: renderer},
	}
}

func TestCustomHeadlineReplacesToolName(t *testing.T) {
	if got := toolHeadline(customCard(fakeRenderer{headline: "calendar list"}, `{}`)); got != "calendar list" {
		t.Fatalf("headline = %q", got)
	}
	if got := toolHeadline(customCard(fakeRenderer{}, `{}`)); got != "google_calendar" {
		t.Fatalf("empty headline = %q", got)
	}
}

func TestCustomResultRendersViewInFrame(t *testing.T) {
	renderer := fakeRenderer{result: toolview.View{
		Fields:   []toolview.Field{{Label: "Summary", Value: "Standup"}, {Label: "Start", Value: "09:00"}},
		Lines:    []string{"one event"},
		Markdown: "**done**",
	}}
	card := customCard(renderer, `{"events":[]}`)
	if hideToolResult(card) {
		t.Fatal("result with a view is hidden")
	}
	got := stripANSI(renderFriendlyResult(card, card.Result, 60))
	for _, want := range []string{"Summary  Standup", "Start    09:00", "one event", "done", "╭"} {
		if !strings.Contains(got, want) {
			t.Fatalf("result missing %q:\n%s", want, got)
		}
	}
}

func TestCustomResultHideShowsOnlyHeadline(t *testing.T) {
	card := customCard(fakeRenderer{result: toolview.View{Hide: true}}, `{"ok":true}`)
	if !hideToolResult(card) {
		t.Fatal("hidden view still shows the result")
	}
}

func TestCustomResultFallsBack(t *testing.T) {
	zero := customCard(fakeRenderer{}, `{"ok":true}`)
	if hideToolResult(zero) || renderFriendlyResult(zero, zero.Result, 60) != "" {
		t.Fatal("zero view did not fall back to the default")
	}

	failed := customCard(fakeRenderer{result: toolview.View{Hide: true, Lines: []string{"nope"}}}, `{"error":"quota"}`)
	if hideToolResult(failed) || renderFriendlyResult(failed, failed.Result, 60) != "" {
		t.Fatal("error result reached the renderer")
	}

	broken := customCard(fakeRenderer{panics: true}, `{"ok":true}`)
	if toolHeadline(broken) != "google_calendar" || hideToolResult(broken) || renderFriendlyResult(broken, broken.Result, 60) != "" {
		t.Fatal("panicking renderer did not fall back")
	}
}

func TestCustomRendererOutputIsSanitizedAndRedacted(t *testing.T) {
	secret := "sk-calendar-secret-123"
	redact := gopisecrets.NewRedactor(map[string]string{"gcal_token": secret}).Apply
	renderers := safeRenderers(map[string]toolview.Renderer{"google_calendar": fakeRenderer{
		headline: "calendar \x1b[31mred\x1b[0m\nline",
		result:   toolview.View{Lines: []string{"token " + secret, "bell\a\x1b]0;title\x07done"}},
	}}, redact)
	card := customCard(nil, `{"ok":true}`)
	card.Renderer = renderers["google_calendar"]

	if got := toolHeadline(card); got != "calendar red line" {
		t.Fatalf("headline = %q", got)
	}
	got := renderFriendlyResult(card, card.Result, 60)
	plain := stripANSI(got)
	if strings.Contains(plain, secret) || !strings.Contains(plain, "bell") || !strings.Contains(plain, "done") {
		t.Fatalf("result = %q", plain)
	}
	if strings.Contains(got, "\a") || strings.Contains(got, "]0;") {
		t.Fatalf("escape survived: %q", got)
	}
}

func TestApprovalPromptUsesCustomRenderer(t *testing.T) {
	pending := gogent.PendingToolCall{
		ToolName: "google_calendar",
		Args:     json.RawMessage(`{"operation":"create_event","summary":"Standup"}`),
		Reason:   "creates an event",
	}
	renderer := safeRenderer{inner: fakeRenderer{
		headline: "calendar create Standup",
		approval: toolview.View{Fields: []toolview.Field{{Label: "Summary", Value: "Standup"}}},
	}}
	view := stripANSI(renderApprovalPrompt(pending, renderer, 60))
	if !strings.Contains(view, "calendar create Standup") || !strings.Contains(view, "Summary  Standup") || strings.Contains(view, `"operation"`) {
		t.Fatalf("prompt = %q", view)
	}

	fallback := stripANSI(renderApprovalPrompt(pending, safeRenderer{inner: fakeRenderer{}}, 60))
	if !strings.Contains(fallback, `"operation"`) || !strings.Contains(fallback, "creates an event") {
		t.Fatalf("fallback prompt = %q", fallback)
	}
}
