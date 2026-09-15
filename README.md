<div align="center">

# RestXtra AI

**AI 驱动的自动化渗透测试与安全众测平台**

自主探索引擎 × 多智能体编排 × 平台治理，融合为单二进制产品（Go 后端 + Next.js 前端）

</div>

---

## 项目简介

RestXtra AI 面向**授权安全测试**场景，把一次完整渗透测试从「信息收集 → 关键资产深度测试 → 漏洞验证 → 报告编写」全流程自动化：以多智能体协同的**自主探索引擎**驱动真实工具执行，把过程与结论约束在**可核验的证据**上，并用平台化治理（RBAC、审计、报告、流量留痕）保证可控、可追溯、可交付。

核心不变量：

| 维度 | 统一实现 |
|---|---|
| Agent 运行时 | 内嵌自研 agent 运行时 **norma SDK**（Planner/Worker 双图探索） |
| 数据 | 统一 **PostgreSQL**（任务图、资产、漏洞、事件、日志一体） |
| 前端 | 统一 **Next.js**（静态导出后内嵌进单二进制，`-tags embedui`） |
| 鉴权 | 统一 **JWT + RBAC** 多用户与操作审计 |
| 证据 | 结论必须引用真实工具输出；越权/注入类必须基线差分证明 |

---

## 核心能力

### 多智能体编排（红队总指挥 + 专用专家）

一个「红队总指挥」负责拆解、派活、验收、汇总，不亲自执行；所有实操通过 `spawn_task` 交给专用专家 Agent，每个子任务带**结构化交接包**（objective / asset_ids / required_evidence / budget），只拿到引用、不复制父任务历史。

| Agent | 职责 |
|---|---|
| `red_team_lead` 红队总指挥 | 拆解任务、委派专家、验收成果、汇总报告 |
| `asset_intel` 信息收集 | OSINT + FOFA 被动测绘、去重归档、范围管理 |
| `web_vuln` 漏洞猎人 | 注入 / SSTI / SSRF / XXE / 反序列化 / 认证绕过 |
| `exploit` 利用专家 | PoC / 利用链 / 绕过 |
| `pentest_chain` 渗透链指挥 | 侦察→利用→提权→横向→后渗透 |
| `cloud_attack` 云攻击专家 | IAM / S3 / 容器 / K8s / 云元数据 |
| `binary_vuln` 二进制猎人 | 逆向 / 补丁对比 / 源码审计 |
| `code_audit` 代码审计 | 数据流追踪、覆盖检查与非破坏性验证 |
| `evasion` 规避专家 | WAF / AV / EDR 对抗与流量混淆 |

编排能力：子任务**父子树 + 会话归属**、委派预算、**任务边界强制**（子任务只在下发的 `asset_ids` 范围内探索，越界目标被拒收）、**演尽自动完成**（无待处理方向 + 无在跑工作 + 图不再变化即判定完成，而非耗到超时）。

### 自主探索引擎

- 目标拆解 → 规划探索方向 → 多 Worker 并行执行 → 事实 / 资产 / 结论写回知识图谱 → 持续迭代收敛
- 异构 Agent 协同承担「信息收集 — 漏洞验证 — 利用落地」全链路；worker 与 planner 职责边界清晰
- **人在环路**：任务级对话操舵、`steer_work` 实时纠偏、`kill_work` 止损、`add_hint` 战略提示、审批决策（allow / deny / ask + 超时决策）
- **证据约束**：结论必须引用真实工具输出；越权 / 注入类必须给出基线差分（同请求换对象 / 参数对比响应）
- **Reflexion 自适应**：工具调用反复失败时自动分类并换招重试（编码 / 关键字 / 语法）
- 长任务上下文治理：Working Set 恢复、上下文压缩、提示前缀缓存、无延续判定

### 信息收集与资产治理

- **空间测绘数据源**：FOFA 被动测绘（企业 / 根域 / ICP），可扩展接入 Hunter / Quake
- **资产管理**：根域 / 子域 / IP / 服务 / 端点 / 应用多维归档；资产 DSL 检索；企业（多企业）归属与授权范围管理
- ScopeSentry 数据源同步；资产覆盖图

### 漏洞与报告

- 漏洞登记：结构化报告 + **原始请求包 / 响应包（PoC 报文）**、严重等级、复现步骤、修复建议
- **发现去重**：同一漏洞类 + 目标（URL / 端点 / 资产）自动合并，避免重复上报
- 任务报告：任务详情页直接生成**确定性、证据约束的 Markdown 报告**，可复制 / 下载

### 工具与接入

- 内置工具目录 + **YAML 工具配方**：数据驱动把 `curl` / `nmap` / `sqlmap` / `nuclei` / `httpx` / `ffuf` / `gobuster` / `subfinder` 等 CLI 工具接入 Agent
- **MCP 发现与接入**、**Skill 技能库**（打法 / 方法论注入）、**自定义脚本工具**
- LLM 多供应商预设（一键填充格式 / 端点 / 模型 / 认证头），强 / 弱模型按角色路由
- 流量录制与回放（go-mitmproxy）、拦截与审批（HITL）

