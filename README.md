<div align="center">

# RestXtra AI

AI 自主渗透测试平台（Go 后端 + Next.js 前端）

**ARTEX 自主探索引擎 × Pentest-RestXtra 平台能力** 的融合项目

</div>

---

## 项目简介

RestXtra AI 由两个上游项目融合而来（详见 [NOTICE](NOTICE) 与融合蓝图
[AI-Pentest-Fusion-Blueprint](https://github.com/RestXtra/RestXtraAI)）：

| 来源 | 提供能力 |
|---|---|
| **ARTEX**（基层） | Next.js 前端、norma agent 驱动的 Planner/Worker 自主探索引擎、探索图、流量录制、拦截审批、资产/漏洞/Agent/LLM/MCP 管理 |
| **Pentest-RestXtra**（移植） | RBAC 多用户权限、审计日志、知识库、攻击模式库（playbook）、批量任务、确定性报告与证据门控、C2 / WebShell / 沙箱 / 机器人（规划中） |

核心不变量：**Agent 运行时统一 norma SDK**（不引入 Eino ADK）、**数据库统一 PostgreSQL**、
**前端统一 Next.js**、**统一 JWT + RBAC 鉴权**。

---

## 功能

- **自主探索引擎**：目标拆解 → 规划探索方向 → 多 Worker 并行执行 → 事实/资产/漏洞写回知识图谱 → 持续迭代收敛
- **人在环路**：任务级对话操舵（`add_hint` / `add_intent`）、拦截审批（allow/deny/ask + 超时决策）
- **资产与流量**：资产 DSL 检索、ScopeSentry 数据源同步、go-mitmproxy 流量录制与回放
- **平台治理**：多用户 RBAC、操作审计、AI Agent / 角色 / MCP / Skill / 工具 管理
- **移植能力**：审计日志、证据门控报告、攻击模式库（后续迭代：知识库检索、批量任务、沙箱、WebShell、C2）

---

## 安装

> 依赖数据库 **PostgreSQL 16+**；探索需配置 **LLM**（`ANTHROPIC_API_KEY` 或 `OPENAI_API_KEY`，也可在 UI 里配）。

### 方式一：Docker Compose

```bash
git clone https://github.com/RestXtra/RestXtraAI.git
cd RestXtraAI
cp .env.example .env          # 填 POSTGRES_PASSWORD、可选 LLM key
docker compose up -d          # postgres + restxtra 镜像
# → http://localhost:8787（首次进入 /setup 设置管理员密码）
```

### 方式二：从源码编译单二进制（内嵌前端）

```bash
# 1) 前端静态导出
cd web && npm ci && npm run build:static && cd ..
# 2) 拷进内嵌目录
cp -r web/out server/webui/dist
# 3) 编译（-tags embedui 才内嵌前端）
CGO_ENABLED=0 go build -tags embedui -o restxtra ./cmd/restxtra
./restxtra
```

---

## 配置

**数据库**（`config.json`，或用环境变量 `RESTXTRA_PG_DSN` 覆盖；旧 `ARTEX_PG_DSN` 仍兼容）：

```json
{
  "database": {
    "host": "127.0.0.1", "port": 5432,
    "user": "restxtra", "password": "yourpass",
    "dbname": "restxtra", "sslmode": "disable"
  }
}
```

**LLM**：`export ANTHROPIC_API_KEY=sk-...`（或 `OPENAI_API_KEY`），也可在 UI 的「LLM 配置」页填写。
可选：`RESTXTRA_LLM_PROVIDER` / `RESTXTRA_LLM_MODEL` / `RESTXTRA_LLM_BASE_URL` / `RESTXTRA_LLM_PROXY`。

**并发**：每个任务的 work agent 数在「系统设置」里配置（默认 3）。

**常用参数**：`./restxtra -addr :8787 -proxy :8788`（`-addr` 前端+API，`-proxy` 流量录制代理）。

---

## 开发

```bash
./dev.sh    # 后端(:8787) + 流量代理(:8788) + 前端 next dev(:5173) → http://localhost:5173
```

- 后端：`go run ./cmd/restxtra`（不带 `-tags embedui` 则不内嵌前端）
- 前端：`cd web && npm run dev`（`/api` 反代到后端，带热更新）
- 测试：`go test ./...`
- Mock 预览（无后端）：`cd web && NEXT_PUBLIC_MOCK=1 npm run dev`

---

## 许可

仅供授权测试与研究使用。代码以 Apache License 2.0 分发（见 [LICENSE](LICENSE)），
上游归属见 [NOTICE](NOTICE)。
