package sandbox

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/cgund98/gopi/internal/policy"
)

func TestProxyAllowsListedHostAndBlocksMetadata(t *testing.T) {
	proxy, err := ListenProxy(policy.NetworkPolicy{Allow: []string{"example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	proxy.Resolve = func(host string) ([]net.IP, error) {
		if host == "example.com" {
			return []net.IP{net.ParseIP("1.2.3.4")}, nil
		}
		return []net.IP{net.ParseIP("169.254.169.254")}, nil
	}
	dialed := false
	proxy.Dial = func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		client, server := net.Pipe()
		go func() { _, _ = io.Copy(io.Discard, server) }()
		return client, nil
	}

	ok := httpConnect(t, proxy.http.Addr().String(), "example.com:443")
	if !strings.Contains(ok, "200") || !dialed {
		t.Fatalf("connect = %q dialed = %v", ok, dialed)
	}
	dialed = false
	denied := httpConnect(t, proxy.http.Addr().String(), "metadata.google.internal:80")
	if !strings.Contains(denied, "403") || dialed {
		t.Fatalf("metadata = %q dialed = %v", denied, dialed)
	}
}

func httpConnect(t *testing.T, addr, target string) string {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := io.WriteString(conn, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 128)
	n, _ := conn.Read(buf)
	return string(buf[:n])
}
