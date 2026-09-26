package tui

import (
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Cursor's terminal sends Shift+Enter as Enter until the program asks for
// disambiguated keys. Flag 1 is the Kitty "disambiguate escape codes" mode:
// plain Enter stays a carriage return, and Shift+Enter becomes CSI 13;2 u.
const (
	enableDisambiguateKeys  = "\x1b[>1u"
	disableDisambiguateKeys = "\x1b[<u"
)

func enableDisambiguateKeysMode() {
	writeTTY(enableDisambiguateKeys)
}

func disableDisambiguateKeysMode() {
	writeTTY(disableDisambiguateKeys)
}

func writeTTY(sequence string) {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer tty.Close()
	_, _ = tty.WriteString(sequence)
}

// translateKitty turns Kitty keyboard sequences back into the keys Bubble Tea
// already understands. Shift+Enter is left alone so the composer can insert a
// newline instead of submitting.
func translateKitty(msg tea.Msg) tea.Msg {
	body, ok := csiBody(msg)
	if !ok {
		return msg
	}
	code, bits, ok := parseKittyKey(body)
	if !ok {
		return msg
	}
	if code == 13 && bits == kittyShift {
		return msg
	}
	if bits == 0 {
		switch code {
		case 27:
			return tea.KeyMsg{Type: tea.KeyEscape}
		case 13:
			return tea.KeyMsg{Type: tea.KeyEnter}
		case 9:
			return tea.KeyMsg{Type: tea.KeyTab}
		case 127:
			return tea.KeyMsg{Type: tea.KeyBackspace}
		}
	}
	if bits == kittyCtrl && code >= 'a' && code <= 'z' {
		return tea.KeyMsg{Type: tea.KeyType(code - 'a' + 1)}
	}
	return msg
}

const (
	kittyShift = 1
	kittyCtrl  = 4
)

func parseKittyKey(body string) (code int, bits int, ok bool) {
	if !strings.HasSuffix(body, "u") {
		return 0, 0, false
	}
	body = strings.TrimSuffix(body, "u")
	codeText, modText, hasMod := strings.Cut(body, ";")
	if strings.Contains(codeText, ":") {
		codeText, _, _ = strings.Cut(codeText, ":")
	}
	code, err := strconv.Atoi(codeText)
	if err != nil {
		return 0, 0, false
	}
	mod := 1
	if hasMod {
		mod, err = strconv.Atoi(modText)
		if err != nil {
			return 0, 0, false
		}
	}
	if mod > 1 {
		bits = mod - 1
	}
	return code, bits, true
}

func csiBody(msg tea.Msg) (string, bool) {
	text, ok := msg.(interface{ String() string })
	if !ok {
		return "", false
	}
	raw := text.String()
	const prefix = "?CSI["
	const suffix = "]?"
	if !strings.HasPrefix(raw, prefix) || !strings.HasSuffix(raw, suffix) {
		return "", false
	}
	fields := strings.Fields(raw[len(prefix) : len(raw)-len(suffix)])
	if len(fields) == 0 {
		return "", false
	}
	buf := make([]byte, len(fields))
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 || n > 255 {
			return "", false
		}
		buf[i] = byte(n)
	}
	return string(buf), true
}
