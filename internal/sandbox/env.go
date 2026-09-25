package sandbox

import (
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

// SessionTemp creates a private temp directory for one command.
func SessionTemp() (string, error) {
	return os.MkdirTemp("", "gopi-sandbox-")
}
