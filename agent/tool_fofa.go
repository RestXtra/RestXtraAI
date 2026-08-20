package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	actool "github.com/Autumn-27/norma/tool"
	"github.com/RestXtra/RestXtraAI/db"
)

const fofaAssetSearchURL = "https://fofa.info/api/v1/search/all"

type fofaAssetRow struct {
	IP       string
	Port     int
	Protocol string
	Domain   string
	URL      string
	Title    string
	Server   string
}

// fofaAssetDiscover performs passive FOFA asset discovery for an explicitly
// supplied enterprise scope, then writes the normalised results into Assets.
func (t *ToolSet) fofaAssetDiscover() actool.CoreTool {
	return writeTool(
		"fofa_asset_discover",
		"对用户明确授权的企业根域名执行【被动】FOFA 资产测绘，并自动按企业归入资产管理。"+
			"会登记根域名为企业范围，查询该根域名，并把结果去重写入根域名、子域名、IP、HTTP 服务和其他服务。"+
			"只访问 FOFA 官方 API；不会发起端口扫描、目录爆破、漏洞验证或登录尝试。"+
			"company 与 root_domain 必须来自用户明确提供或已确认的资产范围。ICP/公司名扩展查询只用于补充候选结果，"+
			"候选结果仍必须由用户确认后才能扩大企业范围。",
		obj(map[string]any{
			"company":     str("企业名称，必须是用户明确授权的信息收集对象"),
			"root_domain": str("已确认的企业根域名，例如 example.com；不得传 URL、IP 或通配符"),
			"icp":         str("ICP 备案号（可选）。填写后额外查询该备案号，但不自动把新根域名加入企业范围"),
			"include_company_query": map[string]any{
				"type": "boolean", "description": "是否用企业全称执行 FOFA icp_company 补充查询；默认 false，结果会作为候选来源", "default": false,
			},
			"limit": map[string]any{
				"type": "integer", "description": "每个 FOFA 条件最多获取的记录数，1-200，默认 100", "default": 100,
			},
		}, "company", "root_domain"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.as == nil || t.cs == nil {
				return actool.Errorf("fofa_asset_discover 未启用: 资产库未初始化"), nil
			}
			var req struct {
				Company             string `json:"company"`
				RootDomain          string `json:"root_domain"`
				ICP                 string `json:"icp"`
				IncludeCompanyQuery bool   `json:"include_company_query"`
				Limit               int    `json:"limit"`
			}
			if err := json.Unmarshal(in, &req); err != nil {
				return actool.Errorf("参数解析失败: " + err.Error()), nil
			}
			req.Company = strings.TrimSpace(req.Company)
			root, ok := canonicalRootDomain(req.RootDomain)
			if req.Company == "" || !ok {
				return actool.Errorf("company 不能为空，root_domain 必须是有效根域名（如 example.com）"), nil
			}
			key := t.as.SpaceSearchKey("fofa")
			if key == "" {
				return actool.Errorf("FOFA API Key 未配置，请先在「空间测绘」填写并测试"), nil
			}
			if req.Limit < 1 || req.Limit > 200 {
				req.Limit = 100
			}

			companyID, _, err := t.cs.UpsertCompany(req.Company, "")
			if err != nil {
				return actool.Errorf("创建/获取企业失败: " + err.Error()), nil
			}
			if _, _, _, errMsgs := t.cs.AddScope(companyID, []string{root}, "用户授权的 FOFA 被动资产测绘根域名"); len(errMsgs) > 0 {
				return actool.Errorf("登记企业资产范围失败: " + strings.Join(errMsgs, "; ")), nil
			}
			if _, err := t.as.UpsertRootDomain(db.UpsertRootDomainReq{Domain: root, ICP: strings.TrimSpace(req.ICP), TaskID: t.taskID}); err != nil {
				return actool.Errorf("登记根域名失败: " + err.Error()), nil
			}

			queries := []string{fmt.Sprintf(`domain="%s"`, root)}
			if icp := strings.TrimSpace(req.ICP); icp != "" {
				queries = append(queries, fmt.Sprintf(`icp="%s"`, icp))
			}
			if req.IncludeCompanyQuery {
				queries = append(queries, fmt.Sprintf(`icp_company="%s"`, req.Company))
			}
			seen := map[string]bool{}
			var rows []fofaAssetRow
			for _, query := range queries {
				found, err := queryFOFAAssets(ctx, key, query, req.Limit)
				if err != nil {
					return actool.Errorf("FOFA 查询失败: " + err.Error()), nil
				}
				for _, row := range found {
					id := strings.Join([]string{row.IP, fmt.Sprint(row.Port), row.Protocol, row.Domain, row.URL}, "|")
					if !seen[id] {
						seen[id] = true
						rows = append(rows, row)
					}
				}
			}

			stats := map[string]int{"root_domain": 1, "ip": 0, "subdomain": 0, "service": 0, "candidate_out_of_scope": 0}
			for _, row := range rows {
				inScope := domainMatchesRoot(row.Domain, root) || hostMatchesRoot(row.URL, root)
				if !inScope && row.Domain != "" {
					stats["candidate_out_of_scope"]++
					continue
				}
				if row.Domain != "" && row.Domain != root {
					if _, err := t.as.UpsertSubdomain(db.UpsertSubdomainReq{Domain: row.Domain, TaskID: t.taskID}); err == nil {
						stats["subdomain"]++
					}
				}
				if net.ParseIP(row.IP) != nil {
					ports := []db.PortService{}
					if row.Port > 0 {
						ports = append(ports, db.PortService{Port: row.Port, Service: row.Server})
					}
					if _, err := t.as.UpsertIP(db.UpsertIPReq{IP: row.IP, BoundDomains: nonEmptyStrings(row.Domain), OpenPorts: ports, TaskID: t.taskID}); err == nil {
						stats["ip"]++
					}
				}
				if row.Port > 0 {
					if isHTTPProtocol(row.Protocol, row.URL) {
						u := row.URL
						if u == "" {
							u = fofaURL(row.Protocol, row.Domain, row.IP, row.Port)
						}
						if u != "" {
							_, _ = t.as.UpsertHTTPService(db.UpsertHTTPServiceReq{URL: u, PageTitle: row.Title, Technologies: splitTechnologies(row.Server), IP: row.IP, TaskID: t.taskID})
							stats["service"]++
						}
					} else if row.IP != "" {
						_, _ = t.as.UpsertOtherService(db.UpsertOtherServiceReq{IP: row.IP, Port: row.Port, ServiceName: row.Server, TaskID: t.taskID})
						stats["service"]++
					}
				}
			}
			t.writes.Assets += stats["root_domain"] + stats["ip"] + stats["subdomain"] + stats["service"]
			return jsonResult(map[string]any{"company_id": companyID, "root_domain": root, "queries": queries, "fofa_rows": len(rows), "imported": stats, "notice": "仅根域名范围内的结果已自动入库；FOFA 公司/备案查询中发现的其它根域名作为候选，需人工确认后再追加企业范围。"})
		},
	)
}

