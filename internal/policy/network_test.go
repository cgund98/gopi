package policy

import (
	"net"
	"testing"
)

func TestNetworkPolicy(t *testing.T) {
	policy := NetworkPolicy{
		Allow: []string{"github.com", "*.npmjs.org"},
		Deny:  []string{"evil.npmjs.org"},
	}
	if err := policy.Check("registry.npmjs.org", []net.IP{net.ParseIP("1.2.3.4")}); err != nil {
		t.Fatal(err)
	}
	if err := policy.Check("evil.npmjs.org", []net.IP{net.ParseIP("1.2.3.4")}); err == nil {
		t.Fatal("deny entry should beat allow")
	}
	if err := policy.Check("example.com", []net.IP{net.ParseIP("1.2.3.4")}); err == nil {
		t.Fatal("unlisted host should be refused")
	}
	if err := policy.Check("github.com", []net.IP{net.ParseIP("10.0.0.1")}); err == nil {
		t.Fatal("private address should be refused")
	}
	if err := policy.Check("github.com", []net.IP{net.ParseIP("169.254.169.254")}); err == nil {
		t.Fatal("metadata address should be refused")
	}
	if err := policy.Check("metadata.google.internal", []net.IP{net.ParseIP("1.2.3.4")}); err == nil {
		t.Fatal("metadata hostname should be refused")
	}
	if err := policy.Check("a.b.npmjs.org", []net.IP{net.ParseIP("1.2.3.4")}); err == nil {
		t.Fatal("* should match one label")
	}
}
