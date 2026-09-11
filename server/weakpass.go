package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/reiver/go-telnet"
	"golang.org/x/crypto/ssh"
)

// ---- 有界弱口令探测（weak-pass）----
// 自包含、可移植（HTTP 表单/Basic、SSH、RDP、Telnet），不依赖外部 hydra。
// 安全边界：单线程；默认 ≤20 次、硬上限 50 次；命中即停；遇锁定/限流/验证码立即熔断；
// 需人工审批（见 weakpass_tools.go）。仅用于已授权目标。

const (
	weakpassMaxAttemptsHard = 50
	weakpassMaxAttemptsSoft = 20
	weakpassMaxUsers        = 5
	weakpassMaxPasswords    = 50
	weakpassDefaultDelayMS  = 250
	weakpassHTTPTimeout     = 12 * time.Second
)

// weakpassBuiltin 内置常见弱口令小字典（够"小范围尝试"，不做字典爆破）。
var weakpassBuiltin = []string{
	"admin", "admin123", "admin888", "admin@123", "administrator",
	"123456", "12345678", "123456789", "123123", "111111", "000000", "888888", "666666",
	"password", "password1", "P@ssw0rd", "qwerty", "abc123", "letmein", "welcome",
	"root", "root123", "toor", "test", "test123", "guest", "guest123", "1q2w3e", "Aa123456",
}

// weakpassLockoutRe 匹配锁定/限流/验证码等"应停手"的信号。
var weakpassLockoutRe = regexp.MustCompile(`(?i)(captcha|recaptcha|验证码|滑块|too many|rate.?limit|try again later|temporarily (locked|blocked)|account (is )?(locked|disabled)|登录失败次数过多|账户(已被)?(锁定|冻结)|请稍后|操作频繁|请求过于频繁|429|403 forbidden)`)

// weakpassShellRe 匹配交互式 shell 提示符（telnet 成功标志，尽力而为）。
var weakpassShellRe = regexp.MustCompile(`(?m)([#$>]\s*$|welcome|login successful|last login)`)

// weakpassSpec 是一次弱口令探测的参数。
type weakpassSpec struct {
	Kind      string   `json:"kind"` // http-form|http-basic|ssh|rdp|telnet
	URL       string   `json:"url"`  // http-*
	Host      string   `json:"host"` // ssh/rdp/telnet
	Port      int      `json:"port"`
	Users     []string `json:"users"`
	Username  string   `json:"username"`
	Passwords []string `json:"passwords"`
	// http-form / http-json 专用
	BodyType      string            `json:"body_type"` // "form"(默认) | "json"
	UserField     string            `json:"user_field"`
	PassField     string            `json:"pass_field"`
	ExtraFields   map[string]string `json:"extra_fields"`
	Headers       map[string]string `json:"headers"`
	Cookie        string            `json:"cookie"`
	CSRFField     string            `json:"csrf_field"`
	CSRFURL       string            `json:"csrf_url"`
	CSRFHeader    string            `json:"csrf_header"`
	SuccessMarker string            `json:"success_marker"`
	FailMarker    string            `json:"fail_marker"`
	SuccessStatus int               `json:"success_status"`
	// telnet 专用（提示符，可选）
	UserPrompt string `json:"user_prompt"`
	PassPrompt string `json:"pass_prompt"`
	// 边界
	MaxAttempts int `json:"max_attempts"`
	DelayMS     int `json:"delay_ms"`
}

func (s weakpassSpec) target() string {
	if s.URL != "" {
		return s.URL
	}
	host := s.Host
	if host == "" {
		host = "?"
	}
	if s.Port > 0 {
		return fmt.Sprintf("%s:%d", host, s.Port)
	}
	return host
}

// weakpassHit 是一次成功的弱口令命中。
type weakpassHit struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// weakpassOutcome 是探测结果（有界、可审计）。
type weakpassOutcome struct {
	Kind        string        `json:"kind"`
	Target      string        `json:"target"`
	Attempts    int           `json:"attempts"`
	MaxAttempts int           `json:"max_attempts"`
	Found       []weakpassHit `json:"found"`
	Aborted     bool          `json:"aborted"`
	AbortReason string        `json:"abort_reason,omitempty"`
	Locked      bool          `json:"locked"`
	Detail      []string      `json:"detail,omitempty"`
}

