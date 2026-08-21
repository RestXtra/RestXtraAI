package db

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var errBadHostPort = errors.New("bad host:port")

func parseInt(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}

// ---------- 代理池（Proxy Pool） ----------

// Proxy 是一条代理池记录。凭证明文落库（与 llm_profiles.api_key 同策略），
// 读接口永不回显 password，仅通过 PasswordSet 告知 UI 是否已配置。
type Proxy struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Protocol    string     `json:"protocol"` // http | https | socks5 | socks5h
	Host        string     `json:"host"`
	Port        int        `json:"port"`
	Username    string     `json:"username"`
	Password    string     `json:"-"`
	PasswordSet bool       `json:"password_set"`
	Region      string     `json:"region"`
	Enabled     bool       `json:"enabled"`
	Note        string     `json:"note"`
	Source      string     `json:"source"` // manual | import | subscription
	LastCheckAt *time.Time `json:"last_check_at"`
	LastCheckOK bool       `json:"last_check_ok"`
	LatencyMs   int        `json:"latency_ms"`
	FailCount   int        `json:"fail_count"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

const proxyCols = `id, name, protocol, host, port, username, password, region, enabled, note, source,
last_check_at, last_check_ok, latency_ms, fail_count, created_at, updated_at`

func scanProxy(d *DB, row interface{ Scan(...any) error }) (*Proxy, error) {
	var p Proxy
	var password string
	err := row.Scan(&p.ID, &p.Name, &p.Protocol, &p.Host, &p.Port, &p.Username, &password,
		&p.Region, &p.Enabled, &p.Note, &p.Source, &p.LastCheckAt, &p.LastCheckOK,
		&p.LatencyMs, &p.FailCount, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	password, err = d.RevealSecret(password)
	if err != nil {
		return nil, err
	}
	p.PasswordSet = password != ""
	return &p, nil
}

// ListProxies 列出全部代理（密码不回显）。
func (d *DB) ListProxies() ([]*Proxy, error) {
	rows, err := d.Query(`SELECT ` + proxyCols + ` FROM proxies ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Proxy
	for rows.Next() {
		p, err := scanProxy(d, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProxyByID 返回单条代理（密码不回显）。
func (d *DB) ProxyByID(id int64) (*Proxy, error) {
	row := d.QueryRow(`SELECT `+proxyCols+` FROM proxies WHERE id=$1`, id)
	p, err := scanProxy(d, row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// SaveProxy 新建（id==0）或更新一条代理。password 为空时更新操作保留原密码。
func (d *DB) SaveProxy(p *Proxy) (int64, error) {
	password, err := d.ProtectSecret(p.Password)
	if err != nil {
		return 0, err
	}
	if p.ID == 0 {
		var id int64
		err := d.QueryRow(`INSERT INTO proxies(name,protocol,host,port,username,password,region,enabled,note,source)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
			p.Name, p.Protocol, p.Host, p.Port, p.Username, password,
			p.Region, p.Enabled, p.Note, p.Source).Scan(&id)
		return id, err
	}
	if p.Password == "" {
		_, err := d.Exec(`UPDATE proxies SET name=$1,protocol=$2,host=$3,port=$4,username=$5,region=$6,enabled=$7,note=$8 WHERE id=$9`,
			p.Name, p.Protocol, p.Host, p.Port, p.Username, p.Region, p.Enabled, p.Note, p.ID)
		return p.ID, err
	}
	_, err = d.Exec(`UPDATE proxies SET name=$1,protocol=$2,host=$3,port=$4,username=$5,password=$6,region=$7,enabled=$8,note=$9 WHERE id=$10`,
		p.Name, p.Protocol, p.Host, p.Port, p.Username, password, p.Region, p.Enabled, p.Note, p.ID)
	return p.ID, err
}

// DeleteProxy 删除一条代理。
func (d *DB) DeleteProxy(id int64) error {
	_, err := d.Exec(`DELETE FROM proxies WHERE id=$1`, id)
	return err
}

// DeleteProxies 批量删除；ids 为空时清空全部。返回删除行数。
func (d *DB) DeleteProxies(ids []int64) (int64, error) {
	if len(ids) == 0 {
		res, err := d.Exec(`DELETE FROM proxies`)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	res, err := d.Exec(`DELETE FROM proxies WHERE id IN (`+idList(ids)+`)`, idsToArgs(ids)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RecordProxyCheck 记录一次测活结果（成功/失败 + 延迟）。
func (d *DB) RecordProxyCheck(id int64, ok bool, latencyMs int) error {
	if ok {
		_, err := d.Exec(`UPDATE proxies SET last_check_at=now(), last_check_ok=true, latency_ms=$2, fail_count=0 WHERE id=$1`,
			id, latencyMs)
		return err
	}
	_, err := d.Exec(`UPDATE proxies SET last_check_at=now(), last_check_ok=false, fail_count=fail_count+1 WHERE id=$1`, id)
	return err
}

// RecordProxyCheckResult 由 server 层使用已扫描的 id 列表批量写回测活结果。
func (d *DB) RecordProxyCheckResult(ok bool, ids []int64) {
	for _, id := range ids {
		_ = d.RecordProxyCheck(id, ok, 0)
	}
}

// ProxyStats 聚合统计。
type ProxyStats struct {
	Total      int `json:"total"`
	Enabled    int `json:"enabled"`
	Healthy    int `json:"healthy"` // enabled && last_check_ok
	Unchecked  int `json:"unchecked"`
	Failing    int `json:"failing"` // enabled && 最近测活失败
	AvgLatency int `json:"avg_latency_ms"`
}

// ProxyPoolStats 返回代理池统计。
func (d *DB) ProxyPoolStats() (*ProxyStats, error) {
	var s ProxyStats
	err := d.QueryRow(`SELECT
		count(*) FILTER (WHERE enabled) AS enabled,
		count(*) FILTER (WHERE enabled AND last_check_ok) AS healthy,
		count(*) FILTER (WHERE enabled AND last_check_at IS NULL) AS unchecked,
		count(*) FILTER (WHERE enabled AND last_check_at IS NOT NULL AND NOT last_check_ok) AS failing,
		COALESCE(round(avg(latency_ms) FILTER (WHERE enabled AND last_check_ok)), 0)::int AS avg_latency,
		count(*) AS total
		FROM proxies`).
		Scan(&s.Enabled, &s.Healthy, &s.Unchecked, &s.Failing, &s.AvgLatency, &s.Total)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ProxyRaw 带密码的完整记录（仅内部使用：测活/选择器）。
type ProxyRaw struct {
	Proxy
	Password string
}

// ProxyRawByID 返回含密码的代理（仅服务端内部使用）。
func (d *DB) ProxyRawByID(id int64) (*ProxyRaw, error) {
	row := d.QueryRow(`SELECT `+proxyCols+` FROM proxies WHERE id=$1`, id)
	var p Proxy
	var password string
	err := row.Scan(&p.ID, &p.Name, &p.Protocol, &p.Host, &p.Port, &p.Username, &password,
		&p.Region, &p.Enabled, &p.Note, &p.Source, &p.LastCheckAt, &p.LastCheckOK,
		&p.LatencyMs, &p.FailCount, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	password, err = d.RevealSecret(password)
	if err != nil {
		return nil, err
	}
	return &ProxyRaw{Proxy: p, Password: password}, nil
}

// ListProxyRawAll 返回全部含密码的代理（供全量测活）。
func (d *DB) ListProxyRawAll() ([]*ProxyRaw, error) {
	rows, err := d.Query(`SELECT ` + proxyCols + ` FROM proxies ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProxyRaw
	for rows.Next() {
		var p Proxy
		var password string
		if err := rows.Scan(&p.ID, &p.Name, &p.Protocol, &p.Host, &p.Port, &p.Username, &password,
			&p.Region, &p.Enabled, &p.Note, &p.Source, &p.LastCheckAt, &p.LastCheckOK,
			&p.LatencyMs, &p.FailCount, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		password, err = d.RevealSecret(password)
		if err != nil {
			return nil, err
		}
		out = append(out, &ProxyRaw{Proxy: p, Password: password})
	}
	return out, rows.Err()
}

// ListHealthyProxies 返回当前可用的代理（enabled && 最近测活成功），供选择器使用。
func (d *DB) ListHealthyProxies(region string) ([]*ProxyRaw, error) {
	q := `SELECT ` + proxyCols + ` FROM proxies WHERE enabled AND last_check_ok`
	args := []any{}
	if region != "" {
		q += ` AND region = $1`
		args = append(args, region)
	}
	q += ` ORDER BY latency_ms`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProxyRaw
	for rows.Next() {
		var p Proxy
		var password string
		if err := rows.Scan(&p.ID, &p.Name, &p.Protocol, &p.Host, &p.Port, &p.Username, &password,
			&p.Region, &p.Enabled, &p.Note, &p.Source, &p.LastCheckAt, &p.LastCheckOK,
			&p.LatencyMs, &p.FailCount, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		password, err = d.RevealSecret(password)
		if err != nil {
			return nil, err
		}
		out = append(out, &ProxyRaw{Proxy: p, Password: password})
	}
	return out, rows.Err()
}

// ---------- 代理订阅源 ----------

// ProxySource 是一个代理订阅源（远程 URL 或粘贴文本）。
type ProxySource struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	URL           string     `json:"url"`
	IntervalSec   int        `json:"interval_sec"`
	Enabled       bool       `json:"enabled"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
	LastError     string     `json:"last_error"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// ListProxySources 列出全部订阅源。
func (d *DB) ListProxySources() ([]*ProxySource, error) {
	rows, err := d.Query(`SELECT id,name,COALESCE(url,''),interval_sec,enabled,last_checked_at,COALESCE(last_error,''),created_at,updated_at FROM proxy_sources ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProxySource
	for rows.Next() {
		var s ProxySource
		if err := rows.Scan(&s.ID, &s.Name, &s.URL, &s.IntervalSec, &s.Enabled,
			&s.LastCheckedAt, &s.LastError, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// ProxySourceByID 返回单个订阅源。
func (d *DB) ProxySourceByID(id int64) (*ProxySource, error) {
	var s ProxySource
	err := d.QueryRow(`SELECT id,name,COALESCE(url,''),interval_sec,enabled,last_checked_at,COALESCE(last_error,''),created_at,updated_at FROM proxy_sources WHERE id=$1`, id).
		Scan(&s.ID, &s.Name, &s.URL, &s.IntervalSec, &s.Enabled, &s.LastCheckedAt, &s.LastError, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveProxySource 新建/更新订阅源。
func (d *DB) SaveProxySource(s *ProxySource) (int64, error) {
	if s.IntervalSec <= 0 {
		s.IntervalSec = 3600
	}
	if s.ID == 0 {
		var id int64
		err := d.QueryRow(`INSERT INTO proxy_sources(name,url,interval_sec,enabled) VALUES ($1,$2,$3,$4) RETURNING id`,
			s.Name, s.URL, s.IntervalSec, s.Enabled).Scan(&id)
		return id, err
	}
	_, err := d.Exec(`UPDATE proxy_sources SET name=$1,url=$2,interval_sec=$3,enabled=$4 WHERE id=$5`,
		s.Name, s.URL, s.IntervalSec, s.Enabled, s.ID)
	return s.ID, err
}

// DeleteProxySource 删除订阅源。
func (d *DB) DeleteProxySource(id int64) error {
	_, err := d.Exec(`DELETE FROM proxy_sources WHERE id=$1`, id)
	return err
}

// MarkProxySourceChecked 记录订阅源最近一次检查（成功/失败）。
func (d *DB) MarkProxySourceChecked(id int64, errMsg string) error {
	if errMsg == "" {
		_, err := d.Exec(`UPDATE proxy_sources SET last_checked_at=now(), last_error='' WHERE id=$1`, id)
		return err
	}
	_, err := d.Exec(`UPDATE proxy_sources SET last_checked_at=now(), last_error=$2 WHERE id=$1`, id, truncateStr(errMsg, 500))
	return err
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ParseProxyLines 解析多行代理文本（ip:port / ip:port:user:pass / scheme://host:port / scheme://user:pass@host:port）。
// 返回可写入的代理项（未含 name，由调用方生成）。
func ParseProxyLines(text string) []*Proxy {
	var out []*Proxy
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := parseProxyLine(line)
		if p != nil {
			out = append(out, p)
		}
	}
	return out
}

func parseProxyLine(line string) *Proxy {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	// 去除行内 # 注释（保留 @，它是 user@host 的合法分隔符）。
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if line == "" {
		return nil
	}
	// 1) 显式 scheme:// 形式
	if idx := strings.Index(line, "://"); idx > 0 {
		scheme := strings.ToLower(line[:idx])
		rest := line[idx+3:]
		switch scheme {
		case "http", "https", "socks5", "socks5h":
			p := parseAuthHostPort(scheme, rest)
			if p != nil {
				return p
			}
		default:
			return nil
		}
		return nil
	}
	// 2) 裸 host:port[:user:pass] 或 host:port:user:pass
	parts := strings.Split(line, ":")
	// 需至少 host:port
	if len(parts) < 2 {
		return nil
	}
	host := strings.TrimSpace(parts[0])
	// 支持 IPv6 裸地址 [::1]:8080
	if host == "[" || (len(parts) > 2 && strings.Count(host, ":") > 0) {
		return nil // 复杂 IPv6 请用 socks5:// 显式形式
	}
	var port int
	var username, password string
	for i := 1; i < len(parts); i++ {
		portStr := strings.TrimSpace(parts[i])
		if portStr == "" {
			return nil
		}
		if n, err := parseInt(portStr); err == nil {
			port = n
			if i+1 < len(parts) {
				username = parts[i+1]
			}
			if i+2 < len(parts) {
				password = parts[i+2]
			}
			break
		}
		return nil
	}
	if port <= 0 || port > 65535 {
		return nil
	}
	return &Proxy{Protocol: "http", Host: host, Port: port, Username: username, Password: password, Source: "import"}
}

func parseAuthHostPort(scheme, rest string) *Proxy {
	rest = strings.TrimSpace(rest)
	// user:pass@host:port
	var username, password string
	hostPort := rest
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		userInfo := rest[:at]
		hostPort = rest[at+1:]
		if c := strings.Index(userInfo, ":"); c >= 0 {
			username = userInfo[:c]
			password = userInfo[c+1:]
		} else {
			username = userInfo
		}
	}
	host, portStr, err := splitHostPort(hostPort)
	if err != nil {
		return nil
	}
	port, err := parseInt(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return nil
	}
	return &Proxy{Protocol: scheme, Host: host, Port: port, Username: username, Password: password, Source: "import"}
}

func splitHostPort(s string) (host, port string, err error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") {
		// [v6]:port
		end := strings.Index(s, "]")
		if end < 0 {
			return "", "", errBadHostPort
		}
		host = s[1:end]
		rest := s[end+1:]
		if strings.HasPrefix(rest, ":") {
			port = rest[1:]
		}
		return host, port, nil
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		return s[:i], s[i+1:], nil
	}
	return s, "", nil
}

// ---------- Clash YAML 订阅解析 ----------

// clashProxyNode 是 Clash proxies: 列表里单个节点的最小字段集。
type clashProxyNode struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"` // socks5 | socks5h | http | https | ...
	Server   string `yaml:"server"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// clashConfig 是 Clash 配置的顶层结构（只取我们关心的字段）。
type clashConfig struct {
	Proxies     []clashProxyNode `yaml:"proxies"`
	ProxyGroups []map[string]any `yaml:"proxy-groups"`
	Rules       []string         `yaml:"rules"`
	MixedPort   int              `yaml:"mixed-port"`
	Port        int              `yaml:"port"`
	SocksPort   int              `yaml:"socks-port"`
	Mode        string           `yaml:"mode"`
}

// ParseClashText 解析 Clash YAML 文本。返回可导入的代理（仅 socks5/socks5h/http/https，
// 其余类型如 vmess/trojan 跳过）以及统计信息。
func ParseClashText(text string) (proxies []*Proxy, groups []string, rules []string, skipped int, err error) {
	var cfg clashConfig
	if err = yaml.Unmarshal([]byte(text), &cfg); err != nil {
		return nil, nil, nil, 0, err
	}
	for _, n := range cfg.Proxies {
		proto := clashTypeToProtocol(n.Type)
		if proto == "" {
			skipped++
			continue
		}
		if strings.TrimSpace(n.Server) == "" || n.Port <= 0 || n.Port > 65535 {
			skipped++
			continue
		}
		proxies = append(proxies, &Proxy{
			Name:     strings.TrimSpace(n.Name),
			Protocol: proto,
			Host:     strings.TrimSpace(n.Server),
			Port:     n.Port,
			Username: n.Username,
			Password: n.Password,
			Source:   "import",
		})
	}
	for _, g := range cfg.ProxyGroups {
		if name, ok := g["name"].(string); ok && name != "" {
			groups = append(groups, name)
		}
	}
	rules = cfg.Rules
	return proxies, groups, rules, skipped, nil
}

// clashTypeToProtocol 把 Clash 节点 type 映射到代理池协议；不支持的类型返回空串。
func clashTypeToProtocol(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "socks5":
		return "socks5"
	case "socks5h":
		return "socks5h"
	case "http":
		return "http"
	case "https":
		return "https"
	default:
		return ""
	}
}
