// Package agent wires real LLM-driven planner and work agents (on top of the
// agent-core SDK) to the dual SQLite graph. See docs/架构设计.md
// §4.3 (planner) and §4.4 (work agent).
//
// Provider configuration is read from the environment so the system runs with
// any Anthropic- or OpenAI-format endpoint. If no key is configured, FromEnv
// returns ok=false and the exploration engine stays idle (an LLM is required).
package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/compaction"
	"github.com/Autumn-27/norma/llm"
	acperm "github.com/Autumn-27/norma/permission"
)

// Auth header selection for the Anthropic format. The SDK sends x-api-key by
// default; some Anthropic-compatible relay gateways (OpenCode GO, …) authenticate
// via Authorization: Bearer instead — that credential is named
// "ANTHROPIC_AUTH_TOKEN". Values map to llm_profiles.auth_mode.
const (
	AuthModeDefault = "" // 默认：anthropic→x-api-key，openai→Bearer（SDK 原生行为）
	AuthModeXAPIKey = "x-api-key"
	AuthModeBearer  = "bearer" // Authorization: Bearer <key>
)

// Config describes the LLM backend resolved from the environment.
type Config struct {
	Format  llm.Format
	BaseURL string
	APIKey  string
	Model   string
	// Proxy routes all LLM requests through the given proxy URL (http/https/socks5).
	// Empty falls back to the standard *_PROXY environment variables.
	Proxy string
	// RatePerSecond / RatePerMinute cap the shared request rate across ALL agents
	// using the provider (0 = that window unlimited).
	RatePerSecond float64
	RatePerMinute float64
	// ContextWindowK is the model's context window in K tokens (user-configured),
	// used to size compaction thresholds. 0 = default; see CompactionWindow.
	ContextWindowK int
	// ReasoningEffort selects the thinking mode: "" = 默认(不发送思考参数);
	// "off" = 关闭(thinking.type=disabled); "low"/"medium"/"high"/"max" = 开启并设强度.
	// NewProvider derives the provider-specific request fields from it.
	ReasoningEffort string
	// AuthMode selects the credential header for the Anthropic format: "" 或
	// "x-api-key" = SDK 默认 x-api-key；"bearer" = Authorization: Bearer（兼容
	// ANTHROPIC_AUTH_TOKEN 类网关）。OpenAI 格式恒为 Bearer，本字段对它是 no-op。
	AuthMode string
	// SessionID 是发往网关的 x-opencode-session 头值（OpenCode GO 等要求稳定会话 ID
	// 用于路由与提示词缓存）。空 = 不发送该头。设置时同时覆盖 User-Agent 为
	// RestXtraAI 自定义标识（网关要求非通用 SDK UA）。
	SessionID string
}

// compaction window resolution bounds (in K tokens). Below the floor the
// threshold math (window − summary reserve − buffer) would go non-positive and
// compaction would fire every turn; above the cap it would never fire.
const (
	defaultWindowK = 200  // unset → assume a 200K window (Claude default)
	minWindowK     = 32   // floor so effectiveWindow stays comfortably positive
	maxWindowK     = 1000 // cap at 1M tokens (user request)
)

// CompactionWindow returns the model context window in TOKENS for compaction
// thresholds, resolved from the user-configured size (ContextWindowK). 0/unset →
// a 200K default; otherwise clamped to [32K, 1M] so compaction stays effective.
func (c Config) CompactionWindow() int {
	k := c.ContextWindowK
	if k <= 0 {
		k = defaultWindowK
	}
	if k < minWindowK {
		k = minWindowK
	}
	if k > maxWindowK {
		k = maxWindowK
	}
	return k * 1000
}

// compactionConfig builds the agent-core compaction config for a context window
// in tokens. agentcore.NewSession wires the summarizer (same provider) when this
// is set on Options.Compaction.
//
// counter, when non-nil, is the exact token counter used for threshold math; nil
// falls back to norma's local length estimate (EstimateTokens). The counter is
// resolved per provider via Config.CompactionTokenCounter — never wired blindly,
// because a doomed remote count_tokens call would run on every turn.
// ToolResultBudget 收紧到 500 清更多陈旧大工具结果。
func compactionConfig(windowTokens int, counter llm.TokenCounter) *compaction.Config {
	if windowTokens <= 0 {
		windowTokens = defaultWindowK * 1000
	}
	return &compaction.Config{
		ContextWindow:    windowTokens,
		CountTokens:      counter,
		ToolResultBudget: 500,
	}
}

