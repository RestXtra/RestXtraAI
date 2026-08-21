package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// TSecBenchmark 跑分接入：
//   - 凭证：BENCHMARK_TOKEN / BENCHMARK_BASE_URL（settings 表，可被环境变量覆盖）
//   - VPN 预检：GET http://10.0.100.58 （status=="ok" 即连通）
//   - 代理到平台 /openapi/v1/challenges/*（请求头携带 BENCHMARK_TOKEN）
// 提供 REST 处理器给前端 + agent 工具（bench_*）给 worker/红队总指挥自主跑分。

const (
	settingBenchToken = "benchmark_token"
	settingBenchBase  = "benchmark_base_url"
	benchVPNProbe     = "http://10.0.100.58"
)

var benchmarkHTTPClient = &http.Client{Timeout: 30 * time.Second}

func (s *Server) benchConfig() (token, base string) {
	token, _, _ = s.m.pg.GetSecretSetting(settingBenchToken)
	base, _, _ = s.m.pg.GetSetting(settingBenchBase)
	if strings.TrimSpace(token) == "" {
		token = os.Getenv("BENCHMARK_TOKEN")
	}
	if strings.TrimSpace(base) == "" {
		base = os.Getenv("BENCHMARK_BASE_URL")
	}
	return strings.TrimSpace(token), strings.TrimSpace(strings.TrimRight(base, "/"))
}

// benchHTTP 向平台发一次带 token 的请求，返回状态码与原始 body（透传业务错误）。
func benchHTTP(ctx context.Context, token, base, method, path string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("BENCHMARK_TOKEN", token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := benchmarkHTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, b, nil
}

// ---------- 配置 ----------

func (s *Server) benchGetConfig(w http.ResponseWriter, r *http.Request) {
	token, base := s.benchConfig()
	writeJSON(w, 200, map[string]any{
		"token_set": token != "", "base_url": base,
	})
}

func (s *Server) benchSetConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := s.m.pg.SetSecretSetting(settingBenchToken, strings.TrimSpace(req.Token)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := s.m.pg.SetSetting(settingBenchBase, strings.TrimSpace(req.BaseURL)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---------- VPN 预检 ----------

func (s *Server) benchVPNCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, benchVPNProbe, nil)
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var j struct {
		Status string `json:"status"`
		IP     string `json:"client_ip"`
	}
	_ = json.Unmarshal(b, &j)
	writeJSON(w, 200, map[string]any{"ok": j.Status == "ok", "http": resp.StatusCode, "body": string(b)})
}

// ---------- 平台接口代理 ----------

func (s *Server) benchChallenges(w http.ResponseWriter, r *http.Request) {
	token, base := s.benchConfig()
	if token == "" || base == "" {
		writeErr(w, 400, "未配置 BENCHMARK_TOKEN / BENCHMARK_BASE_URL（POST /api/benchmark/config 或环境变量）")
		return
	}
	code, body, err := benchHTTP(r.Context(), token, base, http.MethodGet, "/openapi/v1/challenges", nil)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func (s *Server) benchStart(w http.ResponseWriter, r *http.Request) {
	token, base := s.benchConfig()
	uc := r.URL.Query().Get("unique_code")
	if token == "" || base == "" {
		writeErr(w, 400, "未配置 BENCHMARK_TOKEN / BENCHMARK_BASE_URL")
		return
	}
	code, body, err := benchHTTP(r.Context(), token, base, http.MethodPost,
		"/openapi/v1/challenges/start?unique_code="+urlEncode(uc), nil)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func (s *Server) benchHint(w http.ResponseWriter, r *http.Request) {
	token, base := s.benchConfig()
	uc := r.URL.Query().Get("unique_code")
	if token == "" || base == "" {
		writeErr(w, 400, "未配置 BENCHMARK_TOKEN / BENCHMARK_BASE_URL")
		return
	}
	code, body, err := benchHTTP(r.Context(), token, base, http.MethodGet,
		"/openapi/v1/challenges/hint?unique_code="+urlEncode(uc), nil)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func (s *Server) benchSubmit(w http.ResponseWriter, r *http.Request) {
	token, base := s.benchConfig()
	if token == "" || base == "" {
		writeErr(w, 400, "未配置 BENCHMARK_TOKEN / BENCHMARK_BASE_URL")
		return
	}
	var req struct {
		UniqueCode string `json:"unique_code"`
		Flag       string `json:"flag"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	body, _ := json.Marshal(req)
	code, out, err := benchHTTP(r.Context(), token, base, http.MethodPost, "/openapi/v1/challenges/submit", body)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	w.WriteHeader(code)
	_, _ = w.Write(out)
}

func (s *Server) benchClose(w http.ResponseWriter, r *http.Request) {
	token, base := s.benchConfig()
	uc := r.URL.Query().Get("unique_code")
	if token == "" || base == "" {
		writeErr(w, 400, "未配置 BENCHMARK_TOKEN / BENCHMARK_BASE_URL")
		return
	}
	code, body, err := benchHTTP(r.Context(), token, base, http.MethodPost,
		"/openapi/v1/challenges/close?unique_code="+urlEncode(uc), nil)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func urlEncode(s string) string {
	return url.QueryEscape(s)
}

// benchmarkCallForAgent 是 agent.BenchmarkCall 的实现：worker/红队总指挥的 bench_* 工具调用入口。
func (s *Server) benchmarkCallForAgent(ctx context.Context, op string, params map[string]string) (string, error) {
	token, base := s.benchConfig()
	if token == "" || base == "" {
		return "", fmt.Errorf("未配置 BENCHMARK_TOKEN / BENCHMARK_BASE_URL（请先在跑分页配置）")
	}
	do := func(method, path string, body []byte) (string, error) {
		code, out, err := benchHTTP(ctx, token, base, method, path, body)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("HTTP %d %s", code, strings.TrimSpace(string(out))), nil
	}
	switch op {
	case "vpn_check":
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		client := &http.Client{Timeout: 10 * time.Second}
		req, _ := http.NewRequestWithContext(cctx, http.MethodGet, benchVPNProbe, nil)
		resp, err := client.Do(req)
		if err != nil {
			return "VPN 未连通: " + err.Error(), nil
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Sprintf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(b))), nil
	case "challenges":
		return do(http.MethodGet, "/openapi/v1/challenges", nil)
	case "start":
		return do(http.MethodPost, "/openapi/v1/challenges/start?unique_code="+urlEncode(params["unique_code"]), nil)
	case "hint":
		return do(http.MethodGet, "/openapi/v1/challenges/hint?unique_code="+urlEncode(params["unique_code"]), nil)
	case "submit":
		body, _ := json.Marshal(map[string]string{"unique_code": params["unique_code"], "flag": params["flag"]})
		return do(http.MethodPost, "/openapi/v1/challenges/submit", body)
	case "close":
		return do(http.MethodPost, "/openapi/v1/challenges/close?unique_code="+urlEncode(params["unique_code"]), nil)
	}
	return "", fmt.Errorf("未知跑分操作: %s", op)
}
