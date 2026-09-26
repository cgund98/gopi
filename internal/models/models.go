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

// noThinkSuffix names a Kimi model that is sent with thinking disabled.
const noThinkSuffix = "-nothink"

// Model describes one supported model.
type Model struct {
	Provider         string
	DisableThinking  bool
	ContextWindow    int
	InputPerMillion  float64
	CachedPerMillion float64
	OutputPerMillion float64
}

// supported maps a config name to its provider and prices.
// Prices are USD per one million tokens. A zero price means the cost is unknown.
// GPT-5.6 windows stop at 272K because longer prompts bill at 2x input and 1.5x output.
var supported = map[string]Model{
	"gpt-5.6-sol": {
		Provider:         ProviderOpenAI,
		ContextWindow:    272000,
		InputPerMillion:  4,
		CachedPerMillion: 0.40,
		OutputPerMillion: 20,
	},
	"gpt-5.6-terra": {
		Provider:         ProviderOpenAI,
		ContextWindow:    272000,
		InputPerMillion:  2,
		CachedPerMillion: 0.20,
		OutputPerMillion: 12,
	},
	"gpt-5.6-luna": {
		Provider:         ProviderOpenAI,
		ContextWindow:    272000,
		InputPerMillion:  0.20,
		CachedPerMillion: 0.02,
		OutputPerMillion: 1.20,
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
	"kimi/kimi-k2.6" + noThinkSuffix: {
		Provider:         ProviderKimi,
		DisableThinking:  true,
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

// Parse checks the whitelist and strips the kimi/ prefix and -nothink suffix from the wire model id.
func Parse(name string) (provider, modelID string, err error) {
	name = strings.TrimSpace(name)
	model, ok := supported[name]
	if !ok {
		return "", "", fmt.Errorf("unsupported model %q", name)
	}
	if model.Provider == ProviderKimi {
		modelID = strings.TrimPrefix(name, "kimi/")
		if model.DisableThinking {
			modelID = strings.TrimSuffix(modelID, noThinkSuffix)
		}
		return model.Provider, modelID, nil
	}
	if strings.Contains(name, "/") {
		return "", "", fmt.Errorf("unknown provider in %q", name)
	}
	return model.Provider, name, nil
}
