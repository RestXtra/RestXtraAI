package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RestXtra/RestXtraAI/db"
)

// ---------- 空间测绘（FOFA / Hunter / Quake）搜索代理 ----------

const (
	fofaSearchURL   = "https://fofa.info/api/v1/search/all"
	hunterSearchURL = "https://hunter.qianxin.com/openApi/search"
	quakeSearchURL  = "https://quake.360.cn/api/v3/search/quake_service"
)

// spaceSearchResult 是统一的结果行（各引擎字段收敛后的平坦结构）。
type spaceSearchResult struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Domain   string `json:"domain"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Server   string `json:"server"`
	Country  string `json:"country"`
	City     string `json:"city"`
	// Raw 保留原始字段供前端展示/明细（不进资产库）。
	Raw map[string]any `json:"raw,omitempty"`
}

// spaceSearchResponse 是统一响应。
type spaceSearchResponse struct {
	Provider string             `json:"provider"`
	Query    string             `json:"query"`
	Total    int                `json:"total"`
	Size     int                `json:"size"`
	Page     int                `json:"page"`
	Results  []*spaceSearchResult `json:"results"`
	Error    string             `json:"error,omitempty"`
}

// spaceSearchCreds 校验并返回某引擎的凭证；未配置返回 error。
func (s *Server) spaceSearchCreds(pg *db.DB, provider string) (string, error) {
	key := pg.SpaceSearchKey(provider)
	if key == "" {
		return "", fmt.Errorf("%s 未配置 API Key，请先在「配置」中填写", strings.ToUpper(provider))
	}
	return key, nil
}

// spaceSearch 统一入口：按 provider 分派到 FOFA / Hunter / Quake。
func (s *Server) spaceSearch(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Query    string `json:"query"`
		Size     int    `json:"size"`
		Page     int    `json:"page"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = "fofa"
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		writeErr(w, 400, "查询语法不能为空")
		return
	}
	if req.Size <= 0 || req.Size > 200 {
		req.Size = 50
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var resp *spaceSearchResponse
	var err error
	switch provider {
	case "fofa":
		resp, err = s.fofaSearch(ctx, pg, req.Query, req.Size, req.Page)
	case "hunter":
		resp, err = s.hunterSearch(ctx, pg, req.Query, req.Size, req.Page)
	case "quake":
		resp, err = s.quakeSearch(ctx, pg, req.Query, req.Size, req.Page)
	default:
		writeErr(w, 400, "provider 仅支持 fofa/hunter/quake")
		return
	}
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, resp)
}

// spaceSearchConfig 读写三个引擎的 API key 配置。
func (s *Server) spaceSearchConfigGet(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	writeJSON(w, 200, map[string]any{"providers": pg.SpaceSearchConfigs()})
}

