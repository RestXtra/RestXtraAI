# RestXtraAI 项目历史难点、痛点与优化方案全量复盘

> 文档范围：从项目基础运行时、安全边界、Agent Harness、上下文压缩、多 Agent 协作、工具调用、前端体验、Demo 发布到可重复性能基线的完整优化过程。
> 当前远程基准提交：`f545fc4 docs: summarize project optimization journey`
> 当前本地状态：在远程基准之上保留 Demo、宝塔部署和漏洞链路图优化，尚未提交到远程 Git。
> 说明：本文只把有代码、测试、构建或浏览器验证记录支撑的内容写成“已完成”。没有真实 TSecBenchmark 结果数据的部分，不宣称已经获得具体百分比性能提升。

## 1. 优化背景

RestXtraAI 已经具备资产管理、任务探索、漏洞发现、Agent、MCP、Skill、工作流、报告和平台治理能力，但早期实现更关注功能可用，随着任务持续时间、资产规模、Agent 数量和工具输出增长，逐渐暴露出以下系统性问题：

- 部署脚本、容器权限和数据库生命周期混在一起，存在误操作和凭据暴露风险。
- Agent 状态分散在内存、activity、transcript、工具输出和数据库图中，无法从单一事实源恢复。
- 上下文压缩停留在“缩短文本”，没有成为真正可恢复的结构化工作集。
- 多 Agent 只有并发，没有可靠的租约、依赖、资源冲突和结果交付协议。
- 工具 schema、长工具结果和重复图查询持续消耗 token 与数据库资源。
- 缺少可重复 benchmark，只能看到代码变化，无法回答优化前后是否真的更快、更省、更有效。
- 前端存在重复轮询、大页面、权限门禁分散、移动端溢出和运行状态不可观察等问题。
- 沙箱管理 API 的权限边界不够严格，可能操作同一 Docker 主机上的 PostgreSQL 等基础设施容器。

这轮优化的核心目标不是单纯提高 QPS，而是提高“结果效率”：在授权范围内，用更少 token、更少无效工具调用和更短时间获得可验证事实与漏洞，同时保证任务可恢复、可审计、可复现。

## 2. 总体优化路线

优化按依赖关系分为五层推进：

1. 先修部署、认证、密钥、RBAC、容器和宿主执行边界。
2. 再降低前端轮询、数据库查询、Agent assembly 和工具目录成本。
3. 建立 Event Log、Artifact、Working Set 和可恢复任务租约。
4. 将多 Agent 升级为带依赖、租约、资源冲突和结构化结果的工作图。
5. 最后建立 benchmark replay、cohort compare 和仪表盘可视化，形成闭环。

### 2.1 项目历史演进时间线

项目不是一次性完成，而是在“能力扩张暴露问题、建立约束、再补可观测与验证”的循环中演进：

| 阶段 | 代表提交或状态 | 当时解决的核心问题 | 留下的新挑战 |
| --- | --- | --- | --- |
| 2026-08-10：建立基线 | `4ece60b` | 搭建 Go 控制面、PostgreSQL、Agent、资产/任务/漏洞、Next.js Web、Compose 和发布骨架 | 模块规模很大，但认证、治理、恢复和性能边界尚未形成系统模型 |
| 2026-08-10：平台治理 | `b574b88`、`8ca4754` | 增加 RBAC、多用户、审计、能力切片和 UI Shell | 前后端权限容易漂移，大页面和导航复杂度上升 |
| 2026-08-16：能力与 Harness 优化 | `4907f86` 至 `8a90c97` | 增加工作流、沙箱等能力，并完成 Prompt、工具、规划、可靠性、安全边界和确定性摘要优化 | 运行态仍主要依赖内存和分散记录，缺少可恢复事实源 |
| 2026-08-17 至 08-20：产品化 | `b4b70ff` 至 `4aff3cf` | 完成平台运营、报告、代理入口、品牌、企业项目、侧栏和发布链路 | 暴露 PostgreSQL 生命周期冲突、嵌入资源不同步和部署环境差异 |
| 2026-08-21：安全与覆盖能力 | `1ed6856`、`c0101a1` | 加固 Token、Cookie、密钥、命令边界、任务租约、资产覆盖与漏洞链路 | 需要把成本、事件、上下文和 Agent 工作图真正持久化并可观察 |
| 2026-08-22 至 08-23：运行时闭环 | `7de0f73` 至 `a7210b5` | 建立成本、缓存、聚合 API、Event Log、Working Set、工具预算、意图租约、子 Agent 协议、benchmark、遥测和沙箱隔离 | 仍需真实 cohort、容量测试、生命周期治理和更强基础设施隔离 |
| 2026-08-23：本地 Demo 与交付 | 本地工作区 | 补全资产覆盖、漏洞详情、运行仪表盘、沙箱只读展示、宝塔静态发布和链路图四边路由 | Demo 必须继续与远程源码、真实 API、数据库和秘密严格隔离 |

### 2.2 为什么这批问题特别难

这些难点不是单文件 bug，而是跨越模型、数据库、工具、容器、前端和发布产物的系统问题：

- **状态有多个副本**：同一事实可能同时存在于图、Event Log、transcript、Working Set、Activity 和 UI 状态中，修复一个投影不代表整个系统一致。
- **安全与可用性相互制约**：Agent 需要执行工具，但不能继承控制面秘密；沙箱需要访问网络，但不能接触 PostgreSQL、Docker API 和云元数据。
- **并发提升吞吐，也放大冲突**：Worker 越多，重复意图、账号状态竞争、限流和目标压力越严重，不能用一个全局并发数解决。
- **性能不能只看函数耗时**：Agent 系统真正关心首个事实、首个漏洞、证据覆盖和单位成果成本，普通 QPS 无法证明结果变好。
- **源码不是最终运行产物**：前端要经过 Next.js、静态导出、嵌入目录、Go embed 和 Linux 二进制；Demo 又是另一套 Mock 静态构建，任何一层遗漏都会出现版本错位。
- **前端图形是几何问题**：节点数量、布局、边路由、缩放和移动端容器共同决定可读性，增加一个组件并不能自动得到清晰图形。

### 2.3 全量难点、痛点与修改决策总表

