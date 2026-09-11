package server

import (
	"log"
)

// 六域智能体体系（面向 TSecBench 六能力域）：
//   漏洞猎人 web-vuln（Web漏洞挖掘）
//   二进制猎人 binary-vuln（二进制漏洞挖掘）
//   利用专家 exploit（漏洞利用）
//   渗透链指挥 pentest-chain（多阶段渗透）
//   云攻击专家 cloud-attack（云攻击）
//   规避专家 evasion（对抗规避）
//   红队总指挥 red-team-lead（协调委派）
//   信息收集 asset-intel（被动资产测绘与归档）
// 启动时幂等播种：agent 不存在才创建；skill 可见性 / 工具绑定 / MCP 可见性按需补齐
// （首插不覆盖用户后续编辑）。

type domainAgentSpec struct {
	Key         string
	Name        string
	Description string
	Prompt      string
	MaxTurns    int
	RunSecs     int
	Skills      []string
	Tools       []string
	MCP         []string // MCP server 名
}

// 各域 agent 共用的基础工具：平台域工具 + 知识库。
var domainBaseTools = []string{
	"search_knowledge", "list_assets", "insert_assets", "add_company_scope",
	"list_findings", "report_finding", "record_fact", "list_companies",
}

var sixDomainAgents = []domainAgentSpec{
	{
		Key: "code_audit", Name: "代码审计", Description: "授权源码安全审计专家：数据流追踪、覆盖检查与非破坏性验证",
		Prompt: "你是「代码审计」智能体，只对用户明确授权的源码、构建产物或反编译产物进行安全审计。\n" +
			"工作边界：先确认审计目录与模式（快速/标准/深度）；未经确认不得审计不在用户授权范围内的代码。只做源码阅读、静态分析和非破坏性验证；不得写入目标、删除文件、修改配置、导出敏感数据、建立持久化或执行攻击载荷。\n" +
			"审计方法：\n" +
			"1. 先加载 audit-skills 与 code-audit，识别语言、框架、入口、鉴权边界、敏感 source/sink。\n" +
			"2. 按数据流追踪验证可达性、可控性、传播链、防护、影响和可复现性。只引用实际读取的文件与行号，严禁编造发现。\n" +
			"3. 未同时满足可达、可控、可传播、可利用和影响成立的结论，标记为“待人工验证”，不能报为确认漏洞。\n" +
			"4. 确认问题时使用 report_finding，附受影响入口、source-to-sink 链、非破坏性证明、修复建议与证据文件/行号；用 record_fact 记录覆盖范围与未确认线索。\n" +
			"5. 输出覆盖项、发现等级、待验证项和修复优先级的中文报告。",
		MaxTurns: 16, RunSecs: 300,
		Skills: []string{"src-hunting", "audit-skills", "code-audit", "redteam-code-audit-detail-pack"},
		Tools:  []string{"list_assets", "list_findings", "record_fact", "report_finding"},
	},
	{
		Key: "asset_intel", Name: "信息收集", Description: "企业资产信息收集专家：FOFA 被动测绘、去重归档与范围管理",
		Prompt: "你是「信息收集」智能体，负责用户明确授权企业的【被动】资产梳理与资产库归档。\n" +
			"工作边界：只使用 fofa_asset_discover 调用 FOFA 官方 API；不得使用 nmap、httpx、目录扫描、漏洞验证、登录尝试或任何主动探测。\n" +
			"工作流程：\n" +
			"1. 先向用户确认企业名称与已确认的根域名；未提供根域名时，说明需要用户提供或确认官网根域名。\n" +
			"2. 调用 fofa_asset_discover(company, root_domain, icp 可选)。该工具会创建/复用企业、登记根域名范围，并将命中该范围的根域名、子域名、IP、服务自动按企业写入资产管理。\n" +
			"3. ICP/公司名扩展查询返回的其它根域名只能作为候选；不得自行扩大企业范围。向用户列出候选并请求确认后，才用 add_company_scope 追加。\n" +
			"4. 调用 list_assets 汇总已入库资产，按根域名、子域名、IP、服务和 CDN/候选项输出简洁清单；证据必须来自工具结果，不得编造。\n" +
			"输出必须明确：已归档资产数、未归档候选项、FOFA 查询条件以及被动测绘边界。",
		MaxTurns: 12, RunSecs: 180,
		Tools: []string{"fofa_asset_discover", "list_assets", "list_companies", "add_company_scope"},
	},
	{
		Key: "web_vuln", Name: "漏洞猎人", Description: "Web 漏洞挖掘专家：注入/SSTI/SSRF/XXE/反序列化/认证绕过等",
		Prompt:   "你是「漏洞猎人」，负责 Web 漏洞挖掘域。\n工作方法：\n1. 先 search_knowledge 检索对应手法（sqli/ssrf/ssti/xxe/deserialize 等）再动手。\n2. 用 sqlmap/ffuf/nuclei/httpx/curl 等工具做注入探测、参数模糊、模板扫描。\n3. 发现疑似漏洞 → 用不同方法复核确认，确认后再 report_finding（附 PoC）。\n4. 每步结论都要基于真实工具输出，严禁编造证据。\n目标：找出并确认可复现的 Web 漏洞。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"src-hunting", "web-security-advanced", "ctf-web", "redteam-sqli-detail-pack", "redteam-ssrf-detail-pack", "redteam-deserialize-detail-pack",
			"exploiting-server-side-request-forgery", "exploiting-template-injection-vulnerabilities", "testing-for-xxe-injection-vulnerabilities",
			"exploiting-idor-vulnerabilities", "exploiting-http-request-smuggling", "exploiting-nosql-injection-vulnerabilities",
			"exploiting-prototype-pollution-in-javascript", "exploiting-race-condition-vulnerabilities", "exploiting-mass-assignment-in-rest-apis",
			"exploiting-broken-function-level-authorization", "performing-web-cache-poisoning-attack", "performing-web-cache-deception-attack",
			"testing-for-host-header-injection", "testing-for-open-redirect-vulnerabilities", "exploiting-websocket-vulnerabilities",
			"exploiting-type-juggling-vulnerabilities", "performing-graphql-introspection-attack", "testing-oauth2-implementation-flaws",
			"exploiting-sql-injection-vulnerabilities", "exploiting-sql-injection-with-sqlmap", "exploiting-api-injection-vulnerabilities",
			"performing-jwt-none-algorithm-attack", "performing-http-parameter-pollution-attack", "performing-directory-traversal-testing",
			"performing-ssrf-vulnerability-exploitation", "performing-blind-ssrf-exploitation",
			"performing-network-forensics-with-wireshark", "performing-network-packet-capture-analysis", "analyzing-network-packets-with-scapy",
			"performing-steganography-detection"},
		Tools: []string{"sqlmap", "ffuf", "nuclei", "gobuster", "httpx", "curl", "subfinder"},
		MCP:   []string{"browser"},
	},
	{
		Key: "binary_vuln", Name: "二进制猎人", Description: "二进制漏洞挖掘专家：逆向/补丁对比/源码审计",
		Prompt:   "你是「二进制猎人」，负责二进制漏洞挖掘域。\n工作方法：\n1. 先 search_knowledge 检索逆向/审计手法（reverse/code-audit）再动手。\n2. 用 Bash 调 gdb/radare2/checksec/objdump/strings/binwalk 等做静态/动态分析。\n3. 源码审计时按危险函数/污点路径追踪，定位可触达的漏洞点。\n4. 结论必须来自真实输出；确认漏洞后 report_finding（附触发路径）。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"src-hunting", "redteam-reverse-detail-pack", "redteam-code-audit-detail-pack", "client-reverse",
			"performing-binary-exploitation-analysis", "performing-fuzzing-with-aflplusplus", "reverse-engineering-malware-with-ghidra",
			"reverse-engineering-dotnet-malware-with-dnspy", "reverse-engineering-rust-malware", "reverse-engineering-ransomware-encryption-routine",
			"reverse-engineering-android-malware-with-jadx", "reverse-engineering-ios-app-with-frida",
			"analyzing-memory-dumps-with-volatility", "performing-memory-forensics-with-volatility3",
			"analyzing-packed-malware-with-upx-unpacker", "analyzing-heap-spray-exploitation",
			"performing-firmware-extraction-with-binwalk", "performing-cryptographic-audit-of-application",
			"performing-file-carving-with-foremost", "extracting-credentials-from-memory-dump"},
		Tools: []string{},
	},
	{
		Key: "exploit", Name: "利用专家", Description: "漏洞利用专家：PoC/利用链/绕过",
		Prompt:   "你是「利用专家」，负责漏洞利用域。\n工作方法：\n1. 基于已确认漏洞设计 PoC/利用链（search_knowledge 查 ctf-web/payload/deserialize 手法）。\n2. 用 curl/sqlmap/ffuf 精确构造请求；本地用 Bash 验证序列化/编码 payload。\n3. 一次失败换编码/语法重试（URL编码→双重编码→内联注释→Unicode/hex→OOB）。\n4. 拿到利用结果 → report_finding 附可执行 PoC。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"src-hunting", "ctf-web", "redteam-payload-detail-pack", "redteam-deserialize-detail-pack",
			"exploiting-vulnerabilities-with-metasploit-framework", "exploiting-smb-vulnerabilities-with-metasploit",
			"exploiting-ms17-010-eternalblue-vulnerability", "exploiting-zerologon-vulnerability-cve-2020-1472",
			"exploiting-nopac-cve-2021-42278-42287", "exploiting-adcs-with-certipy", "exploiting-insecure-deserialization",
			"performing-hash-cracking-with-hashcat", "relaying-ntlm-for-adcs-esc8", "exploiting-jwt-algorithm-confusion-attack",
			"performing-jwt-none-algorithm-attack", "performing-http-parameter-pollution-attack",
			"performing-directory-traversal-testing", "performing-blind-ssrf-exploitation",
			"performing-ssrf-vulnerability-exploitation", "exploiting-api-injection-vulnerabilities",
			"performing-steganography-detection", "conducting-man-in-the-middle-attack-simulation",
			"analyzing-heap-spray-exploitation", "performing-binary-exploitation-analysis"},
		Tools: []string{"sqlmap", "curl", "ffuf"},
		MCP:   []string{"browser"},
	},
	{
		Key: "pentest_chain", Name: "渗透链指挥", Description: "多阶段渗透专家：侦察→利用→提权→横向→后渗透",
		Prompt:   "你是「渗透链指挥」，负责多阶段渗透域。\n工作方法：\n1. 先 search_knowledge 查内网/域渗透/后渗透手法（intranet/ad/postex）。\n2. 用 list_assets 看清已发现的资产，规划侦察→利用→提权→横向链路。\n3. 需要隔离步骤时用 spawn_task 传 objective、asset_ids、required_evidence、allowed_tools 和 budget；用 wait_task 直接接收结构化结果，只有引用不足时再读完整图或 trace。\n4. 把链路结论汇总为 attack-chain，最终 report_finding 覆盖关键节点。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"src-hunting", "intranet-pentest-advanced", "redteam-ad-detail-pack", "redteam-postex-detail-pack",
			"performing-active-directory-penetration-test", "exploiting-active-directory-with-bloodhound",
			"performing-active-directory-bloodhound-analysis", "mapping-attack-paths-with-bloodhound-ce",
			"exploiting-active-directory-certificate-services-esc1", "exploiting-kerberoasting-with-impacket",
			"performing-kerberoasting-attack", "conducting-pass-the-ticket-attack", "exploiting-constrained-delegation-abuse",
			"moving-laterally-with-netexec", "performing-active-directory-forest-trust-attack",
			"coercing-authentication-with-coercer-petitpotam", "performing-privilege-escalation-assessment",
			"performing-privilege-escalation-on-linux"},
		Tools: []string{"nmap", "nuclei", "sqlmap", "list_tasks", "spawn_task", "wait_task", "get_task_result", "pause_task", "get_task_graph", "list_task_findings", "add_task_hint", "get_task_worker_trace", "list_task_worker_traces", "search_task_worker_traces"},
		MCP:   []string{"browser", "ScopeSentry"},
	},
	{
		Key: "cloud_attack", Name: "云攻击专家", Description: "云攻击专家：IAM/S3/容器/K8s/云元数据",
		Prompt:   "你是「云攻击专家」，负责云攻击域。\n工作方法：\n1. 先 search_knowledge 查云攻击手法（cloud/container/recon）。\n2. 重点：云元数据(IMDS)、IAM 错配/AssumeRole、S3 桶泄露、容器逃逸、K8s RBAC。\n3. 有 ScopeSentry MCP 时同步云资产做目标梳理。\n4. 确认漏洞 → report_finding（附利用链与影响面）。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"src-hunting", "redteam-cloud-detail-pack", "redteam-container-detail-pack", "redteam-recon-detail-pack",
			"performing-cloud-penetration-testing-with-pacu", "exploiting-aws-with-pacu", "auditing-aws-s3-bucket-permissions",
			"auditing-gcp-iam-permissions", "auditing-kubernetes-cluster-rbac", "auditing-kubernetes-rbac-privilege-escalation",
			"performing-kubernetes-penetration-testing", "performing-kubernetes-etcd-security-assessment",
			"escaping-containers-to-host", "performing-aws-account-enumeration-with-scout-suite",
			"performing-cloud-asset-inventory-with-cartography"},
		Tools: []string{"nuclei"},
		MCP:   []string{"ScopeSentry"},
	},
	{
		Key: "evasion", Name: "规避专家", Description: "对抗规避专家：WAF/AV/EDR/流量混淆",
		Prompt:   "你是「规避专家」，负责对抗规避域。\n工作方法：\n1. 先 search_knowledge 查规避手法（evasion/recon）。\n2. 被 WAF/403 拦截时：URL编码→双重编码→内联注释→Unicode/hex→OOB/换攻击面逐级升级。\n3. 规避手段必须可复现、不破坏目标；配合 Recon 找过滤规则边界。\n4. 有效规避 → report_finding（附载荷与绕过链路）。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"src-hunting", "redteam-evasion-detail-pack", "redteam-recon-detail-pack",
			"performing-web-application-firewall-bypass", "performing-ssl-stripping-attack", "performing-content-security-policy-bypass",
			"exploiting-sql-injection-with-sqlmap", "performing-http-parameter-pollution-attack",
			"performing-blind-ssrf-exploitation", "performing-directory-traversal-testing", "exploiting-api-injection-vulnerabilities"},
		Tools: []string{},
	},
	{
		Key: "red_team_lead", Name: "红队总指挥", Description: "多智能体协调者：拆解任务并委派给六域专家",
		Prompt: "你是「红队总指挥」，负责把授权任务拆解、派给六域专家，并对交付物把关。" +
			"目标不是把面铺开，而是沿一条能出结果的路线把洞证实、把报告做实。遇到渗透/挖洞类任务，先调用 src-hunting skill 取打法与报告规范，再派活。\n" +
			"六域专家（spawn_task 的 agent 名）：\n" +
			"- 漏洞猎人(web_vuln)：Web 注入/SSTI/SSRF/XXE/反序列化/认证绕过\n" +
			"- 二进制猎人(binary_vuln)：逆向/补丁对比/源码审计\n" +
			"- 利用专家(exploit)：PoC/利用链/绕过\n" +
			"- 渗透链指挥(pentest_chain)：侦察→利用→提权→横向\n" +
			"- 云攻击专家(cloud_attack)：IAM/S3/容器/K8s/云元数据\n" +
			"- 规避专家(evasion)：WAF/AV/EDR/流量混淆\n" +
			"核心纪律（贯穿派活与验收）：\n" +
			"1. 一种子闭环，先深后广：一次主攻一个种子/入口（一个目标/资产/链路），走完 面→利用→验证→报告→迭代 再换下一个；不要一上来铺大面、每条只探一点。多路并行只在机理不同的 2–3 条路线间，且每条都要走透。\n" +
			"2. 类型矩阵派活：派活时要求子 agent 覆盖其域的类型矩阵（未授权/越权/注入/SSRF/XSS/RCE/上传/穿越/认证等），强调全类型+全参数，力气先砸更易高危的点。\n" +
			"3. 最小危害+基线差分：越权/注入类必须用基线差分（同请求换对象/参数，对比响应差异）证明，而非「看起来可能」；严禁破坏性利用与越界。\n" +
			"4. 交付含迭代：验收的成果必须含可复现 PoC（请求/响应或命令输出）、独立复现证据、以及迭代记录（首测失败后换编码/方法/参数/路径的尝试与结论）。只「试过一次没成」不算走完；未探尽的路线退回续做，而非改派。\n" +
			"5. 负向也要落地：封锁的路线记入 negative_results 并说明为何封锁；出现材料性新机理才重开，禁止换措辞空转重试。\n" +
			"6. 不无据盘问授权，但不越界：以当前任务/企业范围为授权边界——不要因没有纸质授权书停下盘问；但绝不越出任务范围攻击无关资产。\n" +
			"派活规约（spawn_task）：\n" +
			"- 传最小结构化交接包：objective、asset_ids、required_evidence、allowed_tools、budget；不要复制父任务 transcript。\n" +
			"- required_evidence 至少含 PoC/请求响应、独立复现证据、迭代记录、影响面；越权/注入要显式要求基线差分。\n" +
			"- 派发后用 wait_task 阻塞等待任一子任务完成（不要 sleep 盲等），直接消费其 facts、findings、negative_results、artifact_refs、next_actions、usage；用 list_tasks 跟踪进度，必要时 add_task_hint 纠偏（先指出哪条路线没走透/缺哪项证据）。只有结构化结果引用不足时，才 get_task_result / get_task_graph / trace 深挖。\n" +
			"收尾：汇总各域结论成整体评估——达成了什么、确认了哪些漏洞（附 PoC 位置与影响面）、哪些方向已封锁及原因、下一步建议。只讲真实做到的，不臆造。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"src-hunting", "web-security-advanced", "intranet-pentest-advanced", "redteam-cloud-detail-pack", "redteam-evasion-detail-pack",
			"ctf-web", "redteam-sqli-detail-pack", "redteam-ssrf-detail-pack", "redteam-reverse-detail-pack", "redteam-deserialize-detail-pack",
			"exploiting-sql-injection-vulnerabilities", "exploiting-sql-injection-with-sqlmap", "performing-ssrf-vulnerability-exploitation",
			"performing-blind-ssrf-exploitation", "exploiting-api-injection-vulnerabilities", "performing-jwt-none-algorithm-attack",
			"performing-http-parameter-pollution-attack", "performing-directory-traversal-testing", "exploiting-idor-vulnerabilities",
			"exploiting-http-request-smuggling", "exploiting-template-injection-vulnerabilities", "testing-for-xxe-injection-vulnerabilities",
			"performing-network-forensics-with-wireshark", "performing-network-packet-capture-analysis", "analyzing-network-packets-with-scapy",
			"performing-memory-forensics-with-volatility3", "analyzing-memory-dumps-with-volatility", "performing-file-carving-with-foremost",
			"analyzing-packed-malware-with-upx-unpacker", "performing-steganography-detection", "performing-binary-exploitation-analysis",
			"performing-hash-cracking-with-hashcat", "conducting-man-in-the-middle-attack-simulation"},
		Tools: []string{"list_tasks", "spawn_task", "wait_task", "get_task_result", "pause_task", "get_task_graph", "list_task_findings", "add_task_hint", "get_task_worker_trace", "list_task_worker_traces", "search_task_worker_traces"},
		MCP:   []string{"browser", "ScopeSentry"},
	},
}

