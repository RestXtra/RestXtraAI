package server

import (
	"context"
	"encoding/json"

	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/memory"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
	"github.com/RestXtra/RestXtraAI/agent"
	"github.com/RestXtra/RestXtraAI/db"
	"github.com/RestXtra/RestXtraAI/llmrec"
	"github.com/RestXtra/RestXtraAI/metrics"
	"github.com/RestXtra/RestXtraAI/report"
)

// Server exposes the RestXtra AI backend over a JSON HTTP API for the shadcn/ui
// frontend.
type Server struct {
	m      *Manager
	engine *Engine
	ctx    context.Context

	skillDir string // root directory for skill subdirectories
	jwtKey   []byte // HS256 signing key loaded from / generated into dataDir/jwt.key

	cfgMu         sync.Mutex
	mainAgent     *agent.MainAgent // nil when no LLM provider is configured
	chatAgent     *agent.ChatAgent // conversational runner for the chat page; nil w/o LLM
	llmCfg        agent.Config     // current LLM config (key not exposed)
	llmOn         bool
	llmProf       string // active LLM profile name (for llmrec tagging)
	loginAttempts loginLimiter

	// chatBusy guards the per-task main-agent run: the chat handler launches the
	// agent on the server's background ctx (not the request ctx) and returns
	// immediately, so a page reload / proxy timeout can't abort a live run. This
	// map serializes turns per task — one main-agent run at a time per task, so
	// concurrent messages don't corrupt the shared exp<id>-main transcript.
	chatMu   sync.Mutex
	chatBusy map[string]bool
	// chatCancel holds the cancel func for each in-flight conversation run (keyed by
	// convBusyKey), so a manual stop can abort JUST that session's agent run. Set/
	// cleared alongside chatBusy under chatMu. Aborting a run does NOT touch the P3
	// trigger queue — the drain goroutine simply proceeds to the next queued fire.
	chatCancel map[string]context.CancelFunc

	// triggerQ serializes P3 trigger fires PER AGENT: multiple fires for the same
	// custom agent queue up (FIFO) and run one at a time — the next conversation
	// only starts after the previous one finishes. triggerRun marks that a drain
	// goroutine is already active for that agent key so we don't spawn two. Distinct
	// agents still run concurrently. Queue is in-memory (matches chatBusy); a restart
	// drops pending fires — the scheduler re-fires on its next tick from watermarks.
	queueMu    sync.Mutex
	triggerQ   map[string][]triggeredRun
	triggerRun map[string]bool

	// profAgents caches the dedicated planner/worker built for a specific (non-active)
	// LLM profile, keyed by profile id, so tasks pinned to that profile share one
	// provider (and its rate limiter). Built lazily on first use; invalidated when any
	// profile is saved/activated/deleted so edits take effect. The active-profile path
	// stays on the global engine planner/worker (applyLLM).
	profMu         sync.Mutex
	profAgents     map[int64]*profBundle
	profChatAgents map[int64]*agent.ChatAgent // per-profile ChatAgent cache (chat page)
}

// profBundle is a planner/worker pair built from one LLM profile.
type profBundle struct {
	pl *agent.Planner
	wk *agent.Worker
}

// triggeredRun is one queued P3 trigger fire awaiting its turn for an agent.
// taskID + mergeable let the drainer coalesce several event triggers (finding/goal)
// from the SAME task into one conversation before it starts (interval fires don't merge).
type triggeredRun struct {
	agentKey  string
	title     string
	message   string
	taskID    int64 // source task for finding/goal triggers; 0 for interval/none
	mergeable bool  // true for finding/goal event triggers (merge by taskID)
}

func New(ctx context.Context, m *Manager, skillDir string, dataDir string) *Server {
	key, err := loadOrCreateJWTKey(dataDir)
	if err != nil {
		log.Fatalf("[auth] JWT key: %v", err)
	}
	s := &Server{m: m, engine: NewEngine(m), ctx: ctx, skillDir: skillDir, jwtKey: key, chatBusy: map[string]bool{},
		chatCancel: map[string]context.CancelFunc{}, triggerQ: map[string][]triggeredRun{}, triggerRun: map[string]bool{},
		profAgents: map[int64]*profBundle{}, profChatAgents: map[int64]*agent.ChatAgent{}}
	// per-task LLM: a task pinned to a specific profile runs on that profile's
	// dedicated planner/worker; unpinned tasks fall back to the global active pair.
	s.engine.SetAgentResolver(func(t *Task) (*agent.Planner, *agent.Worker) {
		if t.LLMProfileID == nil {
			return nil, nil
		}
		return s.agentsForProfile(*t.LLMProfileID)
	})
	// Wire DB-stored prompt templates into the agents (新版方案 §3.3 / §5a). With no
	// override row, agents keep their built-in defaults — behavior is unchanged.
	if m.pg != nil {
		agent.PromptOverride = func(key string) (string, bool) {
			a, err := m.pg.GetAgentByKey(key)
			if err != nil || a == nil {
				return "", false
			}
			t, err := m.pg.CurrentPrompt(a.ID)
			if err != nil || t == "" {
				return "", false
			}
			return t, true
		}
		// Wire DB-stored wrap-up (settlement) prompts. Empty column → built-in default.
		agent.WrapupOverride = func(key string) (string, bool) {
			a, err := m.pg.GetAgentByKey(key)
			if err != nil || a == nil || a.WrapupPrompt == "" {
				return "", false
			}
			return a.WrapupPrompt, true
		}
		// Wire DB-stored wrap-up turn budgets. 0 / missing → built-in per-agent default.
		agent.WrapupMaxTurnsOverride = func(key string) (int, bool) {
			a, err := m.pg.GetAgentByKey(key)
			if err != nil || a == nil || a.WrapupMaxTurns <= 0 {
				return 0, false
			}
			return a.WrapupMaxTurns, true
		}
		// Wire DB-stored TASK-TIMEOUT wrap-up prompt/turns (worker/planner). Empty/0 → default.
		agent.WrapupTaskTimeoutOverride = func(key string) (string, bool) {
			a, err := m.pg.GetAgentByKey(key)
			if err != nil || a == nil || a.TaskTimeoutWrapupPrompt == "" {
				return "", false
			}
			return a.TaskTimeoutWrapupPrompt, true
		}
		agent.WrapupTaskTimeoutTurnsOverride = func(key string) (int, bool) {
			a, err := m.pg.GetAgentByKey(key)
			if err != nil || a == nil || a.TaskTimeoutWrapupMaxTurns <= 0 {
				return 0, false
			}
			return a.TaskTimeoutWrapupMaxTurns, true
		}
		// 知识库检索工具接线：worker/planner/mainagent/pentest/auto 任务内可查 KB。
		agent.KnowledgeSearch = func(ctx context.Context, q string, limit int) (string, error) {
			items, err := m.pg.SearchKnowledge(q, limit)
			if err != nil {
				return "", err
			}
			rows := make([]struct{ Title, Content, Tags string }, 0, len(items))
			for _, it := range items {
				rows = append(rows, struct{ Title, Content, Tags string }{Title: it.Title, Content: it.Content, Tags: it.Tags})
			}
			return agent.FmtKnowledgeResult(rows), nil
		}
		// 攻击模式库检索工具接线：agent 按 CVE/技术/关键词复用已验证 playbook。
		agent.PlaybookSearch = func(ctx context.Context, cve, technique, keywords string, limit int) (string, error) {
			results, err := m.pg.SearchPlaybook(db.PlaybookQuery{
				CVE: cve, Technique: technique, Keywords: keywords,
			}, limit)
			if err != nil {
				return "", err
			}
			rows := make([]struct{ Title, CveID, Tags, Verification, ExecutionSteps string }, 0, len(results))
			for _, r := range results {
				if r.Pattern == nil {
					continue
				}
				rows = append(rows, struct{ Title, CveID, Tags, Verification, ExecutionSteps string }{
					Title: r.Pattern.Title, CveID: r.Pattern.CveID, Tags: r.Pattern.Tags,
					Verification: r.Pattern.Verification, ExecutionSteps: r.Pattern.ExecutionSteps,
				})
			}
			return agent.FmtPlaybookResults(rows), nil
		}
		// search_playbook 默认绑定（一次性）。
		if v, _, _ := m.pg.GetSetting("playbook_tool_bind_v1"); v != "true" {
			_ = m.pg.AddAgentToToolBinding("search_playbook", []string{"worker", "pentest", "auto"})
			_ = m.pg.SetSetting("playbook_tool_bind_v1", "true")
		}
		// TSecBenchmark 跑分工具接线：worker/红队总指挥/pentest 可用 bench_* 自主跑分。
		agent.BenchmarkCall = s.benchmarkCallForAgent
		// P2.2 工具渐进披露开关（默认开）：低频工具 schema 隐藏经 SearchExtraTools 发现。
		agent.ProgressiveDisclosure = func() bool { return m.pg.GetBool("tool_progressive_disclosure", true) }
		if v, _, _ := m.pg.GetSetting("bench_tool_bind_v1"); v != "true" {
			for _, t := range []string{"bench_vpn_check", "bench_challenges", "bench_start", "bench_hint", "bench_submit", "bench_close"} {
				_ = m.pg.AddAgentToToolBinding(t, []string{"worker", "red_team_lead", "pentest"})
			}
			_ = m.pg.SetSetting("bench_tool_bind_v1", "true")
		}
		// search_knowledge 默认额外绑定 pentest/auto（一次性，不覆盖用户改动）。
		if v, _, _ := m.pg.GetSetting("knowledge_tool_bind_v1"); v != "true" {
			_ = m.pg.AddAgentToToolBinding("search_knowledge", []string{"pentest", "auto"})
			_ = m.pg.SetSetting("knowledge_tool_bind_v1", "true")
		}
		// 六域智能体体系（幂等播种：创建领域 agent + 绑定技能/MCP/工具）。
		s.seedSixDomainAgents()
		s.seedAgentModelBindings()                      // P1.4 强/弱模型路由：按模型名把 planner 绑强模型、worker 绑弱模型(一次性)
		wireAgentAugment(m.pg, s.skillDir, s.hostTools) // 可见 skills/MCP + 流量/编排 host 工具装配进 agent 工具集
		domainReg := buildDomainReg(m.Assets())
		wireTools(m.pg, domainReg)    // 内置工具表：按 agent 过滤 + 覆盖描述/schema + 注入默认值
		seedPrompts(m.pg)             // 内置 agent 默认提示词正文播种进 agent_prompts(仅空时)
		s.seedOrchestrationTools()    // P2 跨任务编排工具 seed 进 tools 表(可按 agent 绑定)
		s.seedPythonInterpreter()     // 自定义脚本工具:开机检测 python 解释器入库(仅空时)
		go newScheduler(s).Run(s.ctx) // P3 触发器调度(定时/finding/目标事件),仅自定义 agent
		s.startBatchScheduler()       // 批量任务队列后台排空(骨架执行器)
		// Fill the tool cache for any enabled MCP that has none yet (notably the
		// seeded browser MCP on first run). Async so it never blocks startup.
		go s.discoverEmptyMCPsOnStartup()
		logSink.SetDB(ctx, m.pg) // restore last 100 log rows and enable async persistence
	}
	// precedence: persisted DB config > env.
	if cfg, ok := s.loadLLMConfig(); ok {
		if err := s.applyLLM(cfg); err != nil {
			log.Printf("[engine] saved LLM config init failed — engine idle: %v", err)
		} else {
			log.Printf("[engine] LLM configured from DB: %s / %s", cfg.Provider(), cfg.Model)
		}
	} else if cfg, ok := agent.FromEnv(); ok {
		if err := s.applyLLM(cfg); err != nil {
			log.Printf("[engine] env provider init failed — engine idle: %v", err)
		} else {
			log.Printf("[engine] LLM configured from env: %s / %s", cfg.Provider(), cfg.Model)
		}
	} else {
		log.Printf("[engine] no LLM provider configured — engine idle until set via /api/llm or env")
	}
	// reload tasks persisted on disk so the task list survives a restart, and
	// restore persisted paused state (so a task paused before restart stays paused).
	for _, t := range m.LoadExisting() {
		// clear stale 'running' intents from a prior crash/restart (no live worker
		// owns them) so they re-claim instead of spinning forever in the UI.
		if n, _ := t.Store.ResetRunningIntents(); n > 0 {
			log.Printf("[engine] task %s 重置 %d 个残留 running 意图为 open", t.ID, n)
		}
		if t.Paused {
			s.engine.Pause(t.ID)
		}
		// 任务级超时:为每个未终态、带 timeout 的任务起 deadline 协调器,独立于 planner/worker
		// loop——非活跃任务重启后也能在到点后被收尾(deadline 已过则立即走收尾时序)。
		if !isTerminalStatus(t.Status) {
			s.engine.startDeadlineCoordinator(ctx, t)
		}
	}
	// resume the active task's engine; other tasks resume when opened (setActive).
	// (a restored-paused task's loops still start but idle until resumed.)
	if t := m.ActiveTask(); t != nil {
		s.engine.Run(ctx, t)
	}
	return s
}