| 领域 | 为什么是难点 | 实际问题与痛点 | 为什么必须修改 | 采用的方案 |
| --- | --- | --- | --- | --- |
| PostgreSQL 生命周期 | 本机服务与容器都可能占用同一端口 | 5432 已占用、socket 不存在、脚本连接错误实例 | 可能导致部署失败或在错误数据库建库 | TCP 探活优先，已有实例不再启动本机 cluster |
| 工具秘密继承 | 子进程默认继承服务环境 | Agent 工具可读 DSN、API Key、Token | 提示注入或误调用可能泄露控制面凭据 | 统一 ToolEnvironment，默认过滤秘密变量 |
| 沙箱容器归属 | Docker 主机混有业务和基础设施容器 | 沙箱 API 可停止或删除 PostgreSQL/控制面 | 会造成停机和数据风险，前端隐藏按钮无效 | managed/protected 标签、inspect 二次校验、后端强制拒绝 |
| 容器权限 | 工具执行和控制面共享容器权限面 | root、可写根、默认 capabilities 扩大影响 | 一次工具逃逸会影响整个应用 | 非 root、只读根、cap-drop、tmpfs、资源限制 |
| 多用户认证 | 菜单、页面和 API 都有权限判断 | 谁能看/改/审计不一致 | 多人平台无法可信运营 | RBAC、JWT 用户身份、后端门禁、审计日志 |
| Token/Cookie/密钥 | 浏览器、数据库和配置文件有不同威胁模型 | Token 暴露、数据库 dump 可带走完整秘密 | XSS 或备份泄露影响过大 | 受控 Cookie、独立秘密层、统一 401 行为 |
| Prompt 缓存 | 动态任务态势每轮变化 | system 前缀无法缓存，输入 token 重复计费 | 长任务成本随回合数快速增长 | 静态 system，动态 Working Context 分离，assembly 缓存 |
| 上下文压缩 | 摘要本身也可能漂移 | LLM 摘要额外花费并丢失约束/证据 | 恢复后可能越权或重复探索 | 确定性摘要、原始历史外置、结构化 Working Set |
| 工具目录 | 工具数量和 schema 持续增长 | 全量 schema 占用 token，高风险工具始终可见 | 成本和误调用概率同时上升 | core/catalog/privileged 三级目录和按需披露 |
| 长工具输出 | 扫描结果可能远大于模型窗口 | 结果反复进入上下文并触发压缩 | 成本高且证据难稳定引用 | Artifact 哈希、摘要、分页读取和事件引用 |
| 无效探索 | 模型可能重复同类动作 | 重复工具、重复意图、连续零产出 | 浪费预算并对目标制造压力 | 原子去重、Reflector、stall 检测和零产出 fuse |
| Planner 唤醒 | 轮询简单但不及时 | 无变化时空转，有发现时响应慢 | 同时浪费查询和发现窗口 | 事件唤醒加低频心跳兜底 |
| 任务依赖 | 不同意图有事实和认证前置条件 | Worker 抢跑导致必然失败 | 失败会污染状态并浪费工具预算 | parent/evidence readiness 检查 |
| 进程恢复 | 内存 running 状态不能跨重启 | 崩溃后重复执行或永久卡住 | 长任务无法可靠运行 | PostgreSQL lease、续租、过期接管、watchdog |
| 多 Agent 冲突 | 并行读写的安全性不同 | 同资产、账号、限流域并发冲突 | 可能锁号、触发限流或重复写入 | shared/exclusive resource lease 和冲突事件 |
| 子 Agent 上下文 | 直接共享历史最容易实现 | transcript 和秘密在线性复制 | token 随 Agent 数增长并扩大信息暴露 | 任务契约输入、结构化结果输出、Artifact 引用 |
| 模型与工具异常 | 错误类型多且恢复策略不同 | Key 故障切换不及时、畸形 JSON 中断回合 | 出现“正常结束但零结果”的假成功 | 错误传播、Key 熔断轮换、有限 JSON 修复 |
| 证据与提示注入 | 模型会同时看到可信指令和不可信数据 | 低 ID 证据绕过、网页诱导外发或破坏命令 | 漏洞结论和工具执行都可能失真 | 全 ID 校验、证据门禁、命令分类和注入边界 |
| Canonical Event Log | 记录事件不等于能恢复 | UI、审计、成本和 benchmark 各自拼数据 | 无法判断哪份状态可信 | 追加事件流、事务写入、shadow replay、投影哈希 |
| 数据库查询 | Agent 装配涉及多类配置 | 每回合串行读取 Agent、Tool、MCP、Skill、图 | Worker 增长会放大首 token 延迟 | 查询折叠、目录缓存、snapshot 复用、版本失效 |
| 前端轮询 | 页面需要多种实时指标 | 一次刷新触发多个 API，快照时间不一致 | 请求风暴和 UI 抖动 | overview/operations-dashboard 聚合 API、分级轮询 |
| 资产与漏洞可解释性 | 列表不能表达覆盖和证据关系 | 用户看不到未覆盖资产和漏洞来源 | 无法判断任务是否真的完成 | 资产覆盖图、漏洞摘要/证据/报告/lineage 接口 |
| 大前端模块 | 数据、权限、图表和弹窗耦合 | 修改一处容易引发全页回归 | 维护和审查成本持续上升 | Tab 拆分、共享组件、统一 API client 和 Context |
| 沙箱主机页面 | 滚动、加载和导航共享布局状态 | 进入后像卡住，难以切换页面 | 属于阻断核心操作的体验故障 | 独立滚动区、局部错误、`min-w-0/min-h-0` |
| 侧栏响应式 | 折叠与展开几何不同 | 图标错列、裁剪、Logo 位置不一致 | 高频导航直接受影响 | 两套几何约束并分别做视觉验证 |
| 链路图边路由 | 默认节点只有上下锚点 | 线都挤在上下边并大量交叉 | 证据链难读，图失去解释价值 | 四边中点锚点、按相对位置选边、smoothstep 路由 |
| 嵌入式发布 | `.next`、`out`、embed 目录和 ELF 是四层产物 | 源码已改但运行二进制仍是旧页面 | 用户无法获得实际修改 | 正式静态构建、同步 dist、`-tags embedui` 重建和哈希/ELF 验证 |
| Demo 与正式版本 | 两者使用同一源码但不同数据/认证 | Mock 可能被误打进正式产物，Demo 可能请求真实 API | 会泄露能力、秘密或污染正式版本 | 正式与 Mock 分两次构建，Demo Nginx 拒绝 `/api/` |
| Demo 远程 Git | 源码仓库和演示数据的发布边界不同 | Demo 曾误提交到远程主分支 | 不符合“Demo 本地维护”的交付边界 | `force-with-lease` 精确撤回，保留本地备份分支和工作区 |
| Demo 云部署 | 宝塔服务器不应保存完整源码和密钥 | Git clone/Docker 路径过重且扩大攻击面 | 纯展示无需后端，却承担后端风险 | 只上传 `web/out` ZIP，版本目录、软链接、静态 Nginx、HTTPS |
| Benchmark 可信度 | LLM 和目标环境具有随机性 | 单次 before/after 或 microbenchmark 会误导 | 无法证明优化带来真实收益 | 版本化 scenario、replay、cohort p50/p95、环境哈希和严格比较 |

## 3. 部署、数据库与容器安全问题

### 3.1 部署脚本会错误管理 PostgreSQL

**问题与痛点**

`run-linux.sh` 曾默认尝试启动本机 PostgreSQL cluster。当 Docker 中已经有 PostgreSQL 映射到 5432 时，本机 cluster 会因端口冲突启动失败，后续脚本仍使用本地 Unix socket 执行 `psql`，最终出现“端口已占用”和“socket 不存在”交替发生的故障。

这会导致：

- 同一台机器出现两个 PostgreSQL 生命周期管理者。
- 部署行为依赖机器当前状态，难以复现。
- 脚本可能在错误实例上创建角色和数据库。
- 用户看到的是数据库连接失败，但真正根因是启动策略冲突。