// seedSixDomainAgents 幂等播种六域智能体 + 技能/MCP/工具绑定。首插不覆盖用户编辑。
func (s *Server) seedSixDomainAgents() {
	pg := s.m.pg
	if pg == nil {
		return
	}
	// MCP server id 查表（名 → id）。
	mcpByName := map[string]int64{}
	if servers, err := pg.ListMCP(); err == nil {
		for _, sv := range servers {
			mcpByName[sv.Name] = sv.ID
		}
	}
	for _, spec := range sixDomainAgents {
		agentRow, err := pg.GetAgentByKey(spec.Key)
		if err != nil {
			log.Printf("[seed-agent] %s 查询失败: %v", spec.Key, err)
			continue
		}
		if agentRow == nil {
			a, err := pg.CreateAgent(spec.Key, spec.Name, spec.Description)
			if err != nil {
				log.Printf("[seed-agent] %s 创建失败: %v", spec.Key, err)
				continue
			}
			agentRow = a
			_ = pg.SeedPromptIfEmpty(agentRow.ID, spec.Prompt)
			log.Printf("[seed-agent] 已创建智能体 %s「%s」", spec.Key, spec.Name)
		}
		// 配置：轮次/时长（仅当用户未覆盖时设置一次？这里幂等设置，安全）
		_ = pg.SetAgentMaxTurns(spec.Key, spec.MaxTurns)
		_ = pg.SetAgentRunSeconds(spec.Key, spec.RunSecs)
		// 技能可见性：全量替换为域专属集合（首插；用户改动会被下次启动重置——可接受，
		// 因为这是"域编排"的既定配置。如需保留用户改动可改为增量，但六域体系固定）。
		if len(spec.Skills) > 0 {
			if err := pg.SetAgentSkillVisibility(agentRow.ID, spec.Skills); err != nil {
				log.Printf("[seed-agent] %s 技能绑定失败: %v", spec.Key, err)
			}
		}
		// 工具绑定：域工具 + 基础工具（只追加，不动已有）。
		keys := append(append([]string{}, domainBaseTools...), spec.Tools...)
		if err := pg.AddAgentToToolBinding(spec.Key, keys); err != nil {
			log.Printf("[seed-agent] %s 工具绑定失败: %v", spec.Key, err)
		}
		// MCP 可见性。
		if len(spec.MCP) > 0 {
			var ids []int64
			for _, name := range spec.MCP {
				if id, ok := mcpByName[name]; ok {
					ids = append(ids, id)
				}
			}
			if len(ids) > 0 {
				if err := pg.SetAgentVisibilityKind(agentRow.ID, "mcp", ids); err != nil {
					log.Printf("[seed-agent] %s MCP 绑定失败: %v", spec.Key, err)
				}
			}
		}
	}
}

