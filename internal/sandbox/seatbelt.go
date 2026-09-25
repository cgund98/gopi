package sandbox

import (
	"fmt"
	"strings"
)

// SeatbeltProfile renders a Seatbelt profile for sandbox-exec.
// Seatbelt is last-match-wins, so protected-path denials follow the workspace allow.
func SeatbeltProfile(profile Profile) (string, error) {
	switch profile.Network {
	case "", NetworkDeny, NetworkAllowlist, NetworkUnrestricted:
	default:
		return "", fmt.Errorf("network mode %q is not available", profile.Network)
	}
	if profile.Network == NetworkAllowlist && len(profile.ProxyPorts) == 0 {
		return "", fmt.Errorf("allowlist network requires a proxy port")
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
	for _, path := range profile.ExtraReads {
		writeSubpath(&b, "allow file-read*", path)
	}
	for _, path := range profile.ExtraWrites {
		writeSubpath(&b, "allow file-write*", path)
		writeSubpath(&b, "allow file-read*", path)
	}
	// /etc/ssl is a symlink into /private, and the *.pem floor would deny cert.pem.
	// This allow is last so the system CA bundle stays readable.
	writeSubpath(&b, "allow file-read*", "/private/etc/ssl")
	// Compilers and linters open /dev/null for write. It is not under a write root.
	writeLiteral(&b, "allow file-read*", "/dev/null")
	writeLiteral(&b, "allow file-write*", "/dev/null")
	b.WriteString("(deny network*)\n")
	switch profile.Network {
	case NetworkAllowlist:
		for _, port := range profile.ProxyPorts {
			fmt.Fprintf(&b, "(allow network* (remote tcp \"localhost:%d\"))\n", port)
		}
	case NetworkUnrestricted:
		b.WriteString("(allow network*)\n")
	}
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

func writeLiteral(b *strings.Builder, op, path string) {
	fmt.Fprintf(b, "(%s (literal %s))\n", op, quoteSeatbelt(path))
}

func writeRegex(b *strings.Builder, op, pattern string) {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(pattern)
	fmt.Fprintf(b, "(%s (regex \"%s\"))\n", op, escaped)
}

func quoteSeatbelt(path string) string {
	return `"` + strings.ReplaceAll(path, `\`, `\\`) + `"`
}