// agentMaxTurns returns the configured max_turns for an agent key (0 = unlimited,
// also the fallback when no DB or no row).
func (s *Server) agentMaxTurns(key string) int {
	if s.m.pg == nil {
		return 0
	}
	a, err := s.m.pg.GetAgentByKey(key)
	if err != nil || a == nil {
		return 0
	}
	return a.MaxTurns
}

// agentRunSeconds returns the configured wall-clock run budget (seconds) for an
// agent key (0 = unlimited; 600 fallback when no DB or no row, matching schema).
func (s *Server) agentRunSeconds(key string) int {
	if s.m.pg == nil {
		return 600
	}
	a, err := s.m.pg.GetAgentByKey(key)
	if err != nil || a == nil {
		return 600
	}
	return a.RunSecs
}

// loadLLMConfig reads the active LLM profile from PG (llm_profiles).
func (s *Server) loadLLMConfig() (agent.Config, bool) {
	p, err := s.m.pg.ActiveProfile()
	if err != nil || p == nil {
		return agent.Config{}, false
	}
	cfg := agent.ConfigFrom(p.Format, p.Model, p.BaseURL, p.APIKey, p.Proxy)
	cfg.RatePerSecond, cfg.RatePerMinute = p.RatePerSecond, p.RatePerMinute
	cfg.ContextWindowK = p.ContextWindowK
	cfg.ReasoningEffort = p.ReasoningEffort
	cfg.AuthMode = p.AuthMode
	if cfg.APIKey == "" {
		return cfg, false
	}
	s.cfgMu.Lock()
	s.llmProf = p.Name
	s.cfgMu.Unlock()
	return cfg, true
}

// saveLLMConfig persists the LLM config as the active "default" profile in PG.
func (s *Server) saveLLMConfig(cfg agent.Config) {
	format := cfg.Provider()
	if format != "anthropic" {
		format = "openai"
	}
	var id int64
	if profs, _ := s.m.pg.ListProfiles(); profs != nil {
		for _, p := range profs {
			if p.Name == "default" {
				id = p.ID
				break
			}
		}
	}
	newID, err := s.m.pg.SaveProfile(&db.LLMProfile{
		ID: id, Name: "default", Format: format, Model: cfg.Model, BaseURL: cfg.BaseURL, Proxy: cfg.Proxy,
		APIKey: cfg.APIKey, RatePerSecond: cfg.RatePerSecond, RatePerMinute: cfg.RatePerMinute,
		ContextWindowK: cfg.ContextWindowK, ReasoningEffort: cfg.ReasoningEffort, AuthMode: cfg.AuthMode, IsDefault: true,
	})
	if err == nil {
		_ = s.m.pg.SetActiveProfile(newID)
	}
}

// reapplyActiveProfile hot-reloads the engine from the active DB profile so that
// saving or activating a profile takes effect without a restart. Best-effort:
// logs on failure and leaves the running engine untouched.
func (s *Server) reapplyActiveProfile() {
	cfg, ok := s.loadLLMConfig()
	if !ok {
		return
	}
	if err := s.applyLLM(cfg); err != nil {
		log.Printf("[engine] reapply active profile failed: %v", err)
		return
	}
	log.Printf("[engine] LLM reapplied from active profile: %s / %s", cfg.Provider(), cfg.Model)
}

// webSearchFor gates the global web-search opts (backend/key) by an agent's own
// web_search flag: the backend/key come from the global config, each agent decides on/off.
func (s *Server) webSearchFor(key string) agent.WebSearchOpts {
	o := s.m.WebSearchOpts()
	if a, err := s.m.pg.GetAgentByKey(key); err != nil || a == nil || !a.WebSearch {
		o.Enabled = false
	}
	return o
}

// providerForRole returns the (provider, config) for an agent role ("planner" /
// "worker" / …) when that role has an agent_llm_profiles binding (P1.4 强/弱模型路由),
// else the caller's base provider+cfg. Missing/invalid bindings fall back silently.
func (s *Server) providerForRole(role string, baseCfg agent.Config, baseProv llm.Provider) (llm.Provider, agent.Config) {
	pid, err := s.m.pg.GetAgentLLMProfile(role)
	if err != nil || pid <= 0 {
		return baseProv, baseCfg
	}
	cfg, ok := s.loadProfileConfig(pid)
	if !ok {
		return baseProv, baseCfg
	}
	prov, err := cfg.NewProvider()
	if err != nil {
		log.Printf("[engine] build provider for agent %q -> profile %d failed: %v", role, pid, err)
		return baseProv, baseCfg
	}
	if p, _ := s.m.pg.ProfileByID(pid); p != nil {
		prov = llmrec.Wrap(prov, s.m.PG(), cfg.Model, p.Name, s.m.LLMRecordEnabled)
	}
	return prov, cfg
}

// buildPlannerWorker builds a planner+worker pair on an already-constructed provider
// + cfg, with all engine callbacks / proxy / web-search / memory wiring. Shared by the
// global apply path (applyLLM) and the per-profile path (agentsForProfile), so a task
// pinned to a specific profile behaves identically to the active one — just a different LLM.
// planner/worker may individually be overridden by agent_llm_profiles bindings (P1.4).
func (s *Server) buildPlannerWorker(prov llm.Provider, cfg agent.Config) (*agent.Planner, *agent.Worker) {
	tx := transcript.NewStore(filepath.Join(s.m.dir, "transcripts")) // raw LLM conversation logs
	wkProv, wkCfg := s.providerForRole("worker", cfg, prov)
	plProv, plCfg := s.providerForRole("planner", cfg, prov)
	// traffic host tools flow through ToolAugment for every agent and are filtered by
	// the tools-table binding (default = worker), so worker behavior is unchanged.
	wk := agent.NewWorker(wkProv, wkCfg.Model, s.m.dir, tx, wkCfg.CompactionWindow(), s.agentMaxTurns("worker"))
	wk.SetRunTimeout(time.Duration(s.agentRunSeconds("worker")) * time.Second)
	wk.SetProxy(s.m.ProxyAddr(), s.m.ProxyCACert())
	wk.SetMemory(memory.NewStore(filepath.Join(s.m.dir, "memory")))
	wk.SetWebSearch(s.webSearchFor("worker"))
	pl := agent.NewPlanner(plProv, plCfg.Model, s.m.dir, tx, plCfg.CompactionWindow(), s.agentMaxTurns("planner"))
	pl.SetKillWork(s.engine.KillWork)               // planner kill_work → terminate a running work
	pl.SetSteerWork(s.engine.SteerWork)             // planner steer_work → inject mid-run course-correction
	pl.SetProxy(s.m.ProxyAddr(), s.m.ProxyCACert()) // WebFetch through the recording proxy
	pl.SetWebSearch(s.webSearchFor("planner"))
	return pl, wk
}

// applyLLM (re)builds the planner/worker/main-agent from cfg and installs them on
// the running engine as the GLOBAL active pair. Safe to call at runtime (UI configures LLM).
func (s *Server) applyLLM(cfg agent.Config) error {
	prov, err := cfg.NewProvider()
	if err != nil {
		return err
	}
	// Wrap provider with the LLM call recorder (persists request/response to PG).
	s.cfgMu.Lock()
	profName := s.llmProf
	s.cfgMu.Unlock()
	prov = llmrec.Wrap(prov, s.m.PG(), cfg.Model, profName, s.m.LLMRecordEnabled)
	pl, wk := s.buildPlannerWorker(prov, cfg)
	s.engine.UseLLM(pl, wk)

	tx := transcript.NewStore(filepath.Join(s.m.dir, "transcripts"))
	win := cfg.CompactionWindow()
	s.cfgMu.Lock()
	s.mainAgent = agent.NewMainAgent(prov, cfg.Model, s.m.dir, tx, win, s.agentMaxTurns("mainagent"))
	s.mainAgent.SetProxy(s.m.ProxyAddr(), s.m.ProxyCACert()) // WebFetch through the recording proxy
	s.mainAgent.SetWebSearch(s.webSearchFor("mainagent"))
	// chat agent serves MANY custom agents by key → it holds the GLOBAL opts
	// (backend/key) and gates Enabled per-conversation-agent at Chat time. 对话始终用激活配置。
	s.chatAgent = agent.NewChatAgent(prov, cfg.Model, s.m.dir, tx, win) // chat page runner
	s.chatAgent.SetProxy(s.m.ProxyAddr(), s.m.ProxyCACert())
	s.chatAgent.SetWebSearch(s.m.WebSearchOpts())
	s.chatAgent.SetGuard(s.chatGuard())
	s.llmCfg = cfg
	s.llmOn = true
	s.cfgMu.Unlock()

	// wake the active task so a task created while idle starts exploring.
	if t := s.m.ActiveTask(); t != nil {
		t.Notify()
	}
	return nil
}

// loadProfileConfig builds an agent.Config from a specific profile id (with its key).
// ok=false when the profile is missing or has no api key.
func (s *Server) loadProfileConfig(id int64) (agent.Config, bool) {
	p, err := s.m.pg.ProfileByID(id)
	if err != nil || p == nil {
		return agent.Config{}, false
	}
	cfg := agent.ConfigFrom(p.Format, p.Model, p.BaseURL, p.APIKey, p.Proxy)
	cfg.RatePerSecond, cfg.RatePerMinute = p.RatePerSecond, p.RatePerMinute
	cfg.ContextWindowK = p.ContextWindowK
	cfg.ReasoningEffort = p.ReasoningEffort
	cfg.AuthMode = p.AuthMode
	if cfg.APIKey == "" {
		return cfg, false
	}
	return cfg, true
}

// agentsForProfile returns the dedicated planner/worker for a specific LLM profile,
// built + cached on first use (tasks on the same profile share one provider + limiter).
// nil,nil when the profile is invalid → the caller falls back to the global active pair.
func (s *Server) agentsForProfile(id int64) (*agent.Planner, *agent.Worker) {
	s.profMu.Lock()
	defer s.profMu.Unlock()
	if b := s.profAgents[id]; b != nil {
		return b.pl, b.wk
	}
	cfg, ok := s.loadProfileConfig(id)
	if !ok {
		return nil, nil
	}
	prov, err := cfg.NewProvider()
	if err != nil {
		log.Printf("[engine] build provider for LLM profile %d failed: %v", id, err)
		return nil, nil
	}
	// Wrap with recorder, tagged with this profile's name.
	if p, _ := s.m.pg.ProfileByID(id); p != nil {
		prov = llmrec.Wrap(prov, s.m.PG(), cfg.Model, p.Name, s.m.LLMRecordEnabled)
	}
	pl, wk := s.buildPlannerWorker(prov, cfg)
	s.profAgents[id] = &profBundle{pl: pl, wk: wk}
	log.Printf("[engine] built dedicated planner/worker for LLM profile %d (%s / %s)", id, cfg.Provider(), cfg.Model)
	return pl, wk
}

// chatAgentForProfile returns a ChatAgent built from a specific LLM profile, cached
// per profile id. Returns nil if the profile is missing or has no API key.
func (s *Server) chatAgentForProfile(id int64) *agent.ChatAgent {
	s.profMu.Lock()
	defer s.profMu.Unlock()
	if ca := s.profChatAgents[id]; ca != nil {
		return ca
	}
	cfg, ok := s.loadProfileConfig(id)
	if !ok {
		return nil
	}
	prov, err := cfg.NewProvider()
	if err != nil {
		log.Printf("[chat] build provider for LLM profile %d failed: %v", id, err)
		return nil
	}
	// Wrap with recorder, tagged with this profile's name.
	if p, _ := s.m.pg.ProfileByID(id); p != nil {
		prov = llmrec.Wrap(prov, s.m.PG(), cfg.Model, p.Name, s.m.LLMRecordEnabled)
	}
	tx := transcript.NewStore(filepath.Join(s.m.dir, "transcripts"))
	win := cfg.CompactionWindow()
	ca := agent.NewChatAgent(prov, cfg.Model, s.m.dir, tx, win)
	ca.SetProxy(s.m.ProxyAddr(), s.m.ProxyCACert())
	ca.SetWebSearch(s.m.WebSearchOpts())
	ca.SetGuard(s.chatGuard())
	s.profChatAgents[id] = ca
	return ca
}

// invalidateProfileAgents drops the per-profile agent cache so a profile save/activate/
// delete rebuilds pinned tasks' planner/worker on their next round.
func (s *Server) invalidateProfileAgents() {
	s.profMu.Lock()
	s.profAgents = map[int64]*profBundle{}
	s.profChatAgents = map[int64]*agent.ChatAgent{}
	s.profMu.Unlock()
}

func (s *Server) mainAgentRef() *agent.MainAgent {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	return s.mainAgent
}