// CompactionTokenCounter returns an exact token counter for compaction thresholds,
// or nil to use norma's local length estimate.
//
// The count_tokens endpoint is Anthropic-specific, so a counter is wired only for
// an Anthropic provider that has a key. Previously this counter was wired for every
// provider with an empty key and the default host, so each model turn attempted a
// doomed HTTPS request to api.anthropic.com (no timeout) — pure per-turn latency,
// and the result was discarded on failure anyway. Non-Anthropic providers now skip
// the network entirely.
func (c Config) CompactionTokenCounter() llm.TokenCounter {
	if c.Format != llm.FormatAnthropic || strings.TrimSpace(c.APIKey) == "" {
		return nil
	}
	baseURL := strings.TrimSpace(c.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return llm.NewAnthropicTokenCounter(llm.Config{
		Format:  llm.FormatAnthropic,
		BaseURL: baseURL,
		APIKey:  c.APIKey,
		// A cheap, stable model id used purely to tokenize; the count endpoint
		// requires a known model name regardless of which model the agent runs.
		Model:      "claude-3-5-sonnet-20241022",
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	})
}

// FromEnv resolves the LLM provider config:
//
//	RESTXTRA_LLM_PROVIDER = anthropic|openai (default: inferred from keys)
//	RESTXTRA_LLM_MODEL    = model id        (default: per provider)
//	RESTXTRA_LLM_BASE_URL = endpoint        (optional)
//	RESTXTRA_LLM_PROXY    = proxy URL        (optional; http/https/socks5)
//	ANTHROPIC_API_KEY / OPENAI_API_KEY         = credentials
func FromEnv() (Config, bool) {
	env := func(names ...string) string {
		for _, n := range names {
			if v := os.Getenv(n); v != "" {
				return v
			}
		}
		return ""
	}
	prov := env("RESTXTRA_LLM_PROVIDER")
	anthKey := os.Getenv("ANTHROPIC_API_KEY")
	oaiKey := os.Getenv("OPENAI_API_KEY")

	if prov == "" {
		switch {
		case anthKey != "":
			prov = "anthropic"
		case oaiKey != "":
			prov = "openai"
		default:
			return Config{}, false
		}
	}

	c := Config{
		BaseURL: env("RESTXTRA_LLM_BASE_URL"),
		Model:   env("RESTXTRA_LLM_MODEL"),
		Proxy:   strings.TrimSpace(env("RESTXTRA_LLM_PROXY")),
	}
	switch prov {
	case "openai":
		c.Format = llm.FormatOpenAI
		c.APIKey = oaiKey
		if c.Model == "" {
			c.Model = "gpt-4o"
		}
	default:
		c.Format = llm.FormatAnthropic
		c.APIKey = anthKey
		if c.Model == "" {
			c.Model = "claude-opus-4-8"
		}
	}
	if c.APIKey == "" {
		return Config{}, false
	}
	return c, true
}

// ConfigFrom builds a Config from UI-provided strings (provider defaults to
// anthropic; model defaults per provider). Inputs are trimmed and the base URL
// is normalized to the API base the provider expects (the provider appends the
// endpoint path itself), so a full endpoint URL is tolerated.
func ConfigFrom(provider, model, baseURL, apiKey, proxy string) Config {
	c := Config{
		Model:   strings.TrimSpace(model),
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:  strings.TrimSpace(apiKey),
		Proxy:   strings.TrimSpace(proxy),
	}
	switch strings.TrimSpace(provider) {
	case "openai":
		c.Format = llm.FormatOpenAI
		// provider appends "/chat/completions"; tolerate a full endpoint URL.
		c.BaseURL = strings.TrimRight(strings.TrimSuffix(c.BaseURL, "/chat/completions"), "/")
		if c.Model == "" {
			c.Model = "gpt-4o"
		}
	default:
		c.Format = llm.FormatAnthropic
		// provider appends "/v1/messages".
		c.BaseURL = strings.TrimRight(strings.TrimSuffix(c.BaseURL, "/v1/messages"), "/")
		if c.Model == "" {
			c.Model = "claude-opus-4-8"
		}
	}
	return c
}

// Provider returns the short provider name ("anthropic"/"openai").
func (c Config) Provider() string {
	if c.Format == llm.FormatOpenAI {
		return "openai"
	}
	return "anthropic"
}

// NewProvider builds an llm.Provider from the config. When a rate is set, the
// limiter lives on the single provider instance — so planner + all workers +
// main agent (which share this provider) are bounded by one shared rate limit.
// P5.2：APIKey 支持逗号分隔多 key → 构建 keyPoolProvider（鉴权/限流失败自动切换）。
func (c Config) NewProvider() (llm.Provider, error) {
	keys := splitAPIKeys(c.APIKey)
	provs := make([]llm.Provider, 0, len(keys))
	for _, key := range keys {
		p, err := c.buildProvider(key)
		if err != nil {
			return nil, err
		}
		provs = append(provs, p)
	}
	if len(provs) == 1 {
		return provs[0], nil
	}
	return newKeyPool(provs), nil
}

// splitAPIKeys 按逗号切分多 key（去空格、去空项）。空串 → 单元素 [""]（保持原行为）。
func splitAPIKeys(apiKey string) []string {
	if !strings.Contains(apiKey, ",") {
		return []string{apiKey}
	}
	var out []string
	for _, part := range strings.Split(apiKey, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		out = []string{apiKey}
	}
	return out
}

// buildProvider 用单个 key 构建 provider（含 bearer 认证 / 限流 / thinking 参数）。
func (c Config) buildProvider(key string) (llm.Provider, error) {
	lc := llm.Config{
		Format:  c.Format,
		BaseURL: c.BaseURL,
		APIKey:  key,
		Model:   c.Model,
		Proxy:   c.Proxy,
	}
	// Derive the provider thinking params from the single UI selector:
	//   "" → omit both (provider default); "off" → disable; else → enable + effort.
	switch c.ReasoningEffort {
	case "":
		// leave both empty → omitted from the request
	case "off":
		lc.ThinkingType = "disabled"
	default:
		lc.ThinkingType = "enabled"
		lc.ReasoningEffort = c.ReasoningEffort
	}
	if c.RatePerSecond > 0 || c.RatePerMinute > 0 {
		lc.RateLimit = &llm.RateLimit{PerSecond: c.RatePerSecond, PerMinute: c.RatePerMinute}
	}
	// Anthropic 格式 + bearer 认证：注入一个把 x-api-key 换成 Authorization: Bearer 的
	// transport（SDK 硬编码 x-api-key，在 RoundTrip 层改写头，无需 fork SDK）。
	// 需要会话头（OpenCode GO）或自定义 UA 时也走同一 transport 注入。
	if c.Format == llm.FormatAnthropic && c.AuthMode == AuthModeBearer || c.SessionID != "" {
		client, err := c.gatewayHTTPClient(c.Proxy, key)
		if err != nil {
			return nil, err
		}
		lc.HTTPClient = client
	}
	return llm.NewProvider(lc)
}

// gatewayHTTPClient 构建一个注入网关所需头的 http.Client：可选的
// x-api-key→Bearer 改写（auth_mode=bearer）、x-opencode-session 会话头、以及
// 自定义 User-Agent（OpenCode GO 等网关要求非通用 SDK UA）。默认 transport 被克隆，
// 保留标准超时/连接池/*_PROXY 环境变量；显式 proxy 优先于环境。
func (c Config) gatewayHTTPClient(proxy, key string) (*http.Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("llm: invalid proxy %q: %w", proxy, err)
		}
		tr.Proxy = http.ProxyURL(u)
	} else {
		tr.Proxy = http.ProxyFromEnvironment
	}
	key = strings.TrimSpace(key)
	return &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if c.Format == llm.FormatAnthropic && c.AuthMode == AuthModeBearer {
			r.Header.Del("x-api-key") // the SDK set this; the gateway wants Bearer instead
			r.Header.Set("Authorization", "Bearer "+key)
		}
		if c.SessionID != "" {
			r.Header.Set("x-opencode-session", c.SessionID)
		}
		r.Header.Set("User-Agent", "RestXtraAI/2.4.0")
		return tr.RoundTrip(r)
	})}, nil
}

// roundTripperFunc adapts a func to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestConnection makes a minimal real completion to verify the provider/model/
// endpoint/key actually work. Returns the round-trip latency.
func TestConnection(ctx context.Context, c Config) (time.Duration, error) {
	prov, err := c.NewProvider()
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	start := time.Now()
	_, err = agentcore.Run(ctx, agentcore.Options{
		Provider:       prov,
		SystemPrompt:   []string{"你是连接测试。只回复 OK，不要别的。"},
		PermissionMode: acperm.ModeBypass,
		MaxTurns:       1,
		MaxTokens:      32,
	}, "ping")
	return time.Since(start), err
}
