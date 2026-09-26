package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cgund98/gogent"
)

// WrapRedacting redacts secrets from results and undoes json.Marshal's HTML
// escaping, so the model sees & < > as written and copies them back exactly.
func WrapRedacting(inner gogent.Tool, redact func(string) string) gogent.Tool {
	return redactingTool{inner: inner, redact: redact}
}

type redactingTool struct {
	inner  gogent.Tool
	redact func(string) string
}

func (t redactingTool) Name() string                { return t.inner.Name() }
func (t redactingTool) Description() string         { return t.inner.Description() }
func (t redactingTool) Parameters() json.RawMessage { return t.inner.Parameters() }

func (t redactingTool) RequiresApproval(ctx context.Context, args json.RawMessage) (gogent.ApprovalDecision, error) {
	return t.inner.RequiresApproval(ctx, args)
}

func (t redactingTool) Execute(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	result, err := t.inner.Execute(ctx, args)
	if err != nil || len(result) == 0 {
		return result, err
	}
	text := unescapeHTML(string(result))
	if t.redact != nil {
		text = t.redact(text)
	}
	return json.RawMessage(text), nil
}

var htmlEscapes = map[string]string{`\u0026`: "&", `\u003c`: "<", `\u003e`: ">"}

// unescapeHTML consumes other escapes in pairs, so an escaped backslash
// followed by u0026 stays literal text.
func unescapeHTML(text string) string {
	if !strings.Contains(text, `\u00`) {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(text); {
		if text[i] == '\\' && i+6 <= len(text) {
			if plain, ok := htmlEscapes[text[i:i+6]]; ok {
				out.WriteString(plain)
				i += 6
				continue
			}
			out.WriteString(text[i : i+2])
			i += 2
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}

var _ gogent.Tool = redactingTool{}
