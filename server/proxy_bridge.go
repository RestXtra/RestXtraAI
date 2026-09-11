package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/RestXtra/RestXtraAI/db"
	"github.com/RestXtra/RestXtraAI/proxybridge"
)

// 代理入口（proxy bridge）：把一个启用的代理池节点变成本地混合 HTTP/SOCKS5 正向代理，
// 供 agent / 工具 / 外部流量按规则走代理（规则匹配的走直连）。配置持久化在 settings，
// 运行态在内存。参见 proxybridge 包。

const proxyBridgeCfgKey = "proxy_bridge_config"

// proxyBridgeConfig 持久化的代理入口配置。
type proxyBridgeConfig struct {
	Port       int                `json:"port"`
	NodeID     int64              `json:"node_id"`
	Rules      []proxybridge.Rule `json:"rules"`
	ClientUser string             `json:"client_user"`
	ClientPass string             `json:"client_password"`
}

// proxyBridgeState 运行时状态（bridge 实例）。
type proxyBridgeState struct {
	mu     sync.Mutex
	bridge *proxybridge.Bridge
}

func (s *Server) proxyBridgeState() *proxyBridgeState {
	if s.proxyBridge == nil {
		s.proxyBridge = &proxyBridgeState{}
	}
	return s.proxyBridge
}

func (s *Server) loadProxyBridgeConfig() (proxyBridgeConfig, error) {
	var cfg proxyBridgeConfig
	v, _, _ := s.m.pg.GetSetting(proxyBridgeCfgKey)
	if v == "" {
		cfg.Port = 7890
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(v), &cfg); err != nil {
		return cfg, fmt.Errorf("解析代理入口配置失败: %w", err)
	}
	if cfg.Port <= 0 {
		cfg.Port = 7890
	}
	return cfg, nil
}

func (s *Server) saveProxyBridgeConfig(cfg proxyBridgeConfig) error {
	b, _ := json.Marshal(cfg)
	return s.m.pg.SetSetting(proxyBridgeCfgKey, string(b))
}

// buildBridgeOutbound 从选中的代理节点构建出站（socks5/5h 或 http CONNECT）。
func (s *Server) buildBridgeOutbound(nodeID int64) (proxybridge.Outbound, error) {
	if nodeID <= 0 {
		return nil, fmt.Errorf("请先在「代理节点」里选择并启用一个节点")
	}
	p, err := s.m.pg.ProxyByID(nodeID)
	if err != nil || p == nil {
		return nil, fmt.Errorf("代理节点 %d 不存在", nodeID)
	}
	if !p.Enabled {
		return nil, fmt.Errorf("代理节点「%s」已停用，请先在节点列表启用", p.Name)
	}
	protocol := p.Protocol
	switch protocol {
	case "http", "https":
		protocol = "http"
	case "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("不支持的代理协议 %s", p.Protocol)
	}
	return &proxybridge.ProxyOutbound{
		Protocol: protocol,
		Addr:     fmt.Sprintf("%s:%d", p.Host, p.Port),
		Username: p.Username,
		Password: p.Password,
	}, nil
}

// startProxyBridge 按持久化配置启动本地混合代理入口。
func (s *Server) startProxyBridge() (proxyBridgeConfig, error) {
	st := s.proxyBridgeState()
	st.mu.Lock()
	defer st.mu.Unlock()
	cfg, err := s.loadProxyBridgeConfig()
	if err != nil {
		return cfg, err
	}
	if st.bridge != nil && st.bridge.Running() {
		return cfg, nil // already running
	}
	out, err := s.buildBridgeOutbound(cfg.NodeID)
	if err != nil {
		return cfg, err
	}
	b := proxybridge.New(proxybridge.Options{
		Port: cfg.Port, Rules: cfg.Rules, Outbound: out,
		ClientUser: cfg.ClientUser, ClientPassword: cfg.ClientPass,
	})
	if _, err := b.Start(); err != nil {
		return cfg, fmt.Errorf("启动代理入口失败: %w", err)
	}
	st.bridge = b
	s.applyTrafficUpstream(cfg.Port)
	_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: "system", Category: "proxy", Action: "bridge_start", Result: "success",
		Message: fmt.Sprintf("代理入口已启动 :%d → 节点 %d", cfg.Port, cfg.NodeID)})
	return cfg, nil
}