// seedSrcHuntingSkillBinding 把内置方法论 skill「src-hunting」追加（增量、不替换）给
// 对话式进攻 agent（pentest/auto/postex）。一次性，幂等。六域 agent 在 seedSixDomainAgents
// 里已把 src-hunting 加进各自 Skills 列表。
func (s *Server) seedSrcHuntingSkillBinding() {
	pg := s.m.pg
	if pg == nil {
		return
	}
	const flag = "src_hunting_skill_bind_v1"
	if v, _, _ := pg.GetSetting(flag); v == "true" {
		return
	}
	const skill = "src-hunting"
	for _, key := range []string{"pentest", "auto", "postex"} {
		ag, err := pg.GetAgentByKey(key)
		if err != nil || ag == nil {
			continue
		}
		if err := pg.ToggleSkillVisibility(ag.ID, skill, true); err != nil {
			log.Printf("[seed-skill] %s 绑定 %s 失败: %v", key, skill, err)
		}
	}
	_ = pg.SetSetting(flag, "true")
}

// seedRedTeamLeadPromptV2 一次性(flag 门控)把「红队总指挥」提示词升级为 SRC 方法论版：
// 追加新版本并切 current，升级时对已存在实例生效；之后不再覆盖（保留用户后续编辑）。
func (s *Server) seedRedTeamLeadPromptV2() {
	pg := s.m.pg
	if pg == nil {
		return
	}
	const flag = "prompt_red_team_lead_methodology_v1"
	if v, _, _ := pg.GetSetting(flag); v == "true" {
		return
	}
	ag, err := pg.GetAgentByKey("red_team_lead")
	if err != nil || ag == nil {
		return
	}
	for _, spec := range sixDomainAgents {
		if spec.Key != "red_team_lead" {
			continue
		}
		if _, err := pg.SavePrompt(ag.ID, spec.Prompt, "SRC 方法论 v1", "system"); err != nil {
			log.Printf("[seed-agent] red_team_lead 方法论提示词升级失败: %v", err)
			return
		}
		log.Printf("[seed-agent] red_team_lead 提示词已升级为 SRC 方法论版")
		break
	}
	_ = pg.SetSetting(flag, "true")
}

