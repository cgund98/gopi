package sandbox

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/cgund98/gopi/internal/policy"
)

// Proxy is a loopback HTTP and SOCKS5 proxy. It dials only hosts the policy allows.
type Proxy struct {
	Policy  policy.NetworkPolicy
	Resolve func(host string) ([]net.IP, error)
	Dial    func(ctx context.Context, network, addr string) (net.Conn, error)

	http  net.Listener
	socks net.Listener
	once  sync.Once
	done  chan struct{}
}

// ListenProxy binds the HTTP and SOCKS listeners on 127.0.0.1.
func ListenProxy(rules policy.NetworkPolicy) (*Proxy, error) {
	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	socksLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = httpLn.Close()
		return nil, err
	}
	proxy := &Proxy{
		Policy: rules,
		http:   httpLn,
		socks:  socksLn,
		done:   make(chan struct{}),
	}
	go proxy.serve(httpLn, proxy.serveHTTP)
	go proxy.serve(socksLn, proxy.serveSOCKS)
	return proxy, nil
}

// HTTPPort is the loopback HTTP proxy port.
func (p *Proxy) HTTPPort() int { return p.http.Addr().(*net.TCPAddr).Port }

// SOCKSPort is the loopback SOCKS5 proxy port.
func (p *Proxy) SOCKSPort() int { return p.socks.Addr().(*net.TCPAddr).Port }

// Close stops both listeners.
func (p *Proxy) Close() {
	p.once.Do(func() {
		close(p.done)
		_ = p.http.Close()
		_ = p.socks.Close()
	})
}

func (p *Proxy) serve(ln net.Listener, handle func(net.Conn)) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handle(conn)
	}
}

func (p *Proxy) serveHTTP(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return
	}
	method, target := fields[0], fields[1]
	host := target
	if method == "CONNECT" {
		host = target
	} else if parsed, ok := httpTargetHost(target); ok {
		host = parsed
	}
	for {
		header, err := reader.ReadString('\n')
		if err != nil || header == "\r\n" || header == "\n" {
			break
		}
	}
	upstream, err := p.dialHost(context.Background(), host)
	if err != nil {
		_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer func() { _ = upstream.Close() }()
	if method == "CONNECT" {
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
	} else {
		_, _ = io.WriteString(upstream, line)
	}
	go func() { _, _ = io.Copy(upstream, reader) }()
	_, _ = io.Copy(conn, upstream)
}

func httpTargetHost(target string) (string, bool) {
	rest := target
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	} else {
		return "", false
	}
	rest = strings.SplitN(rest, "/", 2)[0]
	return rest, rest != ""
}

func (p *Proxy) serveSOCKS(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	buf := make([]byte, 258)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	nmethods := int(buf[1])
	if _, err := io.ReadFull(conn, buf[:nmethods]); err != nil {
		return
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return
	}
	host, err := readSOCKSAddr(conn, buf[3])
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	upstream, err := p.dialHost(context.Background(), host)
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x02, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer func() { _ = upstream.Close() }()
	_, _ = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	go func() { _, _ = io.Copy(upstream, conn) }()
	_, _ = io.Copy(conn, upstream)
}

func readSOCKSAddr(r io.Reader, atyp byte) (string, error) {
	switch atyp {
	case 0x01:
		var ip [4]byte
		if _, err := io.ReadFull(r, ip[:]); err != nil {
			return "", err
		}
		var port uint16
		if err := binary.Read(r, binary.BigEndian, &port); err != nil {
			return "", err
		}
		return net.JoinHostPort(net.IP(ip[:]).String(), strconv.Itoa(int(port))), nil
	case 0x03:
		var n [1]byte
		if _, err := io.ReadFull(r, n[:]); err != nil {
			return "", err
		}
		name := make([]byte, n[0])
		if _, err := io.ReadFull(r, name); err != nil {
			return "", err
		}
		var port uint16
		if err := binary.Read(r, binary.BigEndian, &port); err != nil {
			return "", err
		}
		return net.JoinHostPort(string(name), strconv.Itoa(int(port))), nil
	case 0x04:
		var ip [16]byte
		if _, err := io.ReadFull(r, ip[:]); err != nil {
			return "", err
		}
		var port uint16
		if err := binary.Read(r, binary.BigEndian, &port); err != nil {
			return "", err
		}
		return net.JoinHostPort(net.IP(ip[:]).String(), strconv.Itoa(int(port))), nil
	default:
		return "", fmt.Errorf("unsupported socks address")
	}
}

func (p *Proxy) dialHost(ctx context.Context, hostport string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
		port = "80"
	}
	addrs, err := p.lookup(host)
	if err != nil {
		return nil, err
	}
	if err := p.Policy.Check(host, addrs); err != nil {
		return nil, err
	}
	dial := p.Dial
	if dial == nil {
		dialer := &net.Dialer{}
		dial = dialer.DialContext
	}
	return dial(ctx, "tcp", net.JoinHostPort(addrs[0].String(), port))
}

func (p *Proxy) lookup(host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	if p.Resolve != nil {
		return p.Resolve(host)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	return ips, nil
}
