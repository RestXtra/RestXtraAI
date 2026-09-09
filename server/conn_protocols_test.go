package server

import (
	"net"
	"strings"
	"testing"
	"time"
)

// bannerListener is an in-process TCP server that sends a fixed banner on
// connect (like a real telnetd login banner), so telnetRun can be exercised
// without a live telnet service.
type bannerListener struct {
	net.Listener
}

func newBannerListener() (*bannerListener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = c.Write([]byte("Welcome. RESTXTRA_TELNET_OK banner\n"))
				buf := make([]byte, 1024)
				for {
					if _, err := c.Read(buf); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return &bannerListener{Listener: ln}, nil
}

func (l *bannerListener) HostPort() (string, int) {
	addr := l.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

// TestProtocolHelpersFailFast verifies the RDP/Telnet helpers return an error
// promptly for unreachable hosts (connection refused) rather than hanging.
func TestProtocolHelpersFailFast(t *testing.T) {
	// closed port on loopback → connection refused quickly.
	start := time.Now()
	_, err := telnetRun("127.0.0.1", 1, "echo x", 3*time.Second)
	if err == nil {
		t.Fatal("telnet to closed port should error")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("telnetRun too slow on refused connection: %v", time.Since(start))
	}

	if err := rdpCredentialCheck("127.0.0.1", 1, "u", "p"); err == nil {
		t.Fatal("rdp to closed port should error")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("rdpCredentialCheck too slow on refused connection")
	}
}

// TestTelnetBannerServer exercises telnetRun against an in-process banner server.
func TestTelnetBannerServer(t *testing.T) {
	ln, err := newBannerListener()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	host, port := ln.HostPort()
	out, err := telnetRun(host, port, "whoami", 3*time.Second)
	if err != nil {
		t.Fatalf("telnetRun: %v", err)
	}
	if !strings.Contains(out, "RESTXTRA_TELNET_OK") {
		t.Fatalf("expected banner marker in output, got %q", out)
	}
}