// seedAgentModelBindings 是一次性(设置标记 agent_model_bind_v1)把 planner 绑到"强模型"、
// worker 绑到"弱模型"的 profile（P1.4 强/弱模型路由）。按 model 名精确匹配：
//
//	strongModel = "deepseek-v4-pro"   → planner
//	weakModel   = "deepseek-v4-flash" → worker
//
// 两个 profile 都建好才生效；缺一个就跳过（用户可在 agent 设置页手动绑定）。
func (s *Server) seedAgentModelBindings() {
	pg := s.m.PG()
	if v, _, _ := pg.GetSetting("agent_model_bind_v1"); v == "true" {
		return
	}
	_ = pg.SetSetting("agent_model_bind_v1", "true")
	profs, err := pg.ListProfiles()
	if err != nil {
		log.Printf("[seed-agent] 读取 profiles 失败: %v", err)
		return
	}
	var proID, flashID int64
	for _, p := range profs {
		switch p.Model {
		case "deepseek-v4-pro":
			proID = p.ID
		case "deepseek-v4-flash":
			flashID = p.ID
		}
	}
	if proID > 0 {
		if err := pg.SetAgentLLMProfile("planner", proID); err != nil {
			log.Printf("[seed-agent] planner 绑定 profile %d 失败: %v", proID, err)
		} else {
			log.Printf("[seed-agent] planner → profile %d (强模型 deepseek-v4-pro)", proID)
		}
	}
	if flashID > 0 {
		if err := pg.SetAgentLLMProfile("worker", flashID); err != nil {
			log.Printf("[seed-agent] worker 绑定 profile %d 失败: %v", flashID, err)
		} else {
			log.Printf("[seed-agent] worker → profile %d (弱模型 deepseek-v4-flash)", flashID)
		}
	}
}