// applyTrafficUpstream 让录制代理(:8788)的出站走代理入口(SOCKS)，agent 的 HTTP/HTTPS
// 流量即经所选节点出口；port=0 恢复直连。
func (s *Server) applyTrafficUpstream(port int) {
	if s.m == nil || s.m.traffic == nil {
		return
	}
	if port > 0 {
		s.m.traffic.SetUpstream(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	} else {
		s.m.traffic.SetUpstream("")
	}
}

func (s *Server) stopProxyBridge() error {
	st := s.proxyBridgeState()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.bridge != nil {
		_ = st.bridge.Stop()
		st.bridge = nil
	}
	s.applyTrafficUpstream(0)
	return nil
}

// GET /api/proxy-bridge —— 状态
func (s *Server) proxyBridgeGet(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.loadProxyBridgeConfig()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	st := s.proxyBridgeState()
	st.mu.Lock()
	running := st.bridge != nil && st.bridge.Running()
	st.mu.Unlock()
	nodeName := ""
	if cfg.NodeID > 0 {
		if p, err := s.m.pg.ProxyByID(cfg.NodeID); err == nil && p != nil {
			nodeName = p.Name
		}
	}
	writeJSON(w, 200, map[string]any{
		"enabled":         running,
		"running":         running,
		"port":            cfg.Port,
		"node_id":         cfg.NodeID,
		"node_name":       nodeName,
		"rules":           cfg.Rules,
		"client_auth":     cfg.ClientUser != "",
		"client_username": cfg.ClientUser,
	})
}

// POST /api/proxy-bridge —— 保存（enabled=true 启动，false 停止）。载荷 {port,enabled,node_id,rules}
func (s *Server) proxyBridgeSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Port    int                `json:"port"`
		Enabled bool               `json:"enabled"`
		NodeID  int64              `json:"node_id"`
		Rules   []proxybridge.Rule `json:"rules"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	cfg, err := s.loadProxyBridgeConfig()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	cfg.Port = req.Port
	if cfg.Port <= 0 {
		cfg.Port = 7890
	}
	cfg.NodeID = req.NodeID
	cfg.Rules = req.Rules
	if err := s.saveProxyBridgeConfig(cfg); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if req.Enabled {
		if _, err := s.startProxyBridge(); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	} else {
		_ = s.stopProxyBridge()
	}
	s.proxyBridgeGet(w, r)
}

// POST /api/proxy-bridge/start
func (s *Server) proxyBridgeStart(w http.ResponseWriter, r *http.Request) {
	if _, err := s.startProxyBridge(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.proxyBridgeGet(w, r)
}

// POST /api/proxy-bridge/stop
func (s *Server) proxyBridgeStop(w http.ResponseWriter, r *http.Request) {
	_ = s.stopProxyBridge()
	cfg, _ := s.loadProxyBridgeConfig()
	_ = s.saveProxyBridgeConfig(cfg)
	_ = s.m.pg.RecordAudit(db.AuditEntry{Actor: "system", Category: "proxy", Action: "bridge_stop", Result: "success",
		Message: "代理入口已停止"})
	writeJSON(w, 200, map[string]any{"ok": true, "running": false})
}

// POST /api/proxy-bridge/import-rules —— 从多行文本导入直连规则。每行：含 / → ip-cidr；*. → domain-suffix；否则 domain。
func (s *Server) proxyBridgeImportRules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rules []string `json:"rules"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	cfg, err := s.loadProxyBridgeConfig()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rules := make([]proxybridge.Rule, 0, len(req.Rules))
	for _, line := range req.Rules {
		v := strings.TrimSpace(line)
		if v == "" {
			continue
		}
		var kind string
		switch {
		case strings.Contains(v, "/"):
			kind = "ip-cidr"
		case strings.HasPrefix(v, "*."):
			kind = "domain-suffix"
			v = strings.TrimPrefix(v, "*.")
		default:
			kind = "domain"
		}
		rules = append(rules, proxybridge.Rule{Kind: kind, Value: v, Direct: true})
	}
	cfg.Rules = rules
	if err := s.saveProxyBridgeConfig(cfg); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"rules": rules})
}