**解决方法**

- 启动前先使用 `pg_isready` 探测 `127.0.0.1:5432`。
- 已有实例响应时，直接使用现有数据库，不再启动本机 cluster。
- 只有 TCP 探测失败时才进入本机服务兜底路径。
- Docker Compose 中 PostgreSQL 只绑定 `127.0.0.1`，避免直接暴露到外网。

**验证**

- 对已有 TCP 5432 实例和无实例两条路径分别检查。
- Compose 配置通过 `docker compose config --quiet`。

### 3.2 应用工具进程可能继承数据库和 LLM 密钥

**问题与痛点**

Agent 的 Bash、自定义脚本或子任务如果直接继承服务进程环境，会同时拿到 `RESTXTRA_PG_DSN`、LLM API Key、Token、Password 等部署凭据。工具一旦被提示注入或误调用，就可能读取控制面密钥。

**解决方法**

- 建立统一 `ToolEnvironment`，默认过滤名称匹配 `API_KEY`、`TOKEN`、`SECRET`、`PASSWORD`、`PRIVATE_KEY`、`CREDENTIALS`、`DSN` 的环境变量。
- `RESTXTRA_ALLOW_TOOL_SECRET_ENV` 默认关闭，仅作为显式兼容开关。
- WSL 执行同时移除不可用的 localhost 代理和宿主 CA 环境。
- 测试验证宿主和 session 两类秘密都不会进入工具环境。

### 3.3 沙箱可以管理 PostgreSQL 等非沙箱容器

**问题与痛点**

沙箱容器页面会列出 Docker 主机上的所有容器，但早期启停、重启、kill、remove 和“删除全部”接口只信任容器 ID，没有验证容器归属。只要拿到 PostgreSQL 容器 ID，就可能通过沙箱 API 停止或删除项目数据库。

这会造成：

- 数据库意外停机或数据卷关联容器被删除。
- RestXtra 控制面可以被自己的沙箱管理功能停止。
- “删除全部”语义实际是删除整台 Docker 主机的所有容器。
- 前端隐藏按钮也无法形成安全边界，API 仍可被直接调用。

**解决方法**

- 每次容器生命周期操作前调用 Docker inspect。
- 只允许管理 `sandbox.managed=true` 的容器。
- PostgreSQL 和 RestXtra Compose 服务增加 `restxtra.protected=true`。
- 受保护容器即使误加 `sandbox.managed=true` 也拒绝操作。
- “删除全部”通过 Docker label filter 只查询受管容器，并逐个再次 inspect。
- 容器 ID 使用严格格式校验，阻止路径注入。
- 新沙箱强制只读根、`cap-drop ALL`、`no-new-privileges` 和受管标签，旧客户端不能通过传 `false` 降级。
- 网络模式只允许 `bridge` 或 `none`，拒绝 `host`、Compose 网络名和 `container:postgres`。
- 禁止向沙箱传入 `RESTXTRA_PG_DSN`、`DATABASE_URL`、`POSTGRES_*`、`PG*`、`DOCKER_HOST` 等环境变量。
- 前端对非受管容器只显示“仅查看”，不允许选择或执行动作。

**验证**

- 模拟 Docker API，验证受管容器可操作、PostgreSQL 容器被拒绝、受保护容器被拒绝。
- 覆盖路径注入、网络模式、数据库变量和强制安全配置测试。
- `go test -race ./server` 通过。

### 3.4 容器本身权限过大

**问题与痛点**

应用容器如果使用可写根目录、默认 capabilities 或 root 用户，Agent 工具和第三方依赖的影响范围会扩大。

**解决方法**

- RestXtra 镜像使用非 root 用户运行。
- Compose 设置只读根文件系统、受限 tmpfs、`cap_drop: ALL`、只按需要增加 `NET_RAW`。
- 启用 `no-new-privileges`，限制内存、CPU 和 PID。
- 数据、JWT key 和 transcript 只通过明确持久化目录保存。

## 4. 认证、授权与秘密管理问题

### 4.1 单一密码和分散门禁无法支撑多人平台

**问题与痛点**

早期认证更接近单管理员模式，前端菜单隐藏、页面门禁和后端 API 权限没有形成一致模型。多人使用时无法明确回答“谁能看、谁能改、谁做了什么”。

**解决方法**

- 建立 users、roles、permissions、role_permissions、user_roles 和 audit_logs。
- JWT 增加用户身份 claim，登录迁移到用户表。
- 后端路由统一执行 RBAC，不能依赖前端隐藏。
- 系统角色播种 admin、operator、auditor、viewer。
- 拒绝操作记录 actor、权限和资源信息。
- 前端根据 `/api/platform/my` 的角色与权限生成菜单和 PermissionGate。

### 4.2 前端重复请求当前用户

**问题与痛点**

Sidebar、PermissionGate 和多个页面各自请求当前用户，导致相同 API 重复调用、首屏闪烁和权限状态短暂不一致。

**解决方法**

- 用共享 CurrentUser Context 集中请求和缓存用户信息。
- 所有权限组件消费同一个状态投影。
- 未加载、未授权和管理员绕过行为统一处理。

### 4.3 Token、Cookie 和配置密钥边界不清晰

**问题与痛点**

认证 token 如果暴露给前端脚本或存储方式不一致，会扩大 XSS 后的影响；第三方服务密钥全部放入 PostgreSQL，也意味着单独泄露数据库备份就能获得完整凭据。

**解决方法**

- 认证改为受控 Cookie 路径并补 Cookie 行为测试。
- API 401 统一跳转登录，后端仍作为最终授权者。
- 敏感配置使用独立秘密存储和加密/密钥文件，避免数据库 dump 单独构成完整泄露。
- MCP、自定义工具、空间搜索等凭据读取统一经过权限和秘密层。

## 5. Agent Harness、上下文和 Token 问题

### 5.1 动态 system prompt 破坏前缀缓存

**问题与痛点**

每轮把任务态势、图快照和动态内容写入 system prompt，会导致前缀不断变化，模型缓存无法命中；即使只变一个事实，也可能重复计费整段静态指令。

**解决方法**

- system prompt 保持静态。
- 动态态势放到首轮 user/context 区域。
- 无 deferred block 时仍标记静态缓存边界。
- 将 Agent、工具和 MCP assembly 分层缓存。

### 5.2 上下文压缩会额外调用模型并发生摘要漂移

**问题与痛点**

使用 LLM 生成摘要会产生额外 token 和延迟；多次压缩后摘要会递归膨胀，并可能丢失授权范围、证据引用、未完成依赖或工具错误。

**解决方法**

- 引入 Deterministic Summarizer，不调用 LLM。
- 按角色、工具名、结果头和长度进行确定性压缩。
- 使用精确 tokenizer 估算上下文，控制 ToolResultBudget。
- 完整原始历史外置保留，压缩结果作为替代历史而不是覆盖原始证据。

### 5.3 Working Set 只存储但没有真正用于恢复

**问题与痛点**