// weakpassVerify 验一组凭据：ok=命中；blocked=触发锁定/限流（应熔断）。
type weakpassVerify func(ctx context.Context, user, pass string) (ok bool, blocked bool, err error)

func weakpassCleanList(in []string, max int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
		if len(out) >= max {
			break
		}
	}
	return out
}

// runWeakPasswordProbe 串行、有界地尝试。任何情况下都不会超过硬上限。
func (s *Server) runWeakPasswordProbe(ctx context.Context, spec weakpassSpec) (*weakpassOutcome, error) {
	users := weakpassCleanList(spec.Users, weakpassMaxUsers)
	if u := strings.TrimSpace(spec.Username); u != "" {
		users = weakpassCleanList(append([]string{u}, users...), weakpassMaxUsers)
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("至少需要一个用户名（username 或 users）")
	}
	passwords := weakpassCleanList(spec.Passwords, weakpassMaxPasswords)
	if len(passwords) == 0 {
		passwords = weakpassCleanList(weakpassBuiltin, weakpassMaxPasswords)
	}
	maxAttempts := spec.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = weakpassMaxAttemptsSoft
	}
	if maxAttempts > weakpassMaxAttemptsHard {
		maxAttempts = weakpassMaxAttemptsHard
	}
	delay := time.Duration(spec.DelayMS) * time.Millisecond
	if spec.DelayMS <= 0 {
		delay = weakpassDefaultDelayMS * time.Millisecond
	}

	verify, err := s.weakpassVerifier(spec)
	if err != nil {
		return nil, err
	}
	return weakpassLoop(ctx, spec, users, passwords, maxAttempts, delay, verify), nil
}

// weakpassLoop 是串行有界循环（从 runWeakPasswordProbe 抽出，便于无网络单测）。
func weakpassLoop(ctx context.Context, spec weakpassSpec, users, passwords []string, maxAttempts int, delay time.Duration, verify weakpassVerify) *weakpassOutcome {
	out := &weakpassOutcome{Kind: spec.Kind, Target: spec.target(), MaxAttempts: maxAttempts}
	stop := func(reason string) *weakpassOutcome {
		out.Aborted = true
		out.AbortReason = reason
		return out
	}
	for _, u := range users {
		for _, p := range passwords {
			if out.Attempts >= maxAttempts {
				return stop(fmt.Sprintf("达到尝试上限 %d，已停止", maxAttempts))
			}
			if ctx.Err() != nil {
				return stop("上下文取消")
			}
			out.Attempts++
			ok, blocked, verr := verify(ctx, u, p)
			if ok {
				out.Found = append(out.Found, weakpassHit{Username: u, Password: p})
				out.Detail = append(out.Detail, fmt.Sprintf("命中: %s / %s", u, maskWeakpass(p)))
				return stop("命中弱口令，已停止后续尝试")
			}
			if blocked {
				out.Locked = true
				return stop("疑似触发锁定/限流/验证码，已熔断")
			}
			if verr != nil && len(out.Detail) < 20 {
				out.Detail = append(out.Detail, fmt.Sprintf("%s/%s: %v", u, maskWeakpass(p), verr))
			}
			if delay > 0 {
				select {
				case <-ctx.Done():
					return stop("上下文取消")
				case <-time.After(delay):
				}
			}
		}
	}
	return out
}

func maskWeakpass(p string) string {
	if len(p) <= 2 {
		return strings.Repeat("*", len(p))
	}
	return p[:1] + strings.Repeat("*", len(p)-2) + p[len(p)-1:]
}

