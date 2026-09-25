package sandbox

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestSeatbeltDeniesAfterWorkspaceAllow(t *testing.T) {
	profile := Profile{
		Home:       "/Users/me",
		ReadRoots:  []string{"/Users/me/work"},
		WriteRoots: []string{"/Users/me/work"},
		DenyRead:   []string{"^(.*/)?\\.[eE][nN][vV](/.*)?$"},
		DenyWrite:  []string{"^/Users/me/work/\\.git/hooks(/.*)?$"},
		Network:    NetworkDeny,
	}
	body, err := SeatbeltProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	homeDeny := strings.Index(body, `(deny file-read* (subpath "/Users"))`)
	allowAt := strings.Index(body, `(allow file-write* (subpath "/Users/me/work"))`)
	denyAt := strings.Index(body, `(deny file-read* (regex "^(.*/)?\\.[eE][nN][vV](/.*)?$"))`)
	if homeDeny < 0 || allowAt < 0 || denyAt < 0 || homeDeny >= allowAt || allowAt >= denyAt {
		t.Fatalf("home deny, workspace allow, then protected deny:\n%s", body)
	}
	privateDeny := strings.Index(body, `(deny file-read* (subpath "/private"))`)
	selectAllow := strings.Index(body, `(allow file-read* (subpath "/private/var/select"))`)
	if privateDeny < 0 || selectAllow < 0 || privateDeny >= selectAllow {
		t.Fatalf("shell locale path must be readable after the /private deny:\n%s", body)
	}
	if !strings.Contains(body, "(deny network*)") {
		t.Fatalf("missing network deny:\n%s", body)
	}
	if strings.Contains(body, "allow network") {
		t.Fatalf("network must stay denied:\n%s", body)
	}
}

func TestLaunchEcho(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS")
	}
	tmp, err := SessionTemp()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	profile := Profile{
		WriteRoots: []string{tmp},
		Network:    NetworkDeny,
		Env:        ScrubbedEnv(tmp),
		WorkDir:    tmp,
		Argv:       []string{"/bin/echo", "hi"},
	}
	body, err := SeatbeltProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Launch(context.Background(), profile)
	if err != nil || result.ExitCode != 0 || result.Stdout != "hi\n" {
		t.Fatalf("result=%#v err=%v\n%s", result, err, body)
	}
}

func TestScrubbedEnvOmitsParentSecrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "secret")
	t.Setenv("DYLD_INSERT_LIBRARIES", "/tmp/evil.dylib")
	env := ScrubbedEnv("/private/tmp/gopi")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "secret") || strings.Contains(joined, "DYLD_") || strings.Contains(joined, "OPENAI") {
		t.Fatalf("env = %q", env)
	}
	if !strings.Contains(joined, "TMPDIR=/private/tmp/gopi") {
		t.Fatalf("env = %q", env)
	}
}