仅把摘要写入数据库不能称为恢复。Planner/Worker 重启后如果仍重新注入完整 transcript，既浪费 token，也可能因为再次总结产生语义漂移。

**解决方法**

- 持久化结构化 Working Set，而不是单段文本。
- 固定层保存目标、授权、关键约束、事实和风险策略。
- 滑动层保存最近意图、未完成依赖、工具错误和待验证证据。
- 外置层只保留 artifact/event 引用。
- Planner/Worker 恢复时真正装配 Working Set，不重新注入完整 transcript 或旧 ModelSummary。
- 实时探索图优先于历史快照，并记录 `working_set_restored` 事件。

### 5.4 工具 schema 一次性全部进入上下文

**问题与痛点**

工具数量增加后，全量 schema 会占用大量输入 token。高风险工具如果始终可见，也会增加误调用概率。

**解决方法**

- 建立三级工具目录：core、catalog、privileged。
- core 工具始终提供 schema；低频 catalog 只先暴露名称、说明和成本；privileged 只有技能激活后可见。
- 工具元数据包含 token 成本、延迟等级、副作用、并发类别和 artifact 策略。
- 使用 SearchExtraTools/渐进披露按需解锁 schema。
- Agent 工具目录和 assembly catalog 增加缓存。

### 5.5 长工具输出反复挤入上下文

**问题与痛点**

nmap、HTTP 响应、日志和扫描结果可能很长。如果每轮原样注入，会迅速触发 compaction，并让模型反复读取已经处理过的内容。

**解决方法**

- 大输出落为 Artifact。
- Artifact 记录内容哈希、MIME、行数、摘要、来源工具、命令和权限标签。
- 上下文只放摘要与引用，需要时分页读取原文。
- 工具 capture 路径统一生成 `artifact_created` 事件。

### 5.6 重复工具调用和无效探索持续消耗预算

**问题与痛点**

模型可能以相同参数连续调用同一工具，或在已经证明零产出的方向上继续派发意图，导致 token、目标请求和时间被浪费。

**解决方法**

- 同工具同参数连续达到阈值时拦截。
- Worker 零写回时触发一次 Reflector，要求落地事实、负面结果或明确失败原因。
- 任务级记录连续零产出意图并发出 stall 警告。
- 对重复零产出意图类别建立 fuse，Planner 不再继续派发同类工作。
- 原子去重意图，避免并发 Planner 创建等价任务。

## 6. Agent 可靠性与决策质量问题

### 6.1 Planner 依赖固定轮询，发现不能及时触发规划

**问题与痛点**

只靠固定周期轮询会造成两种浪费：没有变化时持续查询；出现新漏洞或 Worker 完成时又不能立即规划。

**解决方法**

- 使用 finding、done、图变化事件唤醒 Planner。
- 保留低频心跳作为丢事件兜底。
- 新发现通过 notifyFinding 触发即时规划。
- 任务详情和仪表盘使用聚合接口降低轮询请求数。

### 6.2 Worker 开场重复普查

**问题与痛点**

每个 Worker 拿到意图后再次做全局资产枚举，会造成重复工具调用、目标压力和 token 消耗，也削弱 Agent 隔离。

**解决方法**

- Worker 输入限定为 objective、asset_ids、required_evidence、budget 和 allowed_tools。
- 禁止前置全局普查，直接从意图绑定资产开始。
- 自动注入任务 scope 和资产上下文。
- 子 Agent 输出限定为 facts、findings、negative_results、artifact_refs、next_actions 和 usage。

### 6.3 依赖链任务抢跑

**问题与痛点**

依赖认证态、账号或上游事实的意图如果提前被 Worker claim，会产生必然失败的工具调用，并可能污染目标状态。

**解决方法**

- IntentReady 检查 parent intent 和证据要求。
- 未满足依赖的意图不进入 claim 候选集。
- 同账号、同限流域和写操作通过资源租约串行化。

### 6.4 运行态只在内存，进程重启后任务重复执行

**问题与痛点**

内存中的 running 标记无法跨进程恢复。崩溃后既不知道工作是否完成，也无法判断是否应该重试，可能导致重复扫描或永久卡住。

**解决方法**

- 使用 PostgreSQL intent lease，记录 owner、lease_expires_at、attempt_count 和状态。
- 使用 `FOR UPDATE SKIP LOCKED` 原子 claim。
- Worker 定期续租；正常结束释放；过期后允许其他 Worker 接管。
- watchdog 对长期 idle Worker 执行 cancel、requeue，超过重试上限转 blocked。

### 6.5 多 Agent 只有并发，没有冲突协调

**问题与痛点**

不同 Worker 可能同时操作同一资产、账号或限流域。只设置 worker 数量无法区分安全的并行读取和危险的并发写入。

**解决方法**

- 建立 `agent_resource_leases`。
- 支持 shared/exclusive 两种租约。
- 自动生成 `asset:*` 资源键，并支持 account_scopes、rate_limit_domains。
- claim 时跳过冲突意图，续租和释放与 intent lease 同步。
- 记录 `resource_conflict_wait`、lease acquired/released 事件并去重。

### 6.6 子 Agent 共享整段上下文

**问题与痛点**

父子 Agent 共享完整 transcript 会线性放大 token，并把不相关秘密、错误和工具输出传播到其他工作单元。

**解决方法**

- 建立结构化 delegation 数据模型。
- 子 Agent 只接收任务契约，不接收父 Agent 全历史。
- 结果通过结构化协议回传，并持久化 usage、状态与 artifact 引用。
- 父 Agent只吸收结论和引用。

### 6.7 模型错误被吞掉或 Key 切换不及时

**问题与痛点**

单 Key 场景下网络、500 或解析错误如果被当成正常结束，会产生“任务正常结束但没有结果”的假象；多 Key 场景如果熔断游标不及时推进，则无法真正故障切换。

**解决方法**

- 非可切换错误原样返回。
- 401、403、429 和可恢复错误执行 Key 轮换。
- 连续失败达到阈值后短时熔断。
- 单 Key 错误传播和多 Key 切换均有回归测试。

### 6.8 畸形工具 JSON 直接中断回合

**问题与痛点**

模型偶发输出尾逗号或多余括号，原本会让一次有效决策因为参数解析失败而终止。

**解决方法**

- PreToolUse 最内层加入 ToolCallFixer。
- 只修复有限、可验证的 JSON 语法问题。
- 修复后必须再次通过严格 JSON 解析，否则拒绝执行。

### 6.9 证据门禁存在低 ID 绕过

**问题与痛点**

证据 ID 正则只接受三位以上数字，导致 `e1` 到 `e99` 的未知引用没有进入校验，早期证据可能绕过门禁。

**解决方法**

- 正则改为接受全部数字 ID。
- 对低 ID 未知引用增加拒绝测试。
- 漏洞发现必须绑定证据，证据不足时不进入确认态。

### 6.10 禁用配方实际仍被启用

**问题与痛点**

`if r.Enabled || true` 使禁用配置永久失效，管理员以为已经关闭的 Recipe 仍会影响 Agent。