func (s *Server) spaceSearchConfigSet(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Key      string `json:"key"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	switch req.Provider {
	case "fofa", "hunter", "quake":
	default:
		writeErr(w, 400, "provider 仅支持 fofa/hunter/quake")
		return
	}
	if err := pg.SaveSpaceSearchKey(req.Provider, strings.TrimSpace(req.Key)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "providers": pg.SpaceSearchConfigs()})
}

// spaceSearchTest 探测配置是否可用（轻量连通性检查）。返回 ok 与可读的错误详情。
func (s *Server) spaceSearchTest(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req struct {
		Provider string `json:"provider"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	fail := func(msg string) {
		writeJSON(w, 200, map[string]any{"ok": false, "error": msg})
	}
	switch req.Provider {
	case "fofa":
		_, err := s.fofaSearch(ctx, pg, "domain=\"example.com\"", 1, 1)
		if err != nil {
			fail(err.Error())
			return
		}
	case "hunter":
		_, err := s.hunterSearch(ctx, pg, "port=80", 1, 1)
		if err != nil {
			fail(err.Error())
			return
		}
	case "quake":
		_, err := s.quakeSearch(ctx, pg, "port: 80", 1, 1)
		if err != nil {
			fail(err.Error())
			return
		}
	default:
		writeErr(w, 400, "provider 仅支持 fofa/hunter/quake")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---------- FOFA ----------

func (s *Server) fofaSearch(ctx context.Context, pg *db.DB, query string, size, page int) (*spaceSearchResponse, error) {
	key, err := s.spaceSearchCreds(pg, "fofa")
	if err != nil {
		return nil, err
	}
	qb64 := base64.StdEncoding.EncodeToString([]byte(query))
	fields := "ip,port,protocol,domain,url,title,server,country,city"
	u := fmt.Sprintf("%s?key=%s&qbase64=%s&size=%d&page=%d&fields=%s",
		fofaSearchURL, url.QueryEscape(key), url.QueryEscape(qb64), size, page, url.QueryEscape(fields))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	body, err := doJSON(req)
	if err != nil {
		return nil, err
	}
	var out struct {
		Error   bool       `json:"error"`
		ErrMsg  string     `json:"errmsg"`
		Total   int        `json:"total"`
		Size    int        `json:"size"`
		Page    int        `json:"page"`
		Results [][]string `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("FOFA 响应解析失败: %v", err)
	}
	if out.Error {
		return nil, fmt.Errorf("FOFA 错误: %s", out.ErrMsg)
	}
	resp := &spaceSearchResponse{Provider: "fofa", Query: query, Total: out.Total, Size: len(out.Results), Page: page}
	for _, row := range out.Results {
		resp.Results = append(resp.Results, fofaRow(row))
	}
	return resp, nil
}

func fofaRow(row []string) *spaceSearchResult {
	col := func(i int) string {
		if i < len(row) {
			return row[i]
		}
		return ""
	}
	// fields: ip,port,protocol,domain,url,title,server,country,city
	res := &spaceSearchResult{
		IP:       col(0),
		Port:     atoiSafe(col(1)),
		Protocol: col(2),
		Domain:   col(3),
		URL:      col(4),
		Title:    col(5),
		Server:   col(6),
		Country:  col(7),
		City:     col(8),
	}
	res.Raw = map[string]any{"ip": col(0), "port": col(1), "protocol": col(2), "domain": col(3),
		"url": col(4), "title": col(5), "server": col(6), "country": col(7), "city": col(8)}
	return res
}

// ---------- Hunter（奇安信鹰图） ----------

func (s *Server) hunterSearch(ctx context.Context, pg *db.DB, query string, size, page int) (*spaceSearchResponse, error) {
	key, err := s.spaceSearchCreds(pg, "hunter")
	if err != nil {
		return nil, err
	}
	// Hunter API: 分页 page/page_size；时间窗默认近一年。
	now := time.Now()
	startTime := now.AddDate(-1, 0, 0).Format("2006-01-02 15:04:05")
	endTime := now.Format("2006-01-02 15:04:05")
	payload := map[string]any{
		"search":     query,
		"page":       page,
		"page_size":  size,
		"start_time": startTime,
		"end_time":   endTime,
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hunterSearchURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hunter-ApiKey", key)
	body, err := doJSON(req)
	if err != nil {
		return nil, err
	}
	var out struct {
		Code int `json:"code"`
		Data struct {
			Arr   []map[string]any `json:"arr"`
			Total int              `json:"total"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("Hunter 响应解析失败: %v", err)
	}
	if out.Code != 200 {
		return nil, fmt.Errorf("Hunter 错误(code=%d): %s", out.Code, out.Message)
	}
	resp := &spaceSearchResponse{Provider: "hunter", Query: query, Total: out.Data.Total, Size: len(out.Data.Arr), Page: page}
	for _, m := range out.Data.Arr {
		resp.Results = append(resp.Results, hunterRow(m))
	}
	return resp, nil
}

func hunterRow(m map[string]any) *spaceSearchResult {
	str := func(k string) string { v, _ := m[k].(string); return v }
	num := func(k string) int { v, _ := m[k].(float64); return int(v) }
	res := &spaceSearchResult{
		IP:       str("ip"),
		Port:     num("port"),
		Protocol: str("protocol"),
		Domain:   str("domain"),
		URL:      str("url"),
		Title:    str("web_title"),
		Server:   str("component"),
		Country:  str("country"),
		City:     str("city"),
	}
	res.Raw = m
	return res
}

// ---------- Quake（360 钟馗之眼） ----------

func (s *Server) quakeSearch(ctx context.Context, pg *db.DB, query string, size, page int) (*spaceSearchResponse, error) {
	key, err := s.spaceSearchCreds(pg, "quake")
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"query":   query,
		"size":    size,
		"start":   (page - 1) * size,
		"include": []string{"ip", "port", "protocol", "domain", "service.http.title", "service.name", "location.country_cn", "location.city_cn"},
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, quakeSearchURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-QuakeToken", key)
	body, err := doJSON(req)
	if err != nil {
		return nil, err
	}
	var out struct {
		Code       int `json:"code"`
		Message    string `json:"message"`
		TotalCount int `json:"total_count"`
		Data       []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("Quake 响应解析失败: %v", err)
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("Quake 错误(code=%d): %s", out.Code, out.Message)
	}
	resp := &spaceSearchResponse{Provider: "quake", Query: query, Total: out.TotalCount, Size: len(out.Data), Page: page}
	for _, m := range out.Data {
		resp.Results = append(resp.Results, quakeRow(m))
	}
	return resp, nil
}

func quakeRow(m map[string]any) *spaceSearchResult {
	str := func(path string) string {
		parts := strings.Split(path, ".")
		var cur any = m
		for _, p := range parts {
			mm, ok := cur.(map[string]any)
			if !ok {
				return ""
			}
			cur = mm[p]
		}
		if s, ok := cur.(string); ok {
			return s
		}
		return ""
	}
	num := func(k string) int {
		if v, ok := m[k].(float64); ok {
			return int(v)
		}
		return 0
	}
	res := &spaceSearchResult{
		IP:       str("ip"),
		Port:     num("port"),
		Protocol: str("protocol"),
		Domain:   str("domain"),
		Title:    str("service.http.title"),
		Server:   str("service.name"),
		Country:  str("location.country_cn"),
		City:     str("location.city_cn"),
	}
	res.Raw = m
	return res
}

// ---------- helpers ----------

func doJSON(req *http.Request) ([]byte, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, firstLine(string(body), 300))
	}
	return body, nil
}

func atoiSafe(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			break
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
