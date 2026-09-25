package sandbox

import (
	"fmt"
	"os"
)

// ScrubbedEnv is a minimal environment. The parent process environment is not copied.
// DYLD injection variables and secret-bearing names are never set.
func ScrubbedEnv(tmpdir string) []string {
	return []string{
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin",
		"TMPDIR=" + tmpdir,
		"TMP=" + tmpdir,
		"TEMP=" + tmpdir,
		"LANG=C",
		"LC_ALL=C",
	}
}

// WithProxyEnv points HTTP clients at the loopback proxies and clears NO_PROXY.
func WithProxyEnv(env []string, httpPort, socksPort int) []string {
	httpURL := fmt.Sprintf("http://127.0.0.1:%d", httpPort)
	socksURL := fmt.Sprintf("socks5://127.0.0.1:%d", socksPort)
	env = append(env,
		"HTTP_PROXY="+httpURL,
		"HTTPS_PROXY="+httpURL,
		"http_proxy="+httpURL,
		"https_proxy="+httpURL,
		"ALL_PROXY="+socksURL,
		"all_proxy="+socksURL,
		"NO_PROXY=",
		"no_proxy=",
	)
	return env
}

// SessionTemp creates a private temp directory for one command.
func SessionTemp() (string, error) {
	return os.MkdirTemp("", "gopi-sandbox-")
}