func (s *Server) weakpassVerifier(spec weakpassSpec) (weakpassVerify, error) {
	switch spec.Kind {
	case "http-basic":
		if spec.URL == "" {
			return nil, fmt.Errorf("http-basic 需要 url")
		}
		cl := weakpassHTTPClient()
		return func(ctx context.Context, u, p string) (bool, bool, error) {
			return verifyHTTPBasic(ctx, cl, spec.URL, u, p)
		}, nil
	case "http-form", "http-json":
		if spec.URL == "" {
			return nil, fmt.Errorf("%s 需要 url", spec.Kind)
		}
		if spec.SuccessMarker == "" && spec.SuccessStatus == 0 && spec.FailMarker == "" {
			return nil, fmt.Errorf("%s 需要成功/失败判据：success_marker 或 success_status 或 fail_marker（否则无法可靠判定）", spec.Kind)
		}
		if spec.Kind == "http-json" {
			spec.BodyType = "json"
		}
		cl := weakpassHTTPClient()
		return func(ctx context.Context, u, p string) (bool, bool, error) {
			return verifyHTTPLogin(ctx, cl, spec, u, p)
		}, nil
	case "ssh":
		if spec.Host == "" {
			return nil, fmt.Errorf("ssh 需要 host")
		}
		return func(ctx context.Context, u, p string) (bool, bool, error) {
			return verifySSH(ctx, spec.Host, spec.Port, u, p)
		}, nil
	case "rdp":
		if spec.Host == "" {
			return nil, fmt.Errorf("rdp 需要 host")
		}
		return func(_ context.Context, u, p string) (bool, bool, error) {
			if err := rdpCredentialCheck(spec.Host, spec.Port, u, p); err != nil {
				return false, false, nil // RDP 无可靠锁定信号，失败即未命中
			}
			return true, false, nil
		}, nil
	case "telnet":
		if spec.Host == "" {
			return nil, fmt.Errorf("telnet 需要 host")
		}
		return func(_ context.Context, u, p string) (bool, bool, error) {
			return verifyTelnet(spec, u, p)
		}, nil
	default:
		return nil, fmt.Errorf("kind 需为 http-form | http-basic | ssh | rdp | telnet")
	}
}

func weakpassHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil) // 跨请求保留会话 cookie（登录页 → 登录 POST）
	return &http.Client{
		Timeout: weakpassHTTPTimeout,
		Jar:     jar,
		// 不自动跟随跳转：登录成功常是 302，需要在原响应上判定。
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			Proxy:           http.ProxyFromEnvironment,
		},
	}
}

func applyWeakpassHeaders(req *http.Request, spec weakpassSpec, csrf string) {
	for k, v := range spec.Headers {
		req.Header.Set(k, v)
	}
	if spec.Cookie != "" {
		req.Header.Set("Cookie", spec.Cookie)
	}
	if csrf != "" && spec.CSRFHeader != "" {
		req.Header.Set(spec.CSRFHeader, csrf)
	}
}

func verifyHTTPBasic(ctx context.Context, cl *http.Client, rawURL, user, pass string) (bool, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return false, false, err
	}
	req.SetBasicAuth(user, pass)
	resp, err := cl.Do(req)
	if err != nil {
		return false, false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 423:
		return false, true, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 400:
		return true, false, nil
	default:
		return false, false, nil
	}
}

