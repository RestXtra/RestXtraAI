package server

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/RestXtra/RestXtraAI/db"
)

// spaceSearchImport 把用户勾选的搜索结果批量导入资产管理。
// 请求体：results（spaceSearchResult 数组）+ provider + query。
// 公司关联由 upsert 内部按 company_scope 自动完成。
func (s *Server) spaceSearchImport(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Results  []*spaceSearchResult `json:"results"`
		Provider string               `json:"provider"`
		Query    string               `json:"query"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if len(req.Results) == 0 {
		writeErr(w, 400, "未选择要导入的资产")
		return
	}
	if len(req.Results) > 10000 {
		writeErr(w, 400, "单次最多导入 10000 条")
		return
	}
	as := s.m.Assets()
	stats := map[string]int{"ip": 0, "subdomain": 0, "service": 0, "skipped": 0}
	var errs []string
	for _, res := range req.Results {
		if res == nil {
			continue
		}
		kind, err := importSpaceResult(as, res)
		if err != nil {
			errs = append(errs, err.Error())
			stats["skipped"]++
			continue
		}
		stats[kind]++
	}
	// 记录审计（带当前操作者，若请求上下文有 principal）。
	actor := "unknown"
	if me, ok := principalOf(r); ok && me.Username != "" {
		actor = me.Username
	}
	_ = s.m.pg.RecordAudit(db.AuditEntry{
		Actor:   actor,
		Action:  "spacesearch.import",
		Result:  "success",
		Message: fmt.Sprintf("provider=%s query=%q imported=%d", req.Provider, req.Query, len(req.Results)-stats["skipped"]),
	})
	writeJSON(w, 200, map[string]any{
		"imported": len(req.Results) - stats["skipped"],
		"stats":    stats,
		"errors":   errs,
	})
}

// importSpaceResult 把一条搜索结果 upsert 进资产管理，返回资产类型。
func importSpaceResult(as *db.AssetStore, res *spaceSearchResult) (string, error) {
	if res.IP == "" && res.Domain == "" {
		return "skipped", fmt.Errorf("缺少 IP/域名，无法入库")
	}
	// 带端口 → 服务资产（HTTP 或其它协议）
	if res.Port > 0 {
		if err := importSpaceService(as, res); err != nil {
			return "skipped", err
		}
		return "service", nil
	}
	// 纯 IP → IP 资产
	if res.IP != "" {
		if err := importSpaceIP(as, res); err != nil {
			return "skipped", err
		}
		return "ip", nil
	}
	// 纯域名 → 子域名资产
	if err := importSpaceSubdomain(as, res); err != nil {
		return "skipped", err
	}
	return "subdomain", nil
}

func importSpaceIP(as *db.AssetStore, res *spaceSearchResult) error {
	if net.ParseIP(res.IP) == nil {
		return fmt.Errorf("非法 IP: %s", res.IP)
	}
	var ports []db.PortService
	if res.Port > 0 {
		ports = append(ports, db.PortService{Port: res.Port, Service: res.Server})
	}
	req := db.UpsertIPReq{IP: res.IP, BoundDomains: strList(res.Domain), OpenPorts: ports}
	_, err := as.UpsertIP(req)
	return err
}

func importSpaceSubdomain(as *db.AssetStore, res *spaceSearchResult) error {
	if _, err := as.UpsertSubdomain(db.UpsertSubdomainReq{Domain: res.Domain}); err != nil {
		return err
	}
	if res.IP != "" && net.ParseIP(res.IP) != nil {
		return as.AppendIPBoundDomain(res.IP, res.Domain)
	}
	return nil
}

func importSpaceService(as *db.AssetStore, res *spaceSearchResult) error {
	proto := strings.ToLower(res.Protocol)
	// HTTP 服务（有 URL 或协议为 http/https）→ UpsertHTTPService
	if res.URL != "" || proto == "http" || proto == "https" {
		u := res.URL
		if u == "" {
			scheme := "http"
			if proto == "https" {
				scheme = "https"
			}
			host := res.Domain
			if host == "" {
				host = res.IP
			}
			u = fmt.Sprintf("%s://%s:%d", scheme, host, res.Port)
		}
		if _, err := as.UpsertHTTPService(db.UpsertHTTPServiceReq{
			URL:       u,
			PageTitle: res.Title,
			IP:        res.IP,
		}); err != nil {
			return err
		}
		return nil
	}
	// 其它协议 → IP + 端口
	if res.IP != "" {
		if net.ParseIP(res.IP) == nil {
			return fmt.Errorf("非法 IP: %s", res.IP)
		}
		req := db.UpsertIPReq{IP: res.IP, OpenPorts: []db.PortService{{Port: res.Port, Service: res.Server}}}
		_, err := as.UpsertIP(req)
		return err
	}
	if _, err := as.UpsertSubdomain(db.UpsertSubdomainReq{Domain: res.Domain}); err != nil {
		return err
	}
	return nil
}

func strList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return []string{strings.TrimSpace(s)}
}