func (s *Server) chatAgentRef() *agent.ChatAgent {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	return s.chatAgent
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Auth routes — exempt from JWT check (handled in requireAuth)
	mux.HandleFunc("GET /api/auth/status", s.authStatus)
	mux.HandleFunc("POST /api/auth/init", s.authInit)
	mux.HandleFunc("POST /api/auth/login", s.authLogin)
	mux.HandleFunc("POST /api/auth/logout", s.authLogout)
	mux.HandleFunc("POST /api/auth/change-password", s.authChangePassword)

	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/stats", s.stats)
	mux.HandleFunc("GET /api/dashboard/companies", s.dashboardCompanies)
	mux.HandleFunc("GET /api/metrics", s.metrics) // P5.4 关键路径指标
	mux.HandleFunc("GET /api/logs", s.getLogs)
	mux.HandleFunc("GET /api/logs/history", s.getLogsHistory)
	mux.HandleFunc("GET /api/logs/stream", s.streamLogs)
	mux.HandleFunc("POST /api/logs/clear", s.clearLogs)
	mux.HandleFunc("DELETE /api/logs", s.deleteLogs)

	mux.HandleFunc("GET /api/tasks", s.listTasks)
	mux.HandleFunc("POST /api/tasks", s.createTask)
	mux.HandleFunc("GET /api/tasks/{id}", s.getTask)
	mux.HandleFunc("GET /api/tasks/{id}/attack-chain", s.taskAttackChain)
	mux.HandleFunc("GET /api/tasks/{id}/coverage-graph", s.taskCoverageGraph)
	mux.HandleFunc("POST /api/tasks/{id}/control", s.control)
	mux.HandleFunc("POST /api/active", s.setActive)

	mux.HandleFunc("GET /api/llm", s.getLLM)
	mux.HandleFunc("POST /api/llm", s.setLLM)
	mux.HandleFunc("POST /api/llm/test", s.testLLM)

	// asset system
	mux.HandleFunc("GET /api/assets", s.listAssets)
	mux.HandleFunc("GET /api/assets/counts", s.assetCounts)
	mux.HandleFunc("POST /api/assets", s.insertAssets)
	mux.HandleFunc("DELETE /api/assets", s.deleteAssets)

	// company system
	mux.HandleFunc("GET /api/companies", s.listCompanies)
	mux.HandleFunc("POST /api/companies", s.createCompany)
	mux.HandleFunc("GET /api/companies/{id}", s.getCompany)
	mux.HandleFunc("DELETE /api/companies/{id}", s.deleteCompany)
	mux.HandleFunc("POST /api/companies/{id}/scope", s.addCompanyScope)
	mux.HandleFunc("POST /api/companies/reattribute", s.reattribute)

	mux.HandleFunc("GET /api/exploration/frontier", s.frontier)
	mux.HandleFunc("GET /api/exploration/findings", s.findings)
	mux.HandleFunc("GET /api/exploration/findings/stats", s.findingStats)
	mux.HandleFunc("GET /api/exploration/findings/{id}", s.findingDetail)
	mux.HandleFunc("PATCH /api/exploration/findings/{id}", s.patchFinding)
	mux.HandleFunc("GET /api/exploration/findings/{id}/lineage", s.findingLineage)
	mux.HandleFunc("GET /api/exploration/intents", s.intents)
	mux.HandleFunc("GET /api/exploration/graph", s.explorationGraph)
	mux.HandleFunc("GET /api/exploration/activity", s.activity)
	mux.HandleFunc("GET /api/exploration/activity/stream", s.streamActivity)
	mux.HandleFunc("GET /api/exploration/activity/company", s.activityByCompany)
	mux.HandleFunc("GET /api/exploration/activity/{seq}", s.activityDetail)
	mux.HandleFunc("GET /api/exploration/tokens", s.tokenStats)
	mux.HandleFunc("GET /api/tokens/daily", s.tokenDailyStats)
	mux.HandleFunc("GET /api/tokens/conversations", s.conversationTokens)

	mux.HandleFunc("GET /api/audit", s.getAudit)
	mux.HandleFunc("POST /api/gc", s.gc)
	mux.HandleFunc("GET /api/traffic", s.getTraffic)
	mux.HandleFunc("GET /api/traffic/exchange", s.getTrafficExchange)
	mux.HandleFunc("POST /api/traffic/clear", s.clearTraffic)
	mux.HandleFunc("DELETE /api/traffic", s.deleteTraffic)
	mux.HandleFunc("GET /api/commands", s.pgListCommands)
	mux.HandleFunc("DELETE /api/commands", s.pgDeleteCommands)
	mux.HandleFunc("GET /api/llm/records", s.pgListLLMRecords)
	mux.HandleFunc("GET /api/llm/records/{id}", s.pgGetLLMRecord)
	mux.HandleFunc("DELETE /api/llm/records/{id}", s.pgDeleteLLMRecord)
	mux.HandleFunc("DELETE /api/llm/records", s.pgDeleteLLMRecords)
	mux.HandleFunc("POST /api/llm/records/clear", s.pgClearLLMRecords)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.putSettings)
	mux.HandleFunc("POST /api/settings/web-search/test", s.testWebSearch)
	mux.HandleFunc("GET /api/report", s.getReport)
	mux.HandleFunc("POST /api/chat", s.chat)
	mux.HandleFunc("POST /api/tasks/{id}/chat/stop", s.stopChat)

	// --- 管理后台 API (PostgreSQL 数据源; 新版数据库与管理后台方案) ---
	mux.HandleFunc("DELETE /api/tasks/{id}", s.pgDeleteTask)
	mux.HandleFunc("PUT /api/tasks/{id}/companies", s.pgSetTaskCompanies)
	// Agents
	// conversations (chat page)
	mux.HandleFunc("GET /api/conversations", s.pgListConversations)
	mux.HandleFunc("POST /api/conversations", s.pgCreateConversation)
	mux.HandleFunc("PATCH /api/conversations/{id}", s.pgRenameConversation)
	mux.HandleFunc("PATCH /api/conversations/{id}/profile", s.pgUpdateConversation)
	mux.HandleFunc("DELETE /api/conversations/{id}", s.pgDeleteConversation)
	mux.HandleFunc("POST /api/conversations/clear", s.pgDeleteAllConversations)
	mux.HandleFunc("GET /api/conversations/{id}/messages", s.pgConversationMessages)
	mux.HandleFunc("POST /api/conversations/{id}/messages", s.pgSendConversationMessage)
	mux.HandleFunc("POST /api/conversations/{id}/stop", s.pgStopConversation)
	mux.HandleFunc("GET /api/conversations/{id}/messages/{seq}", s.pgConversationMsgDetail)

	mux.HandleFunc("GET /api/agents", s.pgListAgents)
	mux.HandleFunc("POST /api/agents", s.pgCreateAgent)
	mux.HandleFunc("GET /api/agents/{key}", s.pgGetAgent)
	mux.HandleFunc("PATCH /api/agents/{key}", s.pgUpdateAgent)
	mux.HandleFunc("DELETE /api/agents/{key}", s.pgDeleteAgent)
	mux.HandleFunc("PUT /api/agents/{key}/config", s.pgSaveAgentConfig)
	mux.HandleFunc("PUT /api/agents/{key}/prompt", s.pgSavePrompt)
	mux.HandleFunc("POST /api/agents/{key}/prompt/reset", s.pgResetPrompt)
	mux.HandleFunc("PUT /api/agents/{key}/wrapup", s.pgSaveWrapup)
	mux.HandleFunc("POST /api/agents/{key}/wrapup/reset", s.pgResetWrapup)
	mux.HandleFunc("PUT /api/agents/{key}/wrapup/task-timeout", s.pgSaveTaskTimeoutWrapup)
	mux.HandleFunc("POST /api/agents/{key}/wrapup/task-timeout/reset", s.pgResetTaskTimeoutWrapup)
	mux.HandleFunc("GET /api/agents/{key}/triggers", s.pgListTriggers)
	mux.HandleFunc("POST /api/agents/{key}/triggers", s.pgCreateTrigger)
	mux.HandleFunc("PATCH /api/triggers/{id}", s.pgUpdateTrigger)
	mux.HandleFunc("DELETE /api/triggers/{id}", s.pgDeleteTrigger)
	mux.HandleFunc("GET /api/agents/{key}/prompts", s.pgListPromptVersions)
	mux.HandleFunc("GET /api/agents/{key}/variables", s.pgPromptVars)
	mux.HandleFunc("POST /api/agents/{key}/prompt/preview", s.pgPreviewPrompt)
	mux.HandleFunc("GET /api/agents/{key}/visibility", s.pgGetAgentVisibility)
	mux.HandleFunc("PUT /api/agents/{key}/visibility", s.pgSetAgentVisibility)
	mux.HandleFunc("GET /api/agents/{key}/profile", s.pgGetAgentLLMProfile)
	mux.HandleFunc("POST /api/agents/{key}/profile", s.pgSetAgentLLMProfile)
	// 内置工具目录（描述/参数默认值可改、按 agent 绑定；key 与 handler 在代码层）
	mux.HandleFunc("GET /api/tools", s.pgListTools)
	mux.HandleFunc("PUT /api/tools/{key}", s.pgUpdateTool)
	mux.HandleFunc("POST /api/tools/custom", s.pgCreateCustomTool)
	mux.HandleFunc("POST /api/tools/custom/test", s.pgTestCustomTool)
	mux.HandleFunc("PUT /api/tools/custom/{key}", s.pgUpdateCustomTool)
	mux.HandleFunc("DELETE /api/tools/custom/{key}", s.pgDeleteCustomTool)
	mux.HandleFunc("POST /api/settings/python/detect", s.pgDetectPython)
	mux.HandleFunc("POST /api/tools/{key}/reset", s.pgResetTool)
	// MCP CRUD
	mux.HandleFunc("GET /api/mcp", s.pgListMCP)
	mux.HandleFunc("POST /api/mcp", s.pgSaveMCP)
	mux.HandleFunc("DELETE /api/mcp/{id}", s.pgDeleteMCP)
	mux.HandleFunc("GET /api/mcp/{id}/tools", s.pgMCPTools)
	mux.HandleFunc("POST /api/mcp/{id}/refresh", s.pgRefreshMCP)
	// 资产同步 — ScopeSentry 数据源
	mux.HandleFunc("GET /api/sync/scopesentry/status", s.syncSSStatus)
	mux.HandleFunc("POST /api/sync/scopesentry/datasource", s.syncSSDatasource)
	mux.HandleFunc("GET /api/sync/scopesentry/projects", s.syncSSProjects)
	mux.HandleFunc("GET /api/sync/scopesentry/tasks", s.syncSSTasks)
	mux.HandleFunc("POST /api/sync/scopesentry/sync", s.syncSSRun)
	// Skill CRUD (文件系统)
	mux.HandleFunc("GET /api/skills", s.fsListSkills)
	mux.HandleFunc("POST /api/skills", s.fsCreateSkill)
	mux.HandleFunc("POST /api/skills/upload", s.fsUploadSkill)
	mux.HandleFunc("DELETE /api/skills/{name}", s.fsDeleteSkill)
	mux.HandleFunc("PUT /api/skills/{name}/meta", s.fsUpdateSkillMeta)
	mux.HandleFunc("POST /api/skills/{name}/dirs", s.fsCreateDir)
	mux.HandleFunc("GET /api/skills/{name}/files", s.fsListFiles)
	// {file...} captures path segments including slashes (e.g. scripts/extract.py)
	mux.HandleFunc("GET /api/skills/{name}/files/{file...}", s.fsReadFile)
	mux.HandleFunc("PUT /api/skills/{name}/files/{file...}", s.fsWriteFile)
	mux.HandleFunc("DELETE /api/skills/{name}/files/{file...}", s.fsDeletePath)
	// MCP 资源侧可见性（更具体的 skill 路由会优先匹配）
	mux.HandleFunc("GET /api/visibility/{kind}/{id}", s.pgResourceVisibility)
	mux.HandleFunc("POST /api/visibility/toggle", s.pgToggleVisibility)
	// Skill 可见性（按名称，更具体，优先于上面的通配路由）
	mux.HandleFunc("GET /api/visibility/skill/{name}", s.pgSkillVisibility)
	mux.HandleFunc("POST /api/visibility/skill/toggle", s.pgToggleSkillVisibility)
	// LLM 多 profile
	mux.HandleFunc("GET /api/llm/profiles", s.pgListProfiles)
	mux.HandleFunc("POST /api/llm/profiles", s.pgSaveProfile)
	mux.HandleFunc("DELETE /api/llm/profiles/{id}", s.pgDeleteProfile)
	mux.HandleFunc("POST /api/llm/profiles/active", s.pgActivateProfile)

	// 拦截规则管理
	mux.HandleFunc("GET /api/intercept/rules", s.interceptListRules)
	mux.HandleFunc("POST /api/intercept/rules", s.interceptCreateRule)
	mux.HandleFunc("PUT /api/intercept/rules/{id}", s.interceptUpdateRule)
	mux.HandleFunc("DELETE /api/intercept/rules/{id}", s.interceptDeleteRule)
	mux.HandleFunc("POST /api/intercept/rules/{id}/toggle", s.interceptToggleRule)
	mux.HandleFunc("GET /api/intercept/pending", s.interceptListPending)
	mux.HandleFunc("GET /api/intercept/pending/{id}", s.interceptGetOne)
	mux.HandleFunc("POST /api/intercept/pending/{id}/decide", s.interceptDecide)
	mux.HandleFunc("GET /api/intercept/history", s.interceptHistory)
	mux.HandleFunc("GET /api/intercept/task/{taskID}", s.interceptListTaskItems)
	mux.HandleFunc("GET /api/intercept/tool-config", s.interceptGetToolConfig)
	mux.HandleFunc("PUT /api/intercept/tool-config", s.interceptSetToolConfig)

	// --- 平台层：多用户 RBAC + 审计（RestXtra 移植；统一 JWT + 权限点） ---
	mux.HandleFunc("GET /api/platform/my", s.platformMy)
	mux.HandleFunc("GET /api/platform/permissions", s.rbac("platform.role.read", s.platformPermissions))
	// 成员管理
	mux.HandleFunc("GET /api/platform/users", s.rbac("platform.user.read", s.platformListUsers))
	mux.HandleFunc("POST /api/platform/users", s.rbac("platform.user.write", s.platformCreateUser))
	mux.HandleFunc("PATCH /api/platform/users/{id}", s.rbac("platform.user.write", s.platformUpdateUser))
	mux.HandleFunc("DELETE /api/platform/users/{id}", s.rbac("platform.user.write", s.platformDeleteUser))
	mux.HandleFunc("DELETE /api/platform/users", s.rbac("platform.user.write", s.platformDeleteUsers))
	mux.HandleFunc("POST /api/platform/users/{id}/password", s.rbac("platform.user.write", s.platformResetPassword))
	mux.HandleFunc("POST /api/platform/users/{id}/roles", s.rbac("platform.user.role", s.platformSetUserRoles))
	// 平台角色
	mux.HandleFunc("GET /api/platform/roles", s.rbac("platform.role.read", s.platformListRoles))
	mux.HandleFunc("POST /api/platform/roles", s.rbac("platform.role.write", s.platformCreateRole))
	mux.HandleFunc("PATCH /api/platform/roles/{id}", s.rbac("platform.role.write", s.platformUpdateRole))
	mux.HandleFunc("DELETE /api/platform/roles/{id}", s.rbac("platform.role.write", s.platformDeleteRole))
	mux.HandleFunc("DELETE /api/platform/roles", s.rbac("platform.role.write", s.platformDeleteRoles))
	mux.HandleFunc("GET /api/platform/roles/{id}/permissions", s.rbac("platform.role.read", s.platformGetRolePermissions))
	mux.HandleFunc("PUT /api/platform/roles/{id}/permissions", s.rbac("platform.role.write", s.platformSetRolePermissions))
	// 审计日志
	mux.HandleFunc("GET /api/audit/logs", s.rbac("sec.audit.read", s.platformListAudit))
	mux.HandleFunc("GET /api/audit/stats", s.rbac("sec.audit.read", s.platformAuditStats))
	mux.HandleFunc("POST /api/audit/gc", s.rbac("sec.audit.export", s.platformAuditGC))
	mux.HandleFunc("POST /api/audit/clear", s.rbac("sec.audit.export", s.platformAuditClear))
	// 攻击模式库 / playbook
	mux.HandleFunc("GET /api/playbook/patterns", s.rbac("playbook.read", s.playbookListPatterns))
	mux.HandleFunc("POST /api/playbook/patterns", s.rbac("playbook.write", s.playbookCreatePattern))
	mux.HandleFunc("POST /api/playbook/reproduce", s.rbac("playbook.write", s.playbookReproduce))
	mux.HandleFunc("DELETE /api/playbook/patterns/{id}", s.rbac("playbook.write", s.playbookDeletePattern))
	mux.HandleFunc("POST /api/playbook/search", s.rbac("playbook.read", s.playbookSearch))
	mux.HandleFunc("GET /api/playbook/stats", s.rbac("playbook.read", s.playbookStats))
	// 批量任务队列
	mux.HandleFunc("GET /api/batch/queues", s.rbac("batch.read", s.batchListQueues))
	mux.HandleFunc("POST /api/batch/queues", s.rbac("batch.write", s.batchCreateQueue))
	mux.HandleFunc("PATCH /api/batch/queues/{id}", s.rbac("batch.write", s.batchUpdateQueue))
	mux.HandleFunc("DELETE /api/batch/queues/{id}", s.rbac("batch.write", s.batchDeleteQueue))
	mux.HandleFunc("POST /api/batch/queues/{id}/run", s.rbac("batch.write", s.batchRunQueue))
	mux.HandleFunc("GET /api/batch/queues/{id}/tasks", s.rbac("batch.read", s.batchListTasks))
	mux.HandleFunc("POST /api/batch/queues/{id}/tasks", s.rbac("batch.write", s.batchAddTask))
	mux.HandleFunc("DELETE /api/batch/tasks/{id}", s.rbac("batch.write", s.batchDeleteTask))

	// 沙箱管理（主机 / 容器 / 出口范围）
	mux.HandleFunc("GET /api/sandbox/hosts", s.rbac("sandbox.read", s.sandboxListHosts))
	mux.HandleFunc("POST /api/sandbox/hosts", s.rbac("sandbox.write", s.sandboxUpsertHost))
	mux.HandleFunc("DELETE /api/sandbox/hosts/{id}", s.rbac("sandbox.write", s.sandboxDeleteHost))
	mux.HandleFunc("POST /api/sandbox/hosts/{id}/ping", s.rbac("sandbox.read", s.sandboxPingHost))
	mux.HandleFunc("GET /api/sandbox/hosts/{id}/containers", s.rbac("sandbox.read", s.sandboxListContainers))
	mux.HandleFunc("GET /api/sandbox/hosts/{id}/images", s.rbac("sandbox.read", s.sandboxListImages))
	mux.HandleFunc("POST /api/sandbox/hosts/{id}/containers", s.rbac("sandbox.write", s.sandboxCreateContainer))
	mux.HandleFunc("POST /api/sandbox/hosts/{id}/containers/{cid}/{action}", s.rbac("sandbox.write", s.sandboxContainerAction))
	mux.HandleFunc("DELETE /api/sandbox/hosts/{id}/containers", s.rbac("sandbox.write", s.sandboxRemoveContainersBatch))
	mux.HandleFunc("GET /api/sandbox/egress", s.rbac("sandbox.read", s.sandboxListEgress))
	mux.HandleFunc("POST /api/sandbox/egress", s.rbac("sandbox.write", s.sandboxUpsertEgress))
	mux.HandleFunc("DELETE /api/sandbox/egress/{id}", s.rbac("sandbox.write", s.sandboxDeleteEgress))
	mux.HandleFunc("DELETE /api/sandbox/egress", s.rbac("sandbox.write", s.sandboxDeleteEgressBatch))

	// 工作流图引擎
	mux.HandleFunc("POST /api/workflows/validate", s.workflowValidate)
	mux.HandleFunc("POST /api/workflows/save", s.workflowSave)
	mux.HandleFunc("GET /api/workflows", s.workflowList)
	mux.HandleFunc("GET /api/workflows/{id}", s.workflowGet)
	mux.HandleFunc("PUT /api/workflows/{id}", s.workflowSave)
	mux.HandleFunc("DELETE /api/workflows/{id}", s.workflowDelete)
	mux.HandleFunc("POST /api/workflows/{id}/run", s.workflowRun)
	mux.HandleFunc("GET /api/workflows/{id}/runs", s.workflowRuns)
	mux.HandleFunc("GET /api/workflow-runs/{id}", s.workflowRunDetail)
	mux.HandleFunc("POST /api/workflows/dry-run", s.workflowDryRun)

	// 平台扩展：工作空间 / 知识库 / WebShell / C2
	mux.HandleFunc("GET /api/workspace/list", s.workspaceList)
	mux.HandleFunc("GET /api/workspace/read", s.workspaceRead)
	mux.HandleFunc("DELETE /api/workspace/delete", s.workspaceDelete)
	mux.HandleFunc("POST /api/workspace/clear", s.workspaceClear)
	mux.HandleFunc("GET /api/knowledge", s.knowledgeList)
	mux.HandleFunc("POST /api/knowledge", s.knowledgeSave)
	mux.HandleFunc("POST /api/knowledge/search", s.knowledgeSearch)
	mux.HandleFunc("DELETE /api/knowledge/{id}", s.knowledgeDelete)
	mux.HandleFunc("GET /api/webshell", s.webshellList)
	mux.HandleFunc("POST /api/webshell", s.webshellSave)
	mux.HandleFunc("DELETE /api/webshell/{id}", s.webshellDelete)
	mux.HandleFunc("DELETE /api/webshell", s.webshellDeleteBatch)
	mux.HandleFunc("POST /api/webshell/test", s.webshellTest)
	mux.HandleFunc("GET /api/c2", s.c2List)
	mux.HandleFunc("POST /api/c2/listeners", s.c2SaveListener)
	mux.HandleFunc("DELETE /api/c2/listeners/{id}", s.c2DeleteListener)
	mux.HandleFunc("DELETE /api/c2/listeners", s.c2DeleteListenersBatch)
	mux.HandleFunc("DELETE /api/c2/sessions", s.c2DeleteSessionsBatch)
	mux.HandleFunc("POST /api/c2/ingest", s.c2Ingest)
	mux.HandleFunc("POST /api/c2/status", s.c2SetStatus)

	// 能力：代理池
	mux.HandleFunc("GET /api/proxies", s.rbac("cap.proxy.read", s.proxyList))
	mux.HandleFunc("POST /api/proxies", s.rbac("cap.proxy.write", s.proxySave))
	mux.HandleFunc("DELETE /api/proxies/{id}", s.rbac("cap.proxy.write", s.proxyDelete))
	mux.HandleFunc("DELETE /api/proxies", s.rbac("cap.proxy.write", s.proxyDeleteBatch))
	mux.HandleFunc("POST /api/proxies/import", s.rbac("cap.proxy.write", s.proxyImport))
	mux.HandleFunc("POST /api/proxies/{id}/test", s.rbac("cap.proxy.write", s.proxyTest))
	mux.HandleFunc("POST /api/proxies/test-all", s.rbac("cap.proxy.write", s.proxyTestAll))
	mux.HandleFunc("POST /api/proxies/pick", s.rbac("cap.proxy.read", s.proxyPick))
	mux.HandleFunc("GET /api/proxy-sources", s.rbac("cap.proxy.read", s.proxySourceList))
	mux.HandleFunc("POST /api/proxy-sources", s.rbac("cap.proxy.write", s.proxySourceSave))
	mux.HandleFunc("DELETE /api/proxy-sources/{id}", s.rbac("cap.proxy.write", s.proxySourceDelete))
	mux.HandleFunc("POST /api/proxy-sources/{id}/refresh", s.rbac("cap.proxy.write", s.proxySourceRefresh))

	// 空间测绘：FOFA / Hunter / Quake 搜索与资产导入
	mux.HandleFunc("GET /api/spacesearch/config", s.rbac("cap.spacesearch.read", s.spaceSearchConfigGet))
	mux.HandleFunc("POST /api/spacesearch/config", s.rbac("cap.spacesearch.write", s.spaceSearchConfigSet))
	mux.HandleFunc("POST /api/spacesearch/search", s.rbac("cap.spacesearch.read", s.spaceSearch))
	mux.HandleFunc("POST /api/spacesearch/test", s.rbac("cap.spacesearch.read", s.spaceSearchTest))
	mux.HandleFunc("POST /api/spacesearch/import", s.rbac("cap.spacesearch.write", s.spaceSearchImport))

	// TSecBenchmark 跑分
	mux.HandleFunc("GET /api/benchmark/config", s.benchGetConfig)
	mux.HandleFunc("POST /api/benchmark/config", s.benchSetConfig)
	mux.HandleFunc("GET /api/benchmark/vpn", s.benchVPNCheck)
	mux.HandleFunc("GET /api/benchmark/challenges", s.benchChallenges)
	mux.HandleFunc("POST /api/benchmark/start", s.benchStart)
	mux.HandleFunc("GET /api/benchmark/hint", s.benchHint)
	mux.HandleFunc("POST /api/benchmark/submit", s.benchSubmit)
	mux.HandleFunc("POST /api/benchmark/close", s.benchClose)

	// /api/* goes through CORS + JWT; everything else is served by the embedded
	// frontend (public — auth is enforced client-side and on the API). With the
	// no-embed build the webui handler just 404s (run `next dev` separately).
	api := requestID(cors(s.requireAuth(s.authorizeAPI(mux))))
	root := http.NewServeMux()
	root.Handle("/api/", api)
	root.Handle("/", s.webuiHandler())
	return root
}

