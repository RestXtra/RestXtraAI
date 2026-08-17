// Real backend client. /api/* is proxied to the Go backend (next.config rewrites).
// Returns the domain types in lib/types.ts. Shapes match the backend handlers;
// a few fields the backend serializes differently (e.g. created_at as a unix int)
// are passed through and formatted at the call site.

import { MOCK } from "@/lib/mock/enabled";
import { mockHandle } from "@/lib/mock/handler";
import type {
  Activity,
  Agent,
  AgentDetail,
  AgentTrigger,
  Asset,
  AttackPattern,
  Audit,
  AuditLogEntry,
  BatchQueue,
  BatchTask,
  CommandRecord,
  Company,
  CompanyStat,
  Conversation,
  ConvTokenSummary,
  DailyTokenBucket,
  DockerImage,
  Edge,
  Finding,
  InterceptApprovalRow,
  InterceptPending,
  InterceptRule,
  LLMProfile,
  LLMRecordDetail,
  LLMRecordItem,
  MCPServer,
  MCPTool,
  MyProfile,
  PermissionPoint,
  PlatformRole,
  PlatformUser,
  PlaybookResult,
  PromptVar,
  PromptVersion,
  SandboxContainer,
  SandboxEgress,
  SandboxHost,
  Settings,
  SkillItem,
  SSProject,
  SSTask,
  Stats,
  Task,
  TaskNode,
  TaskWorkflow,
  TokenTotal,
  TokenUsage,
  Tool,
  TrafficDetail,
  TrafficResp,
  WorkflowGraphDef,
  WorkflowGraphMeta,
  WorkflowRunItem,
  KnowledgeItem, WebshellConn, C2Listener, C2Session, WorkspaceEntry,
} from "@/lib/types";

function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem("restxtra_token");
}

async function http<T>(path: string, init?: RequestInit): Promise<T> {
  if (MOCK) return mockHandle<T>(init?.method ?? "GET", path, init?.body ?? null);
  const token = getToken();
  const r = await fetch(`/api${path}`, {
    ...init,
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(init?.headers as Record<string, string> | undefined),
    },
  });
  if (r.status === 401) {
    if (typeof window !== "undefined") {
      localStorage.removeItem("restxtra_token");
      document.cookie = "restxtra_token=; path=/; max-age=0";
      window.location.href = "/login";
    }
    throw new Error("未授权");
  }
  if (!r.ok) throw new Error(`${init?.method ?? "GET"} ${path}: ${r.status}`);
  if (r.status === 204) return undefined as T;
  return r.json();
}

// sseUrl builds a URL for Server-Sent Events streams. SSE must NOT go through the
// Next.js dev `/api` rewrite: that proxy buffers the streamed response, so event
// frames never reach the browser (the EventSource opens but receives 0 messages).
// We therefore connect straight to the Go backend, whose CORS is open. Override
// with NEXT_PUBLIC_SSE_BASE; set it to "" to force same-origin (e.g. behind a
// production reverse proxy that flushes SSE correctly).
// Token is appended as ?token= because SSE bypasses the Next.js proxy and the
// browser does not send cookies cross-port.
// mockReport returns a canned Markdown report for the demo.
function mockReport(_task?: string): string {
  return `# RestXtra 渗透测试报告 — Acme Corp

## 概览
- 范围：acme.com（含 www / admin / api / shop / vpn 子域）
- 已确认发现：6 项（高危 3 · 中危 3 · 低危 2）
- 引擎模式：exploring

## 关键发现
1. **[高] 后台默认口令** admin.acme.com admin/admin123 → 可完全接管后台。
2. **[高] SQL 注入** www.acme.com/search?q= → 可读取 acme_prod 库。
3. **[高] IDOR** api.acme.com/v1/orders?id= → 可越权读取他人订单（含手机号/地址）。
4. **[中] 反射型 XSS**、**暴露 .git 源码**、**登录无速率限制**。

## 建议
- 后台强制改密 + 启用 MFA、封禁默认口令。
- search 接口参数化查询、输出编码。
- API 增加对象级授权校验（IDOR）、更换强 JWT 密钥。

> （demo）本报告由 mock 数据生成，仅用于界面演示。`;
}

export function sseUrl(path: string): string {
  const base =
    process.env.NEXT_PUBLIC_SSE_BASE ??
    (typeof window !== "undefined" ? `${window.location.protocol}//${window.location.hostname}:8787` : "");
  const token = getToken();
  const sep = path.includes("?") ? "&" : "?";
  return token ? `${base}${path}${sep}token=${encodeURIComponent(token)}` : `${base}${path}`;
}

const get = <T>(p: string) => http<T>(p);
const post = <T>(p: string, body?: unknown) =>
  http<T>(p, { method: "POST", body: body ? JSON.stringify(body) : undefined });
const put = <T>(p: string, body?: unknown) =>
  http<T>(p, { method: "PUT", body: body ? JSON.stringify(body) : undefined });
const patch = <T>(p: string, body?: unknown) =>
  http<T>(p, { method: "PATCH", body: body ? JSON.stringify(body) : undefined });
const del = <T>(p: string) => http<T>(p, { method: "DELETE" });

// Go serializes nil slices as JSON null — coerce to [].
const arr = <T>(x: T[] | null | undefined): T[] => x ?? [];
const tq = (task?: string, sep: "?" | "&" = "?") => (task ? `${sep}task=${encodeURIComponent(task)}` : "");

