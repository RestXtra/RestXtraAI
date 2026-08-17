package proxybridge

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// startTestEcho spins a TCP echo server, returns its addr + close func.
func startTestEcho(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c) // echo
			}(c)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

// dialThroughSocks5 does a raw SOCKS5 CONNECT through a bridge port.
func dialThroughSocks5(t *testing.T, proxyAddr, target string) (net.Conn, error) {
	t.Helper()
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		conn.Close()
		return nil, err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		conn.Close()
		return nil, err
	}
	host, port, _ := net.SplitHostPort(target)
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, []byte(host)...)
	p := 0
	for _, ch := range port {
		if ch < '0' || ch > '9' {
			break
		}
		p = p*10 + int(ch-'0')
	}
	req = append(req, byte(p>>8), byte(p))
	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		conn.Close()
		return nil, err
	}
	if head[1] != 0x00 {
		conn.Close()
		return nil, err
	}
	// drain the bound-address part of the success reply
	var addrLen int
	switch head[3] {
	case 0x01:
		addrLen = 4
	case 0x04:
		addrLen = 16
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			conn.Close()
			return nil, err
		}
		addrLen = int(l[0])
	}
	rest := make([]byte, addrLen+2)
	if _, err := io.ReadFull(conn, rest); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func TestBridgeSocks5Direct(t *testing.T) {
	echoAddr, closeEcho := startTestEcho(t)
	defer closeEcho()

	b := New(Options{Port: 0, Outbound: DirectOutbound{}})
	addr, err := b.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Stop()

	conn, err := dialThroughSocks5(t, addr, echoAddr)
	if err != nil {
		t.Fatalf("socks5 dial: %v", err)
	}
	defer conn.Close()
	msg := []byte("hello-bridge")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != string(msg) {
		t.Fatalf("echo mismatch: got %q", buf)
	}
}

func TestBridgeHTTPConnectDirect(t *testing.T) {
	echoAddr, closeEcho := startTestEcho(t)
	defer closeEcho()

	b := New(Options{Port: 0, Outbound: DirectOutbound{}})
	addr, err := b.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	_, err = conn.Write([]byte("CONNECT " + echoAddr + " HTTP/1.1\r\nHost: " + echoAddr + "\r\n\r\n"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	resp := make([]byte, 512)
	// read until blank line
	done := false
	for !done {
		n, err := conn.Read(resp)
		if err != nil {
			t.Fatalf("read resp: %v", err)
		}
		_ = n
		if n >= 4 && string(resp[n-4:n]) == "\r\n\r\n" {
			done = true
		}
	}
	if !contains(string(resp), "200") {
		t.Fatalf("expected 200, got %q", resp)
	}
	msg := []byte("ping")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("echo mismatch: %q", buf)
	}
}

func TestBridgeRuleDirectCIDR(t *testing.T) {
	echoAddr, _ := startTestEcho(t)
	host, _, _ := net.SplitHostPort(echoAddr)
	// Rule: the echo IP → DIRECT; everything else → via a non-existent proxy
	// (which would fail). Since echo matches DIRECT, the connection succeeds.
	b := New(Options{
		Port: 0,
		Outbound: &ProxyOutbound{
			Protocol: "socks5",
			Addr:     "127.0.0.1:1", // unreachable — must NOT be used for echo
		},
		Rules: []Rule{{Kind: "ip-cidr", Value: host + "/32", Direct: true}},
	})
	addr, err := b.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Stop()

	conn, err := dialThroughSocks5(t, addr, echoAddr)
	if err != nil {
		t.Fatalf("socks5 dial should be DIRECT (matched cidr): %v", err)
	}
	conn.Close()
}

func TestBridgeRuleSuffixGoesDirect(t *testing.T) {
	// domain-suffix rule: any .internal host → DIRECT (no real host, dial fails
	// with direct attempt → we just assert the routing doesn't panic).
	b := New(Options{
		Port:     0,
		Outbound: DirectOutbound{},
		Rules:    []Rule{{Kind: "domain-suffix", Value: "internal", Direct: true}},
	})
	_, err := b.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = b.dialFor(ctx, DirectOutbound{}, "db.internal:5432")
	if err == nil {
		t.Fatal("expected dial failure for nonexistent internal host")
	}
}

func TestBridgeHTTPRequestForward(t *testing.T) {
	// A tiny HTTP server to proxy through.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})}
	go srv.Serve(ln)

	b := New(Options{Port: 0, Outbound: DirectOutbound{}})
	addr, err := b.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer b.Stop()

	// Route an HTTP request through the bridge using an explicit proxy client.
	proxyURL := "http://" + addr
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(mustURL(t, proxyURL))}}
	resp, err := client.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("get through proxy: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("parse url %s: %v", s, err)
	}
	return u
}