// --- handlers ---

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "service": "restxtra"})
}

// metrics 返回进程级关键路径计数器（P5.4）。
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, metrics.M.Snapshot())
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}
	out["engine_mode"] = "idle"              // spec enum; overridden below when a task is active
	out["llm_configured"] = s.engine.Ready() // is an LLM provider installed at all
	if tr := s.m.Traffic(); tr != nil {
		c, _ := tr.Count()
		out["traffic"] = c
		out["traffic_enabled"] = true
	}
	if counts, err := s.m.Assets().CountsByType(); err == nil {
		total := 0
		for _, n := range counts {
			total += n
		}
		out["assets"] = total
		out["asset_counts"] = counts
	}

	// resolve the task: explicit ?task=<id> binds to that task (so a detail view
	// never silently follows a globally-changed active task); empty = active.
	taskParam := r.URL.Query().Get("task")
	t := s.m.ResolveTask(taskParam)
	if t == nil {
		if taskParam != "" && taskParam != "active" {
			writeErr(w, 404, "task not found")
			return
		}
		writeJSON(w, 200, out) // no task selected: only global fields
		return
	}

	st, _ := t.Store.Stats()
	out["exploration"] = st

	// per-task running state + heartbeat (distinct from "LLM configured").
	intents, _ := t.Store.ListByKind(db.KindIntent, 100000)
	inFlight := 0
	for _, in := range intents {
		if in.State == "running" {
			inFlight++
		}
	}
	goals, _ := t.Store.ListByKind(db.KindGoal, 10000)
	goalsMet := 0
	for _, g := range goals {
		if g.State == "met" {
			goalsMet++
		}
	}
	last := s.engine.LastActivity(t.ID)
	paused := s.engine.IsPaused(t.ID)
	running := s.engine.Ready() && s.engine.Started(t.ID) && !paused
	// stalled: running but no activity for a while and nothing in flight.
	stalled := running && inFlight == 0 && last > 0 && time.Now().Unix()-last > 60

	// engine_mode follows the spec enum (exploring|paused|stalled|idle).
	switch {
	case paused:
		out["engine_mode"] = "paused"
	case stalled:
		out["engine_mode"] = "stalled"
	case running:
		out["engine_mode"] = "exploring"
	default:
		out["engine_mode"] = "idle"
	}

	out["active_task"] = map[string]any{
		"id": t.ID, "description": t.Description, "goal": t.Goal,
		"running":       running,
		"paused":        paused,
		"in_flight":     inFlight,
		"last_activity": last,
		"stalled":       stalled,
		"goals_total":   len(goals),
		"goals_met":     goalsMet,
	}
	writeJSON(w, 200, out)
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	active := ""
	if t := s.m.ActiveTask(); t != nil {
		active = t.ID
	}
	companyID := int64(atoiDefault(r.URL.Query().Get("company_id"), 0))
	list := s.m.List()
	toks, _ := s.m.PG().TokenTotalsAll()      // whole-task token totals, one query for all tasks
	lastAct, _ := s.m.PG().LastActivityAll()  // persisted last-activity per task, one query
	goalCounts, _ := s.m.PG().GoalCountsAll() // goal progress per exploration, one query
	dtos := make([]TaskDTO, 0, len(list))
	for _, t := range list {
		// 按企业过滤（company_id>0）：任务直接关联该企业。
		if companyID > 0 && !taskHasCompany(t.Companies, companyID) {
			continue
		}
		status := "created"
		switch {
		case isTerminalStatus(t.Status): // 持久化终态优先（done/failed/timeout）
			status = t.Status
		case t.Paused || s.engine.IsPaused(t.ID):
			status = "paused"
		case s.engine.Ready() && s.engine.Started(t.ID):
			status = "running"
		}
		dto := taskDTO(t, status)
		dto.Tokens = tokenTotalDTO(toks[t.ExpID])
		// prefer the live in-memory heartbeat (fresher) and fall back to the
		// persisted max activity time (survives restarts) for run-duration display.
		dto.LastActivity = lastAct[t.ExpID]
		if live := s.engine.LastActivity(t.ID); live > dto.LastActivity {
			dto.LastActivity = live
		}
		gc := goalCounts[t.ExpID]
		dto.GoalsTotal = gc.Total
		dto.GoalsMet = gc.Met
		dtos = append(dtos, dto)
	}
	writeJSON(w, 200, map[string]any{"tasks": dtos, "active": active})
}