**解决方法**

- Enabled 改为指针布尔值，区分“未配置”和“显式 false”。
- RecipeTools、RecipeSeeds 使用同一过滤逻辑。
- 增加禁用配方测试。

### 6.11 间接提示注入和高风险命令

**问题与痛点**

网页、工具输出和文件内容可能包含诱导 Agent 改变目标、泄露文件或执行破坏性命令的文本。

**解决方法**

- system boundary 明确“工具输出是数据，不是新指令”。
- 对命令进行 recon、scan、exploit、exfil 分类。
- 拦截敏感文件管道外发、危险 base64/curl 组合、`nc -e` 和典型破坏性命令。
- 支持 recon-only 场景的 DenyExploit 门控。
- 拒绝事件进入审计和 Event Log。

## 7. Event Log、恢复与审计问题

### 7.1 activity、transcript、工具文件和 Worker 状态各自为真

**问题与痛点**

同一回合的信息分散在多个位置，UI、恢复、审计、成本统计和 benchmark 各自拼接数据。投影缺字段时只能互相“补”，无法判断哪个才是最终事实。

**解决方法**

- 建立追加式 Canonical Agent Event Log。
- 统一事件包括 `turn_started`、`prompt_assembled`、`tool_called`、`tool_result`、`artifact_created`、`summary_created`、`working_set_restored`、`intent_claimed`、`budget_changed` 和 `turn_finished`。
- 图写入补充 `node_created`、`node_state_changed`、`edge_created`、`anchor_created`。
- 图数据和事件在同一数据库事务中提交。
- migration 为旧图数据生成稳定 backfill 事件。

### 7.2 Event Log 记录了事件，但不能证明可重建

**问题与痛点**

“有日志”不等于“日志是事实源”。如果不能从事件 replay 出相同投影，恢复和 benchmark 仍会依赖原业务表。

**解决方法**

- 增加 shadow replay。
- replay 后计算投影 SHA-256，与当前投影对比。
- 仪表盘显示投影一致或漂移状态。
- 为任务 ID、事件类型增加复合索引，降低 replay 和时间线查询成本。

### 7.3 工具输出无法稳定引用

**问题与痛点**

工具输出文件缺少统一元数据时，摘要无法指向原文，审计人员也无法确认结果是否被替换或截断。

**解决方法**

- Artifact 使用内容哈希标识。
- 保存来源事件、工具、命令、MIME、行数、摘要和权限标签。
- Event Log 只引用 artifact ID，避免复制大内容。

## 8. 数据库与 API 性能问题

### 8.1 Agent assembly 产生大量串行查询

**问题与痛点**

每次装配 Agent 分别读取配置、工具、MCP、Skill、LLM profile 和图状态，随着 Agent 数量增长，回合开始时间被数据库 round trip 放大。

**解决方法**

- 合并 assembly 查询。
- 对稳定目录建立缓存和版本失效机制。
- 同一次装配复用 Agent snapshot，工具解析不再重复查库。

### 8.2 graph_overview 重复构建

**问题与痛点**

同一图版本下 Planner、Worker 和工具查询重复生成概览，既消耗数据库资源，也重复占用 token。

**解决方法**

- 使用 exploration version 作为缓存键。
- 图变更时才失效。
- recent facts 和 intents 使用有界数量，并保留总数信息。

### 8.3 页面一次刷新触发多个 API

**问题与痛点**

任务概览和仪表盘分别请求任务、成本、事件、Working Set、租约和投影，轮询时形成请求风暴，且不同请求完成时间不同会造成 UI 快照不一致。

**解决方法**

- 任务详情增加聚合 overview。
- 仪表盘增加 `GET /api/tasks/{id}/operations-dashboard`。
- 一次返回 baseline、round costs、Event Log、Working Set、资源租约和 projection 状态。
- 快速、慢速和企业统计采用不同轮询周期。

### 8.4 Finding、资产覆盖和链路数据缺少统一接口

**问题与痛点**

早期漏洞列表只能看到标题，用户无法从发现快速进入摘要、证据、报告和来源链路；资产页也无法看到哪些资产已经验证、哪些仍未覆盖。

**解决方法**

- 建立 Finding 详情、证据引用、Markdown 报告和 lineage graph。
- 建立任务资产覆盖图，展示资产类型、验证状态、关联事实和漏洞。
- 覆盖率计算进入数据库/API，前端不再临时拼接。
- 任务详情增加 Coverage Graph tab 和 Finding lineage 视图。

## 9. 前端体验与模块问题

### 9.1 沙箱主机页面访问后像“卡住”

**问题与痛点**

管理页面包含长列表、同步加载和布局溢出时，主内容区域可能占满滚动上下文，用户进入后难以继续导航；请求失败时缺少局部错误恢复也会让页面表现为一直加载。

**解决方法**

- 主内容容器统一 `min-w-0`、`min-h-0` 和受控 overflow。
- 页面请求使用局部 loading/error 状态，不阻塞全局导航。
- Sidebar 内容与主内容使用独立滚动区。
- 桌面和移动端分别验证导航、横向溢出和卡片边界。

### 9.2 大页面职责过多

**问题与痛点**

任务详情、资产、漏洞、仪表盘和设置页面同时承担数据请求、状态计算、表格、图、弹窗和权限判断，修改一个功能容易影响整个页面，也增加 TypeScript 编译和代码审查成本。

**解决方法**

- 任务详情按 Overview、Assets、Coverage Graph、Findings、Sessions、Intercept、Report 拆分 tab 组件。
- API 请求收敛到 `api-client.ts` 和 `api.ts`。
- 状态 Badge、分页、Markdown、PermissionGate、Agent Editor 等抽为共享组件。
- 当前用户和主题状态使用共享 Context/Provider。

### 9.3 侧栏折叠态与展开态使用同一布局假设

**问题与痛点**

折叠侧栏中图标可能被裁剪、错列或被导航区挤出；展开态 logo 的尺寸调整也不能直接套用到折叠态。

**解决方法**

- 折叠态和展开态分别定义 logo/图标几何。
- 导航区使用 `min-h-0 overflow-y-auto`，底部入口 `shrink-0`。
- 图标列统一 padding 基准。
- 视觉验证分别检查两种状态，而不是只检查源码。

### 9.4 源码改了，但运行程序仍显示旧页面

**问题与痛点**

Go 二进制使用 `//go:embed all:webui/dist`。只运行 Next.js build 或只检查 `.next`，不会更新实际运行二进制中的页面，因此会出现“源码和构建目录已经更新，但部署后仍是旧 logo/旧 UI”。

**解决方法**

固定发布链路：

1. 在 `web` 设置 `NEXT_EXPORT=1` 并执行静态构建。
2. 将 `web/out` 完整同步到 `server/webui/dist`。
3. 对关键 HTML/资源计算 SHA-256，确认两侧一致。
4. 使用 `-tags embedui` 重建 Linux 二进制。
5. 检查 ELF magic、大小和 SHA-256。

### 9.5 移动端性能区块可能横向溢出

**问题与痛点**

