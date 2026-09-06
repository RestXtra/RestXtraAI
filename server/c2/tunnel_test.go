package c2

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RestXtra/RestXtraAI/db"
)

// TestTunnelSOCKS5E2E verifies the full SOCKS5 relay: operator -> teamserver
// SOCKS proxy -> beacon held link -> target (echo server).
func TestTunnelSOCKS5E2E(t *testing.T) {
	dsn := os.Getenv("RESTXTRA_PG_DSN")
	if dsn == "" {
		t.Skip("RESTXTRA_PG_DSN not set")
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer d.Close()

	if err := d.UpsertC2Session(&db.C2Session{SessionID: "tun-sess", Host: "10.0.0.5", Hostname: "beacon-host", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = d.ClearC2Sessions()
	}()

	// target echo server
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoLn.Close()
	go func() {
		for {
			c, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); c.Close() }()
		}
	}()
	echoAddr := echoLn.Addr().String()

	// free bind port for the SOCKS proxy
	bindLn, _ := net.Listen("tcp", "127.0.0.1:0")
	socksPort := bindLn.Addr().(*net.TCPAddr).Port
	bindLn.Close()

	m := New(d)
	tid, err := d.SaveC2Tunnel(&db.C2Tunnel{SessionID: "tun-sess", Kind: "socks5", BindHost: "127.0.0.1", BindPort: socksPort})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = m.StopTunnel(tid)
		_ = d.DeleteC2Tunnel(tid)
	}()

	if err := m.StartTunnel(&db.C2Tunnel{ID: tid, SessionID: "tun-sess", Kind: "socks5", BindHost: "127.0.0.1", BindPort: socksPort}, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}

	// find the newest tunnel_open task to learn the current link address
	deadline := time.Now().Add(5 * time.Second)
	var linkAddr string
	for time.Now().Before(deadline) {
		tasks, _ := d.ListC2Tasks("tun-sess", 10)
		for _, tk := range tasks {
			if strings.HasPrefix(tk.Command, "tunnel_open ") {
				var p struct {
					Addr string `json:"addr"`
				}
				_ = json.Unmarshal([]byte(strings.TrimPrefix(tk.Command, "tunnel_open ")), &p)
				if p.Addr != "" {
					linkAddr = p.Addr
				}
				break // newest task first
			}
		}
		if linkAddr != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if linkAddr == "" {
		t.Fatal("tunnel_open task not enqueued")
	}

	// simulate the beacon: hold the link, dial targets on demand
	linkConn, err := net.Dial("tcp", linkAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer linkConn.Close()
	go beaconSim(linkConn)

	// SOCKS5 client through the proxy
	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	conn, err := net.Dial("tcp", socksAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// greeting
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	rep := make([]byte, 2)
	if _, err := io.ReadFull(conn, rep); err != nil {
		t.Fatal(err)
	}
	if rep[0] != 0x05 || rep[1] != 0x00 {
		t.Fatalf("socks greeting rejected: %v", rep)
	}

	// CONNECT to echo target (domain form)
	host, port, _ := net.SplitHostPort(echoAddr)
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, []byte(host)...)
	req = append(req, byte(portInt(port)>>8), byte(portInt(port)))
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	rrep := make([]byte, 10)
	if _, err := io.ReadFull(conn, rrep); err != nil {
		t.Fatal(err)
	}
	if rrep[1] != 0x00 {
		t.Fatalf("socks connect failed: %v", rrep)
	}

	// echo roundtrip
	if _, err := conn.Write([]byte("ping-through-tunnel")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("ping-through-tunnel"))
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ping-through-tunnel" {
		t.Fatalf("echo mismatch: %q", string(buf))
	}
}

// beaconSim implements the beacon-side link handling for the test: single
// reader, dispatch dials to stream goroutines (mirrors cmd/beacon serveLink).
func beaconSim(conn net.Conn) {
	bl := &testLink{conn: conn}
	streams := map[uint32]chan []byte{}
	closers := map[uint32]chan struct{}{}
	for {
		typ, stream, payload, err := readFrame(conn)
		if err != nil {
			return
		}
		switch typ {
		case frameDial:
			target := string(payload)
			dataCh := make(chan []byte, 64)
			stopCh := make(chan struct{})
			streams[stream] = dataCh
			closers[stream] = stopCh
			go testStream(bl, stream, target, dataCh, stopCh)
		case frameData:
			if ch, ok := streams[stream]; ok {
				select {
				case ch <- payload:
				default:
				}
			}
		case frameClose:
			if stop, ok := closers[stream]; ok {
				close(stop)
			}
			delete(streams, stream)
			delete(closers, stream)
		}
	}
}

type testLink struct {
	conn net.Conn
	wmu  sync.Mutex
}

func (b *testLink) write(typ byte, stream uint32, payload []byte) error {
	b.wmu.Lock()
	defer b.wmu.Unlock()
	return writeFrame(b.conn, typ, stream, payload)
}

func testStream(bl *testLink, stream uint32, target string, dataCh chan []byte, stopCh chan struct{}) {
	tc, err := net.DialTimeout("tcp", target, 5*time.Second)
	ack := []byte("ok")
	if err != nil {
		ack = []byte("err:" + err.Error())
	}
	_ = bl.write(frameDial, stream, ack)
	if err != nil {
		return
	}
	defer tc.Close()
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := tc.Read(buf)
			if n > 0 {
				if werr := bl.write(frameData, stream, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				_ = bl.write(frameClose, stream, nil)
				return
			}
		}
	}()
	for {
		select {
		case data := <-dataCh:
			if _, err := tc.Write(data); err != nil {
				return
			}
		case <-stopCh:
			return
		}
	}
}

func portInt(p string) int {
	var n int
	fmt.Sscanf(p, "%d", &n)
	return n
}