// taskHasCompany reports whether a task's company set includes the given id.
func taskHasCompany(cs []db.CompanyRef, id int64) bool {
	for _, c := range cs {
		if c.ID == id {
			return true
		}
	}
	return false
}

func (s *Server) setActive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if !s.m.SetActive(req.ID) {
		writeErr(w, 404, "task not found")
		return
	}
	// resume the engine for the opened task (idempotent — no-op if already running).
	if t, ok := s.m.Task(req.ID); ok {
		s.engine.Run(s.ctx, t)
	}
	writeJSON(w, 200, map[string]any{"active": req.ID})
}

// control pauses/resumes a task's autonomous execution (planner + workers).
func (s *Server) control(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	switch req.Action {
	case "pause":
		s.engine.Pause(t.ID)
	case "resume":
		s.engine.Run(s.ctx, t) // ensure loops are alive, then un-pause + nudge
		s.engine.Resume(t)
	default:
		writeErr(w, 400, "action must be pause|resume")
		return
	}
	paused := s.engine.IsPaused(t.ID)
	t.Paused = paused                                       // keep the in-memory task (shown in /api/tasks) in sync
	if err := s.m.SetTaskPaused(t.ID, paused); err != nil { // persist (survives restart)
		log.Printf("[control] persist paused %s: %v", t.ID, err)
	}
	log.Printf("[task] #%s %s", t.ID, map[bool]string{true: "已暂停", false: "已恢复"}[paused])
	writeJSON(w, 200, map[string]any{"id": t.ID, "paused": paused})
}

// getLLM returns the current LLM config (key never exposed).
func (s *Server) getLLM(w http.ResponseWriter, r *http.Request) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	writeJSON(w, 200, map[string]any{
		"configured":       s.llmOn,
		"provider":         s.llmCfg.Provider(),
		"model":            s.llmCfg.Model,
		"base_url":         s.llmCfg.BaseURL,
		"proxy":            s.llmCfg.Proxy,
		"key_set":          s.llmCfg.APIKey != "",
		"rate_per_second":  s.llmCfg.RatePerSecond,
		"rate_per_minute":  s.llmCfg.RatePerMinute,
		"context_window_k": s.llmCfg.ContextWindowK,
		"reasoning_effort": s.llmCfg.ReasoningEffort,
		"auth_mode":        s.llmCfg.AuthMode,
	})
}

// setLLM configures the LLM at runtime. A blank api_key keeps the existing key.
func (s *Server) setLLM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider        string  `json:"provider"`
		Model           string  `json:"model"`
		BaseURL         string  `json:"base_url"`
		Proxy           string  `json:"proxy"`
		APIKey          string  `json:"api_key"`
		RatePerSecond   float64 `json:"rate_per_second"`
		RatePerMinute   float64 `json:"rate_per_minute"`
		ContextWindowK  int     `json:"context_window_k"`
		ReasoningEffort string  `json:"reasoning_effort"`
		AuthMode        string  `json:"auth_mode"` // ""|x-api-key|bearer
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	cfg := agent.ConfigFrom(req.Provider, req.Model, req.BaseURL, req.APIKey, req.Proxy)
	cfg.RatePerSecond, cfg.RatePerMinute = req.RatePerSecond, req.RatePerMinute
	cfg.ReasoningEffort = req.ReasoningEffort
	cfg.AuthMode = req.AuthMode
	if k := req.ContextWindowK; k > 0 { // 0 = keep default (200K); cap at 1M
		if k > 1000 {
			k = 1000
		}
		cfg.ContextWindowK = k
	}
	if cfg.APIKey == "" {
		s.cfgMu.Lock()
		cfg.APIKey = s.llmCfg.APIKey // keep existing key if not re-entered
		s.cfgMu.Unlock()
	}
	if cfg.APIKey == "" {
		writeErr(w, 400, "api_key required")
		return
	}
	if err := s.applyLLM(cfg); err != nil {
		writeErr(w, 400, "provider init failed: "+err.Error())
		return
	}
	s.saveLLMConfig(cfg) // persist to DB
	log.Printf("[engine] LLM configured via UI: %s / %s", cfg.Provider(), cfg.Model)
	s.getLLM(w, r)
}

// testLLM makes a real minimal completion to verify the config works.
func (s *Server) testLLM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider        string `json:"provider"`
		Model           string `json:"model"`
		BaseURL         string `json:"base_url"`
		Proxy           string `json:"proxy"`
		APIKey          string `json:"api_key"`
		ReasoningEffort string `json:"reasoning_effort"`
		AuthMode        string `json:"auth_mode"`  // ""|x-api-key|bearer
		ProfileID       *int64 `json:"profile_id"` // 测已存 profile 时传入：api_key 为空则用它存的 key
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	cfg := agent.ConfigFrom(req.Provider, req.Model, req.BaseURL, req.APIKey, req.Proxy)
	// mirror production: send the SAME thinking params so a provider that rejects the
	// reasoning_effort/thinking field fails the test too (no false "test ok, run 400").
	cfg.ReasoningEffort = req.ReasoningEffort
	// 认证头与已存 profile 一致（profile 存的 auth_mode 优先于表单，因表单可能未选）。
	if req.AuthMode == "" && req.ProfileID != nil {
		if p, err := s.m.pg.ProfileByID(*req.ProfileID); err == nil && p != nil {
			req.AuthMode = p.AuthMode
		}
	}
	cfg.AuthMode = req.AuthMode
	// API Key 解析优先级：表单输入 > 指定 profile 存的 key > 全局配置的 key。
	// 已存 profile 的 key 不回传浏览器，所以测试已存配置时表单为空，需从 DB 取。
	if cfg.APIKey == "" && req.ProfileID != nil {
		if p, err := s.m.pg.ProfileByID(*req.ProfileID); err == nil && p != nil {
			cfg.APIKey = p.APIKey
		}
	}
	if cfg.APIKey == "" {
		s.cfgMu.Lock()
		cfg.APIKey = s.llmCfg.APIKey
		s.cfgMu.Unlock()
	}
	if cfg.APIKey == "" {
		writeJSON(w, 200, map[string]any{"ok": false, "error": "未提供 API Key"})
		return
	}
	lat, err := agent.TestConnection(r.Context(), cfg)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "latency_ms": lat.Milliseconds(), "model": cfg.Model})
}

// TaskWorkflow is the optional user-authored "工作流" attached to a task: a
// curated list of initial exploration directions (intents) + strategic hints.
// 工作流不是写死的步骤编排，而是给引擎一个聚焦的
// 起点：steps 作为初始 intent 预填进 frontier（优先级递减、按顺序领取），
// hints 作为战略提示供 planner 首轮读取。随后引擎照常自治推进。
type TaskWorkflow struct {
	Steps []TaskWorkflowStep `json:"steps,omitempty"`
	Hints []string           `json:"hints,omitempty"`
}

type TaskWorkflowStep struct {
	Summary  string `json:"summary"`
	Priority *int   `json:"priority,omitempty"` // 0/缺省 = 按步骤序号自动递减(100-i)
	Agent    string `json:"agent,omitempty"`    // 指定执行 agent key；""=默认 worker
	Kind     string `json:"kind,omitempty"`     // "step"|"decision"；decision=判断节点
}

type createTaskReq struct {
	Description    string `json:"description"`
	Goal           string `json:"goal"`
	LLMProfileID   *int64 `json:"llm_profile_id,omitempty"` // 指定运行本任务的 LLM 配置;省略/null=用激活配置
	TimeoutSeconds int    `json:"timeout_seconds"`          // 任务级超时(秒);0/省略=不限时
	// PlanHeartbeatSeconds 是 planner 心跳触发间隔(秒;0/省略=不心跳, <600 归一 600)。
	PlanHeartbeatSeconds int           `json:"plan_heartbeat_seconds"`
	Workflow             *TaskWorkflow `json:"workflow,omitempty"`    // 可选：初始探索方向 + 战略提示
	CompanyIDs           []int64       `json:"company_ids,omitempty"` // 可选：企业归属(第一个=主企业)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Description) == "" {
		req.Description = "未命名任务"
	}
	// validate the pinned profile up front (must exist + be usable), so a bad id fails
	// task creation instead of silently falling back to the active profile at run time.
	if req.LLMProfileID != nil {
		if _, ok := s.loadProfileConfig(*req.LLMProfileID); !ok {
			writeErr(w, 400, "所选 LLM 配置不存在或未设置 API Key")
			return
		}
	}
	if req.TimeoutSeconds < 0 {
		req.TimeoutSeconds = 0
	}
	t, err := s.m.CreateTask(req.Description, req.Goal, req.LLMProfileID, req.TimeoutSeconds, req.PlanHeartbeatSeconds, req.CompanyIDs)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	log.Printf("[task] 新建任务 #%s «%s» 目标: %s", t.ID, req.Description, req.Goal)
	// seed initial asset(s) from goal/description so the event-driven loop has a root.
	s.seed(t, req.Description+" "+req.Goal)
	// Return immediately — goal decomposition is async so the UI isn't blocked.
	writeJSON(w, 201, t)
	// Background: emit round-0 marker, decompose goals via LLM, then start engine.
	// Engine starts only after goals are seeded to avoid a race where the planner
	// fires before any goal nodes exist.
	go func() {
		s.engine.emitActivity(t, db.Activity{Worker: "planner", Kind: "round",
			Summary: "第 0 轮目标拆解"})
		goals := s.createGoals(s.ctx, t, func(r db.Activity) {
			s.engine.emitActivity(t, r)
		})
		for _, g := range goals {
			summary := g.Text
			if g.VulnClass != "" {
				summary = fmt.Sprintf("[%s] %s", g.VulnClass, g.Text)
			}
			s.engine.emitActivity(t, db.Activity{Worker: "planner", Kind: "text", Summary: summary})
		}
		s.seedWorkflow(t, req.Workflow) // 预填初始探索方向 + 提示，再启动引擎
		s.engine.Run(s.ctx, t)
	}()
}

