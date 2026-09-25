package app

import (
	"strings"
	"testing"

	"github.com/cgund98/gogent"

	"github.com/cgund98/gopi/internal/config"
	gopisecrets "github.com/cgund98/gopi/internal/secrets"
)

func TestFactorySelectsKimi(t *testing.T) {
	factory := newModelFactory(config.Config{KimiAPIKey: "kimi"})
	model, err := factory.New("kimi/kimi-k2.6", gogent.NewToolRegistry(), "sys")
	if err != nil {
		t.Fatal(err)
	}
	if model.SystemPrompt() != "sys" {
		t.Fatalf("prompt = %q", model.SystemPrompt())
	}
	if _, err := factory.New("kimi/kimi-k2.6-nothink", gogent.NewToolRegistry(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.New("kimi/kimi-k3", gogent.NewToolRegistry(), ""); err == nil {
		t.Fatal("expected unlisted model to fail")
	}
	openaiOnly := newModelFactory(config.Config{OpenAIAPIKey: "openai"})
	if _, err := openaiOnly.New("gpt-4o", gogent.NewToolRegistry(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := openaiOnly.New("kimi/kimi-k2.6", gogent.NewToolRegistry(), ""); err == nil || !strings.Contains(err.Error(), gopisecrets.KimiAPIKey) {
		t.Fatalf("err = %v", err)
	}
}
