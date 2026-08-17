package proxybridge

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
)

// bufReader wraps a net.Conn with a bufio.Reader so we can read the HTTP request
// line + headers without consuming the body.
type bufReader struct {
	conn net.Conn
	br   *bufio.Reader
}

func newBufReader(c net.Conn) *bufReader {
	return &bufReader{conn: c, br: bufio.NewReader(c)}
}

func (r *bufReader) readLine() (string, error) {
	line, err := r.br.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readHeaders reads header lines until the blank line, returns a map (first
// occurrence wins, preserving case).
func (r *bufReader) readHeaders() (map[string]string, error) {
	headers := map[string]string{}
	for {
		line, err := r.readLine()
		if err != nil {
			return nil, err
		}
		if line == "" {
			break
		}
		if i := strings.Index(line, ":"); i > 0 {
			key := strings.TrimSpace(line[:i])
			val := strings.TrimSpace(line[i+1:])
			if _, exists := headers[key]; !exists {
				headers[key] = val
			}
		}
	}
	return headers, nil
}

// Read implements io.Reader passthrough (for body forwarding).
func (r *bufReader) Read(p []byte) (int, error) { return r.br.Read(p) }

// copyChunked forwards an HTTP chunked body.
func copyChunked(w io.Writer, r io.Reader) error {
	br, ok := r.(*bufio.Reader)
	if !ok {
		// fall back: raw copy (won't preserve framing but tunnels data)
		_, err := io.Copy(w, r)
		return err
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		sizeStr := strings.TrimSpace(strings.SplitN(line, ";", 2)[0])
		var size int
		if _, err := fmt.Sscanf(sizeStr, "%x", &size); err != nil {
			return err
		}
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
		if size == 0 {
			// trailing headers until blank line
			for {
				line, err := br.ReadString('\n')
				if err != nil {
					return err
				}
				if _, err := io.WriteString(w, line); err != nil {
					return err
				}
				if strings.TrimRight(line, "\r\n") == "" {
					return nil
				}
			}
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(br, buf); err != nil {
			return err
		}
		if _, err := w.Write(buf); err != nil {
			return err
		}
		// trailing CRLF
		crlf := make([]byte, 2)
		if _, err := io.ReadFull(br, crlf); err != nil {
			return err
		}
		if _, err := w.Write(crlf); err != nil {
			return err
		}
	}
}

// ---------- socks5 client (used by ProxyOutbound) ----------

func socks5Handshake(ctx context.Context, conn net.Conn, username, password string) error {
	methods := []byte{0x00}
	if username != "" || password != "" {
		methods = []byte{0x02}
	}
	if _, err := conn.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		return err
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return err
	}
	if buf[0] != 0x05 {
		return fmt.Errorf("socks5 version mismatch: %d", buf[0])
	}
	switch buf[1] {
	case 0x00:
		if username != "" || password != "" {
			return fmt.Errorf("proxy requires auth but accepted NO AUTH")
		}
	case 0x02:
		payload := []byte{0x01, byte(len(username))}
		payload = append(payload, []byte(username)...)
		payload = append(payload, byte(len(password)))
		payload = append(payload, []byte(password)...)
		if _, err := conn.Write(payload); err != nil {
			return err
		}
		rb := make([]byte, 2)
		if _, err := io.ReadFull(conn, rb); err != nil {
			return err
		}
		if rb[1] != 0x00 {
			return fmt.Errorf("socks5 auth failed")
		}
	default:
		return fmt.Errorf("socks5 unsupported method %d", buf[1])
	}
	return nil
}

// socks5Connect sends a CONNECT request over an authenticated socks5 connection.
func socks5Connect(ctx context.Context, conn net.Conn, addr string) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return err
	}
	req := []byte{0x05, 0x01, 0x00}
	// domain-based addressing (works for hostnames and IPs alike)
	req = append(req, 0x03, byte(len(host)))
	req = append(req, []byte(host)...)
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		return err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[1] != 0x00 {
		return fmt.Errorf("socks5 connect failed code=%d", head[1])
	}
	var addrLen int
	switch head[3] {
	case 0x01:
		addrLen = 4
	case 0x04:
		addrLen = 16
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return err
		}
		addrLen = int(l[0])
	default:
		return fmt.Errorf("socks5 bad address type")
	}
	rest := make([]byte, addrLen+2)
	if _, err := io.ReadFull(conn, rest); err != nil {
		return err
	}
	return nil
}

// httpConnect establishes an HTTP CONNECT tunnel through an http forward proxy.
func httpConnect(ctx context.Context, conn net.Conn, addr, username, password string) error {
	auth := ""
	if username != "" || password != "" {
		auth = "Proxy-Authorization: Basic " + base64Std(username+":"+password) + "\r\n"
	}
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", addr, addr, auth)
	if _, err := conn.Write([]byte(req)); err != nil {
		return err
	}
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.Contains(line, " 200 ") {
		return fmt.Errorf("http connect refused: %s", strings.TrimSpace(line))
	}
	// drain headers
	for {
		l, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		if strings.TrimRight(l, "\r\n") == "" {
			break
		}
	}
	// any buffered bytes after headers must be replayed to the tunnel consumer —
	// the proxy may have already sent response bytes. We don't support that edge
	// here (rare for CONNECT); acceptable for a lightweight bridge.
	return nil
}

func base64Std(s string) string {
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out []byte
	src := []byte(s)
	for i := 0; i < len(src); i += 3 {
		var chunk [3]byte
		n := copy(chunk[:], src[i:])
		out = append(out, table[chunk[0]>>2])
		out = append(out, table[(chunk[0]&0x03)<<4|chunk[1]>>4])
		if n > 1 {
			out = append(out, table[(chunk[1]&0x0f)<<2|chunk[2]>>6])
		} else {
			out = append(out, '=')
		}
		if n > 2 {
			out = append(out, table[chunk[2]&0x3f])
		} else {
			out = append(out, '=')
		}
	}
	return string(out)
}
