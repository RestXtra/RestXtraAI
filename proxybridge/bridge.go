// Package proxybridge implements a minimal mixed HTTP/SOCKS5 forward proxy
// ("proxy bridge") that mimics Clash's mixed-port: one listener, first-byte
// sniffing (0x05 → SOCKS5, otherwise HTTP), and rule-based routing — matching
// IP-CIDR / domain rules go DIRECT, everything else is forwarded through a
// chosen outbound (a healthy proxy-pool node). It is a lightweight subset of a
// Clash-style client: no full rules engine, no proxy-groups chaining, no DNS.
package proxybridge

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Rule is one routing rule. Match decides whether the request goes DIRECT.
// An empty rules list means "everything through the outbound".
type Rule struct {
	// Kind: "ip-cidr" | "domain" | "domain-suffix" | "process" | "match"
	Kind   string `json:"kind"`
	Value  string `json:"value"`  // CIDR / domain / process name (process not supported at runtime)
	Direct bool   `json:"direct"` // when true, matched → DIRECT; when false, matched → proxy
}

// Outbound is the interface a Dialer must implement to forward traffic.
type Outbound interface {
	// Dial connects to target through the outbound. For DIRECT this is a plain
	// TCP dial. For a proxy node it must connect to target THROUGH that proxy
	// (e.g. SOCKS5 CONNECT), returning a stream to the target.
	Dial(ctx context.Context, network, addr string) (net.Conn, error)
}

// DirectOutbound dials targets directly.
type DirectOutbound struct{}

func (DirectOutbound) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

// ProxyOutbound dials targets through a fixed forward proxy (http/socks5).
type ProxyOutbound struct {
	Protocol string // http | socks5 | socks5h
	Addr     string // host:port
	Username string
	Password string
}

func (p *ProxyOutbound) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("proxy outbound only supports tcp, got %s", network)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", p.Addr)
	if err != nil {
		return nil, err
	}
	switch p.Protocol {
	case "socks5", "socks5h":
		if err := socks5Handshake(ctx, conn, p.Username, p.Password); err != nil {
			conn.Close()
			return nil, err
		}
		if err := socks5Connect(ctx, conn, addr); err != nil {
			conn.Close()
			return nil, err
		}
	case "http":
		// HTTP forward proxy: use CONNECT for any target (works for both
		// http:// and https:// since we tunnel raw bytes).
		if err := httpConnect(ctx, conn, addr, p.Username, p.Password); err != nil {
			conn.Close()
			return nil, err
		}
	default:
		conn.Close()
		return nil, fmt.Errorf("unsupported proxy protocol %s", p.Protocol)
	}
	return conn, nil
}

// Bridge is the mixed proxy server.
type Bridge struct {
	mu       sync.Mutex
	listener net.Listener
	port     int
	// Rules evaluate target → DIRECT or proxy.
	rules    []Rule
	outbound Outbound // nil → DIRECT everywhere
	// socks5+http auth required from clients (optional).
	username string
	password string

	connWg sync.WaitGroup
	done   chan struct{}
}

// Options configure a Bridge.
type Options struct {
	Port     int    // listen port; <=0 → default 7890
	Rules    []Rule // empty → everything via outbound
	Outbound Outbound
	// ClientAuth, when set, requires clients to present these credentials
	// (HTTP Proxy-Authorization / SOCKS5 user/pass).
	ClientUser     string
	ClientPassword string
}

// New builds a Bridge (not yet started).
func New(o Options) *Bridge {
	port := o.Port
	if port <= 0 {
		port = 7890
	}
	out := o.Outbound
	if out == nil {
		out = DirectOutbound{}
	}
	return &Bridge{
		port:     port,
		rules:    o.Rules,
		outbound: out,
		username: o.ClientUser,
		password: o.ClientPassword,
		done:     make(chan struct{}),
	}
}

// Port returns the configured listen port.
func (b *Bridge) Port() int { return b.port }

// Running reports whether the listener is active.
func (b *Bridge) Running() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.listener != nil
}