// seedWorkflow materializes a user-authored workflow into the exploration graph:
// each step becomes an open intent on the frontier (priority descending → claimed
// in workflow order), each hint becomes a hint node the planner reads on round one.
func (s *Server) seedWorkflow(t *Task, wf *TaskWorkflow) {
	if wf == nil {
		return
	}
	origin, _ := t.Store.OriginFactID()
	if len(wf.Steps) > 0 {
		log.Printf("[workflow] task %s: 预填 %d 个初始探索方向", t.ID, len(wf.Steps))
	}
	for i, st := range wf.Steps {
		summary := strings.TrimSpace(st.Summary)
		if summary == "" {
			continue
		}
		prio := 100 - i // 步骤 1 最高优先级，按顺序领取
		if st.Priority != nil && *st.Priority > 0 {
			prio = *st.Priority
		}
		payload := map[string]any{"summary": summary, "workflow_step": i + 1}
		if strings.TrimSpace(st.Agent) != "" {
			payload["agent"] = strings.TrimSpace(st.Agent)
		}
		if strings.TrimSpace(st.Kind) != "" {
			payload["kind"] = strings.TrimSpace(st.Kind)
		}
		id, err := t.Store.AddIntent(payload, prio, nil, "human")
		if err != nil {
			log.Printf("[workflow] task %s: 播种步骤 %d 失败: %v", t.ID, i+1, err)
			continue
		}
		if origin > 0 && id > 0 {
			_ = t.Store.Link(origin, db.RelSpawns, id)
		}
		s.engine.emitActivity(t, db.Activity{Worker: "planner", Kind: "intent", Summary: summary})
	}
	for _, h := range wf.Hints {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if _, err := t.Store.AddNode(db.KindHint, map[string]any{"text": h}, 0, "active", "human", nil); err != nil {
			log.Printf("[workflow] task %s: 播种提示失败: %v", t.ID, err)
			continue
		}
		s.engine.emitActivity(t, db.Activity{Worker: "planner", Kind: "hint", Summary: h})
	}
	t.Notify()
}

var (
	reURL    = regexp.MustCompile(`https?://[^\s'"]+`)
	reIPPort = regexp.MustCompile(`\b((?:\d{1,3}\.){3}\d{1,3})(?::(\d{1,5}))?`)
	reDomain = regexp.MustCompile(`\b((?:[a-zA-Z0-9-]+\.)+[a-zA-Z]{2,})(?::(\d{1,5}))?`)
)

// parseTarget extracts a target (scheme, host, port) from free text, supporting
// full URLs, IP[:port] and domain[:port]. ok=false when nothing parseable.
func parseTarget(text string) (scheme, host string, port int, ok bool) {
	text = strings.TrimSpace(text)
	if m := reURL.FindString(text); m != "" {
		if u, err := url.Parse(m); err == nil && u.Hostname() != "" {
			scheme = strings.ToLower(u.Scheme)
			host = strings.ToLower(u.Hostname())
			port = portOr(u.Port(), defaultPort(scheme))
			return scheme, host, port, true
		}
	}
	if m := reIPPort.FindStringSubmatch(text); m != nil {
		host = m[1]
		port = portOr(m[2], 80)
		return schemeForPort(port), host, port, true
	}
	if m := reDomain.FindStringSubmatch(text); m != nil {
		host = strings.ToLower(m[1])
		if m[2] == "" {
			return "https", host, 443, true
		}
		port = portOr(m[2], 443)
		return schemeForPort(port), host, port, true
	}
	return "", "", 0, false
}

func portOr(s string, d int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return d
}
func defaultPort(scheme string) int {
	if scheme == "http" {
		return 80
	}
	return 443
}
func schemeForPort(p int) string {
	if p == 443 || p == 8443 {
		return "https"
	}
	return "http"
}

// llmHost returns the host of the configured LLM endpoint (to keep it out of scope).
func (s *Server) llmHost() string {
	s.cfgMu.Lock()
	base := s.llmCfg.BaseURL
	s.cfgMu.Unlock()
	if base == "" {
		return ""
	}
	if u, err := url.Parse(base); err == nil {
		return strings.ToLower(u.Hostname())
	}
	return ""
}

func (s *Server) seed(t *Task, text string) {
	scheme, host, port, ok := parseTarget(text)
	if !ok {
		log.Printf("[seed] task %s: 未能从 %q 解析出目标 host/IP，不创建站点（请手动配置 scope）", t.ID, text)
		t.Notify()
		return
	}
	// P0-1 guard: never treat the configured LLM gateway as a target.
	if gw := s.llmHost(); gw != "" && host == gw {
		log.Printf("[seed] task %s: 目标 %q 是 LLM 网关，拒绝作为渗透目标", t.ID, host)
		t.Notify()
		return
	}

	u := scheme + "://" + host
	if !(scheme == "https" && port == 443) && !(scheme == "http" && port == 80) {
		u += ":" + strconv.Itoa(port)
	}
	var rootID int64
	if as := s.m.Assets(); as != nil {
		if net.ParseIP(host) != nil {
			rootID, _ = as.UpsertIP(db.UpsertIPReq{IP: host})
		} else if scheme == "https" || scheme == "http" {
			rootID, _ = as.UpsertHTTPService(db.UpsertHTTPServiceReq{URL: u})
		} else {
			rootID, _ = as.UpsertRootDomain(db.UpsertRootDomainReq{Domain: host})
		}
	}
	// anchor the seeded assets to this task's begin root as lineage/provenance
	// (the asset graph is global and shared; anchoring no longer gates reads).
	if rootID > 0 {
		if begin, _ := t.Store.OriginFactID(); begin > 0 {
			_ = t.Store.Anchor(begin, rootID)
		}
	}
	log.Printf("[seed] task %s: 目标站点 %s", t.ID, u)
	t.Notify()
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) frontier(w http.ResponseWriter, r *http.Request) {
	t := s.m.ResolveTask(r.URL.Query().Get("task"))
	if t == nil {
		writeJSON(w, 200, []any{})
		return
	}
	fr, _ := t.Store.Frontier(atoiDefault(r.URL.Query().Get("limit"), 100))
	writeJSON(w, 200, taskNodeDTOs(fr))
}

func (s *Server) findings(w http.ResponseWriter, r *http.Request) {
	// 无 task 参数 → 全局「发现」页：从独立 findings 表读取（任务删除后 finding 依然保留）。
	// 带 task 参数 → 仅该任务（任务概览/发现 Tab 用），从 exploration_nodes 读（任务在则节点在）。
	taskParam := r.URL.Query().Get("task")
	companyID := int64(atoiDefault(r.URL.Query().Get("company_id"), 0))
	if taskParam == "" {
		pageParam := r.URL.Query().Get("page")
		limitParam := r.URL.Query().Get("limit")
		if pageParam != "" || limitParam != "" {
			filter := db.FindingFilter{
				Severity: r.URL.Query().Get("severity"), VulnClass: r.URL.Query().Get("vulnclass"),
				Status: r.URL.Query().Get("status"), TaskID: r.URL.Query().Get("task_id"),
				CompanyID: companyID, Sort: r.URL.Query().Get("sort"),
			}
			page := atoiDefault(pageParam, 1)
			limit := atoiDefault(limitParam, 20)
			fs, total, err := s.m.pg.ListFindingsPage(filter, page, limit)
			if err != nil {
				log.Printf("[findings] list page: %v", err)
				writeErr(w, http.StatusInternalServerError, "加载漏洞发现失败")
				return
			}
			items := make([]FindingDTO, 0, len(fs))
			for _, finding := range fs {
				items = append(items, findingFromDB(finding))
			}
			writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "limit": limit})
			return
		}
		fs, err := s.m.pg.ListFindings(500, companyID)
		if err != nil {
			log.Printf("[findings] list global: %v", err)
			writeErr(w, http.StatusInternalServerError, "加载漏洞发现失败")
			return
		}
		out := make([]FindingDTO, 0, len(fs))
		for _, f := range fs {
			out = append(out, findingFromDB(f))
		}
		writeJSON(w, 200, out)
		return
	}
	t := s.m.ResolveTask(taskParam)
	if t == nil {
		writeJSON(w, 200, []any{})
		return
	}
	findings, _, err := s.m.pg.ListFindingsPage(db.FindingFilter{TaskID: taskParam, Sort: "severity"}, 1, 200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载任务漏洞失败")
		return
	}
	out := make([]FindingDTO, 0, len(findings))
	for _, finding := range findings {
		out = append(out, findingFromDB(finding))
	}
	writeJSON(w, 200, out)
}

// taskCoverageGraph returns the task-scoped asset graph used by the coverage
// tab. It is deliberately a compact aggregate; detailed Finding evidence stays
// behind the Finding detail page.
func (s *Server) taskCoverageGraph(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效的任务 ID")
		return
	}
	if s.m.ResolveTask(strconv.FormatInt(id, 10)) == nil {
		writeErr(w, http.StatusNotFound, "任务不存在")
		return
	}
	graph, err := s.m.pg.Assets().CoverageGraph(id)
	if err != nil {
		log.Printf("[coverage] task %d: %v", id, err)
		writeErr(w, http.StatusInternalServerError, "加载资产覆盖图失败")
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

func (s *Server) findingDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效的漏洞 ID")
		return
	}
	finding, err := s.m.pg.GetFinding(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载漏洞详情失败")
		return
	}
	if finding == nil {
		writeErr(w, http.StatusNotFound, "漏洞不存在")
		return
	}
	writeJSON(w, 200, findingFromDB(finding))
}

