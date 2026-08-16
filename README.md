<div align="center">

# RestXtra AI

AI 自主渗透测试平台（Go 后端 + Next.js 前端）

**自主探索引擎 × 平台治理能力** 的一体化产品

</div>

---

## 项目简介

RestXtra AI 是一体化的自主渗透测试平台，将 **自主探索引擎** 与 **平台治理能力**
融合为单二进制产品：

| 能力面 | 内容 |
|---|---|
| **自主探索引擎** | Planner/Worker 多智能体自主探索、探索图、流量录制、拦截审批、Reflexion 自适应、证据门控 |
| **工具与接入** | 内置工具目录、YAML 工具配方、MCP 发现接入、LLM 多供应商预设、工具基准测试 |
| **能力扩展** | 攻击模式库（playbook）、攻击链建模、批量任务、DAG 工作流、沙箱管理、命令执行 |
| **平台治理** | RBAC 多用户、审计日志、知识库、Agent / 工具 / MCP / Skill 管理、报告与证据门控 |

核心不变量：**Agent 运行时统一 norma SDK**、**数据库统一 PostgreSQL**、
**前端统一 Next.js**、**统一 JWT + RBAC 鉴权**。

---

## 功能

### 自主探索引擎
- 目标拆解 → 规划探索方向 → 多 Worker 并行执行 → 事实 / 资产 / 漏洞写回知识图谱 → 持续迭代收敛
- 人在环路：任务级对话操舵、拦截审批（allow / deny / ask + 超时决策）
- Reflexion 自适应：工具调用反复失败时自动分类并升级绕过策略（编码 / 关键字 / 语法换招重试）
- 证据门控：结论必须引用真实工具输出，杜绝模型编造结论 / flag

### 工具与数据接入
- 内置工具目录 + YAML 工具配方：以数据驱动把 curl / nmap / sqlmap / nuclei 等 CLI 工具接入 agent
- MCP 发现与接入、LLM 多供应商预设（一键填充格式 / 端点 / 模型 / 认证头）、工具基准测试
- 资产 DSL 检索、ScopeSentry 数据源同步、go-mitmproxy 流量录制与回放

### 能力扩展
- 攻击模式库（playbook）：攻击模式的编排与一键复现
- 攻击链建模：从真实工具执行轨迹自动生成攻击链 DAG（无执行记录时拒绝杜撰）
- 批量任务：多目标批量任务与基准评测
- DAG 工作流：可视化工作流构建器（start / tool / agent / condition / hitl / output / end 七类节点 + 模板变量 + 条件表达式）
- 沙箱：容器 / 主机 / 出网管理；命令执行：后台命令宿主

### 平台治理
- 多用户 RBAC、操作审计、系统设置、LLM 配置
- Agent / 工具 / MCP / Skill 管理、实时日志流
- LLM / 工具调用工作日志、证据门控报告

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

**数据库**（`config.json`，或用环境变量 `RESTXTRA_PG_DSN` 覆盖）：

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

仅供授权测试与研究使用。代码以 Apache License 2.0 分发（见 [LICENSE](LICENSE)）。