// Start begins listening and serving. Returns the bound address.
func (b *Bridge) Start() (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listener != nil {
		return b.listener.Addr().String(), nil
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", b.port))
	if err != nil {
		return "", err
	}
	b.listener = ln
	b.done = make(chan struct{})
	go b.acceptLoop(ln)
	return ln.Addr().String(), nil
}

// Stop closes the listener and waits for in-flight connections.
func (b *Bridge) Stop() error {
	b.mu.Lock()
	ln := b.listener
	if ln != nil {
		_ = ln.Close()
	}
	b.listener = nil
	b.mu.Unlock()
	if ln == nil {
		return nil
	}
	close(b.done)
	b.connWg.Wait()
	return nil
}

// SetOutbound swaps the outbound (nil → direct). Safe while running.
func (b *Bridge) SetOutbound(o Outbound) {
	if o == nil {
		o = DirectOutbound{}
	}
	b.mu.Lock()
	b.outbound = o
	b.mu.Unlock()
}

// SetRules swaps the rule set. Safe while running.
func (b *Bridge) SetRules(r []Rule) {
	b.mu.Lock()
	b.rules = r
	b.mu.Unlock()
}

func (b *Bridge) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-b.done:
				return
			default:
				if strings.Contains(err.Error(), "use of closed network connection") {
					return
				}
				log.Printf("[proxybridge] accept: %v", err)
				continue
			}
		}
		b.connWg.Add(1)
		go func(c net.Conn) {
			defer b.connWg.Done()
			defer c.Close()
			b.handleConn(c)
		}(conn)
	}
}

// handleConn sniffs the first byte and dispatches to SOCKS5 or HTTP.
func (b *Bridge) handleConn(c net.Conn) {
	// Read the first byte to sniff. Peek via bufio is awkward when passing the
	// conn downstream, so we read 1 byte and prepend it.
	one := make([]byte, 1)
	if _, err := io.ReadFull(c, one); err != nil {
		return
	}
	rc := &prependConn{Conn: c, prefix: one}
	if one[0] == 0x05 {
		b.handleSocks5(rc)
		return
	}
	b.handleHTTP(rc)
}

// prependConn re-injects the sniffed prefix byte into the stream.
type prependConn struct {
	net.Conn
	prefix []byte
	done   bool
}

func (p *prependConn) Read(buf []byte) (int, error) {
	if !p.done && len(p.prefix) > 0 {
		n := copy(buf, p.prefix)
		p.prefix = p.prefix[n:]
		if len(p.prefix) == 0 {
			p.done = true
		}
		return n, nil
	}
	return p.Conn.Read(buf)
}

// route decides the outbound for a target. Returns the outbound to dial.
func (b *Bridge) route(targetHost string) (Outbound, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.rules {
		if ruleMatches(r, targetHost) {
			if r.Direct {
				return DirectOutbound{}, true
			}
			return b.outbound, true
		}
	}
	// default: everything through the outbound (clash MATCH,Proxy)
	return b.outbound, true
}

// ruleMatches tests one rule against the target host:port.
func ruleMatches(r Rule, hostPort string) bool {
	host := hostPort
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return false
	}
	switch r.Kind {
	case "ip-cidr":
		if ip, err := netip.ParseAddr(host); err == nil {
			if prefix, perr := netip.ParsePrefix(r.Value); perr == nil {
				return prefix.Contains(ip)
			}
			if rval, perr := netip.ParsePrefix(normalizeCIDR(r.Value)); perr == nil {
				return rval.Contains(ip)
			}
		}
		return false
	case "domain":
		return host == strings.ToLower(strings.TrimSuffix(r.Value, "."))
	case "domain-suffix":
		val := strings.ToLower(strings.TrimSuffix(r.Value, "."))
		return host == val || strings.HasSuffix(host, "."+val)
	case "process":
		// process-name matching is not implemented at runtime; treated as no-match
		return false
	case "match":
		return true
	}
	return false
}

