package secrets

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

const fileName = "secrets.toml"

// Load reads ~/.gopi/secrets.toml. A missing file yields an empty map.
// A group- or world-readable file is refused.
func Load(homeDir string) (map[string]string, error) {
	path := homeDir + "/" + fileName
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("stat secrets: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is mode %o; refuse to load unless it is 0600", path, info.Mode().Perm())
	}
	var values map[string]string
	if _, err := toml.DecodeFile(path, &values); err != nil {
		return nil, fmt.Errorf("decode secrets: %w", err)
	}
	if values == nil {
		values = map[string]string{}
	}
	return values, nil
}

// Redactor replaces known secret values and common token shapes.
type Redactor struct {
	values []string
}

func NewRedactor(values map[string]string) Redactor {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return Redactor{values: out}
}

func (r Redactor) Apply(text string) string {
	for _, value := range r.values {
		text = strings.ReplaceAll(text, value, "[redacted]")
	}
	for _, shape := range tokenShapes(text) {
		text = strings.ReplaceAll(text, shape, "[redacted]")
	}
	return text
}

func tokenShapes(text string) []string {
	var found []string
	for _, field := range strings.Fields(text) {
		trimmed := strings.Trim(field, `"'`)
		if looksLikeToken(trimmed) {
			found = append(found, trimmed)
		}
	}
	return found
}

func looksLikeToken(value string) bool {
	switch {
	case strings.HasPrefix(value, "sk-") && len(value) > 8:
		return true
	case strings.HasPrefix(value, "github_pat_") && len(value) > 16:
		return true
	case strings.HasPrefix(value, "AKIA") && len(value) >= 16:
		return true
	case strings.Contains(value, "BEGIN ") && strings.Contains(value, "PRIVATE KEY"):
		return true
	default:
		return false
	}
}
