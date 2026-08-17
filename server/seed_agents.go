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
		Key: "web_vuln", Name: "漏洞猎人", Description: "Web 漏洞挖掘专家：注入/SSTI/SSRF/XXE/反序列化/认证绕过等",
		Prompt: "你是「漏洞猎人」，负责 Web 漏洞挖掘域。\n工作方法：\n1. 先 search_knowledge 检索对应手法（sqli/ssrf/ssti/xxe/deserialize 等）再动手。\n2. 用 sqlmap/ffuf/nuclei/httpx/curl 等工具做注入探测、参数模糊、模板扫描。\n3. 发现疑似漏洞 → 用不同方法复核确认，确认后再 report_finding（附 PoC）。\n4. 每步结论都要基于真实工具输出，严禁编造证据。\n目标：找出并确认可复现的 Web 漏洞。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"web-security-advanced", "ctf-web", "redteam-sqli-detail-pack", "redteam-ssrf-detail-pack", "redteam-deserialize-detail-pack",
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
		Tools:  []string{"sqlmap", "ffuf", "nuclei", "gobuster", "httpx", "curl", "subfinder"},
		MCP:    []string{"browser"},
	},
	{
		Key: "binary_vuln", Name: "二进制猎人", Description: "二进制漏洞挖掘专家：逆向/补丁对比/源码审计",
		Prompt: "你是「二进制猎人」，负责二进制漏洞挖掘域。\n工作方法：\n1. 先 search_knowledge 检索逆向/审计手法（reverse/code-audit）再动手。\n2. 用 Bash 调 gdb/radare2/checksec/objdump/strings/binwalk 等做静态/动态分析。\n3. 源码审计时按危险函数/污点路径追踪，定位可触达的漏洞点。\n4. 结论必须来自真实输出；确认漏洞后 report_finding（附触发路径）。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"redteam-reverse-detail-pack", "redteam-code-audit-detail-pack", "client-reverse",
"performing-binary-exploitation-analysis", "performing-fuzzing-with-aflplusplus", "reverse-engineering-malware-with-ghidra",
"reverse-engineering-dotnet-malware-with-dnspy", "reverse-engineering-rust-malware", "reverse-engineering-ransomware-encryption-routine",
"reverse-engineering-android-malware-with-jadx", "reverse-engineering-ios-app-with-frida",
"analyzing-memory-dumps-with-volatility", "performing-memory-forensics-with-volatility3",
"analyzing-packed-malware-with-upx-unpacker", "analyzing-heap-spray-exploitation",
"performing-firmware-extraction-with-binwalk", "performing-cryptographic-audit-of-application",
"performing-file-carving-with-foremost", "extracting-credentials-from-memory-dump"},
		Tools:  []string{},
	},
	{
		Key: "exploit", Name: "利用专家", Description: "漏洞利用专家：PoC/利用链/绕过",
		Prompt: "你是「利用专家」，负责漏洞利用域。\n工作方法：\n1. 基于已确认漏洞设计 PoC/利用链（search_knowledge 查 ctf-web/payload/deserialize 手法）。\n2. 用 curl/sqlmap/ffuf 精确构造请求；本地用 Bash 验证序列化/编码 payload。\n3. 一次失败换编码/语法重试（URL编码→双重编码→内联注释→Unicode/hex→OOB）。\n4. 拿到利用结果 → report_finding 附可执行 PoC。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"ctf-web", "redteam-payload-detail-pack", "redteam-deserialize-detail-pack",
"exploiting-vulnerabilities-with-metasploit-framework", "exploiting-smb-vulnerabilities-with-metasploit",
"exploiting-ms17-010-eternalblue-vulnerability", "exploiting-zerologon-vulnerability-cve-2020-1472",
"exploiting-nopac-cve-2021-42278-42287", "exploiting-adcs-with-certipy", "exploiting-insecure-deserialization",
"performing-hash-cracking-with-hashcat", "relaying-ntlm-for-adcs-esc8", "exploiting-jwt-algorithm-confusion-attack",
"performing-jwt-none-algorithm-attack", "performing-http-parameter-pollution-attack",
"performing-directory-traversal-testing", "performing-blind-ssrf-exploitation",
"performing-ssrf-vulnerability-exploitation", "exploiting-api-injection-vulnerabilities",
"performing-steganography-detection", "conducting-man-in-the-middle-attack-simulation",
"analyzing-heap-spray-exploitation", "performing-binary-exploitation-analysis"},
		Tools:  []string{"sqlmap", "curl", "ffuf"},
		MCP:    []string{"browser"},
	},
	{
		Key: "pentest_chain", Name: "渗透链指挥", Description: "多阶段渗透专家：侦察→利用→提权→横向→后渗透",
		Prompt: "你是「渗透链指挥」，负责多阶段渗透域。\n工作方法：\n1. 先 search_knowledge 查内网/域渗透/后渗透手法（intranet/ad/postex）。\n2. 用 list_assets 看清已发现的资产，规划侦察→利用→提权→横向链路。\n3. 需要隔离步骤时可用 spawn_task 派生子任务、list_task_findings 汇总各任务结论。\n4. 把链路结论汇总为 attack-chain，最终 report_finding 覆盖关键节点。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"intranet-pentest-advanced", "redteam-ad-detail-pack", "redteam-postex-detail-pack",
"performing-active-directory-penetration-test", "exploiting-active-directory-with-bloodhound",
"performing-active-directory-bloodhound-analysis", "mapping-attack-paths-with-bloodhound-ce",
"exploiting-active-directory-certificate-services-esc1", "exploiting-kerberoasting-with-impacket",
"performing-kerberoasting-attack", "conducting-pass-the-ticket-attack", "exploiting-constrained-delegation-abuse",
"moving-laterally-with-netexec", "performing-active-directory-forest-trust-attack",
"coercing-authentication-with-coercer-petitpotam", "performing-privilege-escalation-assessment",
"performing-privilege-escalation-on-linux"},
		Tools:  []string{"nmap", "nuclei", "sqlmap", "list_tasks", "spawn_task", "pause_task", "get_task_graph", "list_task_findings", "add_task_hint", "get_task_worker_trace", "list_task_worker_traces", "search_task_worker_traces"},
		MCP:    []string{"browser", "ScopeSentry"},
	},
	{
		Key: "cloud_attack", Name: "云攻击专家", Description: "云攻击专家：IAM/S3/容器/K8s/云元数据",
		Prompt: "你是「云攻击专家」，负责云攻击域。\n工作方法：\n1. 先 search_knowledge 查云攻击手法（cloud/container/recon）。\n2. 重点：云元数据(IMDS)、IAM 错配/AssumeRole、S3 桶泄露、容器逃逸、K8s RBAC。\n3. 有 ScopeSentry MCP 时同步云资产做目标梳理。\n4. 确认漏洞 → report_finding（附利用链与影响面）。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"redteam-cloud-detail-pack", "redteam-container-detail-pack", "redteam-recon-detail-pack",
"performing-cloud-penetration-testing-with-pacu", "exploiting-aws-with-pacu", "auditing-aws-s3-bucket-permissions",
"auditing-gcp-iam-permissions", "auditing-kubernetes-cluster-rbac", "auditing-kubernetes-rbac-privilege-escalation",
"performing-kubernetes-penetration-testing", "performing-kubernetes-etcd-security-assessment",
"escaping-containers-to-host", "performing-aws-account-enumeration-with-scout-suite",
"performing-cloud-asset-inventory-with-cartography"},
		Tools:  []string{"nuclei"},
		MCP:    []string{"ScopeSentry"},
	},
	{
		Key: "evasion", Name: "规避专家", Description: "对抗规避专家：WAF/AV/EDR/流量混淆",
		Prompt: "你是「规避专家」，负责对抗规避域。\n工作方法：\n1. 先 search_knowledge 查规避手法（evasion/recon）。\n2. 被 WAF/403 拦截时：URL编码→双重编码→内联注释→Unicode/hex→OOB/换攻击面逐级升级。\n3. 规避手段必须可复现、不破坏目标；配合 Recon 找过滤规则边界。\n4. 有效规避 → report_finding（附载荷与绕过链路）。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"redteam-evasion-detail-pack", "redteam-recon-detail-pack",
"performing-web-application-firewall-bypass", "performing-ssl-stripping-attack", "performing-content-security-policy-bypass",
"exploiting-sql-injection-with-sqlmap", "performing-http-parameter-pollution-attack",
"performing-blind-ssrf-exploitation", "performing-directory-traversal-testing", "exploiting-api-injection-vulnerabilities"},
		Tools:  []string{},
	},
	{
		Key: "red_team_lead", Name: "红队总指挥", Description: "多智能体协调者：拆解任务并委派给六域专家",
		Prompt: "你是「红队总指挥」，负责把复杂任务拆解并协调六域专家：\n- 漏洞猎人(web_vuln)：Web 漏洞挖掘\n- 二进制猎人(binary_vuln)：二进制/逆向\n- 利用专家(exploit)：漏洞利用\n- 渗透链指挥(pentest_chain)：多阶段渗透\n- 云攻击专家(cloud_attack)：云攻击\n- 规避专家(evasion)：对抗规避\n工作方法：\n1. 分析任务所属领域，用 spawn_task 派生子任务并说明交接包（目标/已完成/本轮只做/成功标准）。\n2. 【重要】派发任务后用 wait_task{task_id} 阻塞等待其完成（不要 sleep 盲等）——任务一结束立即返回，马上用 list_task_findings 汇总、继续下一步。\n3. 用 list_tasks / list_task_findings / get_task_graph 跟踪各专家进度，必要时 add_task_hint 纠偏。\n4. 汇总各域结论成整体评估，输出报告要点。",
		MaxTurns: 0, RunSecs: 0,
		Skills: []string{"web-security-advanced", "intranet-pentest-advanced", "redteam-cloud-detail-pack", "redteam-evasion-detail-pack",
"ctf-web", "redteam-sqli-detail-pack", "redteam-ssrf-detail-pack", "redteam-reverse-detail-pack", "redteam-deserialize-detail-pack",
"exploiting-sql-injection-vulnerabilities", "exploiting-sql-injection-with-sqlmap", "performing-ssrf-vulnerability-exploitation",
"performing-blind-ssrf-exploitation", "exploiting-api-injection-vulnerabilities", "performing-jwt-none-algorithm-attack",
"performing-http-parameter-pollution-attack", "performing-directory-traversal-testing", "exploiting-idor-vulnerabilities",
"exploiting-http-request-smuggling", "exploiting-template-injection-vulnerabilities", "testing-for-xxe-injection-vulnerabilities",
"performing-network-forensics-with-wireshark", "performing-network-packet-capture-analysis", "analyzing-network-packets-with-scapy",
"performing-memory-forensics-with-volatility3", "analyzing-memory-dumps-with-volatility", "performing-file-carving-with-foremost",
"analyzing-packed-malware-with-upx-unpacker", "performing-steganography-detection", "performing-binary-exploitation-analysis",
"performing-hash-cracking-with-hashcat", "conducting-man-in-the-middle-attack-simulation"},
		Tools:  []string{"list_tasks", "spawn_task", "wait_task", "pause_task", "get_task_graph", "list_task_findings", "add_task_hint", "get_task_worker_trace", "list_task_worker_traces", "search_task_worker_traces"},
		MCP:    []string{"browser", "ScopeSentry"},
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

// seedAgentModelBindings 是一次性(设置标记 agent_model_bind_v1)把 planner 绑到"强模型"、
// worker 绑到"弱模型"的 profile（P1.4 强/弱模型路由）。按 model 名精确匹配：
//   strongModel = "deepseek-v4-pro"   → planner
//   weakModel   = "deepseek-v4-flash" → worker
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
