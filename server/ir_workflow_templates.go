package server

import (
	"encoding/json"
	"log"

	"github.com/RestXtra/RestXtraAI/workflow"
)

// irWorkflowTemplate 是一个内置应急响应 DAG 模板（playbook→DAG，缝②）。
// 模板把 soc-autopilot 的遏制/取证 playbook 结构映射到现有 workflow 引擎；
// 节点用 conn_* 工具，运行时经 {{inputs.*}} 注入连接 id/目标/命令；
// 遏制动作的 HITL 由 conn_contain 服务端审批兜底（DAG 不叠加 hitl 节点）。
type irWorkflowTemplate struct {
	Name        string
	Description string
	Graph       workflow.Graph
}

func irWorkflowTemplateGraphs() []irWorkflowTemplate {
	connID := `{{inputs.conn_id}}`
	return []irWorkflowTemplate{
		{
			Name:        "应急响应 · 取证排查",
			Description: "列出受管连接，对目标主机执行标准取证命令（whoami/进程/网络）并汇总输出，供研判。对应 soc-autopilot 调查阶段。运行输入：conn_id",
			Graph: workflow.Graph{
				Nodes: []workflow.Node{
					{ID: "start", Kind: workflow.KindStart, Label: "开始"},
					{ID: "list", Kind: workflow.KindTool, Label: "选连接", Tool: "conn_list", Args: "{}", OutputKey: "conns"},
					{ID: "who", Kind: workflow.KindTool, Label: "身份", Tool: "conn_exec", Args: `{"id":"` + connID + `","command":"whoami"}`, OutputKey: "who"},
					{ID: "proc", Kind: workflow.KindTool, Label: "进程", Tool: "conn_exec", Args: `{"id":"` + connID + `","command":"ps aux"}`, OutputKey: "proc"},
					{ID: "net", Kind: workflow.KindTool, Label: "网络", Tool: "conn_exec", Args: `{"id":"` + connID + `","command":"netstat -ano"}`, OutputKey: "net"},
					{ID: "out", Kind: workflow.KindOutput, Label: "输出", OutputKey: "brief"},
				},
				Edges: []workflow.Edge{
					{From: "start", To: "list"},
					{From: "list", To: "who"},
					{From: "who", To: "proc"},
					{From: "proc", To: "net"},
					{From: "net", To: "out"},
				},
			},
		},
		{
			Name:        "应急响应 · 遏制恶意进程",
			Description: "对目标连接上确认的恶意进程执行 kill -9 遏制（走人工审批）。对应 soc-autopilot kill_process。运行输入：conn_id/target(pid)/rationale",
			Graph: workflow.Graph{
				Nodes: []workflow.Node{
					{ID: "start", Kind: workflow.KindStart, Label: "开始"},
					{ID: "list", Kind: workflow.KindTool, Label: "选连接", Tool: "conn_list", Args: "{}", OutputKey: "conns"},
					{ID: "kill", Kind: workflow.KindTool, Label: "杀进程", Tool: "conn_contain", Args: `{"id":"` + connID + `","action":"kill_process","target":"{{inputs.target}}","rationale":"{{inputs.rationale}}"}`},
					{ID: "end", Kind: workflow.KindEnd, Label: "完成"},
				},
				Edges: []workflow.Edge{
					{From: "start", To: "list"},
					{From: "list", To: "kill"},
					{From: "kill", To: "end"},
				},
			},
		},
		{
			Name:        "应急响应 · 封禁恶意 IP",
			Description: "对目标连接的网关封禁确认的恶意源 IP（iptables，走人工审批）。对应 soc-autopilot block_ip。运行输入：conn_id/target(源IP)/rationale",
			Graph: workflow.Graph{
				Nodes: []workflow.Node{
					{ID: "start", Kind: workflow.KindStart, Label: "开始"},
					{ID: "list", Kind: workflow.KindTool, Label: "选连接", Tool: "conn_list", Args: "{}", OutputKey: "conns"},
					{ID: "block", Kind: workflow.KindTool, Label: "封禁IP", Tool: "conn_contain", Args: `{"id":"` + connID + `","action":"block_ip","target":"{{inputs.target}}","rationale":"{{inputs.rationale}}"}`},
					{ID: "end", Kind: workflow.KindEnd, Label: "完成"},
				},
				Edges: []workflow.Edge{
					{From: "start", To: "list"},
					{From: "list", To: "block"},
					{From: "block", To: "end"},
				},
			},
		},
		{
			Name:        "应急响应 · 遏制并验证",
			Description: "杀进程遏制后回查验证是否生效（ps -p 应无输出），把验证结论写进输出。对应 soc-autopilot verify 环节。运行输入：conn_id/target(pid)/rationale",
			Graph: workflow.Graph{
				Nodes: []workflow.Node{
					{ID: "start", Kind: workflow.KindStart, Label: "开始"},
					{ID: "list", Kind: workflow.KindTool, Label: "选连接", Tool: "conn_list", Args: "{}", OutputKey: "conns"},
					{ID: "kill", Kind: workflow.KindTool, Label: "杀进程", Tool: "conn_contain", Args: `{"id":"` + connID + `","action":"kill_process","target":"{{inputs.target}}","rationale":"{{inputs.rationale}}"}`},
					{ID: "verify", Kind: workflow.KindTool, Label: "回查", Tool: "conn_exec", Args: `{"id":"` + connID + `","command":"ps -p {{inputs.target}}"}`, OutputKey: "verify"},
					{ID: "end", Kind: workflow.KindEnd, Label: "完成"},
				},
				Edges: []workflow.Edge{
					{From: "start", To: "list"},
					{From: "list", To: "kill"},
					{From: "kill", To: "verify"},
					{From: "verify", To: "end"},
				},
			},
		},
	}
}

// seedIRWorkflowTemplates 幂等地把内置 IR 模板 seed 进 workflow_graphs（老库也补）。
func (s *Server) seedIRWorkflowTemplates() {
	existing, err := s.m.pg.ListWorkflowGraphs()
	if err != nil {
		log.Printf("[ir] 读取工作流列表失败: %v", err)
		return
	}
	have := map[string]bool{}
	for _, g := range existing {
		have[g.Name] = true
	}
	for _, t := range irWorkflowTemplateGraphs() {
		if have[t.Name] {
			continue
		}
		if errs := t.Graph.Validate(); len(errs) > 0 {
			log.Printf("[ir] 模板 %q 校验失败,跳过: %v", t.Name, errs)
			continue
		}
		gj, _ := json.Marshal(t.Graph)
		if _, err := s.m.pg.CreateWorkflowGraph(t.Name, t.Description, string(gj)); err != nil {
			log.Printf("[ir] seed 模板 %q 失败: %v", t.Name, err)
		}
	}
}