// normalizeCIDR tolerates "1.2.3.4/24" style entries (already fine) and bare
// "1.2.3.4" by treating /32.
func normalizeCIDR(v string) string {
	if _, err := netip.ParsePrefix(v); err == nil {
		return v
	}
	return v + "/32"
}

// dialFor connects to the target via the chosen outbound.
func (b *Bridge) dialFor(ctx context.Context, o Outbound, target string) (net.Conn, error) {
	if o == nil {
		return DirectOutbound{}.Dial(ctx, "tcp", target)
	}
	return o.Dial(ctx, "tcp", target)
}

// ---------- SOCKS5 server ----------

func (b *Bridge) handleSocks5(c net.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// The sniffed first byte (0x05 = version) was already re-injected into the
	// stream via prependConn, so the first read here is nmethods.
	nmb := make([]byte, 1)
	if _, err := io.ReadFull(c, nmb); err != nil {
		return
	}
	methods := make([]byte, int(nmb[0]))
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}
	// choose method
	needAuth := b.username != "" || b.password != ""
	method := byte(0x00) // NO AUTH
	if needAuth {
		method = 0x02 // USER/PASS
	} else {
		// prefer NO AUTH if offered; else USER/PASS
		found := false
		for _, m := range methods {
			if m == 0x00 {
				found = true
				break
			}
		}
		if !found {
			method = 0x02
		}
	}
	if _, err := c.Write([]byte{0x05, method}); err != nil {
		return
	}
	if method == 0x02 {
		if err := b.socks5Auth(ctx, c); err != nil {
			return
		}
	}
	// CONNECT request
	addr, err := readSocks5Connect(ctx, c)
	if err != nil {
		_ = writeSocks5Reply(c, 0x08)
		return
	}
	o, _ := b.route(addr)
	upstream, err := b.dialFor(ctx, o, addr)
	if err != nil {
		_ = writeSocks5Reply(c, 0x01)
		return
	}
	defer upstream.Close()
	if err := writeSocks5Reply(c, 0x00); err != nil {
		return
	}
	pump(ctx, c, upstream)
}

func (b *Bridge) socks5Auth(ctx context.Context, c net.Conn) error {
	// version + ulen
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return err
	}
	ulen := make([]byte, 1)
	if _, err := io.ReadFull(c, ulen); err != nil {
		return err
	}
	user := make([]byte, ulen[0])
	if _, err := io.ReadFull(c, user); err != nil {
		return err
	}
	plen := make([]byte, 1)
	if _, err := io.ReadFull(c, plen); err != nil {
		return err
	}
	pass := make([]byte, plen[0])
	if _, err := io.ReadFull(c, pass); err != nil {
		return err
	}
	ok := string(user) == b.username && string(pass) == b.password
	if ok {
		_, _ = c.Write([]byte{0x01, 0x00})
	} else {
		_, _ = c.Write([]byte{0x01, 0x01})
	}
	if !ok {
		return errors.New("socks5 auth failed")
	}
	return nil
}

