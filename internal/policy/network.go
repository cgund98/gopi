package policy

import (
	"fmt"
	"net"
	"strings"
)

// NetworkPolicy decides whether a proxy may dial a host.
// A deny pattern beats an allow pattern. Private and metadata addresses are refused
// even when the name is allowlisted.
type NetworkPolicy struct {
	Allow []string
	Deny  []string
}

var metadataHosts = []string{
	"metadata.google.internal",
	"metadata.google.com",
	"metadata.azure.com",
	"instance-data",
	"instance-data.ec2.internal",
}

var (
	cgnat     = mustCIDR("100.64.0.0/10")
	metadata4 = net.ParseIP("169.254.169.254")
	metadata6 = net.ParseIP("fd00:ec2::254")
)

// Check reports why host and its resolved addresses cannot be dialed.
// An empty address list fails closed.
func (p NetworkPolicy) Check(host string, addrs []net.IP) error {
	name := normalizeHost(host)
	if name == "" {
		return fmt.Errorf("missing host")
	}
	if isMetadataHost(name) {
		return fmt.Errorf("metadata host %s", name)
	}
	if matchHost(name, p.Deny) {
		return fmt.Errorf("host %s is denied", name)
	}
	if !matchHost(name, p.Allow) {
		return fmt.Errorf("host %s is not allowlisted", name)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("host %s did not resolve", name)
	}
	for _, addr := range addrs {
		if blockedAddress(addr) {
			return fmt.Errorf("host %s resolved to %s", name, addr)
		}
	}
	return nil
}

// Public reports whether a host may be fetched without using the shell allowlist.
// An empty address list fails closed. Private and metadata addresses are refused.
func Public(host string, addrs []net.IP) error {
	name := normalizeHost(host)
	if name == "" {
		return fmt.Errorf("missing host")
	}
	if isMetadataHost(name) {
		return fmt.Errorf("metadata host %s", name)
	}
	if ip := net.ParseIP(name); ip != nil {
		addrs = append([]net.IP{ip}, addrs...)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("host %s did not resolve", name)
	}
	for _, addr := range addrs {
		if blockedAddress(addr) {
			return fmt.Errorf("host %s resolved to %s", name, addr)
		}
	}
	return nil
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimSuffix(host, ".")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host
}

func isMetadataHost(host string) bool {
	for _, name := range metadataHosts {
		if host == name {
			return true
		}
	}
	return false
}

func matchHost(host string, patterns []string) bool {
	labels := strings.Split(host, ".")
	for _, pattern := range patterns {
		pattern = normalizeHost(pattern)
		if pattern == "" {
			continue
		}
		if pattern == host {
			return true
		}
		want := strings.Split(pattern, ".")
		if len(want) != len(labels) {
			continue
		}
		ok := true
		for i := range want {
			if want[i] == "*" {
				if labels[i] == "" {
					ok = false
					break
				}
				continue
			}
			if want[i] != labels[i] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func blockedAddress(addr net.IP) bool {
	ip := addr
	if v4 := addr.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if cgnat.Contains(ip) {
		return true
	}
	if ip.Equal(metadata4) || ip.Equal(metadata6) {
		return true
	}
	return false
}

func mustCIDR(cidr string) *net.IPNet {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return network
}