func queryFOFAAssets(ctx context.Context, key, query string, limit int) ([]fofaAssetRow, error) {
	q64 := base64.StdEncoding.EncodeToString([]byte(query))
	u := fmt.Sprintf("%s?key=%s&qbase64=%s&size=%d&page=1&fields=%s", fofaAssetSearchURL, url.QueryEscape(key), url.QueryEscape(q64), limit, url.QueryEscape("ip,port,protocol,domain,url,title,server"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FOFA HTTP %d", resp.StatusCode)
	}
	var out struct {
		Error   bool       `json:"error"`
		ErrMsg  string     `json:"errmsg"`
		Results [][]string `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if out.Error {
		return nil, fmt.Errorf("%s", out.ErrMsg)
	}
	rows := make([]fofaAssetRow, 0, len(out.Results))
	for _, raw := range out.Results {
		col := func(i int) string {
			if i < len(raw) {
				return strings.TrimSpace(raw[i])
			}
			return ""
		}
		port := 0
		_, _ = fmt.Sscan(col(1), &port)
		rows = append(rows, fofaAssetRow{IP: col(0), Port: port, Protocol: col(2), Domain: strings.ToLower(strings.TrimSuffix(col(3), ".")), URL: col(4), Title: col(5), Server: col(6)})
	}
	return rows, nil
}

func canonicalRootDomain(raw string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "*.")
	if strings.Contains(s, "://") {
		return "", false
	}
	s = strings.TrimSuffix(s, ".")
	if strings.Count(s, ".") < 1 || net.ParseIP(s) != nil || strings.ContainsAny(s, "/:*? ") {
		return "", false
	}
	return s, true
}

func domainMatchesRoot(domain, root string) bool {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	return domain == root || strings.HasSuffix(domain, "."+root)
}
func hostMatchesRoot(rawURL, root string) bool {
	u, err := url.Parse(rawURL)
	return err == nil && domainMatchesRoot(u.Hostname(), root)
}
func nonEmptyStrings(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return []string{v}
}
func isHTTPProtocol(proto, rawURL string) bool {
	p := strings.ToLower(proto)
	return p == "http" || p == "https" || strings.HasPrefix(strings.ToLower(rawURL), "http://") || strings.HasPrefix(strings.ToLower(rawURL), "https://")
}
func fofaURL(proto, domain, ip string, port int) string {
	host := domain
	if host == "" {
		host = ip
	}
	if host == "" {
		return ""
	}
	scheme := "http"
	if strings.EqualFold(proto, "https") {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}
func splitTechnologies(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '|' })
}
