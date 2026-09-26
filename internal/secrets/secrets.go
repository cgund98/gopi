package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

const fileName = "secrets.toml"

const (
	OpenAIAPIKey   = "openai_api_key"
	KimiAPIKey     = "kimi_api_key"
	DeepSeekAPIKey = "deepseek_api_key"
	SearchAPIKey   = "search_api_key"
)

// HostOnly reports keys that stay on the host and are never offered to shell.
// extra adds names claimed by custom tool factories.
func HostOnly(name string, extra ...string) bool {
	switch name {
	case OpenAIAPIKey, KimiAPIKey, DeepSeekAPIKey, SearchAPIKey:
		return true
	}
	for _, claimed := range extra {
		if claimed == name {
			return true
		}
	}
	return false
}

// Load reads ~/.gopi/secrets.toml. A missing file yields empty maps.
// A group- or world-readable file is refused. An entry is a string, or a table
// { file = "path" } whose file contents become the value; files maps those names
// to the canonical path.
func Load(homeDir string) (values, files map[string]string, err error) {
	values, files = map[string]string{}, map[string]string{}
	path := filepath.Join(homeDir, fileName)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return values, files, nil
		}
		return nil, nil, fmt.Errorf("stat secrets: %w", err)
	}
	if err := requirePrivate(path); err != nil {
		return nil, nil, err
	}
	var raw map[string]any
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return nil, nil, fmt.Errorf("decode secrets: %w", err)
	}
	for name, entry := range raw {
		switch entry := entry.(type) {
		case string:
			values[name] = entry
		case map[string]any:
			ref, ok := entry["file"].(string)
			if !ok || len(entry) != 1 {
				return nil, nil, fmt.Errorf("secret %s: a table must be { file = \"path\" }", name)
			}
			value, canonical, err := readSecretFile(ref)
			if err != nil {
				return nil, nil, fmt.Errorf("secret %s: %w", name, err)
			}
			values[name] = value
			files[name] = canonical
		default:
			return nil, nil, fmt.Errorf("secret %s: must be a string or { file = \"path\" }", name)
		}
	}
	return values, files, nil
}

func requirePrivate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is mode %o; refuse to load unless it is 0600", path, info.Mode().Perm())
	}
	return nil
}

func readSecretFile(ref string) (value, canonical string, err error) {
	path := ref
	if rest, ok := strings.CutPrefix(ref, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", fmt.Errorf("resolve home directory: %w", err)
		}
		path = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(path) {
		return "", "", fmt.Errorf("file %q must be absolute or start with ~/", ref)
	}
	canonical, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", err
	}
	if err := requirePrivate(canonical); err != nil {
		return "", "", err
	}
	body, err := os.ReadFile(canonical)
	if err != nil {
		return "", "", err
	}
	return strings.TrimRight(string(body), "\r\n"), canonical, nil
}

// OfferNames lists broker keys the user can attach to a shell call.
// Host-only keys, including extra names claimed by tool factories, are omitted.
func OfferNames(values map[string]string, extra ...string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		if HostOnly(name, extra...) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Redactor replaces known secret values and common token shapes.
type Redactor struct {
	values []string
}

func NewRedactor(values map[string]string) Redactor {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
		out = append(out, jsonLeaves(value)...)
	}
	return Redactor{values: out}
}

// minLeafLength keeps short JSON fields such as "Bearer" from redacting ordinary text.
const minLeafLength = 9

// jsonLeaves returns credential fields of a JSON object value, such as
// access_token and refresh_token, so an echo of one field is still redacted.
// Only keys that name a credential count, so URLs and timestamps stay visible.
func jsonLeaves(value string) []string {
	if !strings.HasPrefix(value, "{") {
		return nil
	}
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) != nil {
		return nil
	}
	var leaves []string
	var walk func(key string, node any)
	walk = func(key string, node any) {
		switch node := node.(type) {
		case map[string]any:
			for childKey, child := range node {
				walk(childKey, child)
			}
		case []any:
			for _, child := range node {
				walk(key, child)
			}
		case string:
			if len(node) >= minLeafLength && credentialKey(key) {
				leaves = append(leaves, node)
			}
		}
	}
	walk("", decoded)
	return leaves
}

func credentialKey(key string) bool {
	key = strings.ToLower(key)
	for _, word := range []string{"token", "secret", "key", "password"} {
		if strings.Contains(key, word) {
			return true
		}
	}
	return false
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