Agent 成本、事件流和多个数字指标在桌面三栏正常，但移动端容易产生水平滚动、文字覆盖或卡片被撑宽。

**解决方法**

- 固定卡片内网格在移动端纵向排列。
- 数值、进度条和事件行使用稳定宽度与截断策略。
- 在 `390x844` 实测三段顺序、卡片边界和 `scrollWidth == clientWidth`。

### 9.6 漏洞链路图连线集中在上下边，关系难以阅读

**问题与痛点**

早期 lineage 节点使用 React Flow 默认节点，连接点主要落在上、下边。图中同时存在横向、纵向、回溯和跨列关系时，边会先挤到相同锚点，再以曲线绕回目标，造成大量交叉、重叠和穿越节点。节点数量增加后，用户虽然能看到“有一张图”，却很难沿线确认漏洞由哪些资产、事实和意图推导而来。

**为什么必须修改**

漏洞链路图承担证据解释和审计职责。连线混乱不只是视觉问题，它会增加误读攻击路径、遗漏上游证据和错误判断因果关系的风险，削弱 Finding 详情的核心价值。

**解决方法**

- 将默认节点改为自定义 LineageNode。
- 每个矩形在上、右、下、左四条边的正中间分别提供 source 和 target Handle，两者重叠显示为一个连接点。
- 根据源节点与目标节点的相对坐标选择连接边：横向关系使用左/右边，纵向关系使用上/下边。
- 边使用 `smoothstep` 正交平滑路由，减少无意义弧线和节点穿越。
- MiniMap 从节点数据读取类型颜色，避免自定义节点后缩略图退化为单色。

**验证**

- TypeScript 与 Biome 检查通过。
- 正式和 Mock 静态构建各生成 58 个页面。
- 浏览器实测 14 个节点、14 条边；每个节点包含四个可见边中点，内部 source/target 共 8 个 Handle。
- DOM 几何确认锚点位于四边正中间，横向和纵向路径分别使用对应边。
- 页面控制台无 warning/error，Demo 详情页返回 200。

### 9.7 Demo 必须完整展示新能力，但不能连接真实系统

**问题与痛点**

项目功能持续更新后，旧 Demo 缺少资产覆盖图、漏洞详情、Event Log、Working Set、Agent 成本、性能基线、RBAC 和沙箱权限边界。直接部署正式系统又需要 PostgreSQL、LLM、Skills、MCP 和真实认证，既不便于展示，也会扩大服务器攻击面。

**为什么必须修改**

Demo 是用户理解产品能力的第一入口。如果界面落后于正式项目，会造成“功能不存在”的错误印象；如果为展示而部署完整控制面，则需要管理数据库、密钥、容器和真实 API，风险与收益不匹配。

**解决方法**

- 使用 `NEXT_PUBLIC_MOCK=1` 编译独立静态 Demo。
- Mock 数据覆盖资产、Finding、lineage、operations dashboard、RBAC、审计和沙箱资源。
- PostgreSQL 与控制面容器在 Demo 中保留可见性，但标记为 protected/只读，不提供操作按钮。
- 使用本地 Geist 字体，避免构建依赖 Google Fonts 网络可用性。
- Nginx 对 `/api/` 固定返回 404，证明 Demo 不会回退连接真实后端。
- 正式构建和 Mock 构建分开执行，正式嵌入目录只接收非 Mock 产物。

### 9.8 Demo 不应进入远程 Git，云端只接收静态发布包

**问题与痛点**

Demo 更新曾被提交并推送到远程 `main`。这与用户要求的“远程仓库只保留正式项目，Demo 本地维护”冲突。原部署教程又以服务器 `git clone` 和 Docker build 为主，意味着云服务器会获得完整源码、构建工具和更大的运行面。

**为什么必须修改**

Demo mock、演示数据和部署配置具有不同于正式源码的生命周期。把它们混入主分支会污染正式发布；让宝塔服务器持有源码、Git 凭据或后端依赖，也违反最小暴露原则。

**解决方法**

- 使用 `git push --force-with-lease` 只撤回误推送的最新 Demo 提交，不改写更早正式历史。
- 将本地 `main` 和 `origin/main` 恢复到 `f545fc4`。
- 创建 `codex/demo-local` 备份分支，同时把 Demo 改动保留为本地工作区文件。
- 部署改为“本地 Mock 静态构建 -> `web/out` ZIP -> 宝塔上传 -> Nginx 静态托管”。
- 云端使用 `releases/<timestamp>` 和 `current` 软链接实现原子切换与回滚。
- 安全组只开放 22、80、443；不开放 5432、2375、2376、8080、8787。
- 宝塔不创建数据库，不部署 Go/Node/Docker，不上传 `.git`、`.env`、Skills、MCP 或密钥。

**验证**

- 本地 `HEAD` 与 `origin/main` 均为 `f545fc45bf06bdee8fa569cda0b2dec5469757f6`。
- 本地备份分支 `codex/demo-local` 存在。
- Demo 容器运行于 `127.0.0.1:18080`，状态 healthy、只读根、非 root 用户 101。
- 关键页面返回 200，`/api/platform/my` 返回 404。

## 10. 可观测性和性能基线问题

### 10.1 只有 QPS/耗时，没有 Agent 结果效率

**问题与痛点**

普通 microbenchmark 能告诉我们函数更快，但不能说明 Agent 是否更早发现事实、是否减少无效工具调用、是否提高资产覆盖。

**解决方法**

- 任务 baseline 记录首个有效事实、首个确认漏洞和任务完成时间。
- 记录 input/output/cache-read/tool-output token、工具调用、错误、重试和成本。
- 记录资产覆盖率、已验证率、证据覆盖率、重复意图率和零产出意图。
- 记录 fuse、资源冲突和 Working Set 恢复事件。

### 10.2 Benchmark 无法重复执行

**问题与痛点**

手工点击任务、临时修改目标和直接比较日志无法形成可信基线。不同时间执行的输入、授权范围和采样时点不同，结果不可比较。

**解决方法**

- 定义版本化 benchmark scenario JSON。
- replay runner 使用固定任务、授权目标、等待条件、采样间隔和超时。
- Token 只从环境变量读取，不写入场景和结果文件。
- 结果写入结构化 JSON，记录 scenario hash、服务版本和原始指标。

### 10.3 Before/After 结果缺少统计比较

**问题与痛点**

单次 before 和 after 的差异可能来自随机性，不能据此判断优化有效。

**解决方法**

- compare 工具按 cohort 聚合多次 replay。
- 输出 p50/p95、绝对变化、相对变化和样本数量。
- 对结果效率、token、工具调用、覆盖率、重试和零产出统一比较。
- 本地结果目录加入 `.gitignore`，提交方法和代码，不提交带环境差异的结果。

### 10.4 性能和运行事件不可见

**问题与痛点**

即使后台已经记录成本和事件，用户仍需要查数据库或日志才能理解任务为什么慢、哪个 Agent 最贵、是否在等待冲突。

**解决方法**