func readSocks5Connect(ctx context.Context, c net.Conn) (string, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return "", err
	}
	if hdr[0] != 0x05 || hdr[1] != 0x01 {
		return "", errors.New("unsupported socks5 command")
	}
	var host string
	switch hdr[3] {
	case 0x01: // IPv4
		b := make([]byte, 4)
		if _, err := io.ReadFull(c, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case 0x04: // IPv6
		b := make([]byte, 16)
		if _, err := io.ReadFull(c, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case 0x03: // domain
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return "", err
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(c, b); err != nil {
			return "", err
		}
		host = string(b)
	default:
		return "", errors.New("bad address type")
	}
	portB := make([]byte, 2)
	if _, err := io.ReadFull(c, portB); err != nil {
		return "", err
	}
	port := int(portB[0])<<8 | int(portB[1])
	return net.JoinHostPort(host, fmt.Sprint(port)), nil
}

func writeSocks5Reply(c net.Conn, code byte) error {
	// 0x05, rep, 0x00, 0x01, 0.0.0.0, 0
	_, err := c.Write([]byte{0x05, code, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	return err
}

// ---------- HTTP forward proxy ----------

func (b *Bridge) handleHTTP(c net.Conn) {
	br := newBufReader(c)
	// Parse the request line manually (we may need CONNECT + raw tunnel).
	line, err := br.readLine()
	if err != nil {
		return
	}
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 3 {
		return
	}
	method, target, version := parts[0], parts[1], parts[2]
	// read headers until blank line (we forward them)
	headers, err := br.readHeaders()
	if err != nil {
		return
	}
	// auth check for non-CONNECT too
	if b.username != "" {
		if !httpAuthorized(headers, b.username, b.password) {
			_, _ = c.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\nProxy-Authenticate: Basic realm=\"RestXtra\"\r\n\r\n"))
			return
		}
	}
	host := ""
	if method == "CONNECT" {
		host = target
	} else {
		// absolute-form URI (http://host/path) or origin-form (host from Host header)
		if strings.HasPrefix(strings.ToLower(target), "http://") {
			if u, err := url.Parse(target); err == nil {
				host = u.Host
			} else {
				return
			}
		} else {
			host = headerValue(headers, "Host")
		}
		if host == "" {
			return
		}
	}
	if !strings.Contains(host, ":") {
		if method == "CONNECT" {
			host = host + ":443"
		} else {
			host = host + ":80"
		}
	}
	o, _ := b.route(host)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	upstream, err := b.dialFor(ctx, o, host)
	if err != nil {
		if method == "CONNECT" {
			_, _ = c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		} else {
			_, _ = c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		}
		return
	}
	defer upstream.Close()
	if method == "CONNECT" {
		_, _ = c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		pump(ctx, c, upstream)
		return
	}
	// Rebuild the request line in absolute form for the upstream.
	reqLine := fmt.Sprintf("%s %s %s\r\n", method, target, version)
	// If origin-form, convert to absolute so proxy nodes can route.
	if !strings.HasPrefix(strings.ToLower(target), "http://") {
		scheme := "http"
		reqLine = fmt.Sprintf("%s http://%s%s %s\r\n", method, host, target, version)
		_ = scheme
	}
	payload := reqLine
	payload += headersBlock(headers)
	if _, err := upstream.Write([]byte(payload)); err != nil {
		return
	}
	// forward request body then mirror response
	if contentLength := contentLength(headers); contentLength > 0 {
		if _, err := io.CopyN(upstream, br, contentLength); err != nil {
			return
		}
	} else if chunked(headers) {
		if err := copyChunked(upstream, br); err != nil {
			return
		}
	}
	pump(ctx, upstream, c)
}

func httpAuthorized(headers map[string]string, user, pass string) bool {
	auth := headerValue(headers, "Proxy-Authorization")
	if !strings.HasPrefix(strings.ToLower(auth), "basic ") {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(auth[6:]))
	if err != nil {
		return false
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	return len(parts) == 2 && parts[0] == user && parts[1] == pass
}

func headerValue(h map[string]string, key string) string {
	for k, v := range h {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func contentLength(h map[string]string) int64 {
	v := headerValue(h, "Content-Length")
	var n int64
	_, _ = fmt.Sscanf(v, "%d", &n)
	return n
}

func chunked(h map[string]string) bool {
	return strings.EqualFold(strings.TrimSpace(headerValue(h, "Transfer-Encoding")), "chunked")
}

func headersBlock(h map[string]string) string {
	var sb strings.Builder
	for k, v := range h {
		sb.WriteString(k)
		sb.WriteString(": ")
		sb.WriteString(v)
		sb.WriteString("\r\n")
	}
	sb.WriteString("\r\n")
	return sb.String()
}

// ---------- helpers ----------

func pump(ctx context.Context, a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	<-done
	// half-close the other direction so the peer sees EOF
	if tcpA, ok := a.(*net.TCPConn); ok {
		_ = tcpA.CloseWrite()
	}
	if tcpB, ok := b.(*net.TCPConn); ok {
		_ = tcpB.CloseWrite()
	}
	<-done
}
