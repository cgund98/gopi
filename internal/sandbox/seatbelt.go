package sandbox

import (
	"fmt"
	"strings"
)

// SeatbeltProfile renders a Seatbelt profile for sandbox-exec.
// Seatbelt is last-match-wins, so protected-path denials follow the workspace allow.
func SeatbeltProfile(profile Profile) (string, error) {
	if profile.Network != NetworkDeny && profile.Network != "" {
		return "", fmt.Errorf("network mode %q is not available", profile.Network)
	}
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")
	b.WriteString("(allow process*)\n")
	b.WriteString("(allow signal (target same-sandbox))\n")
	b.WriteString("(allow file-ioctl)\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow file-read* (subpath \"/\"))\n")
	for _, path := range outsideReadDenies(profile.Home) {
		writeSubpath(&b, "deny file-read*", path)
		writeSubpath(&b, "deny file-write*", path)
	}
	// /bin/sh reads this locale file at startup. It sits under /private, which is denied above.
	writeSubpath(&b, "allow file-read*", "/private/var/select")
	for _, root := range profile.ReadRoots {
		writeSubpath(&b, "allow file-read*", root)
	}
	for _, root := range profile.WriteRoots {
		writeSubpath(&b, "allow file-write*", root)
		writeSubpath(&b, "allow file-read*", root)
	}
	for _, pattern := range profile.DenyRead {
		writeRegex(&b, "deny file-read*", pattern)
	}
	for _, pattern := range profile.DenyWrite {
		writeRegex(&b, "deny file-write*", pattern)
	}
	b.WriteString("(deny network*)\n")
	b.WriteString("(deny process-info* (target others))\n")
	b.WriteString("(deny appleevent-send)\n")
	return b.String(), nil
}

func outsideReadDenies(home string) []string {
	paths := []string{"/private", "/var", "/tmp", "/Volumes", "/Users"}
	if home != "" {
		paths = append(paths, home)
	}
	return paths
}

func writeSubpath(b *strings.Builder, op, path string) {
	fmt.Fprintf(b, "(%s (subpath %s))\n", op, quoteSeatbelt(path))
}

func writeRegex(b *strings.Builder, op, pattern string) {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(pattern)
	fmt.Fprintf(b, "(%s (regex \"%s\"))\n", op, escaped)
}

func quoteSeatbelt(path string) string {
	return `"` + strings.ReplaceAll(path, `\`, `\\`) + `"`
}
