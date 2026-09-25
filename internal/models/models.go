package models

import (
	"fmt"
	"sort"
	"strings"
)

const (
	ProviderOpenAI = "openai"
	ProviderKimi   = "kimi"
)

// Model describes one supported model.
type Model struct {
	Provider         string
	ContextWindow    int
	InputPerMillion  float64
	CachedPerMillion float64
	OutputPerMillion float64
}

// supported maps a config name to its provider and prices.
// Prices are USD per one million tokens. A zero price means the cost is unknown.
var supported = map[string]Model{
	"gpt-4o-mini": {
		Provider:         ProviderOpenAI,
		ContextWindow:    128000,
		InputPerMillion:  0.15,
		CachedPerMillion: 0.075,
		OutputPerMillion: 0.60,
	},
	"gpt-4o": {
		Provider:         ProviderOpenAI,
		ContextWindow:    128000,
		InputPerMillion:  2.50,
		CachedPerMillion: 1.25,
		OutputPerMillion: 10,
	},
	"kimi/kimi-k2.6": {
		Provider:         ProviderKimi,
		ContextWindow:    262144,
		InputPerMillion:  0.95,
		CachedPerMillion: 0.16,
		OutputPerMillion: 4,
	},
}

// Names returns the whitelist in a stable order.
func Names() []string {
	names := make([]string, 0, len(supported))
	for name := range supported {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Lookup returns the catalog entry for a whitelisted name.
func Lookup(name string) (Model, bool) {
	model, ok := supported[strings.TrimSpace(name)]
	return model, ok
}

// ContextWindow returns the model's window, or 128000 when the name is unknown.
func ContextWindow(name string) int {
	if model, ok := Lookup(name); ok && model.ContextWindow > 0 {
		return model.ContextWindow
	}
	return 128000
}

// EstimateCost returns the USD cost of one turn. ok is false when the model has no prices.
func EstimateCost(name string, input, output, cached int) (dollars float64, ok bool) {
	model, found := Lookup(name)
	if !found || model.InputPerMillion == 0 && model.OutputPerMillion == 0 {
		return 0, false
	}
	if cached > input {
		cached = input
	}
	fresh := input - cached
	dollars = (float64(fresh)*model.InputPerMillion + float64(cached)*model.CachedPerMillion + float64(output)*model.OutputPerMillion) / 1_000_000
	return dollars, true
}

// Parse checks the whitelist and splits a kimi/ prefix from the wire model id.
func Parse(name string) (provider, modelID string, err error) {
	name = strings.TrimSpace(name)
	model, ok := supported[name]
	if !ok {
		return "", "", fmt.Errorf("unsupported model %q", name)
	}
	if model.Provider == ProviderKimi {
		return model.Provider, strings.TrimPrefix(name, "kimi/"), nil
	}
	if strings.Contains(name, "/") {
		return "", "", fmt.Errorf("unknown provider in %q", name)
	}
	return model.Provider, name, nil
}
