package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSecretsToml(t *testing.T) {
	home := t.TempDir()
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "secrets.toml")
	if err := os.WriteFile(path, []byte("openai_api_key = \"sk-test-value\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if values["openai_api_key"] != "sk-test-value" {
		t.Fatalf("values = %#v", values)
	}
	redacted := NewRedactor(values).Apply("key is sk-test-value and also sk-abcdefghijklmnopqrstuvwxyz")
	if redacted != "key is [redacted] and also [redacted]" {
		t.Fatalf("redacted = %q", redacted)
	}
}

func TestLoadRefusesLooseSecretsFile(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "secrets.toml")
	if err := os.WriteFile(path, []byte("openai_api_key = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(home); err == nil {
		t.Fatal("expected loose secrets file to be refused")
	}
}
