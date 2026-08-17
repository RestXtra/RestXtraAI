package server

import (
	"encoding/json"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/RestXtra/RestXtraAI/db"
	"github.com/RestXtra/RestXtraAI/proxybridge"
)

// ---------- 代理入口（Proxy Bridge）管理 ----------

// proxyBridgeSettings keys (persisted in settings table).
const (
	settingBridgePort     = "proxy_bridge_port"
	settingBridgeEnabled  = "proxy_bridge_enabled"
	settingBridgeNodeID   = "proxy_bridge_node_id" // 0 = 动态挑健康节点
	settingBridgeRules    = "proxy_bridge_rules"   // JSON: []proxybridge.Rule
	settingBridgeUsername = "proxy_bridge_username"
	settingBridgePassword = "proxy_bridge_password"
	defaultBridgePort     = 7890
)

// bridgeManager owns the proxybridge.Bridge lifecycle and binds it to the
// proxy pool. It is safe for concurrent use.
type bridgeManager struct {
	mu      sync.Mutex
	bridge  *proxybridge.Bridge
	enabled bool
	nodeID  int64
	rules   []proxybridge.Rule
}

func newBridgeManager() *bridgeManager {
	return &bridgeManager{}
}

// loadFromSettings restores persisted config and (re)applies it. Called at
// startup after PG is connected.
func (m *bridgeManager) loadFromSettings(pg *db.DB) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	port := defaultBridgePort
	if v, ok, _ := pg.GetSetting(settingBridgePort); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 && n < 65536 {
			port = n
		}
	}
	enabled := pg.GetBool(settingBridgeEnabled, false)
	nodeID := int64(0)
	if v, ok, _ := pg.GetSetting(settingBridgeNodeID); ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			nodeID = n
		}
	}
	rules := []proxybridge.Rule{}
	if v, ok, _ := pg.GetSetting(settingBridgeRules); ok && v != "" {
		_ = json.Unmarshal([]byte(v), &rules)
	}

	m.enabled = enabled
	m.nodeID = nodeID
	m.rules = rules
	m.bridge = proxybridge.New(proxybridge.Options{Port: port})
	m.bridge.SetRules(rules)
	m.applyOutboundLocked(pg)
	if enabled {
		if _, err := m.bridge.Start(); err != nil {
			return err
		}
		log.Printf("[proxybridge] 已恢复：监听 :%d", m.bridge.Port())
	}
	return nil
}

// applyOutboundLocked resolves the current node selection into the bridge
// outbound. Caller holds mu.
func (m *bridgeManager) applyOutboundLocked(pg *db.DB) {
	if m.bridge == nil {
		return
	}
	var out proxybridge.Outbound = proxybridge.DirectOutbound{}
	if m.nodeID > 0 {
		if raw, err := pg.ProxyRawByID(m.nodeID); err == nil && raw != nil && raw.Enabled {
			out = proxyNodeOutbound(raw)
		}
	}
	m.bridge.SetOutbound(out)
}

// proxyNodeOutbound converts a pool node into a proxybridge outbound.
func proxyNodeOutbound(raw *db.ProxyRaw) proxybridge.Outbound {
	addr := net.JoinHostPort(raw.Host, strconv.Itoa(raw.Port))
	return &proxybridge.ProxyOutbound{
		Protocol: raw.Protocol,
		Addr:     addr,
		Username: raw.Username,
		Password: raw.Password,
	}
}

// setConfig updates persisted settings and applies them. enabled=true starts
// the bridge (reconfiguring if running), enabled=false stops it.
func (m *bridgeManager) setConfig(pg *db.DB, port int, enabled bool, nodeID int64, rules []proxybridge.Rule) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if port <= 0 {
		port = defaultBridgePort
	}
	if err := pg.SetSetting(settingBridgePort, strconv.Itoa(port)); err != nil {
		return err
	}
	if err := pg.SetBool(settingBridgeEnabled, enabled); err != nil {
		return err
	}
	if err := pg.SetSetting(settingBridgeNodeID, strconv.FormatInt(nodeID, 10)); err != nil {
		return err
	}
	b, _ := json.Marshal(rules)
	if err := pg.SetSetting(settingBridgeRules, string(b)); err != nil {
		return err
	}

	m.enabled = enabled
	m.nodeID = nodeID
	m.rules = rules

	// Rebuild bridge if port changed; else just update rules/outbound.
	if m.bridge == nil || m.bridge.Port() != port {
		if m.bridge != nil {
			_ = m.bridge.Stop()
		}
		m.bridge = proxybridge.New(proxybridge.Options{Port: port})
	}
	m.bridge.SetRules(rules)
	m.applyOutboundLocked(pg)

	if enabled {
		if _, err := m.bridge.Start(); err != nil {
			return err
		}
		log.Printf("[proxybridge] 已启动：监听 :%d", m.bridge.Port())
	} else {
		_ = m.bridge.Stop()
	}
	return nil
}

// stop shuts the bridge down and marks disabled.
func (m *bridgeManager) stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = false
	if m.bridge != nil {
		return m.bridge.Stop()
	}
	return nil
}

// status returns a snapshot for the API.
func (m *bridgeManager) status(pg *db.DB) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rules := m.rules
	nodeName := ""
	if m.nodeID > 0 {
		if p, err := pg.ProxyByID(m.nodeID); err == nil && p != nil {
			nodeName = p.Name
		}
	}
	port := defaultBridgePort
	running := false
	if m.bridge != nil {
		port = m.bridge.Port()
		running = m.bridge.Running()
	}
	username, _, _ := pg.GetSetting(settingBridgeUsername)
	return map[string]any{
		"enabled":         m.enabled,
		"running":         running,
		"port":            port,
		"node_id":         m.nodeID,
		"node_name":       nodeName,
		"rules":           rules,
		"client_auth":     username != "",
		"client_username": username,
	}, nil
}

// ensureProxyBridgeRecovery is called on startup to restart the bridge if it
// was enabled before shutdown.
func (s *Server) ensureProxyBridgeRecovery() {
	if s.m.pg == nil {
		return
	}
	if err := s.proxyBridge.loadFromSettings(s.m.pg); err != nil {
		log.Printf("[proxybridge] 启动恢复失败: %v", err)
	}
}

// parseBridgeRules decodes a JSON array of rules (tolerant of empty).
func parseBridgeRules(raw string) []proxybridge.Rule {
	rules := []proxybridge.Rule{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return rules
	}
	_ = json.Unmarshal([]byte(raw), &rules)
	return rules
}

// parseClashRules extracts DIRECT rules from a Clash rules list for the bridge:
// IP-CIDR / DOMAIN / DOMAIN-SUFFIX with DIRECT action are kept; others ignored.
func parseClashRules(rules []string) []proxybridge.Rule {
	var out []proxybridge.Rule
	for _, line := range rules {
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		kind := strings.ToUpper(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		action := strings.ToUpper(strings.TrimSpace(parts[2]))
		if action != "DIRECT" {
			continue
		}
		switch kind {
		case "IP-CIDR", "IP-CIDR6":
			if strings.Contains(value, "/") {
				out = append(out, proxybridge.Rule{Kind: "ip-cidr", Value: value, Direct: true})
			}
		case "DOMAIN":
			out = append(out, proxybridge.Rule{Kind: "domain", Value: value, Direct: true})
		case "DOMAIN-SUFFIX":
			out = append(out, proxybridge.Rule{Kind: "domain-suffix", Value: value, Direct: true})
		}
	}
	return out
}
