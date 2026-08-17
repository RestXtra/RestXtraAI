package server

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RestXtra/RestXtraAI/db"
)

// ---------- 代理池：CRUD ----------

func (s *Server) proxyList(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	proxies, err := pg.ListProxies()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	stats, err := pg.ProxyPoolStats()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"proxies": proxies, "stats": stats})
}

func (s *Server) proxySave(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var p db.Proxy
	if err := decode(r, &p); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	p.Host = strings.TrimSpace(p.Host)
	if p.Protocol == "" {
		p.Protocol = "http"
	}
	p.Protocol = strings.ToLower(p.Protocol)
	switch p.Protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		writeErr(w, 400, "protocol 仅支持 http/https/socks5/socks5h")
		return
	}
	if p.Host == "" {
		writeErr(w, 400, "host 必填")
		return
	}
	if p.Port <= 0 || p.Port > 65535 {
		writeErr(w, 400, "port 非法")
		return
	}
	id, err := pg.SaveProxy(&p)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) proxyDelete(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := pg.DeleteProxy(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// proxyDeleteBatch removes a set (or all) of proxies.
func (s *Server) proxyDeleteBatch(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var n int64
	var err error
	if req.All {
		n, err = pg.DeleteProxies(nil)
	} else {
		n, err = pg.DeleteProxies(req.IDs)
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": n})
}

// proxyImport 批量导入代理：支持多行 ip:port / ip:port:user:pass / scheme://host:port /
// scheme://user:pass@host:port 文本，或远程订阅 URL（抓取内容后解析）。
func (s *Server) proxyImport(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Text string `json:"text"`
		URL  string `json:"url"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	text := strings.TrimSpace(req.Text)
	if req.URL != "" {
		u, err := url.Parse(req.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			writeErr(w, 400, "订阅 URL 必须为 http/https")
			return
		}
		body, err := fetchURL(req.URL, 30*time.Second)
		if err != nil {
			writeErr(w, 502, "抓取订阅失败: "+err.Error())
			return
		}
		text = string(body)
	}
	if text == "" {
		writeErr(w, 400, "请输入代理文本或订阅 URL")
		return
	}

	// Clash YAML 订阅：文本含 "proxies:" 顶层键时走 Clash 解析。
	var parsed []*db.Proxy
	var skipped int
	var groups, rules []string
	clashMode := strings.Contains(text, "\nproxies:") || strings.HasPrefix(text, "proxies:")
	if clashMode {
		var err error
		parsed, groups, rules, skipped, err = db.ParseClashText(text)
		if err != nil {
			writeErr(w, 400, "Clash YAML 解析失败: "+err.Error())
			return
		}
	} else {
		parsed = db.ParseProxyLines(text)
	}

	if len(parsed) == 0 {
		writeErr(w, 400, "未解析出任何代理（支持 ip:port / ip:port:user:pass / scheme://host:port / Clash YAML proxies:）")
		return
	}
	var saved int
	var errors []string
	for i, p := range parsed {
		if p.Name == "" {
			p.Name = fmt.Sprintf("导入-%s-%d", p.Host, p.Port)
			if i > 0 {
				p.Name = fmt.Sprintf("%s-%d", p.Name, i)
			}
		}
		p.Source = "import"
		if _, err := pg.SaveProxy(p); err != nil {
			errors = append(errors, fmt.Sprintf("%s:%d: %v", p.Host, p.Port, err))
			continue
		}
		saved++
	}
	resp := map[string]any{"imported": saved, "total": len(parsed), "errors": errors, "skipped": skipped}
	if clashMode {
		resp["format"] = "clash"
		resp["groups"] = groups
		resp["rules_count"] = len(rules)
	}
	writeJSON(w, 200, resp)
}

// fetchURL 下载远程订阅内容（限时、限大小）。
func fetchURL(rawURL string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "RestXtra/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return body, nil
}

// ---------- 代理池：测活 ----------

// proxyTest 测活单条代理：TCP 连通 + 协议握手 + 目标连通，返回延迟。
func (s *Server) proxyTest(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	raw, err := pg.ProxyRawByID(id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if raw == nil {
		writeErr(w, 404, "代理不存在")
		return
	}
	latency, err := testProxy(&raw.Proxy, raw.Password)
	ok = err == nil
	_ = pg.RecordProxyCheck(id, ok, latency)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "latency_ms": 0, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "latency_ms": latency})
}

// proxyTestAll 全量测活：并发探测所有代理，返回统计。
func (s *Server) proxyTestAll(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	raws, err := pg.ListProxyRawAll()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if len(raws) == 0 {
		writeJSON(w, 200, map[string]any{"tested": 0, "ok": 0, "fail": 0})
		return
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	var mu sync.Mutex
	okCount, failCount := 0, 0
	for _, raw := range raws {
		wg.Add(1)
		sem <- struct{}{}
		go func(raw *db.ProxyRaw) {
			defer wg.Done()
			defer func() { <-sem }()
			latency, err := testProxy(&raw.Proxy, raw.Password)
			_ = pg.RecordProxyCheck(raw.ID, err == nil, latency)
			mu.Lock()
			if err == nil {
				okCount++
			} else {
				failCount++
			}
			mu.Unlock()
		}(raw)
	}
	wg.Wait()
	writeJSON(w, 200, map[string]any{"tested": len(raws), "ok": okCount, "fail": failCount})
}

// testProxy 对单个代理做连通性验证：
//   - http/https: 经代理发起 CONNECT 到 example.com:443，隧道建立即视为连通
//   - socks5/socks5h: SOCKS5 握手 + 连目标
//
// 返回握手延迟（ms）。超时 8s。
func testProxy(p *db.Proxy, password string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	proxyAddr := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
	start := time.Now()

	switch p.Protocol {
	case "http", "https":
		// 通过代理发 CONNECT 到 example.com:443
		conn, err := dialTCP(ctx, proxyAddr)
		if err != nil {
			return 0, err
		}
		defer conn.Close()
		if p.Username != "" || password != "" {
			auth := "Basic " + basicAuth(p.Username, password)
			_, err = fmt.Fprintf(conn, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: %s\r\n\r\n", auth)
		} else {
			_, err = fmt.Fprintf(conn, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
		}
		if err != nil {
			return 0, err
		}
		buf := make([]byte, 512)
		n, err := conn.Read(buf)
		if err != nil {
			return 0, err
		}
		if !strings.Contains(string(buf[:n]), " 200 ") && !strings.Contains(string(buf[:n]), "200 Connection Established") {
			return 0, fmt.Errorf("代理拒绝隧道: %s", firstLine(string(buf[:n]), 200))
		}
		// 握手成功即视为连通（不继续传输数据，避免目标响应回显）。
		_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

	case "socks5", "socks5h":
		conn, err := dialTCP(ctx, proxyAddr)
		if err != nil {
			return 0, err
		}
		defer conn.Close()
		if err := socks5Handshake(ctx, conn, p.Username, password); err != nil {
			return 0, err
		}
		// 连目标（example.com:443）验证出口可用。
		if err := socks5Connect(ctx, conn, "example.com", 443); err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("不支持的协议: %s", p.Protocol)
	}

	return int(time.Since(start).Milliseconds()), nil
}

// dialTCP 带上下文的 TCP 拨号。
func dialTCP(ctx context.Context, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 8 * time.Second}
	return d.DialContext(ctx, "tcp", addr)
}

func basicAuth(user, pass string) string {
	// 避免引入 encoding/base64 依赖，直接内联
	const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	src := []byte(user + ":" + pass)
	var sb strings.Builder
	for i := 0; i < len(src); i += 3 {
		var chunk [3]byte
		n := copy(chunk[:], src[i:])
		sb.WriteByte(b64[chunk[0]>>2])
		sb.WriteByte(b64[(chunk[0]&0x03)<<4|chunk[1]>>4])
		if n > 1 {
			sb.WriteByte(b64[(chunk[1]&0x0f)<<2|chunk[2]>>6])
		} else {
			sb.WriteByte('=')
		}
		if n > 2 {
			sb.WriteByte(b64[chunk[2]&0x3f])
		} else {
			sb.WriteByte('=')
		}
	}
	return sb.String()
}

// socks5Handshake 完成 SOCKS5 方法协商。
func socks5Handshake(ctx context.Context, conn net.Conn, username, password string) error {
	// 版本 + 支持方法：0x00 (NO AUTH) 或 0x02 (USER/PASS)
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
		return fmt.Errorf("socks5 版本错误: %d", buf[0])
	}
	switch buf[1] {
	case 0x00:
		if username != "" || password != "" {
			return fmt.Errorf("代理要求认证但服务端接受 NO AUTH")
		}
	case 0x02:
		if username == "" && password == "" {
			return fmt.Errorf("代理要求认证但未提供账号")
		}
		ul := len(username)
		pl := len(password)
		payload := append([]byte{0x01, byte(ul)}, []byte(username)...)
		payload = append(payload, byte(pl))
		payload = append(payload, []byte(password)...)
		if _, err := conn.Write(payload); err != nil {
			return err
		}
		rb := make([]byte, 2)
		if _, err := io.ReadFull(conn, rb); err != nil {
			return err
		}
		if rb[1] != 0x00 {
			return fmt.Errorf("socks5 认证失败")
		}
	default:
		return fmt.Errorf("socks5 不支持的方法: %d", buf[1])
	}
	return nil
}

// socks5Connect 通过已握手的 SOCKS5 连接发起 CONNECT。
func socks5Connect(ctx context.Context, conn net.Conn, host string, port int) error {
	req := []byte{0x05, 0x01, 0x00}
	// 域名方式
	req = append(req, 0x03, byte(len(host)))
	req = append(req, []byte(host)...)
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		return err
	}
	// 响应头固定 4 字节 + 地址
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	if head[1] != 0x00 {
		return fmt.Errorf("socks5 连接失败 code=%d", head[1])
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
		return fmt.Errorf("socks5 非法地址类型")
	}
	rest := make([]byte, addrLen+2)
	if _, err := io.ReadFull(conn, rest); err != nil {
		return err
	}
	return nil
}

// ---------- 代理池：选择器 ----------

// proxyPick 按策略从健康池挑一个代理。strategy: random|round-robin|fastest。
func (s *Server) proxyPick(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Region   string `json:"region"`
		Protocol string `json:"protocol"`
		Strategy string `json:"strategy"` // random | round-robin | fastest
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	raws, err := pg.ListHealthyProxies(strings.TrimSpace(req.Region))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if req.Protocol != "" {
		filtered := raws[:0]
		for _, rw := range raws {
			if rw.Protocol == req.Protocol {
				filtered = append(filtered, rw)
			}
		}
		raws = filtered
	}
	if len(raws) == 0 {
		writeJSON(w, 200, map[string]any{"ok": false, "error": "无可用健康代理（先执行测活）"})
		return
	}
	// 顺序选择状态（round-robin 游标）
	picker := &proxyPickerState{cur: 0}
	picked := picker.pick(raws, req.Strategy)
	writeJSON(w, 200, map[string]any{
		"ok":     true,
		"id":     picked.ID,
		"name":   picked.Name,
		"protocol": picked.Protocol,
		"host":   picked.Host,
		"port":   picked.Port,
		"username": picked.Username,
		"region": picked.Region,
		"latency_ms": picked.LatencyMs,
		"url":   proxyURL(&picked.Proxy, picked.Password),
	})
}

// proxyURL 拼出可用的代理 URL（http://user:pass@host:port 或 socks5://host:port）。
func proxyURL(p *db.Proxy, password string) string {
	proto := p.Protocol
	if proto == "socks5h" {
		proto = "socks5"
	}
	var auth string
	if p.Username != "" || password != "" {
		auth = p.Username + ":" + url.QueryEscape(password) + "@"
	}
	hostPort := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
	return fmt.Sprintf("%s://%s%s", proto, auth, hostPort)
}

type proxyPickerState struct {
	cur int
}

// pick 从候选里按策略选一个。round-robin 使用进程内游标（简单近似，跨请求轮询）。
func (p *proxyPickerState) pick(candidates []*db.ProxyRaw, strategy string) *db.ProxyRaw {
	if len(candidates) == 1 {
		return candidates[0]
	}
	switch strategy {
	case "fastest":
		// candidates 已按 latency 升序（ListHealthyProxies ORDER BY latency_ms）
		return candidates[0]
	case "round-robin":
		idx := p.cur % len(candidates)
		p.cur++
		return candidates[idx]
	default: // random
		return candidates[rand.Intn(len(candidates))]
	}
}

// ---------- 代理订阅源 ----------

func (s *Server) proxySourceList(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	sources, err := pg.ListProxySources()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"sources": sources})
}

func (s *Server) proxySourceSave(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var src db.ProxySource
	if err := decode(r, &src); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	src.Name = strings.TrimSpace(src.Name)
	if src.Name == "" {
		writeErr(w, 400, "名称必填")
		return
	}
	if src.URL != "" {
		u, err := url.Parse(src.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			writeErr(w, 400, "订阅 URL 必须为 http/https")
			return
		}
	}
	id, err := pg.SaveProxySource(&src)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// URL 订阅立即抓取导入（同步刷新）。
	if src.URL != "" && src.Enabled {
		go s.refreshProxySource(id)
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) proxySourceDelete(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := pg.DeleteProxySource(id); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

// proxySourceRefresh 手动触发单个订阅源刷新。
func (s *Server) proxySourceRefresh(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	src, err := pg.ProxySourceByID(id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if src == nil {
		writeErr(w, 404, "订阅源不存在")
		return
	}
	if src.URL == "" {
		writeErr(w, 400, "该订阅源无 URL，无法远程刷新")
		return
	}
	s.refreshProxySource(id)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// refreshProxySource 抓取订阅 URL 并批量导入节点，记录检查结果。
func (s *Server) refreshProxySource(id int64) {
	if s.m.pg == nil {
		return
	}
	src, err := s.m.pg.ProxySourceByID(id)
	if err != nil || src == nil {
		return
	}
	body, err := fetchURL(src.URL, 30*time.Second)
	if err != nil {
		_ = s.m.pg.MarkProxySourceChecked(id, err.Error())
		return
	}
	text := string(body)
	var parsed []*db.Proxy
	clashMode := strings.Contains(text, "\nproxies:") || strings.HasPrefix(text, "proxies:")
	if clashMode {
		parsed, _, _, _, err = db.ParseClashText(text)
		if err != nil {
			_ = s.m.pg.MarkProxySourceChecked(id, "Clash YAML 解析失败: "+err.Error())
			return
		}
	} else {
		parsed = db.ParseProxyLines(text)
	}
	if len(parsed) == 0 {
		_ = s.m.pg.MarkProxySourceChecked(id, "未解析出任何代理节点")
		return
	}
	for i, p := range parsed {
		if p.Name == "" {
			p.Name = fmt.Sprintf("%s-%d", src.Name, i)
		}
		p.Source = "subscription"
		if _, err := s.m.pg.SaveProxy(p); err != nil {
			continue
		}
	}
	_ = s.m.pg.MarkProxySourceChecked(id, "")
}