export const api = {
  // ---- auth ----
  authStatus: () => get<{ initialized: boolean }>("/auth/status"),
  login: (username: string, password: string) => post<{ token: string }>("/auth/login", { username, password }),
  initPassword: (password: string) => post<{ token: string }>("/auth/init", { password }),
  changePassword: (oldPassword: string, newPassword: string) =>
    post<{ ok: boolean }>("/auth/change-password", { old_password: oldPassword, new_password: newPassword }),

  // ---- tasks ----
  tasks: (companyId?: number) =>
    get<{ tasks: Task[]; active: string }>(`/tasks${companyId ? `?company_id=${companyId}` : ""}`).then((r) => ({
      tasks: arr(r.tasks),
      active: r.active ?? "",
    })),
  createTask: (
    description: string,
    goal: string,
    llmProfileId?: number,
    timeoutSeconds?: number,
    workflow?: TaskWorkflow,
    companyIds?: number[],
  ) =>
    post<Task>("/tasks", {
      description,
      goal,
      llm_profile_id: llmProfileId ?? null,
      timeout_seconds: timeoutSeconds ?? 0,
      workflow,
      company_ids: companyIds ?? [],
    }),
  deleteTask: (id: string) => del<{ deleted: number }>(`/tasks/${id}`),
  setTaskCompanies: (id: string, companyIds: number[]) =>
    put<{ ok: boolean }>(`/tasks/${id}/companies`, { company_ids: companyIds }),
  controlTask: (id: string, action: "pause" | "resume") =>
    post<{ id: string; paused: boolean }>(`/tasks/${id}/control`, { action }),
  taskAttackChain: (id: string) =>
    get<{
      summary: string;
      risk_score: number;
      nodes: { id: number; type: string; label: string }[];
      edges: { from: number; to: number; type: string }[];
    }>(`/tasks/${id}/attack-chain`),
  setActive: (id: string) => post<{ active: string }>("/active", { id }),
  // ---- stats ----
  stats: (task?: string) => get<Stats>(`/stats${tq(task)}`),

  // ---- assets ----
  // Server-side paginated: pass limit/offset, get back the page + full match total.
  assets: (type = "", limit = 50, offset = 0, companyId?: number) => {
    const q = new URLSearchParams({ type, limit: String(limit), offset: String(offset) });
    if (companyId) q.set("company_id", String(companyId));
    return get<{ count: number; total: number; assets: Asset[] }>(`/assets?${q.toString()}`).then(
      (r) => ({ assets: r?.assets ?? [], total: r?.total ?? r?.count ?? 0 }),
    );
  },
  searchAssets: (dsl: string, type = "", limit = 50, offset = 0, companyId?: number) => {
    const q = new URLSearchParams({ dsl });
    if (type) q.set("type", type);
    q.set("limit", String(limit));
    q.set("offset", String(offset));
    if (companyId) q.set("company_id", String(companyId));
    return get<{ count: number; total: number; assets: Asset[] }>(`/assets?${q.toString()}`).then(
      (r) => ({ assets: r?.assets ?? [], total: r?.total ?? r?.count ?? 0 }),
    );
  },
  assetCounts: (companyId?: number) =>
    get<Record<string, number>>(`/assets/counts${companyId ? `?company_id=${companyId}` : ""}`),
  deleteAssets: (ids: number[]) =>
    http<{ deleted: number }>("/assets", { method: "DELETE", body: JSON.stringify({ ids }) }),
  // legacy — kept for task-specific views; hits the same endpoint with task_id filter
  taskAssets: (taskId: string, type = "") =>
    get<{ count: number; assets: Asset[] }>(`/assets?task_id=${taskId}&type=${type}`).then((r) => r?.assets ?? []),

  // ---- companies (企业 + 资产范围；归属唯一来源) ----
  companies: () => get<Company[]>("/companies").then(arr),
  createCompany: (name: string, logo: string, scope: string) =>
    post<{ id: number; created: boolean; scope_added?: number; scope_invalid?: number; scope_errors?: string[] }>(
      "/companies",
      {
        name,
        logo,
        scope: scope.trim()
          ? scope
              .split("\n")
              .map((s) => s.trim())
              .filter(Boolean)
          : [],
      },
    ),
  addCompanyScope: (id: number, scope: string, reason = "") =>
    post<{ added: number; skipped: number; invalid: number; errors?: string[] }>(`/companies/${id}/scope`, {
      scope: scope.trim()
        ? scope
            .split("\n")
            .map((s) => s.trim())
            .filter(Boolean)
        : [],
      reason,
    }),
  updateCompanyScope: (id: number, scope: string, reason = "") =>
    post<{ added: number; skipped: number; invalid: number; errors?: string[] }>(`/companies/${id}/scope`, {
      scope: scope.trim()
        ? scope
            .split("\n")
            .map((s) => s.trim())
            .filter(Boolean)
        : [],
      reason,
      reset: true,
    }),
  deleteCompany: (id: number, deleteAssets = false) =>
    http<{ deleted: number; assets_deleted: number }>(`/companies/${id}`, {
      method: "DELETE",
      body: JSON.stringify({ delete_assets: deleteAssets }),
    }),

  // ---- exploration (per task) ----
  frontier: (task?: string) => get<TaskNode[]>(`/exploration/frontier${tq(task)}`).then(arr),
  findings: (task?: string, companyId?: number) => {
    const q = new URLSearchParams();
    if (task) q.set("task", task);
    if (companyId && companyId > 0) q.set("company_id", String(companyId));
    const qs = q.toString();
    return get<Finding[]>(`/exploration/findings${qs ? `?${qs}` : ""}`).then(arr);
  },
  dashboardCompanies: () =>
    get<{ companies: CompanyStat[] }>("/dashboard/companies").then((r) => arr(r.companies)),
  intents: (task?: string) => get<TaskNode[]>(`/exploration/intents${tq(task)}`).then(arr),
  tokenStats: (task?: string) =>
    get<{ workers: TokenUsage[]; total: TokenTotal }>(`/exploration/tokens${tq(task)}`).then((r) => ({
      workers: arr(r.workers),
      total: r.total,
    })),
  tokenDaily: (days = 30) => get<DailyTokenBucket[]>(`/tokens/daily?days=${days}`).then(arr),
  conversationTokens: () =>
    get<ConvTokenSummary[]>("/tokens/conversations")
      .then(arr)
      .catch(() => [] as ConvTokenSummary[]),
  explorationGraph: (task?: string) => get<{ nodes: TaskNode[]; edges: Edge[] }>(`/exploration/graph${tq(task)}`),
  activity: (task?: string, opts?: { intent?: string; since?: number; limit?: number }) => {
    const q = new URLSearchParams();
    if (task) q.set("task", task);
    if (opts?.intent) q.set("intent", opts.intent);
    if (opts?.since) q.set("since", String(opts.since));
    if (opts?.limit) q.set("limit", String(opts.limit));
    return get<{ items: Activity[]; cursor: number }>(`/exploration/activity?${q.toString()}`).then((r) => ({
      items: arr(r.items),
      cursor: r.cursor ?? 0,
    }));
  },
  activityDetail: (id: number, task?: string) => get<{ detail: string }>(`/exploration/activity/${id}${tq(task)}`),
  // 企业级活动流（仪表盘选中企业时用）：跨任务最新活动。
  activityByCompany: (companyId: number, limit = 60) =>
    get<{ items: Activity[]; cursor: number }>(`/exploration/activity/company?company_id=${companyId}&limit=${limit}`).then(
      (r) => ({ items: arr(r.items), cursor: r.cursor ?? 0 }),
    ),

  // ---- traffic / audit / report / chat ----
  audit: (task?: string) => get<Audit>(`/audit${tq(task)}`),
  traffic: (page = 0, size = 100, host = "", method = "", q = "") =>
    get<TrafficResp>(
      `/traffic?page=${page}&size=${size}` +
        (host ? `&host=${encodeURIComponent(host)}` : "") +
        (method && method !== "all" ? `&method=${encodeURIComponent(method)}` : "") +
        (q ? `&q=${encodeURIComponent(q)}` : ""),
    ),
  trafficExchange: (id: string) => get<TrafficDetail>(`/traffic/exchange?id=${encodeURIComponent(id)}`),
  deleteTraffic: (ids: string[]) => http<{ deleted: number; removed?: number }>("/traffic", { method: "DELETE", body: JSON.stringify({ ids }) }),
  clearTraffic: () => post<{ removed: number; deleted?: number }>("/traffic/clear", {}),

  // ---- app settings (runtime toggles) ----
  settings: () => get<Settings>(`/settings`),
  setSettings: (patch: Partial<Settings>) => put<Settings>(`/settings`, patch),
  // Run a real "test" search with the given (or saved) config to verify it works.
  testWebSearch: (patch: {
    web_search_backend?: string;
    web_search_proxy?: string;
    brave_search_api_key?: string;
    tavily_search_api_key?: string;
  }) => post<{ ok: boolean; error?: string; count?: number; backend?: string }>(`/settings/web-search/test`, patch),
  report: async (task?: string) => {
    if (MOCK) return mockReport(task);
    const token = getToken();
    const r = await fetch(`/api/report${tq(task)}`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (!r.ok) throw new Error(`report: ${r.status}`);
    return r.text();
  },
  chat: (message: string, task?: string) => post<{ reply: string; mode: string }>(`/chat${tq(task)}`, { message }),
  stopChat: (taskId: string) => post<{ status: string }>(`/tasks/${taskId}/chat/stop`, {}),
  gc: (ttl = 86400) => post<{ removed: number }>(`/gc?ttl=${ttl}`, {}),

  // ---- LLM ----
  getLLM: () =>
    get<{
      configured: boolean;
      provider: string;
      model: string;
      base_url: string;
      proxy?: string;
      key_set: boolean;
      rate_per_second?: number;
      rate_per_minute?: number;
      context_window_k?: number;
      reasoning_effort?: string;
    }>("/llm"),
  setLLM: (
    provider: string,
    model: string,
    base_url: string,
    api_key: string,
    rate_per_second = 0,
    rate_per_minute = 0,
    proxy = "",
    context_window_k = 0,
  ) => post("/llm", { provider, model, base_url, proxy, api_key, rate_per_second, rate_per_minute, context_window_k }),
  testLLM: (
    provider: string,
    model: string,
    base_url: string,
    api_key: string,
    proxy = "",
    reasoning_effort = "",
    profile_id?: number,
    auth_mode = "",
  ) =>
    post<{ ok: boolean; error?: string; latency_ms?: number; model?: string }>("/llm/test", {
      provider,
      model,
      base_url,
      proxy,
      api_key,
      reasoning_effort,
      auth_mode,
      profile_id,
    }),
  llmProfiles: () => get<{ profiles: LLMProfile[] }>("/llm/profiles").then((r) => arr(r.profiles)),
  saveLLMProfile: (p: {
    id?: number; // omit/0 = create; set = update that profile
    name: string;
    format: string;
    model: string;
    base_url?: string;
    proxy?: string;
    api_key?: string; // blank on update keeps the existing key
    rate_per_second?: number;
    rate_per_minute?: number;
    context_window_k?: number;
    reasoning_effort?: string; // ""|"off"|"low"|"medium"|"high"|"max"
    auth_mode?: string; // ""|"x-api-key"|"bearer" (bearer=Authorization: Bearer, 兼容 ANTHROPIC_AUTH_TOKEN)
  }) => post<{ id: number }>("/llm/profiles", p),
  deleteLLMProfile: (id: string) => del<{ deleted: number }>(`/llm/profiles/${id}`),
  activateLLMProfile: (id: string) => post<{ ok: boolean }>("/llm/profiles/active", { id: Number(id) }),

  // ---- agents ----
  agents: () => get<{ agents: Agent[] }>("/agents").then((r) => arr(r.agents)),
  getAgent: (key: string) => get<AgentDetail>(`/agents/${key}`),
  createAgent: (key: string, name: string, description = "") => post<Agent>("/agents", { key, name, description }),
  updateAgent: (key: string, name: string, description = "") =>
    patch<{ ok: boolean }>(`/agents/${key}`, { name, description }),
  deleteAgent: (key: string) => del<{ deleted: string }>(`/agents/${key}`),

  // ---- conversations (chat page) ----
  conversations: () => get<{ conversations: Conversation[] }>("/conversations").then((r) => arr(r.conversations)),
  createConversation: (agent_key: string, title = "", llm_profile_id?: number | null) =>
    post<Conversation>("/conversations", { agent_key, title, llm_profile_id: llm_profile_id ?? null }),
  renameConversation: (id: number, title: string) => patch<{ ok: boolean }>(`/conversations/${id}`, { title }),
  updateConversationProfile: (id: number, llm_profile_id: number | null) =>
    patch<{ ok: boolean }>(`/conversations/${id}/profile`, { llm_profile_id }),
  deleteConversation: (id: number) => del<{ deleted: number }>(`/conversations/${id}`),
  conversationMessages: (id: number, since = 0) =>
    get<{ items: Activity[]; cursor: number; running: boolean }>(`/conversations/${id}/messages?since=${since}`).then(
      (r) => ({ items: arr(r.items), cursor: r.cursor ?? 0, running: !!r.running }),
    ),
  conversationMsgDetail: (id: number, seq: number) =>
    get<{ detail: string }>(`/conversations/${id}/messages/${seq}`).then((r) => r.detail ?? ""),
  sendConversationMessage: (id: number, message: string) =>
    post<{ status: string }>(`/conversations/${id}/messages`, { message }),
  stopConversation: (id: number) => post<{ status: string }>(`/conversations/${id}/stop`, {}),
  saveAgentPrompt: (key: string, template: string, note = "") =>
    put<{ version: number }>(`/agents/${key}/prompt`, { template, note }),
  resetAgentPrompt: (key: string) => post<{ version: number }>(`/agents/${key}/prompt/reset`, {}),
  // 收尾提示词(超时/步数耗尽的 settlement 提示);prompt 空串=清除覆盖、用内置默认;
  // max_turns 省略则不动、传 0=用内置默认轮数
  saveAgentWrapup: (key: string, prompt: string, maxTurns?: number) =>
    put<{ ok: boolean }>(`/agents/${key}/wrapup`, { prompt, max_turns: maxTurns }),
  resetAgentWrapup: (key: string) =>
    post<{ ok: boolean; wrapup_default: string; wrapup_max_turns_default: number }>(`/agents/${key}/wrapup/reset`, {}),
  // 任务级超时收尾词(仅 worker/planner);prompt 空=清除、用内置默认
  saveAgentTaskTimeoutWrapup: (key: string, prompt: string, maxTurns?: number) =>
    put<{ ok: boolean }>(`/agents/${key}/wrapup/task-timeout`, { prompt, max_turns: maxTurns }),
  resetAgentTaskTimeoutWrapup: (key: string) =>
    post<{ ok: boolean; task_timeout_wrapup_default: string; task_timeout_wrapup_max_turns_default: number }>(
      `/agents/${key}/wrapup/task-timeout/reset`,
      {},
    ),
  // P3 triggers (仅自定义 agent)
  agentTriggers: (key: string) =>
    get<{ triggers: AgentTrigger[] }>(`/agents/${key}/triggers`).then((r) => arr(r.triggers)),
  createTrigger: (key: string, t: Omit<AgentTrigger, "id" | "agent_key" | "last_fire">) =>
    post<AgentTrigger>(`/agents/${key}/triggers`, t),
  updateTrigger: (id: number, t: Omit<AgentTrigger, "id" | "agent_key" | "last_fire">) =>
    patch<{ ok: boolean }>(`/triggers/${id}`, t),
  deleteTrigger: (id: number) => del<{ deleted: number }>(`/triggers/${id}`),
  saveAgentConfig: (
    key: string,
    patch: { max_turns: number; run_seconds?: number; web_search?: boolean; interactive_shell?: boolean },
  ) => put<{ ok: boolean }>(`/agents/${key}/config`, patch),
  agentPromptVersions: (key: string) =>
    get<{ versions: PromptVersion[] }>(`/agents/${key}/prompts`).then((r) => arr(r.versions)),
  agentVariables: (key: string) =>
    get<{ variables: PromptVar[] }>(`/agents/${key}/variables`).then((r) => arr(r.variables)),
  previewAgentPrompt: (key: string, template: string, sample?: Record<string, string>) =>
    post<{ rendered: string; error?: string }>(`/agents/${key}/prompt/preview`, { template, sample }),
  getAgentVisibility: (key: string) => get<{ mcp: number[]; skill: string[] }>(`/agents/${key}/visibility`),
  setAgentVisibility: (key: string, mcp: number[], skill: string[]) =>
    put<{ ok: boolean }>(`/agents/${key}/visibility`, { mcp, skill }),

  // ---- tools (内置工具目录) ----
  tools: () => get<{ tools: Tool[] }>("/tools").then((r) => arr(r.tools)),
  saveTool: (key: string, patch: Pick<Tool, "description" | "schema" | "agents" | "enabled">) =>
    put<{ ok: boolean }>(`/tools/${key}`, patch),
  resetTool: (key: string) => post<{ ok: boolean }>(`/tools/${key}/reset`, {}),
  // custom tools (自定义工具)
  createCustomTool: (
    t: Pick<Tool, "key" | "description" | "schema" | "agents" | "enabled" | "kind" | "exec" | "deferred">,
  ) => post<{ key: string }>("/tools/custom", t),
  updateCustomTool: (
    key: string,
    t: Pick<Tool, "description" | "schema" | "agents" | "enabled" | "kind" | "exec" | "deferred">,
  ) => put<{ ok: boolean }>(`/tools/custom/${key}`, t),
  deleteCustomTool: (key: string) => del<{ deleted: string }>(`/tools/custom/${key}`),
  testCustomTool: (body: { kind: string; exec: Record<string, unknown>; params: Record<string, unknown> }) =>
    post<{ output: string; is_error: boolean }>("/tools/custom/test", body),
  detectPython: () => post<{ python_interpreter: string }>("/settings/python/detect", {}),

  // ---- mcp ----
  mcpServers: () => get<{ servers: MCPServer[] }>("/mcp").then((r) => arr(r.servers)),
  saveMcpServer: (m: Partial<MCPServer>) => post<{ id: number }>("/mcp", m),
  deleteMcpServer: (id: number) => del<{ deleted: number }>(`/mcp/${id}`),
  mcpTools: (id: number) => get<{ tools: MCPTool[] }>(`/mcp/${id}/tools`).then((r) => arr(r.tools)),
  refreshMcpServer: (id: number) => post<{ tools: MCPTool[] }>(`/mcp/${id}/refresh`, {}).then((r) => arr(r.tools)),

  // ---- 资产同步 (ScopeSentry 数据源) ----
  ssStatus: () =>
    get<{ exists: boolean; configured: boolean; enabled: boolean; reachable: boolean; url?: string; tools: string[] }>(
      "/sync/scopesentry/status",
    ),
  ssDatasource: (body: { url?: string; api_key?: string }) =>
    post<{ id: number; enabled: boolean }>("/sync/scopesentry/datasource", body),
  ssProjects: (page = 1, size = 50, search = "") =>
    get<{ projects: SSProject[]; tag: Record<string, number> }>(
      `/sync/scopesentry/projects?page=${page}&size=${size}${search ? `&search=${encodeURIComponent(search)}` : ""}`,
    ).then((r) => ({ projects: arr(r.projects), tag: r.tag ?? {} })),
  ssTasks: (page = 1, size = 50, search = "") =>
    get<{ tasks: SSTask[] }>(
      `/sync/scopesentry/tasks?page=${page}&size=${size}${search ? `&search=${encodeURIComponent(search)}` : ""}`,
    ).then((r) => arr(r.tasks)),
  ssSync: (body: {
    dimension: "project" | "task";
    targets: string[];
    asset_types: string[];
    create_company?: boolean;
    page_size?: number;
  }) =>
    post<{
      synced: Record<string, number>;
      companies: string[] | null;
      warnings: string[] | null;
      errors: string[] | null;
    }>("/sync/scopesentry/sync", body),

  // ---- skills (文件系统) ----
  skills: () => get<{ skills: SkillItem[] }>("/skills").then((r) => arr(r.skills)),
  createSkill: (s: {
    name: string;
    description: string;
    license?: string;
    compatibility?: string;
    mcps?: string[];
    instructions?: string;
  }) => post<{ name: string }>("/skills", s),
  // uploadSkill installs a skill from a .zip (multipart). Surfaces the backend
  // error text (e.g. 已存在 / 缺少 SKILL.md) so the UI can show a precise message.
  uploadSkill: async (file: File, overwrite = false): Promise<{ name: string; files: number }> => {
    if (MOCK) return { name: file.name.replace(/\.zip$/i, ""), files: 1 };
    const fd = new FormData();
    fd.append("file", file);
    const token = getToken();
    const r = await fetch(`/api/skills/upload${overwrite ? "?overwrite=true" : ""}`, {
      method: "POST",
      body: fd,
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    const body = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(body?.error || `上传失败(${r.status})`);
    return body;
  },
  deleteSkill: (name: string) => del<{ deleted: string }>(`/skills/${name}`),
  updateSkillMeta: (
    name: string,
    meta: { mcps?: string[]; description?: string; license?: string; compatibility?: string },
  ) => put<{ ok: boolean }>(`/skills/${name}/meta`, meta),
  createSkillDir: (skill: string, path: string) => post<{ dir: string }>(`/skills/${skill}/dirs`, { path }),
  skillFiles: (name: string) => get<{ files: string[] }>(`/skills/${name}/files`).then((r) => r.files),
  readSkillFile: (name: string, file: string) =>
    get<{ content: string; file: string }>(`/skills/${name}/files/${file}`).then((r) => r.content),
  writeSkillFile: (name: string, file: string, content: string) =>
    put<{ ok: boolean }>(`/skills/${name}/files/${file}`, { content }),
  deleteSkillPath: (skill: string, path: string) => del<{ deleted: string }>(`/skills/${skill}/files/${path}`),

  // ---- visibility (MCP resource side) ---- (agent ids are strings per spec)
  resourceVisibility: (kind: string, id: number) =>
    get<{ agents: string[] }>(`/visibility/${kind}/${id}`).then((r) => arr(r.agents)),
  toggleVisibility: (agentId: string, kind: string, resourceId: number, visible: boolean) =>
    post<{ ok: boolean }>("/visibility/toggle", { agent_id: agentId, kind, resource_id: resourceId, visible }),

  // ---- visibility (Skill，按名称) ----
  skillVisibility: (name: string) => get<{ agents: string[] }>(`/visibility/skill/${name}`).then((r) => arr(r.agents)),
  toggleSkillVisibility: (agentId: string, skillName: string, visible: boolean) =>
    post<{ ok: boolean }>("/visibility/skill/toggle", { agent_id: agentId, skill_name: skillName, visible }),

  // ---- intercept rules ----
  interceptRules: () => get<{ rules: InterceptRule[] }>("/intercept/rules").then((r) => arr(r.rules)),
  createInterceptRule: (rule: Omit<InterceptRule, "id" | "created_at" | "updated_at">) =>
    post<InterceptRule>("/intercept/rules", rule),
  updateInterceptRule: (id: number, rule: Omit<InterceptRule, "id" | "created_at" | "updated_at">) =>
    put<InterceptRule>(`/intercept/rules/${id}`, rule),
  deleteInterceptRule: (id: number) => del<{ deleted: number }>(`/intercept/rules/${id}`),
  toggleInterceptRule: (id: number, enabled: boolean) =>
    post<{ ok: boolean; enabled: boolean }>(`/intercept/rules/${id}/toggle`, { enabled }),

  // ---- intercept pending (ask) ----
  interceptPending: () => get<{ pending: InterceptPending[] }>("/intercept/pending").then((r) => arr(r.pending)),
  interceptGetOne: (id: number) => get<InterceptPending>(`/intercept/pending/${id}`),
  interceptDecide: (id: number, decision: "allowed" | "denied") =>
    post<{ ok: boolean }>(`/intercept/pending/${id}/decide`, { decision }),
  interceptHistory: () => get<{ items: InterceptApprovalRow[] }>("/intercept/history").then((r) => arr(r.items)),
  interceptTask: (taskId: string) =>
    get<{ items: InterceptApprovalRow[] }>(`/intercept/task/${taskId}`).then((r) => arr(r.items)),

  // ---- intercept tool-config (全局工具拦截范围) ----
  interceptGetToolConfig: async (): Promise<{ enabled_tools: string[] }> => {
    if (MOCK) return { enabled_tools: ["bash"] };
    const token = getToken();
    const r = await fetch("/api/intercept/tool-config", {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (!r.ok) throw new Error(await r.text());
    return r.json();
  },
  interceptSetToolConfig: async (enabledTools: string[]): Promise<void> => {
    if (MOCK) return;
    const token = getToken();
    const r = await fetch("/api/intercept/tool-config", {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: JSON.stringify({ enabled_tools: enabledTools }),
    });
    if (!r.ok) throw new Error(await r.text());
  },

  // ---- 平台层：RBAC 多用户 + 审计（RestXtra 移植）----
  platformMy: () => get<MyProfile>("/platform/my"),
  platformPermissions: () =>
    get<{ permissions: PermissionPoint[] }>("/platform/permissions").then((r) => arr(r.permissions)),
  platformUsers: () => get<{ users: PlatformUser[] }>("/platform/users").then((r) => arr(r.users)),
  createPlatformUser: (body: { username: string; display_name?: string; password: string; roles?: string[] }) =>
    post<{ id: number }>("/platform/users", body),
  updatePlatformUser: (id: number, body: { display_name?: string; enabled?: boolean }) =>
    patch<{ ok: boolean }>(`/platform/users/${id}`, body),
  deletePlatformUser: (id: number) => del<{ deleted: number }>(`/platform/users/${id}`),
  deletePlatformUsers: (ids: number[], all = false) =>
    http<{ deleted: number[]; skipped?: string[] }>("/platform/users", { method: "DELETE", body: JSON.stringify({ ids, all }) }),
  resetUserPassword: (id: number, password: string) =>
    post<{ ok: boolean }>(`/platform/users/${id}/password`, { password }),
  setUserRoles: (id: number, roles: string[]) => post<{ ok: boolean }>(`/platform/users/${id}/roles`, { roles }),
  platformRoles: () => get<{ roles: PlatformRole[] }>("/platform/roles").then((r) => arr(r.roles)),
  createPlatformRole: (body: { name: string; description?: string; scope?: string; permissions?: string[] }) =>
    post<{ id: number }>("/platform/roles", body),
  updatePlatformRole: (id: number, body: { description?: string; scope?: string }) =>
    patch<{ ok: boolean }>(`/platform/roles/${id}`, body),
  deletePlatformRole: (id: number) => del<{ deleted: number }>(`/platform/roles/${id}`),
  deletePlatformRoles: (ids: number[], all = false) =>
    http<{ deleted: number[]; skipped?: string[] }>("/platform/roles", { method: "DELETE", body: JSON.stringify({ ids, all }) }),
  rolePermissions: (id: number) => get<{ keys: string[] }>(`/platform/roles/${id}/permissions`).then((r) => r.keys),
  setRolePermissions: (id: number, keys: string[]) =>
    put<{ ok: boolean }>(`/platform/roles/${id}/permissions`, { keys }),
  auditLogs: (
    f: { category?: string; action?: string; result?: string; actor?: string; limit?: number; offset?: number } = {},
  ) => {
    const q = new URLSearchParams();
    if (f.category) q.set("category", f.category);
    if (f.action) q.set("action", f.action);
    if (f.result) q.set("result", f.result);
    if (f.actor) q.set("actor", f.actor);
    q.set("limit", String(f.limit ?? 100));
    q.set("offset", String(f.offset ?? 0));
    return get<{ items: AuditLogEntry[]; total: number; limit: number; offset: number }>(`/audit/logs?${q.toString()}`);
  },
  auditStats: () => get<{ total: number }>("/audit/stats"),
  auditGC: (days = 90) => post<{ removed: number }>(`/audit/gc?days=${days}`, {}),
  deleteLogs: (ids: number[]) => http<{ deleted: number }>("/logs", { method: "DELETE", body: JSON.stringify({ ids }) }),
  clearLogs: () => post<{ removed: number }>("/logs/clear", {}),

  // ---- 攻击模式库 / playbook ----
  playbookPatterns: (
    f: { technique?: string; cve?: string; verification?: string; tag?: string; limit?: number; offset?: number } = {},
  ) => {
    const q = new URLSearchParams();
    if (f.technique) q.set("technique", f.technique);
    if (f.cve) q.set("cve", f.cve);
    if (f.verification) q.set("verification", f.verification);
    if (f.tag) q.set("tag", f.tag);
    q.set("limit", String(f.limit ?? 100));
    q.set("offset", String(f.offset ?? 0));
    return get<{ patterns: AttackPattern[]; total: number }>(`/playbook/patterns?${q.toString()}`);
  },
  createPlaybookPattern: (p: Partial<AttackPattern>) =>
    post<AttackPattern>("/playbook/patterns", p),
  playbookReproduce: (p: {
    host_id?: number;
    image: string;
    cve_id: string;
    title?: string;
    poc: string;
    port?: number;
    marker?: string;
    attack_technique_id?: string;
    tags?: string;
    confidence?: number;
    keep_running?: boolean;
  }) =>
    post<{ ok: boolean; success: boolean; exit_code?: number; verification?: string; seconds?: number; output?: string; pattern_id?: string; error?: string }>(
      "/playbook/reproduce",
      p,
    ),
  deletePlaybookPattern: (id: string) => del<{ deleted: number }>(`/playbook/patterns/${id}`),
  playbookSearch: (q: { cve?: string; technique?: string; components?: string[]; keywords?: string; limit?: number }) =>
    post<{ results: PlaybookResult[] }>("/playbook/search", q).then((r) => r.results ?? []),
  playbookStats: () => get<{ total: number; counts: Record<string, number> }>("/playbook/stats"),

  // ---- 批量任务队列 ----
  batchQueues: () => get<{ queues: BatchQueue[] }>("/batch/queues").then((r) => arr(r.queues)),
  createBatchQueue: (q: { name: string; description?: string; cron?: string }) =>
    post<{ id: number }>("/batch/queues", q),
  updateBatchQueue: (id: number, q: { name?: string; description?: string; cron?: string; enabled?: boolean }) =>
    patch<{ ok: boolean }>(`/batch/queues/${id}`, q),
  deleteBatchQueue: (id: number) => del<{ deleted: number }>(`/batch/queues/${id}`),
  runBatchQueue: (id: number) => post<{ ran: number }>(`/batch/queues/${id}/run`, {}),
  batchTasks: (queueId: number) =>
    get<{ tasks: BatchTask[] }>(`/batch/queues/${queueId}/tasks`).then((r) => ({ tasks: arr(r.tasks) })),
  addBatchTask: (queueId: number, title: string, payload: Record<string, unknown>) =>
    post<{ id: number }>(`/batch/queues/${queueId}/tasks`, { title, payload }),
  deleteBatchTask: (id: number) => del<{ deleted: number }>(`/batch/tasks/${id}`),

  // ---- 工具执行记录（任意工具的 tool_use + tool_result）----
  commands: (params?: { task?: string; q?: string; page?: number; size?: number }) => {
    const sp = new URLSearchParams();
    if (params?.task) sp.set("task", params.task);
    if (params?.q) sp.set("q", params.q);
    sp.set("page", String(params?.page ?? 0));
    sp.set("size", String(params?.size ?? 50));
    return get<{ commands: CommandRecord[]; total: number }>(`/commands?${sp.toString()}`);
  },
  deleteCommands: (ids: number[], all = false) =>
    http<{ deleted: number }>("/commands", { method: "DELETE", body: JSON.stringify({ ids, all }) }),

  // ---- LLM 录制 ----
  llmRecords: (params?: { model?: string; session?: string; page?: number; size?: number }) => {
    const sp = new URLSearchParams();
    if (params?.model) sp.set("model", params.model);
    if (params?.session) sp.set("session", params.session);
    sp.set("page", String(params?.page ?? 0));
    sp.set("size", String(params?.size ?? 50));
    return get<{ records: LLMRecordItem[]; total: number }>(`/llm/records?${sp.toString()}`);
  },
  llmRecordDetail: (id: number) => get<LLMRecordDetail>(`/llm/records/${id}`),
  deleteLLMRecords: (ids: number[], all = false) =>
    http<{ deleted: number; removed?: number }>("/llm/records", { method: "DELETE", body: JSON.stringify({ ids, all }) }),
  clearLLMRecords: () => post<{ removed: number; deleted?: number }>("/llm/records/clear", {}),

  // ---- 沙箱管理（主机 / 容器 / 出口范围）----
  sandboxHosts: () => get<{ hosts: SandboxHost[] }>("/sandbox/hosts").then((r) => arr(r.hosts)),
  saveSandboxHost: (h: { id?: number; name: string; addr: string; description?: string }) =>
    post<{ id: number }>("/sandbox/hosts", h),
  deleteSandboxHost: (id: string) => del<{ deleted: number }>(`/sandbox/hosts/${id}`),
  pingSandboxHost: (id: string) =>
    post<{ ok: boolean; version?: string; api_version?: string; os?: string; arch?: string; error?: string }>(
      `/sandbox/hosts/${id}/ping`,
      {},
    ),
  sandboxContainers: (hostId: string, opts?: { all?: boolean; managed?: boolean }) => {
    const sp = new URLSearchParams();
    if (opts?.all) sp.set("all", "1");
    if (opts?.managed) sp.set("managed", "1");
    return get<{ containers: SandboxContainer[] }>(`/sandbox/hosts/${hostId}/containers?${sp.toString()}`);
  },
  sandboxImages: (hostId: string) => get<{ images: DockerImage[] }>(`/sandbox/hosts/${hostId}/images`),
  createSandboxContainer: (
    hostId: string,
    req: {
      name?: string;
      image: string;
      memory_mb?: number;
      cpus?: number;
      pids_limit?: number;
      read_only?: boolean;
      cap_drop_all?: boolean;
      network_mode?: string;
      auto_start?: boolean;
      env?: string[];
      managed?: boolean;
    },
  ) => post<{ id: string }>(`/sandbox/hosts/${hostId}/containers`, req),
  sandboxContainerAction: (hostId: string, cid: string, action: "start" | "stop" | "restart" | "kill" | "remove") =>
    post<{ ok: boolean }>(`/sandbox/hosts/${hostId}/containers/${encodeURIComponent(cid)}/${action}`, {}),
  removeSandboxContainers: (hostId: string, ids: string[], all = false) =>
    http<{ deleted: string[]; failed?: string[] }>(`/sandbox/hosts/${hostId}/containers`, {
      method: "DELETE",
      body: JSON.stringify({ ids, all }),
    }),
  sandboxEgress: () => get<{ rules: SandboxEgress[] }>("/sandbox/egress").then((r) => arr(r.rules)),
  saveSandboxEgress: (e: {
    id?: number;
    kind: string;
    value: string;
    action?: string;
    note?: string;
    enabled?: boolean;
  }) => post<{ id: number }>("/sandbox/egress", e),
  deleteSandboxEgress: (id: string) => del<{ deleted: number }>(`/sandbox/egress/${id}`),
  deleteSandboxEgresses: (ids: string[], all = false) =>
    http<{ deleted: number }>("/sandbox/egress", { method: "DELETE", body: JSON.stringify({ ids: ids.map(Number), all }) }),

  // ---- 工作流图引擎 ----
  workflowValidate: (g: WorkflowGraphDef) => post<{ ok: boolean; errors?: string[] }>("/workflows/validate", g),
  workflowSave: (req: {
    id?: number;
    name: string;
    description?: string;
    enabled?: boolean;
    graph: WorkflowGraphDef;
  }) => post<{ id: number }>(`/workflows/save`, req),
  workflows: () => get<{ workflows: WorkflowGraphMeta[] }>("/workflows").then((r) => arr(r.workflows)),
  workflowGet: (id: string) => get<WorkflowGraphMeta & { graph: WorkflowGraphDef }>(`/workflows/${id}`),
  workflowDelete: (id: string) => del<{ deleted: number }>(`/workflows/${id}`),
  workflowRun: (id: string, inputs?: Record<string, string>) =>
    post<{ run_id: number }>(`/workflows/${id}/run`, { inputs }),
  workflowRuns: (id: string) => get<{ runs: WorkflowRunItem[] }>(`/workflows/${id}/runs`).then((r) => arr(r.runs)),
  workflowRunDetail: (runId: string) => get<WorkflowRunItem>(`/workflow-runs/${runId}`),
  workflowDryRun: (g: WorkflowGraphDef, inputs?: Record<string, string>) =>
    post<{ status: string; outputs?: Record<string, string>; node_runs?: unknown[]; final?: string }>(
      "/workflows/dry-run",
      { graph: g, inputs },
    ),

  // ---- 工作空间 ----
  workspaceList: (path = "") => get<{ path: string; entries: WorkspaceEntry[] }>(`/workspace/list?path=${encodeURIComponent(path)}`),
  workspaceRead: (path: string) => get<{ path: string; content: string }>(`/workspace/read?path=${encodeURIComponent(path)}`),

  // ---- 知识库 ----
  knowledge: () => get<{ items: KnowledgeItem[] }>("/knowledge").then((r) => arr(r.items)),
  knowledgeSearch: (q: string, limit = 8) => post<{ items: KnowledgeItem[] }>("/knowledge/search", { q, limit }).then((r) => arr(r.items)),
  saveKnowledge: (k: { id?: number; title: string; content: string; tags?: string }) => post<{ id: number }>("/knowledge", k),
  deleteKnowledge: (id: string) => del<{ deleted: number }>(`/knowledge/${id}`),

  // ---- WebShell ----
  webshells: () => get<{ connections: WebshellConn[] }>("/webshell").then((r) => arr(r.connections)),
  saveWebshell: (w: Partial<WebshellConn>) => post<{ id: number }>("/webshell", w),
  deleteWebshell: (id: string) => del<{ deleted: number }>(`/webshell/${id}`),
  deleteWebshells: (ids: string[], all = false) =>
    http<{ deleted: number }>("/webshell", { method: "DELETE", body: JSON.stringify({ ids: ids.map(Number), all }) }),
  webshellTest: (w: Partial<WebshellConn>) =>
    post<{ ok: boolean; status?: number; snippet?: string; error?: string }>("/webshell/test", w),

  // ---- C2 ----
  c2: () => get<{ listeners: C2Listener[]; sessions: C2Session[] }>("/c2"),
  saveC2Listener: (l: Partial<C2Listener>) => post<{ id: number }>("/c2/listeners", l),
  deleteC2Listener: (id: string) => del<{ deleted: number }>(`/c2/listeners/${id}`),
  deleteC2Listeners: (ids: string[], all = false) =>
    http<{ deleted: number }>("/c2/listeners", { method: "DELETE", body: JSON.stringify({ ids: ids.map(Number), all }) }),
  deleteC2Sessions: (ids: string[], all = false) =>
    http<{ deleted: number }>("/c2/sessions", { method: "DELETE", body: JSON.stringify({ ids: ids.map(Number), all }) }),
  c2Ingest: (p: { listener_id?: number; session_id: string; host?: string; meta?: string; status?: string }) =>
    post<{ ok: boolean }>("/c2/ingest", p),
  c2SetStatus: (session_id: string, status: string) => post<{ ok: boolean }>("/c2/status", { session_id, status }),
};