- 仪表盘增加“Agent 运行与性能”。
- 展示首事实、首漏洞、完成时间、证据覆盖和资产验证率。
- 展示分 Agent token、回合、工具调用和错误率。
- 展示 Canonical Event Log 时间线和事件类型计数。
- 展示 Working Set 版本、资源冲突等待、活动租约和 replay 投影一致性。

## 11. 优化实施过程中遇到的工程问题

### 11.1 本机 PostgreSQL 未运行

**痛点**

真实服务依赖 PostgreSQL，数据库未运行时程序无法保持启动，浏览器无法直接使用真实数据验证仪表盘。

**处理方法**

- 数据库逻辑通过测试数据库路径和 PostgreSQL 集成测试验证。
- 前端视觉检查使用临时只读 API stub，不写项目数据。
- 最终报告明确说明没有完成真实本地数据库联调，不能把 stub 结果当作生产性能数据。

### 11.2 Windows 与 Unix 构建命令不兼容

**痛点**

`NEXT_EXPORT=1 next build` 是 Unix 环境变量语法，在 PowerShell 中失败；带通配符的 `-LiteralPath` 复制也会失败。

**处理方法**

- PowerShell 使用 `$env:NEXT_EXPORT='1'`，构建后在 finally 中清理变量。
- 使用 `Get-ChildItem -LiteralPath` 枚举后再 `Copy-Item`。
- 发布过程增加路径边界检查，避免清理或复制到工作区之外。

### 11.3 Go 用户级缓存目录无写权限

**痛点**

构建和测试可能因 `C:\Users\...\go\pkg\mod` 或系统 `go-build` 缓存无法写入而失败，看起来像代码编译错误。

**处理方法**

- 每类测试使用工作区内独立 `GOCACHE`。
- 完成后安全删除对应缓存目录。
- 构建仍可能输出 module stat cache 警告，但必须以退出码、ELF magic 和最终 SHA-256 判断是否成功。

### 11.4 并行测试与 vet 争用工具链

**痛点**

同时运行 `go test ./...` 和 `go vet ./...` 时，曾出现标准库 vet 进程退出，但项目包测试全部通过的情况。

**处理方法**

- 竞态测试和 vet 使用独立 GOCACHE。
- 并行阶段结束后串行重跑 `go test ./...`。
- 只有串行全量测试退出码为 0 才算最终通过。

### 11.5 Git 工作区存在用户 demo 改动

**痛点**

`web/src/lib/mock/data.ts` 和 `web/src/lib/mock/handler.ts` 是用户正在进行的 demo 修改。如果使用无差别 `git add -A` 或回滚，很容易把 demo 一起提交或覆盖。

**处理方法**

- 每轮先执行 `git status --short` 和 `git diff --numstat`。
- 显式列出本次实现文件进行暂存。
- 提交前检查 `git diff --cached --name-only` 和 `--stat`。
- 推送后确认工作区只剩两个 demo 文件。

### 11.6 `.git` 和用户配置目录权限警告

**痛点**

受限环境中 Git 无法读取全局 ignore，或不能创建 `.git/index.lock`，会中断暂存但不影响源码。

**处理方法**

- 将全局 ignore 警告与仓库错误区分处理。
- 需要写 Git 元数据时使用明确授权的 `git add`、`git commit` 和 `git push`。
- 不使用 `reset --hard`、`checkout --` 等破坏性命令。

### 11.7 前端源码构建成功不等于运行产物成功

**痛点**

Next.js、静态导出、嵌入目录和 Go ELF 是四个不同阶段，任一阶段遗漏都会让运行程序继续使用旧资源。

**处理方法**

- 每次前端改动都执行完整四阶段发布链路。
- 比较关键 HTML/图片哈希。
- 最终确认 ELF magic `7F-45-4C-46`。

### 11.8 正式构建与 Demo 构建会互相覆盖 `web/out`

**痛点**

正式静态版和 Demo 都输出到 `web/out`，但 Demo 通过 `NEXT_PUBLIC_MOCK=1` 将认证、API handler 和演示数据编译进浏览器 bundle。如果先构建 Demo，再直接把当前 `out` 同步到 `server/webui/dist`，正式 Go 二进制就可能意外内嵌 Mock 版本。

**处理方法**

- 明确构建顺序：先清除 `NEXT_PUBLIC_MOCK`，生成正式静态产物并同步 embed 目录、重建 ELF；随后再生成 Mock `web/out` 用于 Demo ZIP 或容器。
- 每次构建使用 `try/finally` 清理环境变量，避免污染后续命令。
- 在 bundle 中检查 Mock 标识和链路图新代码，在嵌入目录中单独检查正式产物。
- 不用当前 `web/out` 的存在推断它属于哪个发布形态，必须记录最后一次构建模式。

### 11.9 PowerShell 发布包变量和 ZIP 目录层级容易出错

**痛点**

PowerShell 字符串插值、当前目录和 `Compress-Archive -Path` 组合容易生成名为 `restxtra-demo-$release.zip` 的字面量文件，或者把 `out/` 目录本身套进 ZIP。前者无法对应服务器版本号，后者会让 Nginx 根目录下找不到 `index.html`，表现为白屏或 404。

**处理方法**

- 在双引号中构建发布包名，并在打包后用 `Get-Item` 输出完整路径、大小和时间。
- 打包 `web/out/*`，不是打包 `web/out` 目录本身。
- 上传前检查 ZIP 第一层直接包含 `index.html`、`_next/`、`dashboard/`、`function/` 和 `sandbox/`。
- 云端解压后用 `test -f releases/<version>/index.html` 和关键子页面进行发布门禁。
- 对误生成的包先核对路径和用途，再决定清理，避免使用宽泛递归删除。

### 11.10 撤回远程提交后，`git log --all` 仍可能显示该提交

**痛点**

为了保护本地 Demo，撤回远程提交前创建了 `codex/demo-local`。因此 `git log --all` 仍会显示 Demo 提交 `295f6ef`，容易被误判为“远程撤回失败”。

**处理方法**

- 判断远程边界使用 `git rev-parse origin/main` 和 `git log origin/main`，不能使用 `git log --all`。
- 判断正式本地分支使用 `git rev-parse HEAD`。
- 判断 Demo 是否保留使用 `git branch --list codex/demo-local` 和工作区状态。
- 三者分别核对：远程正式历史、本地正式分支、本地 Demo 备份不是同一个概念。

### 11.11 页面编译成功不代表图形可读

**痛点**

TypeScript、Biome 和 Next.js build 只能证明代码类型、格式与构建有效，不能证明 React Flow 锚点真的位于四边中点、边没有集中交叉、MiniMap 有颜色或移动端没有溢出。

**处理方法**

- 浏览器打开实际 Finding lineage 页面，而不是只读取源码。
- 读取每个节点和 Handle 的 `getBoundingClientRect()`，验证四边中心坐标。
- 检查节点数、边数、每节点 Handle 数和 SVG path。
- 同时检查截图、控制台错误、HTTP 状态和容器健康。
- 对资产覆盖图、侧栏折叠态和移动端沿用同样的视觉/几何验证原则。

## 12. 最终验证体系

当前优化使用以下分层验证：

