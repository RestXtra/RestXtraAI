// Mock 路由：把 (method, path) 映射到 lib/mock/data 的静态数据。
// 未命中的一律返回安全默认（[] / {} / {ok:true}），保证任何页面都不崩。
// 只在 NEXT_PUBLIC_MOCK=1 时经由 api.ts 的 http() 短路进入这里。

import * as D from "./data";

const delay = (ms = 120) => new Promise((r) => setTimeout(r, ms));

function parseBody(body?: BodyInit | null): Record<string, unknown> {
  if (typeof body !== "string") return {};
  try {
    return JSON.parse(body) as Record<string, unknown>;
  } catch {
    return {};
  }
}

export async function mockHandle<T>(method: string, rawPath: string, body?: BodyInit | null): Promise<T> {
  await delay();
  const [path, qs] = rawPath.split("?");
  const q = new URLSearchParams(qs ?? "");
  const seg = path.split("/").filter(Boolean); // ["exploration","activity"]
  const m = method.toUpperCase();
  const b = parseBody(body);
  return route(m, path, seg, q, b) as T;
}

function route(m: string, path: string, seg: string[], q: URLSearchParams, b: Record<string, unknown>): unknown {
  const task = q.get("task") ?? undefined;

  // ── auth：让 demo 直接进主界面 ──
  if (path === "/auth/status") return { initialized: true };
  if (path === "/auth/login" || path === "/auth/init") return { token: "mock-demo" };
  if (path === "/auth/change-password") return { ok: true };

  // ── tasks ──
  if (path === "/tasks" && m === "GET") {
    const cid = Number(q.get("company_id") ?? 0);
    const list = cid > 0 ? D.tasks.filter((t) => (t.companies ?? []).some((c) => c.id === cid)) : D.tasks;
    return { tasks: list, active: D.ACTIVE_TASK };
  }
  if (path === "/tasks" && m === "POST")
    return {
      ...D.tasks[0],
      id: "t-new",
      description: String(b.description ?? "新任务"),
      goal: String(b.goal ?? ""),
      status: "created",
    };
  if (seg[0] === "tasks" && seg.length === 2 && m === "DELETE") return { deleted: 1 };
  if (seg[0] === "tasks" && seg[2] === "coverage-graph" && m === "GET") return D.coverageGraph;
  if (seg[0] === "tasks" && seg[2] === "costs" && m === "GET") return D.taskRoundCosts;
  if (seg[0] === "tasks" && seg[2] === "operations-dashboard" && m === "GET") return D.operationsDashboard(seg[1]);
  if (seg[0] === "tasks" && seg[2] === "overview" && m === "GET") return D.taskOverview(seg[1]);
  if (seg[0] === "tasks" && seg[2] === "control") return { id: seg[1], paused: b.action === "pause" };
  if (seg[0] === "tasks" && seg[2] === "chat" && seg[3] === "stop") return { status: "stopped" };
  if (path === "/active") return { active: String(b.id ?? D.ACTIVE_TASK) };

  // ── stats ──
  if (path === "/stats") return D.stats(task);
  if (path === "/dashboard/companies")
    return {
      companies: [
        { id: 1, name: "Acme Corp", assets: 42, tasks: 3, findings: 5, high: 2 },
        { id: 0, name: "未归属", assets: 7, tasks: 1, findings: 1, high: 0 },
      ],
    };

  // ── assets ──
  if (path === "/assets/counts") return D.assetCounts;
  if (path === "/assets" && m === "GET") {
    const type = q.get("type") ?? "";
    const list = type ? D.assets.filter((a) => a.type === type) : D.assets;
    const limit = Number(q.get("limit") ?? 50);
    const offset = Number(q.get("offset") ?? 0);
    return { count: list.length, total: list.length, assets: list.slice(offset, offset + limit) };
  }
  if (path === "/assets" && m === "DELETE") return { deleted: (b.ids as unknown[])?.length ?? 0 };

  // ── companies ──
  if (path === "/companies" && m === "GET") return D.companies;
  if (path === "/companies" && m === "POST") return { id: 2, created: true, scope_added: 0 };
  if (seg[0] === "companies" && seg[2] === "scope") return { added: 0, skipped: 0, invalid: 0 };
  if (seg[0] === "companies" && seg.length === 2 && m === "DELETE") return { deleted: 1, assets_deleted: 0 };

  // ── exploration ──
  if (path === "/exploration/frontier") return D.frontier;
  if (path === "/exploration/findings/stats") {
    const cid = Number(q.get("company_id") ?? 0);
    const list = cid > 0 ? D.findings.filter((f) => (f.company_ids ?? []).includes(cid)) : D.findings;
    return {
      total: list.length,
      pending: list.filter((f) => f.status === "pending").length,
      high: list.filter((f) => f.severity === "high").length,
      medium: list.filter((f) => f.severity === "medium").length,
      low: list.filter((f) => f.severity === "low").length,
      tasks: new Set(list.map((f) => f.task_id).filter(Boolean)).size,
      vulnclasses: Array.from(new Set(list.map((f) => f.vulnclass))).sort(),
    };
  }
  if (seg[0] === "exploration" && seg[1] === "findings" && seg.length === 4 && seg[3] === "lineage") {
    return D.explorationGraph;
  }
  if (seg[0] === "exploration" && seg[1] === "findings" && seg.length === 3) {
    const finding = D.findings.find((item) => item.id === seg[2]);
    if (!finding) return {};
    if (m === "PATCH") return { ...finding, ...b };
    return { ...finding, report: finding.report ?? "" };
  }
  if (path === "/exploration/findings") {
    const cid = Number(q.get("company_id") ?? 0);
    let list = task ? D.findings.filter((f) => f.task_id === task) : D.findings;
    if (cid > 0) list = list.filter((f) => (f.company_ids ?? []).includes(cid));
    const severity = q.get("severity");
    const status = q.get("status");
    const vulnclass = q.get("vulnclass");
    if (severity) list = list.filter((f) => f.severity === severity);
    if (status) list = list.filter((f) => f.status === status);
    if (vulnclass) list = list.filter((f) => f.vulnclass === vulnclass);
    if (q.has("page")) {
      const page = Math.max(1, Number(q.get("page") ?? 1));
      const limit = Math.max(1, Number(q.get("limit") ?? 20));
      const offset = (page - 1) * limit;
      return { items: list.slice(offset, offset + limit), total: list.length, page, limit };
    }
    return list;
  }
  if (path === "/exploration/intents") return D.intents;
  if (path === "/exploration/tokens") return { workers: D.tokenWorkers, total: D.tokenTotal };
  if (path === "/exploration/graph") return D.explorationGraph;
  if (path === "/exploration/activity" && seg.length === 2) {
    const items = D.activityForTask();
    return { items, cursor: items.length ? items[items.length - 1].seq : 0 };
  }
  if (seg[0] === "exploration" && seg[1] === "activity" && seg.length === 3) {
    const a = D.activity.find((x) => x.seq === Number(seg[2]));
    return { detail: a?.detail ?? a?.summary ?? "" };
  }
  if (path === "/tokens/daily") return D.dailyTokens;
  if (path === "/tokens/conversations") return D.convTokens;

  // ── traffic / audit / settings ──
  if (path === "/audit") return D.audit;
  if (path === "/audit/logs" && m === "GET") {
    let list = D.auditLogs;
    const category = q.get("category");
    const action = q.get("action");
    const result = q.get("result");
    const actor = q.get("actor");
    if (category) list = list.filter((item) => item.category === category);
    if (action) list = list.filter((item) => item.action === action);
    if (result) list = list.filter((item) => item.result === result);
    if (actor) list = list.filter((item) => item.actor.includes(actor));
    const limit = Math.max(1, Number(q.get("limit") ?? 100));
    const offset = Math.max(0, Number(q.get("offset") ?? 0));
    return { items: list.slice(offset, offset + limit), total: list.length, limit, offset };
  }
  if (path === "/audit/stats" && m === "GET") return { total: D.auditLogs.length };
  if (path === "/traffic") return D.traffic;
  if (path === "/traffic" && m === "DELETE") return { deleted: (b.ids as unknown[])?.length ?? 0 };
  if (path === "/traffic" && m === "POST") return { removed: 1 };
  if (path === "/traffic/exchange") return D.trafficDetail;
  if (path === "/commands" && m === "DELETE") return { deleted: b.all ? 1 : ((b.ids as unknown[])?.length ?? 0) };
  if (path === "/logs" && m === "DELETE") return { deleted: (b.ids as unknown[])?.length ?? 0 };
  if (path === "/logs/clear" && m === "POST") return { removed: 1 };
  if (seg[0] === "llm" && seg[1] === "records" && m === "DELETE" && seg.length === 2)
    return { deleted: b.all ? 1 : ((b.ids as unknown[])?.length ?? 0) };
  if (seg[0] === "llm" && seg[1] === "records" && seg[2] === "clear" && m === "POST") return { removed: 1 };
  if (path === "/settings" && m === "GET") return D.settings;
  if (path === "/settings" && m === "PUT") return { ...D.settings, ...b };
  if (path === "/settings/web-search/test") return { ok: true, count: 5, backend: D.settings.web_search_backend };
  if (path === "/settings/python/detect") return { python_interpreter: "/usr/bin/python3" };
  if (path === "/chat")
    return { reply: "（demo）我已把该建议注入为一条高优意图，work agent 会尽快执行。", mode: "hint" };
  if (path === "/gc") return { removed: 0 };

  // ── LLM ──
  if (path === "/llm" && m === "GET") return D.llmConfig;
  if (path === "/llm" && m === "POST") return { ok: true };
  if (path === "/llm/test") return { ok: true, latency_ms: 128, model: String(b.model ?? "claude-opus-4-8") };
  if (path === "/llm/profiles" && m === "GET") return { profiles: D.llmProfiles };
  if (path === "/llm/profiles" && m === "POST") return { id: Number(b.id) || 3 };
  if (path === "/llm/profiles/active") return { ok: true };
  if (seg[0] === "llm" && seg[1] === "profiles" && seg.length === 3 && m === "DELETE")
    return { deleted: Number(seg[2]) };

  // ── agents ──
  if (path === "/agents" && m === "GET") return { agents: D.agents };
  if (path === "/agents" && m === "POST")
    return {
      id: "9",
      key: String(b.key ?? "custom"),
      name: String(b.name ?? ""),
      role: "custom",
      builtin: false,
      enabled: true,
    };
  if (seg[0] === "agents" && seg.length === 2 && m === "GET") return D.agentDetail(seg[1]);
  if (seg[0] === "agents" && seg[2] === "triggers" && m === "GET") return { triggers: [] };
  if (seg[0] === "agents" && seg[2] === "prompts") return { versions: D.agentDetail(seg[1]).versions };
  if (seg[0] === "agents" && seg[2] === "variables") return { variables: D.agentDetail(seg[1]).variables };
  if (seg[0] === "agents" && seg[2] === "prompt" && seg[3] === "preview")
    return { rendered: String(b.template ?? "").replace(/\{\{\.(\w+)\}\}/g, "«$1»") };
  if (seg[0] === "agents" && seg[2] === "visibility" && m === "GET") return D.agentDetail(seg[1]).visibility;

  // ── conversations ──
  if (path === "/conversations" && m === "GET") return { conversations: D.conversations };
  if (path === "/conversations" && m === "POST")
    return {
      id: 3,
      agent_key: String(b.agent_key ?? "mainagent"),
      title: String(b.title ?? "新会话"),
      created_at: "2026-07-26T04:00:00Z",
      updated_at: "2026-07-26T04:00:00Z",
    };
  if (seg[0] === "conversations" && seg[2] === "messages" && seg.length === 3 && m === "GET") {
    const items = D.conversationMessages[Number(seg[1])] ?? [];
    return { items, cursor: items.length ? items[items.length - 1].seq : 0, running: false };
  }
  if (seg[0] === "conversations" && seg[2] === "messages" && seg.length === 4) {
    const msgs = D.conversationMessages[Number(seg[1])] ?? [];
    const a = msgs.find((x) => x.seq === Number(seg[3]));
    return { detail: a?.detail ?? a?.summary ?? "" };
  }
  if (seg[0] === "conversations" && seg[2] === "messages" && m === "POST") return { status: "ok" };
  if (seg[0] === "conversations" && seg[2] === "stop") return { status: "stopped" };

  // ── tools ──
  if (path === "/tools" && m === "GET") return { tools: D.tools };
  if (path === "/tools/custom" && m === "POST") return { key: String(b.key ?? "custom-tool") };
  if (path === "/tools/custom/test") return { output: "（demo）工具执行输出示例。", is_error: false };

  // ── mcp ──
  if (path === "/mcp" && m === "GET") return { servers: D.mcpServers };
  if (path === "/mcp" && m === "POST") return { id: 3 };
  if (seg[0] === "mcp" && seg[2] === "tools") return { tools: D.mcpToolsById[Number(seg[1])] ?? [] };
  if (seg[0] === "mcp" && seg[2] === "refresh") return { tools: D.mcpToolsById[Number(seg[1])] ?? [] };
  if (seg[0] === "mcp" && seg.length === 2 && m === "DELETE") return { deleted: Number(seg[1]) };

  // ── scopesentry（demo：未配置）──
  if (path === "/sync/scopesentry/status")
    return { exists: false, configured: false, enabled: false, reachable: false, tools: [] };
  if (path === "/sync/scopesentry/projects") return { projects: [], tag: {} };
  if (path === "/sync/scopesentry/tasks") return { tasks: [] };
  if (path === "/sync/scopesentry/sync") return { synced: {}, companies: null, warnings: null, errors: null };

  // ── platform / RBAC ──
  if (path === "/platform/my" && m === "GET") return D.myProfile;
  if (path === "/platform/permissions" && m === "GET") return { permissions: D.permissionPoints };
  if (path === "/platform/users" && m === "GET") return { users: D.platformUsers };
  if (path === "/platform/users" && m === "POST") return { id: D.platformUsers.length + 1 };
  if (path === "/platform/roles" && m === "GET") return { roles: D.platformRoles };
  if (path === "/platform/roles" && m === "POST") return { id: D.platformRoles.length + 1 };
  if (seg[0] === "platform" && seg[1] === "roles" && seg[3] === "permissions") {
    if (m === "GET") return { keys: D.permissionPoints.map((permission) => permission.key) };
    return { ok: true };
  }

  // ── sandbox ──
  if (path === "/sandbox/hosts" && m === "GET") return { hosts: D.sandboxHosts };
  if (path === "/sandbox/hosts" && m === "POST") return { id: D.sandboxHosts.length + 1 };
  if (seg[0] === "sandbox" && seg[1] === "hosts" && seg[3] === "ping")
    return { ok: true, version: "27.1.1", api_version: "1.46", os: "linux", arch: "amd64" };
  if (seg[0] === "sandbox" && seg[1] === "hosts" && seg[3] === "containers" && seg.length === 4 && m === "GET") {
    const containers =
      q.get("managed") === "1"
        ? D.sandboxContainers.filter((container) => container.Labels?.["sandbox.managed"] === "true")
        : D.sandboxContainers;
    return { containers };
  }
  if (seg[0] === "sandbox" && seg[1] === "hosts" && seg[3] === "images" && m === "GET")
    return { images: D.sandboxImages };
  if (seg[0] === "sandbox" && seg[1] === "hosts" && seg[3] === "containers" && m === "DELETE") {
    const ids = ((b.ids as string[]) ?? []).filter((id) =>
      D.sandboxContainers.some((container) => container.Id === id && container.Labels?.["sandbox.managed"] === "true"),
    );
    return { deleted: ids, failed: [] };
  }
  if (path === "/sandbox/egress" && m === "GET") return { rules: D.sandboxEgress };
  if (path === "/sandbox/egress" && m === "POST") return { id: D.sandboxEgress.length + 1 };

  // ── skills ──
  if (path === "/skills" && m === "GET") return { skills: D.skills };
  if (path === "/skills" && m === "POST") return { name: String(b.name ?? "new-skill") };
  if (seg[0] === "skills" && seg[2] === "files" && seg.length === 3) return { files: ["SKILL.md"] };
  if (seg[0] === "skills" && seg[2] === "files" && seg.length >= 4)
    return { content: "# SKILL.md\n\n（demo）这是该 skill 的说明文件示例。", file: seg.slice(3).join("/") };

  // ── visibility ──
  if (seg[0] === "visibility" && m === "GET") return { agents: [] };

  // ── intercept ──
  if (path === "/intercept/rules" && m === "GET") return { rules: D.interceptRules };
  if (seg[0] === "intercept" && seg[1] === "rules" && seg[3] === "toggle")
    return { ok: true, enabled: b.enabled ?? true };
  if (path === "/intercept/pending" && m === "GET") return { pending: D.interceptPending };
  if (seg[0] === "intercept" && seg[1] === "pending" && seg[3] === "decide") return { ok: true };
  if (seg[0] === "intercept" && seg[1] === "pending" && seg.length === 3 && m === "GET")
    return D.interceptPending.find((p) => p.id === Number(seg[2])) ?? null;
  if (path === "/intercept/history") return { items: D.interceptHistory };
  if (seg[0] === "intercept" && seg[1] === "task")
    return { items: D.interceptHistory.filter((r) => r.task_id === seg[2]) };
  if (path === "/intercept/tool-config") return { enabled_tools: ["bash"] };

  // ── 能力：代理池 ──
  if (path === "/proxies" && m === "GET") return { proxies: D.proxies, stats: D.proxyStats };
  if (path === "/proxies" && m === "POST") return { id: D.proxies.length + 1 };
  if (path === "/proxies" && m === "DELETE")
    return { deleted: b.all ? D.proxies.length : ((b.ids as unknown[])?.length ?? 0) };
  if (path === "/proxies/import") return { imported: 3, total: 3, errors: [] };
  if (path === "/proxies/test-all")
    return { tested: D.proxies.length, ok: D.proxies.filter((p) => p.last_check_ok).length, fail: 0 };
  if (path === "/proxies/pick")
    return {
      ok: true,
      id: 1,
      name: "海外-住宅A",
      protocol: "http",
      host: "10.0.0.1",
      port: 8080,
      url: "http://10.0.0.1:8080",
      latency_ms: 120,
    };
  if (seg[0] === "proxies" && seg[1] && seg[2] === "test") return { ok: true, latency_ms: 96 };
  if (path === "/proxy-sources" && m === "GET") return { sources: D.proxySources };
  if (path === "/proxy-sources" && m === "POST") return { id: D.proxySources.length + 1 };
  if (seg[0] === "proxy-sources" && seg[1] && m === "DELETE") return { deleted: 1 };
  if (seg[0] === "proxy-sources" && seg[1] && seg[2] === "refresh") return { ok: true };

  // ── 代理入口（本地 mixed 桥）──
  if (path === "/proxy-bridge" && m === "GET")
    return {
      enabled: false,
      running: false,
      port: 7890,
      node_id: 0,
      node_name: "",
      rules: [],
      client_auth: false,
      client_username: "",
    };
  if (path === "/proxy-bridge" && m === "POST")
    return {
      enabled: true,
      running: true,
      port: Number(b.port ?? 7890),
      node_id: Number(b.node_id ?? 0),
      node_name: "",
      rules: b.rules ?? [],
      client_auth: false,
      client_username: "",
    };
  if (path === "/proxy-bridge/start")
    return {
      enabled: true,
      running: true,
      port: 7890,
      node_id: 0,
      node_name: "",
      rules: [],
      client_auth: false,
      client_username: "",
    };
  if (path === "/proxy-bridge/stop") return { ok: true, running: false };
  if (path === "/proxy-bridge/import-rules") return { rules: ((b.rules as string[]) ?? []).slice(0, 0) };

  // ── 空间测绘（FOFA / Hunter / Quake）──
  if (path === "/spacesearch/config" && m === "GET")
    return {
      providers: [
        { provider: "fofa", key_set: true, key_hint: "…f3a9" },
        { provider: "hunter", key_set: true, key_hint: "…c2b1" },
        { provider: "quake", key_set: false, key_hint: "" },
      ],
    };
  if (path === "/spacesearch/config" && m === "POST") return { ok: true, providers: D.spaceConfigs };
  if (path === "/spacesearch/test") return { ok: true };
  if (path === "/spacesearch/search")
    return {
      provider: String(b.provider ?? "fofa"),
      query: String(b.query ?? ""),
      total: D.spaceResults.length,
      size: D.spaceResults.length,
      page: 1,
      results: D.spaceResults,
    };
  if (path === "/spacesearch/import")
    return {
      imported: (b.results as unknown[])?.length ?? 0,
      stats: { ip: 2, subdomain: 1, service: 1, skipped: 0 },
      errors: [],
    };

  // ── C2 ──
  if (path === "/c2" && m === "GET") return { listeners: D.c2Listeners, sessions: D.c2Sessions };
  if (path === "/c2/listeners" && m === "POST") return { id: 99 };
  if (seg[0] === "c2" && seg[1] === "listeners" && seg.length === 3) {
    if (m === "DELETE") return { deleted: 1 };
    if (m === "POST") return { ok: true, status: "running" };
  }
  if (path === "/c2/profiles" && m === "GET") return { profiles: D.c2Profiles };
  if (path === "/c2/profiles" && m === "POST") return { id: 99 };
  if (seg[0] === "c2" && seg[1] === "profiles" && seg.length === 3 && m === "DELETE") return { deleted: 1 };
  if (path === "/c2/auto-tasks" && m === "GET") return { auto_tasks: D.c2AutoTasks };
  if (path === "/c2/auto-tasks" && m === "POST") return { id: 99 };
  if (seg[0] === "c2" && seg[1] === "auto-tasks" && seg.length === 3 && m === "DELETE") return { deleted: 1 };
  if (path === "/c2/plugins" && m === "GET") return { plugins: D.c2Plugins };
  if (path === "/c2/plugins" && m === "POST") return { id: 99 };
  if (seg[0] === "c2" && seg[1] === "plugins" && seg.length === 3 && m === "DELETE") return { deleted: 1 };
  if (seg[0] === "c2" && seg[1] === "plugins" && seg[3] === "run" && m === "POST") return { ok: true, enqueued: 2 };
  if (path === "/c2/tunnels" && m === "GET") return { tunnels: D.c2Tunnels };
  if (path === "/c2/tunnels" && m === "POST") return { id: 99, status: "running" };
  if (seg[0] === "c2" && seg[1] === "tunnels" && seg.length === 3 && m === "DELETE") return { deleted: 1 };
  if (path === "/c2/generated" && m === "GET") return { generated: D.c2Generated };
  if (path === "/c2/generated" && m === "POST")
    return {
      id: 99,
      session_id: "S-demo" + Math.random().toString(16).slice(2, 8),
      format: String(b.format ?? "stageless"),
      message: "演示模式：不实际编译，仅展示配置。",
      download_url: "",
      built: false,
      build_command: `CGO_ENABLED=0 GOOS=${b.os ?? "linux"} GOARCH=${b.arch ?? "amd64"} go build -o beacon ./cmd/beacon`,
      run_command: "./beacon",
    };
  if (seg[0] === "c2" && seg[1] === "generated" && seg.length === 3 && m === "DELETE") return { deleted: 1 };
  if (path === "/c2/postex" && m === "GET") return { modules: D.c2PostexModules };
  if (seg[0] === "c2" && seg[2] === "postex" && m === "POST") return { id: 505, module: String(b.module ?? ""), session_id: seg[1], state: "queued" };
  if (seg[0] === "c2" && seg[1] === "sessions" && seg[2] === "tasks" && m === "GET") return { tasks: D.c2MockTasks };
  if (seg[0] === "c2" && seg[1] === "sessions" && seg[2] === "tasks" && m === "POST") return { id: 506 };
  if (seg[0] === "c2" && seg[1] === "sessions" && seg[2] === "analyze" && m === "GET") {
    const s = D.c2Sessions[0];
    return { session: s, tasks: D.c2MockTasks, summary: { host: s.hostname, ip: s.host, remote_ip: s.remote_ip, os: `${s.os}/${s.arch}`, user: s.username, process: s.process_name, connection: s.connection, status: s.status, first_seen: s.first_seen, last_seen: s.last_seen, task_stats: { completed: 2, failed: 0, pending: 0 } } };
  }
  if (path === "/c2/ingest" && m === "POST") return { ok: true };
  if (path === "/c2/status" && m === "POST") return { ok: true };
  if (path === "/c2/note" && m === "POST") return { ok: true };
  if (seg[0] === "c2" && seg[2] === "result" && m === "POST") return { ok: true };

  // ── 写操作兜底：成功但不落库 ──
  if (["POST", "PUT", "PATCH", "DELETE"].includes(m)) return { ok: true };

  // ── 读兜底：集合类给 []，其余 {} ──
  return /(\/(tasks|profiles|conversations|rules|history|projects|tokens|agents|servers|skills|tools|findings|intents)s?$)|s$/.test(
    path,
  )
    ? []
    : {};
}