func (s *Server) findingStats(w http.ResponseWriter, r *http.Request) {
	companyID := int64(atoiDefault(r.URL.Query().Get("company_id"), 0))
	stats, err := s.m.pg.FindingStats(companyID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载漏洞统计失败")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) patchFinding(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效的漏洞 ID")
		return
	}
	var body struct {
		Status *string `json:"status"`
		Report *string `json:"report"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if body.Status == nil && body.Report == nil {
		writeErr(w, http.StatusBadRequest, "没有可更新的字段")
		return
	}
	if body.Status != nil {
		if !db.ValidFindingStatus(*body.Status) {
			writeErr(w, http.StatusBadRequest, "无效的处理状态")
			return
		}
		if affected, err := s.m.pg.SetFindingStatus(id, *body.Status); err != nil {
			writeErr(w, http.StatusInternalServerError, "更新处理状态失败")
			return
		} else if affected == 0 {
			writeErr(w, http.StatusNotFound, "漏洞不存在")
			return
		}
	}
	if body.Report != nil {
		if affected, err := s.m.pg.SetFindingReport(id, *body.Report); err != nil {
			writeErr(w, http.StatusInternalServerError, "更新详细报告失败")
			return
		} else if affected == 0 {
			writeErr(w, http.StatusNotFound, "漏洞不存在")
			return
		}
	}
	finding, err := s.m.pg.GetFinding(id)
	if err != nil || finding == nil {
		writeErr(w, http.StatusInternalServerError, "重新加载漏洞失败")
		return
	}
	s.recordAudit(r, "finding", "update", "success", fmt.Sprintf("更新漏洞 #%d", id))
	writeJSON(w, 200, findingFromDB(finding))
}

func (s *Server) findingLineage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效的漏洞 ID")
		return
	}
	finding, err := s.m.pg.GetFinding(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载漏洞链路失败")
		return
	}
	empty := map[string]any{"nodes": []any{}, "edges": []any{}}
	if finding == nil {
		writeErr(w, http.StatusNotFound, "漏洞不存在")
		return
	}
	if finding.TaskID == nil || finding.NodeID == nil {
		writeJSON(w, 200, empty)
		return
	}
	task := s.m.ResolveTask(i64s(*finding.TaskID))
	if task == nil {
		writeJSON(w, 200, empty)
		return
	}
	nodes, edges, err := task.Store.FindingLineage(*finding.NodeID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "加载漏洞链路失败")
		return
	}
	writeJSON(w, 200, map[string]any{"nodes": taskNodeDTOs(nodes), "edges": edgeDTOs(edges)})
}

func (s *Server) intents(w http.ResponseWriter, r *http.Request) {
	t := s.m.ResolveTask(r.URL.Query().Get("task"))
	if t == nil {
		writeJSON(w, 200, []any{})
		return
	}
	in, _ := t.Store.ListByKind(db.KindIntent, 300)
	writeJSON(w, 200, taskNodeDTOs(in))
}

// explorationGraph returns the whole exploration chain (task graph) as nodes+edges.
func (s *Server) explorationGraph(w http.ResponseWriter, r *http.Request) {
	t := s.m.ResolveTask(r.URL.Query().Get("task"))
	if t == nil {
		writeJSON(w, 200, map[string]any{"nodes": []any{}, "edges": []any{}})
		return
	}
	nodes, _ := t.Store.Nodes(2000)
	edges, _ := t.Store.Edges(5000)
	writeJSON(w, 200, map[string]any{"nodes": taskNodeDTOs(nodes), "edges": edgeDTOs(edges)})
}

// activity returns the worker execution step log (incremental via ?since=seq).
func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	t := s.m.ResolveTask(r.URL.Query().Get("task"))
	if t == nil {
		writeJSON(w, 200, map[string]any{"items": []any{}, "cursor": 0})
		return
	}
	since := int64(atoiDefault(r.URL.Query().Get("since"), 0))
	limit := atoiDefault(r.URL.Query().Get("limit"), 300)
	var intentPtr *int64
	if iv := r.URL.Query().Get("intent"); iv != "" {
		if n, err := strconv.ParseInt(iv, 10, 64); err == nil {
			intentPtr = &n
		}
	}
	items, cursor, err := t.Store.ActivityList(intentPtr, since, limit)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": activityDTOs(items), "cursor": cursor})
}

// activityByCompany returns recent activity across tasks of one company
// (?company_id=0/omitted = all tasks). Used by the dashboard when a company is
// selected so the 活动流 reflects that company only.
func (s *Server) activityByCompany(w http.ResponseWriter, r *http.Request) {
	companyID := int64(atoiDefault(r.URL.Query().Get("company_id"), 0))
	limit := atoiDefault(r.URL.Query().Get("limit"), 60)
	rows, err := s.m.pg.ActivityAllByCompany(companyID, limit)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	items := make([]ActivityDTO, 0, len(rows))
	for _, rw := range rows {
		d := activityDTO(rw.Activity)
		d.TaskID = i64s(rw.ExplorationID) // the owning exploration id doubles as task id for display
		items = append(items, d)
	}
	writeJSON(w, 200, map[string]any{"items": items, "cursor": 0})
}

// tokenStats returns per-worker token usage (input/output/cache read/write) for a
// task — main agent, planner, and each work#N.
func (s *Server) tokenStats(w http.ResponseWriter, r *http.Request) {
	t := s.m.ResolveTask(r.URL.Query().Get("task"))
	if t == nil {
		writeJSON(w, 200, map[string]any{"workers": []any{}})
		return
	}
	stats, err := t.Store.TokenStatsByWorker()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	total, _ := t.Store.TokenTotal() // whole-task total (all agents)
	writeJSON(w, 200, map[string]any{"workers": stats, "total": tokenTotalDTO(total)})
}

// tokenDailyStats returns global token consumption aggregated by calendar day
// (UTC) across all tasks for the past ?days=N days (default 30).
func (s *Server) tokenDailyStats(w http.ResponseWriter, r *http.Request) {
	days := atoiDefault(r.URL.Query().Get("days"), 30)
	if s.m.pg == nil {
		writeJSON(w, 200, []any{})
		return
	}
	buckets, err := s.m.pg.TokenDailyAll(days)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if buckets == nil {
		buckets = []db.DailyTokenBucket{}
	}
	writeJSON(w, 200, buckets)
}

// conversationTokens returns per-conversation token summaries so the dashboard can
// merge chat (conversation) usage into its per-profile / daily token stats — which
// otherwise count only task (exploration) usage.
func (s *Server) conversationTokens(w http.ResponseWriter, r *http.Request) {
	if s.m.pg == nil {
		writeJSON(w, 200, []any{})
		return
	}
	rows, err := s.m.pg.ConversationTokenSummaries()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}

// streamActivity is the live SSE tail: it replays history after ?since=<seq>, then
// pushes each newly appended activity for the task. The seq cursor makes history +
// live join gap-free; on reconnect the client passes its last seq to catch any
// dropped events. Optional ?intent=<id> scopes the stream to one worker session.
// getLogs returns recent backend log lines (Seq > since), newest-last.
func (s *Server) getLogs(w http.ResponseWriter, r *http.Request) {
	since := int64(atoiDefault(r.URL.Query().Get("since"), 0))
	limit := atoiDefault(r.URL.Query().Get("limit"), 500)
	lines, cursor := logSink.recent(since, limit)
	writeJSON(w, 200, map[string]any{"items": lines, "cursor": cursor})
}

// clearLogs empties the persisted server_logs table and the in-memory ring.
func (s *Server) clearLogs(w http.ResponseWriter, r *http.Request) {
	var removed int64
	if s.m.pg != nil {
		n, err := s.m.pg.ClearServerLogs()
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		removed = n
	}
	logSink.clear()
	writeJSON(w, 200, map[string]any{"removed": removed})
}

// deleteLogs removes a set of persisted server_logs rows by db_id and drops the
// matching lines from the in-memory ring.
func (s *Server) deleteLogs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	var removed int64
	if s.m.pg != nil {
		n, err := s.m.pg.DeleteLogs(req.IDs)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		removed = n
	}
	if len(req.IDs) > 0 {
		ids := make(map[int64]bool, len(req.IDs))
		for _, id := range req.IDs {
			ids[id] = true
		}
		logSink.remove(ids)
	}
	writeJSON(w, 200, map[string]any{"deleted": removed})
}

// getLogsHistory returns older log lines from the DB (before a given db_id).
// GET /api/logs/history?before=<db_id>&limit=200
// Returns {items:[LogLine], has_more: bool}.
func (s *Server) getLogsHistory(w http.ResponseWriter, r *http.Request) {
	if s.m.pg == nil {
		writeJSON(w, 200, map[string]any{"items": []any{}, "has_more": false})
		return
	}
	before := int64(atoiDefault(r.URL.Query().Get("before"), 0))
	limit := atoiDefault(r.URL.Query().Get("limit"), 200)
	if limit > 500 {
		limit = 500
	}
	// If no before given, return the most recent DB rows (mirrors ring restore).
	var (
		rows []*db.DBLog
		err  error
	)
	if before <= 0 {
		rows, err = s.m.pg.RecentLogs(limit)
	} else {
		rows, err = s.m.pg.ListLogsBefore(before, limit)
	}
	if err != nil {
		writeErr(w, 500, "db: "+err.Error())
		return
	}
	items := make([]LogLine, 0, len(rows))
	for _, r := range rows {
		items = append(items, LogLine{
			DBID:  r.ID,
			TS:    r.CreatedAt.Format(time.RFC3339),
			Level: r.Level,
			Tag:   r.Tag,
			Text:  r.Text,
		})
	}
	writeJSON(w, 200, map[string]any{"items": items, "has_more": len(rows) == limit})
}

// streamLogs is the live SSE tail of the backend log: replays history after
// ?since=<seq>, then pushes each new line.
func (s *Server) streamLogs(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}
	since := int64(atoiDefault(r.URL.Query().Get("since"), 0))
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := logSink.subscribe()
	defer unsub()
	send := func(l LogLine) {
		b, _ := json.Marshal(l)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
	lines, cursor := logSink.recent(since, 1000)
	for _, l := range lines {
		send(l)
	}
	if cursor > since {
		since = cursor
	}
	flusher.Flush()

	ctx := r.Context()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case l, ok := <-ch:
			if !ok {
				return
			}
			if l.Seq <= since {
				continue
			}
			since = l.Seq
			send(l)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) streamActivity(w http.ResponseWriter, r *http.Request) {
	t := s.m.ResolveTask(r.URL.Query().Get("task"))
	if t == nil {
		writeErr(w, 404, "task not found")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}
	var intentPtr *int64
	if iv := r.URL.Query().Get("intent"); iv != "" {
		if n, err := strconv.ParseInt(iv, 10, 64); err == nil {
			intentPtr = &n
		}
	}
	since := int64(atoiDefault(r.URL.Query().Get("since"), 0))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering

	// Subscribe BEFORE replaying history so events in between aren't lost; dedup the
	// overlap by skipping channel events whose id was already replayed.
	ch, unsub := s.engine.Broadcaster().Subscribe(t.ID)
	defer unsub()

	sendSSE := func(a db.Activity) {
		b, _ := json.Marshal(activityDTO(a))
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	items, cursor, _ := t.Store.ActivityList(intentPtr, since, 2000)
	for _, a := range items {
		sendSSE(a)
	}
	if cursor > since {
		since = cursor
	}
	flusher.Flush()

	ctx := r.Context()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case a, ok := <-ch:
			if !ok {
				return
			}
			if a.ID <= since {
				continue // already replayed
			}
			if intentPtr != nil && (a.NodeID == nil || *a.NodeID != *intentPtr) {
				continue // scoped session: only this intent's steps
			}
			since = a.ID
			sendSSE(a)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// activityDetail lazily returns the full detail blob for one step.
func (s *Server) activityDetail(w http.ResponseWriter, r *http.Request) {
	t := s.m.ResolveTask(r.URL.Query().Get("task"))
	if t == nil {
		writeErr(w, 404, "no task")
		return
	}
	seq, _ := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	d, err := t.Store.ActivityDetail(seq)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"detail": d})
}

func (s *Server) getTraffic(w http.ResponseWriter, r *http.Request) {
	tr := s.m.Traffic()
	if tr == nil {
		writeJSON(w, 200, map[string]any{"enabled": false, "exchanges": []any{}})
		return
	}
	q := r.URL.Query()
	page := atoiDefault(q.Get("page"), 0)
	size := atoiDefault(q.Get("size"), 100)
	ex, matched, _ := tr.Page(q.Get("host"), q.Get("method"), q.Get("q"), page, size)
	count, _ := tr.Count() // global total, for the stat card
	writeJSON(w, 200, map[string]any{
		"enabled":   s.m.TrafficEnabled(), // reflect the capture toggle
		"proxy":     s.m.ProxyAddr(),
		"count":     count,   // total recorded (unfiltered)
		"total":     matched, // rows matching the current filter (for pagination)
		"page":      page,
		"size":      size,
		"exchanges": trafficDTOs(ex),
	})
}

// clearTraffic empties all recorded HTTP traffic (tool executions).
func (s *Server) clearTraffic(w http.ResponseWriter, r *http.Request) {
	tr := s.m.Traffic()
	if tr == nil {
		writeJSON(w, 200, map[string]any{"removed": 0})
		return
	}
	n, err := tr.Clear()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"removed": n})
}

// deleteTraffic removes a set of recorded exchanges by id.
func (s *Server) deleteTraffic(w http.ResponseWriter, r *http.Request) {
	tr := s.m.Traffic()
	if tr == nil {
		writeJSON(w, 200, map[string]any{"deleted": 0})
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	n, err := tr.Delete(req.IDs)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": n})
}

// getTrafficExchange returns the full raw request/response of one exchange,
// read on demand from the traffic tree (bodies are not in the paged list).
func (s *Server) getTrafficExchange(w http.ResponseWriter, r *http.Request) {
	tr := s.m.Traffic()
	if tr == nil {
		writeErr(w, 404, "traffic disabled")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, 400, "missing id")
		return
	}
	req, resp, err := tr.Get(id)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"req": req, "resp": resp})
}

// getSettings returns the runtime app settings the UI toggles. The Brave API key
// is returned as a boolean presence flag (brave_key_set), never the value itself,
// so the UI can show "configured" without echoing the secret back.
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.settingsPayload())
}

func (s *Server) settingsPayload() map[string]any {
	on, backend, braveKey, tavilyKey, proxy := s.m.WebSearch()
	pyStored, _, _ := s.m.pg.GetSetting(settingPythonInterp)
	return map[string]any{
		"traffic_capture":    s.m.TrafficEnabled(),
		"llm_record":         s.m.LLMRecordEnabled(),
		"web_search_enabled": on,
		"web_search_backend": backend,
		"brave_key_set":      strings.TrimSpace(braveKey) != "",
		"tavily_key_set":     strings.TrimSpace(tavilyKey) != "",
		"web_search_proxy":   proxy,                       // 独立出口代理(http/https/socks5)，空=直连
		"python_interpreter": strings.TrimSpace(pyStored), // 用户/自动设的值(空=用运行时检测)
		"workers":            s.m.Workers(),               // 并发工作 agent 数(默认3)；对之后启动的任务生效
	}
}

// pgDetectPython re-runs interpreter detection, stores + returns it.
func (s *Server) pgDetectPython(w http.ResponseWriter, r *http.Request) {
	p := detectPython()
	if p == "" {
		writeErr(w, 404, "未检测到 python(python3/python 均不在 PATH)")
		return
	}
	if err := s.m.pg.SetSetting(settingPythonInterp, p); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"python_interpreter": p})
}

// putSettings applies a settings change. Toggling traffic_capture rebuilds the
// agents (applyLLM) so the new proxy/traffic-tools/prompt state takes hold — when
// off, agents get no proxy config, no traffic tools, and no proxy prompt content.
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TrafficCapture *bool `json:"traffic_capture"`
		LLMRecord      *bool `json:"llm_record"` // LLM 录制开关（默认关）；即时生效，无需重建 agent
		// Web search. WebSearchEnabled/Backend toggle the tool + backend; BraveKey/TavilyKey
		// are optional — omit (null) to leave a stored key untouched, send "" to clear.
		WebSearchEnabled *bool   `json:"web_search_enabled"`
		WebSearchBackend *string `json:"web_search_backend"`
		BraveKey         *string `json:"brave_search_api_key"`
		TavilyKey        *string `json:"tavily_search_api_key"`
		WebSearchProxy   *string `json:"web_search_proxy"`   // 独立出口代理(http/https/socks5)；null=不改，""=清空
		PythonInterp     *string `json:"python_interpreter"` // 自定义脚本工具的 python 解释器路径
		Workers          *int    `json:"workers"`            // 并发工作 agent 数(>0)；对之后启动的任务生效
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Workers != nil {
		if err := s.m.SetWorkers(*req.Workers); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
	}
	if req.PythonInterp != nil {
		if err := s.m.pg.SetSetting(settingPythonInterp, strings.TrimSpace(*req.PythonInterp)); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	if req.LLMRecord != nil {
		// 录制器每次调用读该标志，切换即时生效，无需 applyLLM 重建。
		if err := s.m.SetLLMRecordEnabled(*req.LLMRecord); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	changed := false
	if req.TrafficCapture != nil {
		if err := s.m.SetTrafficEnabled(*req.TrafficCapture); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		changed = true
	}
	if req.WebSearchEnabled != nil || req.WebSearchBackend != nil || req.BraveKey != nil || req.TavilyKey != nil || req.WebSearchProxy != nil {
		// Fill unspecified fields from current state so a partial PUT doesn't reset them.
		on, backend, _, _, _ := s.m.WebSearch()
		if req.WebSearchEnabled != nil {
			on = *req.WebSearchEnabled
		}
		if req.WebSearchBackend != nil {
			backend = *req.WebSearchBackend
		}
		if err := s.m.SetWebSearch(on, backend, req.BraveKey, req.TavilyKey, req.WebSearchProxy); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		changed = true
	}
	if changed {
		// rebuild agents so the new proxy/tools/prompt/web-search take hold (only if LLM configured).
		s.cfgMu.Lock()
		cfg, on := s.llmCfg, s.llmOn
		s.cfgMu.Unlock()
		if on {
			if err := s.applyLLM(cfg); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
		}
	}
	writeJSON(w, 200, s.settingsPayload())
}

// testWebSearch runs a real "test" search with the given (or currently saved)
// backend/proxy/key to verify the config can actually reach a search backend —
// mirroring testLLM. Backend/proxy come from the request (so the form's unsaved
// edits are tested); empty API keys fall back to stored values so the user need
// not retype them. Always 200 with {ok, error?, count?, backend?}.
func (s *Server) testWebSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Backend   string `json:"web_search_backend"`
		Proxy     string `json:"web_search_proxy"`
		BraveKey  string `json:"brave_search_api_key"`
		TavilyKey string `json:"tavily_search_api_key"`
	}
	// Empty body is fine — fall back entirely to the saved config below.
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeErr(w, 400, err.Error())
		return
	}
	_, backend, storedBraveKey, storedTavilyKey, _ := s.m.WebSearch()
	if strings.TrimSpace(req.Backend) != "" {
		backend = req.Backend
	}
	// Proxy is taken from the form as-is (empty = direct), so testing reflects exactly
	// what's shown — including an intentional "clear proxy to test direct" before saving.
	proxy := strings.TrimSpace(req.Proxy)
	// API keys are secrets the form omits when already saved, so fall back to stored.
	braveKey := storedBraveKey
	if strings.TrimSpace(req.BraveKey) != "" {
		braveKey = req.BraveKey
	}
	tavilyKey := storedTavilyKey
	if strings.TrimSpace(req.TavilyKey) != "" {
		tavilyKey = req.TavilyKey
	}
	// Hard cap so a slow/blocked proxy can't hang the request.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	results, err := actool.WebSearchProbe(ctx, actool.WebSearchConfig{Backend: backend, BraveAPIKey: braveKey, TavilyAPIKey: tavilyKey, Proxy: proxy}, "test", 3)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error(), "backend": backend})
		return
	}
	if len(results) == 0 {
		writeJSON(w, 200, map[string]any{"ok": false, "error": "搜索返回 0 条结果（可能被限流或代理不通）", "backend": backend})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "count": len(results), "backend": backend})
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	t := s.m.ResolveTask(r.URL.Query().Get("task"))
	if t == nil {
		writeErr(w, 404, "no active task")
		return
	}
	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	// Persist + broadcast the human turn so the 主 Agent 编排会话 survives page
	// reloads and updates live: the conversation lives in the activity stream as
	// worker="mainagent" (the per-task activity table, replayed via SSE).
	s.engine.emitActivity(t, db.Activity{Worker: "mainagent", Kind: "user", Summary: req.Message})
	if ma := s.mainAgentRef(); ma != nil {
		// Serialize per task: one main-agent run at a time so concurrent messages
		// don't corrupt the shared exp<id>-main transcript. If a prior turn is still
		// running, tell the caller to wait rather than starting a racing run.
		s.chatMu.Lock()
		if s.chatBusy[t.ID] {
			s.chatMu.Unlock()
			writeErr(w, 409, "主 Agent 正在处理上一条消息，请稍候")
			return
		}
		// Cancellable context so stopChat can abort this turn without affecting
		// the server's root context (mirrors the conversation stop pattern).
		ctx, cancel := context.WithCancel(s.ctx)
		s.chatBusy[t.ID] = true
		s.chatCancel[t.ID] = cancel
		s.chatMu.Unlock()

		// Run the agent on a per-turn cancellable ctx (NOT r.Context()): the turn can
		// take minutes (multi-tool loop), and binding it to the request lifecycle meant
		// a page reload / proxy timeout cancelled it mid-run ("context canceled"). The
		// steps + final answer stream back live via SSE (worker="mainagent"), so the
		// handler returns immediately and the browser never needs to hold the request.
		go func() {
			defer func() {
				cancel()
				s.chatMu.Lock()
				delete(s.chatBusy, t.ID)
				delete(s.chatCancel, t.ID)
				s.chatMu.Unlock()
			}()
			// emit every step (thinking/tool_use/tool_result/text/result) so the main
			// agent session shows its work live, like worker/planner. The final answer
			// is the captured "result" step — no separate reply emit (would duplicate).
			emit := func(rec db.Activity) { s.engine.emitActivity(t, rec) }
			maTaskID, _ := strconv.ParseInt(t.ID, 10, 64)
			if _, err := ma.Chat(ctx, maTaskID, s.m.Assets(), t.Store, t.Goal, req.Message, emit, t.Notify); err != nil && ctx.Err() == nil {
				s.engine.emitActivity(t, db.Activity{Worker: "mainagent", Kind: "text", IsError: true, Summary: "（主 Agent 出错：" + err.Error() + "）"})
			}
		}()
		writeJSON(w, 202, map[string]any{"status": "accepted", "mode": "llm"})
		return
	}
	reply := s.fallbackChat(t, req.Message)
	s.engine.emitActivity(t, db.Activity{Worker: "mainagent", Kind: "text", Summary: reply})
	writeJSON(w, 200, map[string]any{"reply": reply, "mode": "rule"})
}

// stopChat aborts the in-flight main-agent turn for a task (manual stop button).
// Mirrors pgStopConversation: cancels only the current turn; planner/workers are
// unaffected and continue running. The user can send a new message immediately.
func (s *Server) stopChat(w http.ResponseWriter, r *http.Request) {
	t := s.m.ResolveTask(r.PathValue("id"))
	if t == nil {
		writeErr(w, 404, "task not found")
		return
	}
	s.chatMu.Lock()
	cancel := s.chatCancel[t.ID]
	s.chatMu.Unlock()
	if cancel == nil {
		writeJSON(w, 200, map[string]any{"status": "idle"})
		return
	}
	cancel()
	writeJSON(w, 200, map[string]any{"status": "stopping"})
}

// fallbackChat is the no-LLM human-steering handler: simple命令 + 态势摘要.
func (s *Server) fallbackChat(t *Task, msg string) string {
	m := strings.TrimSpace(msg)
	lower := strings.ToLower(m)
	switch {
	case strings.HasPrefix(m, "意图") || strings.HasPrefix(lower, "intent"):
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(m, "意图"), "intent"))
		_, _ = t.Store.AddIntent(map[string]any{"summary": text}, 9, nil, "human")
		return "已注入一条高优先级意图：" + text
	case strings.HasPrefix(m, "提示") || strings.HasPrefix(lower, "hint"):
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(m, "提示"), "hint"))
		_, _ = t.Store.AddNode(db.KindHint, map[string]any{"text": text}, 0, "active", "human", nil)
		return "已记录提示，规划者下次会读到：" + text
	default:
		assetCounts, _ := s.m.Assets().CountsByType()
		assets := 0
		for _, c := range assetCounts {
			assets += c
		}
		fnd, _ := t.Store.ListByKind(db.KindFinding, 1000)
		fr, _ := t.Store.Frontier(1000)
		return fmt.Sprintf("（规则模式，未配置 LLM）当前态势：资产 %d，待领意图 %d，确认发现 %d。\n可用指令：以\"意图 ...\"注入意图，\"提示 ...\"给规划者提示。", assets, len(fr), len(fnd))
	}
}

func (s *Server) getReport(w http.ResponseWriter, r *http.Request) {
	t := s.m.ResolveTask(r.URL.Query().Get("task"))
	if t == nil {
		writeErr(w, 404, "no active task")
		return
	}
	findings, _ := t.Store.ListByKind(db.KindFinding, 1000) // 纯漏洞（事实是独立的 KindFact，不进报告）
	counts := map[string]int{}
	for _, ty := range []string{"root_domain", "ip", "subdomain", "app", "service", "endpoint"} {
		ns, _ := s.m.Assets().QueryByType(ty, 100000, 0)
		if len(ns) > 0 {
			counts[ty] = len(ns)
		}
	}
	md := report.Markdown(report.Input{
		Title: t.Description, Goal: t.Goal, GeneratedAt: time.Now(),
		AssetCounts: counts, Findings: findings,
	})
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write([]byte(md))
}

func (s *Server) getAudit(w http.ResponseWriter, r *http.Request) {
	t := s.m.ResolveTask(r.URL.Query().Get("task"))
	if t == nil {
		writeJSON(w, 200, []any{})
		return
	}
	writeJSON(w, 200, map[string]any{"entries": t.Guard.Audit(), "attributions": t.Guard.Attributions()})
}

// gc is a no-op stub (GC not yet implemented in the new asset store).
func (s *Server) gc(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"removed": 0})
}

// --- utils ---

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		origin := r.Header.Get("Origin")
		if origin != "" && corsOriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Add("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			if origin != "" && !corsOriginAllowed(origin) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(204)
			return
		}
		// Keep JSON and control endpoints bounded. File upload handlers apply a
		// tighter, endpoint-specific limit after this outer safety ceiling.
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
		next.ServeHTTP(w, r)
	})
}

var requestSequence uint64

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 128 {
			id = fmt.Sprintf("rx-%d-%d", time.Now().UnixNano(), atomic.AddUint64(&requestSequence, 1))
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

func corsOriginAllowed(origin string) bool {
	for _, allowed := range strings.Split(os.Getenv("RESTXTRA_CORS_ORIGINS"), ",") {
		if strings.TrimSpace(allowed) == origin {
			return true
		}
	}
	// Development defaults; production deployments should set an explicit list.
	return origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173"
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	requestID := w.Header().Get("X-Request-ID")
	payload := map[string]any{"error": msg, "code": http.StatusText(code)}
	if requestID != "" {
		payload["request_id"] = requestID
	}
	writeJSON(w, code, payload)
}

func atoiDefault(s string, d int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return d
}
