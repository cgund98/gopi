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
	sslAllow := strings.Index(body, `(allow file-read* (subpath "/private/etc/ssl"))`)
	if sslAllow < 0 || privateDeny >= sslAllow || denyAt >= sslAllow {
		t.Fatalf("CA bundle must stay readable after protected-path denies:\n%s", body)
	}
	nullAllow := strings.Index(body, `(allow file-write* (literal "/dev/null"))`)
	if nullAllow < 0 || denyAt >= nullAllow {
		t.Fatalf("/dev/null must be writable after protected-path denies:\n%s", body)
	}
	if !strings.Contains(body, "(deny network*)") {
		t.Fatalf("missing network deny:\n%s", body)
	}
	if strings.Contains(body, "allow network") {
		t.Fatalf("network must stay denied:\n%s", body)
	}
	allowlist, err := SeatbeltProfile(Profile{Network: NetworkAllowlist, ProxyPorts: []int{9}})
	if err != nil {
		t.Fatal(err)
	}
	denyNet := strings.Index(allowlist, "(deny network*)")
	allowNet := strings.Index(allowlist, `(allow network* (remote tcp "localhost:9"))`)
	if denyNet < 0 || allowNet < 0 || denyNet >= allowNet {
		t.Fatalf("proxy allow must follow the network deny:\n%s", allowlist)
	}
	open, err := SeatbeltProfile(Profile{Network: NetworkUnrestricted})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(open, "(deny network*)") >= strings.Index(open, "(allow network*)") {
		t.Fatalf("unrestricted allow must follow the network deny:\n%s", open)
	}
	elevated := Profile{
		DenyRead:   []string{"^(.*/)?\\.[eE][nN][vV](/.*)?$"},
		ExtraReads: []string{"/work/.env"},
		Network:    NetworkDeny,
	}
	elevatedBody, err := SeatbeltProfile(elevated)
	if err != nil {
		t.Fatal(err)
	}
	envDeny := strings.Index(elevatedBody, `(deny file-read* (regex "^(.*/)?\\.[eE][nN][vV](/.*)?$"))`)
	extraAt := strings.Index(elevatedBody, `(allow file-read* (subpath "/work/.env"))`)
	if envDeny < 0 || extraAt < 0 || envDeny >= extraAt {
		t.Fatalf("approved extra path must follow the protected deny:\n%s", elevatedBody)
	}
}

func TestSeatbeltSessionGrantThenFloor(t *testing.T) {
	dir := "/Users/me/other"
	env := dir + "/.env"
	body, err := SeatbeltProfile(Profile{
		ReadRoots:    []string{"/Users/me/work"},
		SessionReads: []string{dir},
		DenyRead:     []string{"^(.*/)?\\.[eE][nN][vV](/.*)?$"},
		ExtraReads:   []string{env},
		Network:      NetworkDeny,
	})
	if err != nil {
		t.Fatal(err)
	}
	allow := strings.Index(body, `(allow file-read* (subpath "`+dir+`"))`)
	deny := strings.LastIndex(body, `(deny file-read* (regex "^(.*/)?\\.[eE][nN][vV](/.*)?$"))`)
	extra := strings.Index(body, `(allow file-read* (subpath "`+env+`"))`)
	if allow < 0 || deny < 0 || extra < 0 || allow >= deny || deny >= extra {
		t.Fatalf("session allow, then floor deny, then approved file:\n%s", body)
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

func TestLaunchWritesDevNull(t *testing.T) {
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
		Argv:       []string{"/bin/sh", "-c", "echo hi >/dev/null"},
	}
	result, err := Launch(context.Background(), profile)
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
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