| 层级 | 验证内容 | 典型命令或方法 |
| --- | --- | --- |
| Go 单元/集成 | DB、Agent、Server、Benchmark 行为 | `go test ./...` |
| 并发安全 | 租约、事件、Server 共享状态 | `go test -race ./db ./agent ./server ...` |
| 静态检查 | Go 可疑代码 | `go vet ./...` |
| 前端类型 | API 类型和组件 | `npx tsc --noEmit` |
| 前端构建 | 正式与 Mock 两种 Next.js 静态导出 | 清除/设置 `NEXT_PUBLIC_MOCK` 后分别执行 `npm run build:static` |
| Compose | 部署配置有效性 | `docker compose config --quiet` |
| 嵌入一致性 | `web/out` 与 `server/webui/dist` | SHA-256 对比 |
| 发布二进制 | Linux 单二进制有效 | ELF magic、大小、SHA-256 |
| 视觉验证 | 桌面/移动布局、溢出、重叠 | 浏览器截图与 DOM 几何 |
| 图形验证 | 四边锚点、节点/边数量、路由方向 | Handle 坐标、SVG path、控制台日志 |
| Demo 隔离 | 静态页面可用且真实 API 不可达 | 页面 200、`/api/*` 404、容器只读/非 root |
| Git 边界 | 正式远程与本地 Demo 分离 | `HEAD`、`origin/main`、`codex/demo-local` 分别核对 |
| 结果效率 | Agent 前后对比 | benchmark replay + cohort compare |

## 13. 已取得的工程收益

- **安全性**：工具拿不到默认部署秘密；沙箱不能管理 PostgreSQL/控制面；高风险命令和间接提示注入有统一边界。
- **可恢复性**：任务、意图、Working Set、资源租约和 Event Log 都可跨进程恢复。
- **可审计性**：模型可见内容、工具调用、图写入、Artifact、预算和冲突都能回放。
- **Token 效率**：静态 system 可缓存；工具 schema 渐进披露；长输出外置；父子 Agent 不复制完整上下文。
- **并发质量**：依赖未满足不抢跑；相同意图原子去重；相同资产、账号和限流域可协调。
- **结果质量**：零写回会被反思；重复零产出方向会熔断；漏洞必须绑定证据。
- **数据库/API 效率**：assembly 查询合并、目录缓存、图版本缓存、前端聚合接口减少 round trip。
- **用户体验**：资产覆盖图、漏洞摘要/证据/报告/链路、Agent 成本和事件流都可直接查看。
- **发布可靠性**：建立从源码到静态导出、嵌入目录和 Linux ELF 的可验证链路。
- **演示安全性**：Demo 无数据库、无后端、无 LLM/Skill/MCP 密钥，云端只托管静态发布包并拒绝 API。
- **图形可解释性**：漏洞链路使用四边中点和方向感知路由，用户可以更清楚地追踪资产、意图、事实与 Finding 的关系。

## 14. 尚不能宣称完成的部分

以下事项必须保持事实边界：

1. 已经建立可重复 benchmark 执行器和比较器，但没有提交真实生产 TSecBenchmark cohort 结果，因此不能宣称 token、时间或漏洞产出提高了某个具体百分比。
2. 本机 PostgreSQL 未持续运行时，前端带数据视觉验证使用的是只读 stub；这证明布局正确，不证明生产数据库容量和 p95 延迟。
3. Event Log 已支持 shadow replay 和哈希漂移检测，但仍需要在长期真实任务上持续监控 backfill、事件增长和归档成本。
4. `bridge` 网络隔离能阻止沙箱直接加入项目 Compose 网络，但高安全部署仍建议把沙箱执行主机与控制面/数据库主机物理或虚拟分离，并在宿主防火墙层阻断数据库网段。
5. Artifact、Event Log 和 benchmark result 会持续增长，后续仍需制定保留期、冷热分层和清理策略。
6. Demo 当前按用户要求只存在于本地工作区和 `codex/demo-local`，不会随正式远程分支自动备份；需要独立制定本地备份和版本命名策略。
7. 四边锚点和方向感知路由已经改善当前图，但超大图仍需要分层布局、边聚合、路径高亮和按子图折叠，不能假设节点增长后仍保持同等可读性。
8. 宝塔静态部署教程和本地容器已验证，实际云服务器的 DNS、证书、Nginx 用户和文件权限仍必须在目标服务器上完成验收。

## 15. 后续建议

### P0：真实基线采集

- 固定一组授权 TSecBenchmark 场景。
- 每个版本至少执行 5 次 replay。
- 保存 before/after cohort，比较 p50/p95、token、工具调用、覆盖率和漏洞产出。

### P1：基础设施网络硬隔离

- 沙箱使用独立 Docker host 或独立 VM。
- 控制面只通过受认证的受限 Docker API gateway 创建受管容器。
- 防火墙显式拒绝沙箱网段访问 PostgreSQL、RestXtra 管理端口和云元数据地址。

### P1：Event Log 生命周期管理

- 按任务终态和时间归档事件。
- Artifact 做内容去重和保留期清理。
- 定期执行 replay 一致性巡检并告警。

### P2：容量和故障演练

- 测试 10、50、100 个并发 Worker 下的 claim、lease 和冲突等待。
- 演练进程崩溃、数据库短断、LLM 限流、Docker host 离线和 Artifact 丢失。
- 用真实数据验证恢复后不会重复写入事实或重复执行危险操作。

### P2：大规模图形与 Demo 生命周期

- 对 100、500、1000 节点 lineage 测量布局时间、首屏时间、交叉边数量和浏览器内存。
- 增加按路径高亮、无关分支淡化、节点聚合和局部展开，避免仅靠缩放承载大图。
- 为本地 Demo 建立独立版本清单和发布包 SHA-256，不把 Demo 合回远程正式主分支。
- 宝塔服务器至少保留当前版本和最近两个验证版本，使用 `current` 软链接原子更新。
- 定期检查 `/api/*` 仍为 404，避免站点配置变更后意外接入真实后端。

## 16. 结论

这轮优化把 RestXtraAI 从“功能可以运行”推进到“运行过程可以约束、恢复、审计和比较”。最重要的变化不是增加了多少页面或指标，而是建立了几条系统不变量：

- PostgreSQL 和控制面不属于沙箱可管理资源。
- 模型可见的重要状态必须能够从 Event Log、Working Set 和 Artifact 重建。
- 多 Agent 并发必须经过依赖、租约和资源冲突协调。
- 长输出和完整历史不应反复进入模型上下文。
- 性能优化必须由可重复 replay 和结果效率指标证明。
- 前端源码、静态导出、嵌入资源和发布二进制必须逐层一致。
- 正式产品与 Mock Demo 必须按不同构建和发布边界管理。
- 图形功能必须通过真实渲染与几何检查证明可读，不能用“编译通过”代替视觉验收。

这些不变量共同降低了误操作、状态漂移、重复探索、token 浪费和“源码已改但部署没变”的风险，也为后续真实 TSecBenchmark 基线、容量规划和生产故障演练提供了统一基础。
