package models

import (
	"fmt"
	"strings"
)

const (
	ProviderOpenAI = "openai"
	ProviderKimi   = "kimi"
)

// supported maps a config name to its provider.
var supported = map[string]string{
	"gpt-4o-mini":    ProviderOpenAI,
	"gpt-4o":         ProviderOpenAI,
	"kimi/kimi-k2.6": ProviderKimi,
}

// Parse checks the whitelist and splits a kimi/ prefix from the wire model id.
func Parse(name string) (provider, modelID string, err error) {
	name = strings.TrimSpace(name)
	provider, ok := supported[name]
	if !ok {
		return "", "", fmt.Errorf("unsupported model %q", name)
	}
	if provider == ProviderKimi {
		return provider, strings.TrimPrefix(name, "kimi/"), nil
	}
	if strings.Contains(name, "/") {
		return "", "", fmt.Errorf("unknown provider in %q", name)
	}
	return provider, name, nil
}