### 能力扩展

- **流程模板库（Playbook）**：流程编排与一键复现
- **链路建模**：从真实工具执行轨迹自动生成链路 DAG（无执行记录时拒绝杜撰）
- **DAG 工作流**：可视化工作流构建器（start / tool / agent / condition / hitl / output / end 七类节点 + 模板变量 + 条件表达式）
- **批量任务与基准评测**：多目标批量下发与效率度量
- **沙箱与命令宿主**：容器 / 主机 / 出网管理；后台命令执行宿主
- **代理池**：HTTP / HTTPS / SOCKS5 节点统一管理（多行文本 / 远程 URL / Clash YAML 导入、测活、健康熔断、按区域 / 协议挑选）；本地混合代理入口（IP-CIDR / 域名直连规则）
- **指令通道与应急处置**：Beacon / 命令控制（C2）、连接管理、事件（Incident）处置与工单化

### 平台治理

- 多用户 **RBAC**、操作审计、系统设置、LLM 配置
- Agent / 工具 / MCP / Skill / 拦截规则管理
- LLM 调用与工具调用**工作日志**、实时日志流、事件与用量核算

---

## 技术栈

| 层 | 选型 |
|---|---|
| 后端 | Go 1.26（`github.com/RestXtra/RestXtraAI`），单二进制，内嵌前端 |
| Agent 运行时 | 内嵌自研 **norma** SDK（`third_party/norma`，Planner/Worker 双图探索 + 压缩 + 流式工具执行） |
| 数据库 | PostgreSQL 16+ |
| 前端 | Next.js 16 / React 19 / Tailwind v4 / shadcn / TanStack Table / xyflow |
| 鉴权 | JWT + RBAC |

---

## 安装

> 依赖 **PostgreSQL 16+**；探索引擎需配置 **LLM**（`ANTHROPIC_API_KEY` 或 `OPENAI_API_KEY`，也可在 UI 里配置）。

### 方式一：Docker Compose

```bash
git clone https://github.com/RestXtra/RestXtraAI.git
cd RestXtraAI
cp .env.example .env          # 填 POSTGRES_PASSWORD，可选 LLM key
docker compose up -d          # postgres + restxtra 镜像
# → http://localhost:8787（首次进入 /setup 设置管理员密码）
```

### 方式二：源码编译单二进制（内嵌前端）

```bash
# 1) 前端静态导出 → web/out
cd web && npm ci && npm run build:static && cd ..
# 2) 拷进内嵌目录
cp -r web/out/. server/webui/dist/
# 3) 编译（仅 -tags embedui 才内嵌前端）
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

**运行参数**：`./restxtra -addr :8787 -proxy :8788`（`-addr` 前端 + API，`-proxy` 流量录制代理）。

**并发**：每个任务的 work agent 数在「系统设置」里配置（默认 3）。

---

## 开发

```bash
./dev.sh    # 后端(:8787) + 流量代理(:8788) + 前端 next dev(:5173) → http://localhost:5173
```

- 后端：`go run ./cmd/restxtra`（不带 `-tags embedui` 则不内嵌前端）
- 前端：`cd web && npm run dev`（`/api` 反代到后端，带热更新）
- 测试：`go test ./...`
- Mock 预览（无后端）：`cd web && NEXT_PUBLIC_MOCK=1 npm run dev`
- 公开 Demo 部署（纯静态、无数据库 / 密钥）：见 [DEMO_DEPLOYMENT.md](DEMO_DEPLOYMENT.md)

---

## 目录结构

```
cmd/        可执行入口（restxtra、beacon）
server/     HTTP API、引擎、编排、任务/资产/漏洞/报告、鉴权与审计
agent/      Planner/Worker 与专用 Agent 的工具集、提示词、记忆
db/         PostgreSQL 数据访问（任务图、资产、漏洞、事件、日志）
third_party/norma   内嵌 agent 运行时（自研 fork）
web/        Next.js 前端
proxybridge/ intercept/ traffic/   代理桥、拦截、流量录制
guard/      RoE / 越权防护
workflow/   工作流引擎
skills/     技能库（打法 / 方法论）
docs/       架构与能力文档（含 docs/features.md 完整能力清单）
```

---

## 授权与合规

本平台**仅限在获得明确书面授权的目标**上用于安全测试、红队演练与漏洞披露（如企业 SRC / 众测项目）。使用者须自行确保：

- 仅在**授权范围内**的资产上操作，遵守目标机构的测试规则与法律（如《网络安全法》等）；
- 采用**非破坏性**手段，不对目标造成业务中断、数据损毁或真实资金损失；
- 不得批量导出、留存或传播任何个人数据与敏感信息；
- 一切测试行为与后果由使用者承担。项目作者不对未授权使用负责。

漏洞请通过目标机构的正规渠道（如 Baidu SRC 等）负责任地披露。

---

## 许可

代码以 **Apache License 2.0** 分发（见 [LICENSE](LICENSE)）；第三方组件许可见 [NOTICE](NOTICE)。
