package app

import (
	"fmt"

	"github.com/cgund98/gogent"
	"github.com/cgund98/gogent/deepseek"
	"github.com/cgund98/gogent/kimi"
	"github.com/cgund98/gogent/openai"
	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/cgund98/gopi/internal/config"
	"github.com/cgund98/gopi/internal/models"
	gopisecrets "github.com/cgund98/gopi/internal/secrets"
)

type modelFactory struct {
	openaiKey   string
	kimiKey     string
	deepseekKey string
}

func newModelFactory(cfg config.Config) *modelFactory {
	return &modelFactory{openaiKey: cfg.OpenAIAPIKey, kimiKey: cfg.KimiAPIKey, deepseekKey: cfg.DeepSeekAPIKey}
}

// New is the only place that turns a model name into a provider client.
func (f *modelFactory) New(name string, registry *gogent.ToolRegistry, systemPrompt string, effort string) (*openai.Model, error) {
	provider, modelID, err := models.Parse(name)
	if err != nil {
		return nil, err
	}
	switch provider {
	case models.ProviderKimi:
		if f.kimiKey == "" {
			return nil, fmt.Errorf("%s is required", gopisecrets.KimiAPIKey)
		}
		var opts []kimi.Option
		if effort == "none" {
			opts = append(opts, kimi.WithoutThinking())
		}
		return kimi.NewChat(f.kimiKey, registry, opts...).WithModel(modelID).WithSystemPrompt(systemPrompt).Build()
	case models.ProviderDeepSeek:
		if f.deepseekKey == "" {
			return nil, fmt.Errorf("%s is required", gopisecrets.DeepSeekAPIKey)
		}
		var opts []deepseek.Option
		if effort != "none" && effort != "" {
			opts = append(opts, deepseek.WithThinking())
		}
		return deepseek.NewChat(f.deepseekKey, registry, opts...).WithModel(modelID).WithSystemPrompt(systemPrompt).Build()
	default:
		if f.openaiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY is required")
		}
		client := openaisdk.NewClient(option.WithAPIKey(f.openaiKey))
		builder := openai.NewChat(&client, registry).WithModel(modelID).WithSystemPrompt(systemPrompt)
		if effort != "" {
			builder = builder.WithReasoningEffort(effort)
		}
		return builder.Build()
	}
}
