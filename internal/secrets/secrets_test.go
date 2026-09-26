package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSecretsToml(t *testing.T) {
	home := t.TempDir()
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "secrets.toml")
	if err := os.WriteFile(path, []byte(OpenAIAPIKey+" = \"sk-test-value\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	values, _, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if values[OpenAIAPIKey] != "sk-test-value" {
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
	if err := os.WriteFile(path, []byte(OpenAIAPIKey+" = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(home); err == nil {
		t.Fatal("expected loose secrets file to be refused")
	}
}

func writeSecrets(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "secrets.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestLoadReadsFileSecret(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "token.json")
	if err := os.WriteFile(token, []byte(`{"refresh_token":"1//refresh-value"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := writeSecrets(t, "plain = \"abc\"\ngcal_token = { file = \""+token+"\" }\n")
	values, files, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := filepath.EvalSymlinks(token)
	if values["gcal_token"] != `{"refresh_token":"1//refresh-value"}` || values["plain"] != "abc" {
		t.Fatalf("values = %#v", values)
	}
	if len(files) != 1 || files["gcal_token"] != canonical {
		t.Fatalf("files = %#v, want %s", files, canonical)
	}
}

func TestLoadRejectsBadFileSecrets(t *testing.T) {
	dir := t.TempDir()
	loose := filepath.Join(dir, "loose.json")
	if err := os.WriteFile(loose, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"loose file":   "gcal_token = { file = \"" + loose + "\" }\n",
		"missing file": "gcal_token = { file = \"" + filepath.Join(dir, "absent.json") + "\" }\n",
		"no file key":  "gcal_token = { path = \"/tmp/x\" }\n",
		"relative":     "gcal_token = { file = \"token.json\" }\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := Load(writeSecrets(t, body))
			if err == nil || !strings.Contains(err.Error(), "gcal_token") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestRedactorCoversJSONCredentialFields(t *testing.T) {
	token := `{"access_token":"ya29.access-value","refresh_token":"1//refresh-value","token_type":"Bearer","expiry":"2026-09-25T15:00:00Z","auth_uri":"https://accounts.google.com/o/oauth2/auth"}`
	redact := NewRedactor(map[string]string{"gcal_token": token}).Apply
	got := redact("refresh=1//refresh-value access=ya29.access-value type=Bearer expiry=2026-09-25T15:00:00Z uri=https://accounts.google.com/o/oauth2/auth")
	want := "refresh=[redacted] access=[redacted] type=Bearer expiry=2026-09-25T15:00:00Z uri=https://accounts.google.com/o/oauth2/auth"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}