// extractCSRF 从登录页 HTML 中提取 CSRF/hidden 字段值（支持 input 两种属性顺序与 meta）。
func extractCSRF(html, field string) string {
	if field == "" {
		return ""
	}
	q := regexp.QuoteMeta(field)
	pats := []string{
		`(?is)<input[^>]*\bname=["']` + q + `["'][^>]*\bvalue=["']([^"']+)["']`,
		`(?is)<input[^>]*\bvalue=["']([^"']+)["'][^>]*\bname=["']` + q + `["']`,
		`(?is)<meta[^>]*\bname=["']` + q + `["'][^>]*\bcontent=["']([^"']+)["']`,
	}
	for _, p := range pats {
		if m := regexp.MustCompile(p).FindStringSubmatch(html); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// fetchCSRF 取登录页并抽出 CSRF 令牌（每次尝试前调用，兼容 token 与会话绑定）。
func fetchCSRF(ctx context.Context, cl *http.Client, spec weakpassSpec) string {
	if spec.CSRFField == "" {
		return ""
	}
	u := spec.CSRFURL
	if u == "" {
		u = spec.URL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	applyWeakpassHeaders(req, spec, "")
	resp, err := cl.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<19))
	return extractCSRF(string(body), spec.CSRFField)
}

// verifyHTTPLogin 处理 http-form 与 http-json：构造登录请求，据 success/fail 判据判定。
func verifyHTTPLogin(ctx context.Context, cl *http.Client, spec weakpassSpec, user, pass string) (bool, bool, error) {
	uf, pf := spec.UserField, spec.PassField
	if uf == "" {
		uf = "username"
	}
	if pf == "" {
		pf = "password"
	}
	csrf := fetchCSRF(ctx, cl, spec)

	var body io.Reader
	contentType := "application/x-www-form-urlencoded"
	if spec.BodyType == "json" {
		m := map[string]string{uf: user, pf: pass}
		for k, v := range spec.ExtraFields {
			m[k] = v
		}
		if csrf != "" && spec.CSRFHeader == "" && spec.CSRFField != "" {
			m[spec.CSRFField] = csrf
		}
		b, _ := json.Marshal(m)
		body = bytes.NewReader(b)
		contentType = "application/json"
	} else {
		form := url.Values{}
		form.Set(uf, user)
		form.Set(pf, pass)
		for k, v := range spec.ExtraFields {
			form.Set(k, v)
		}
		if csrf != "" && spec.CSRFHeader == "" && spec.CSRFField != "" {
			form.Set(spec.CSRFField, csrf)
		}
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, spec.URL, body)
	if err != nil {
		return false, false, err
	}
	req.Header.Set("Content-Type", contentType)
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; RestXtraAI/weakpass)")
	}
	applyWeakpassHeaders(req, spec, csrf)

	resp, err := cl.Do(req)
	if err != nil {
		return false, false, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	bs := string(respBody)
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 423 || weakpassLockoutRe.MatchString(bs) {
		return false, true, nil
	}
	if spec.SuccessMarker != "" && strings.Contains(bs, spec.SuccessMarker) {
		return true, false, nil
	}
	if spec.SuccessStatus != 0 && resp.StatusCode == spec.SuccessStatus {
		return true, false, nil
	}
	if spec.FailMarker != "" && strings.Contains(bs, spec.FailMarker) {
		return false, false, nil
	}
	// 无成功信号 → 保守判未命中（不猜成功）。
	return false, false, nil
}

func verifySSH(_ context.Context, host string, port int, user, pass string) (bool, bool, error) {
	if port == 0 {
		port = 22
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         8 * time.Second,
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", host, port), cfg)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unable to authenticate") {
			return false, false, nil
		}
		return false, false, err
	}
	_ = client.Close()
	return true, false, nil
}

// verifyTelnet 尽力而为：连上后按提示符依次发用户名/口令，据 shell 提示符或
// success_marker 判定成功，据锁定特征词熔断。telnet 无可靠完成信号，判定偏保守。
func verifyTelnet(spec weakpassSpec, user, pass string) (bool, bool, error) {
	port := spec.Port
	if port == 0 {
		port = 23
	}
	addr := fmt.Sprintf("%s:%d", spec.Host, port)
	conn, err := telnet.DialTo(addr)
	if err != nil {
		return false, false, err
	}
	defer conn.Close()

	var mu sync.Mutex
	var all strings.Builder
	go func() {
		buf := make([]byte, 128)
		for {
			n, rerr := conn.Read(buf)
			mu.Lock()
			if n > 0 {
				all.Write(buf[:n])
			}
			mu.Unlock()
			if rerr != nil {
				return
			}
		}
	}()
	snapshot := func() string {
		mu.Lock()
		defer mu.Unlock()
		return all.String()
	}
	send := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }

	time.Sleep(1200 * time.Millisecond) // banner
	send(user)
	time.Sleep(1200 * time.Millisecond)
	send(pass)
	time.Sleep(1800 * time.Millisecond)

	out := snapshot()
	if weakpassLockoutRe.MatchString(out) {
		return false, true, nil
	}
	if spec.SuccessMarker != "" && strings.Contains(out, spec.SuccessMarker) {
		return true, false, nil
	}
	if weakpassShellRe.MatchString(out) {
		return true, false, nil
	}
	return false, false, nil
}
