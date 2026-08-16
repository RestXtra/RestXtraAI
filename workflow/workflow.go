// Package workflow implements a DAG-based workflow graph engine (基于
// 可视化工作流设计思路)：start / tool / agent / condition / hitl / output / end
// 七类节点，{{inputs/previous/outputs/节点ID}} 模板变量 + 条件表达式。
// 校验（必须 DAG、有 start+output、无自环、全可达）通过后可 dry-run 或正式运行。
package workflow

import (
	"fmt"
	"strings"
)

// NodeKind 是工作流节点类型。
type NodeKind string

const (
	KindStart     NodeKind = "start"
	KindTool      NodeKind = "tool"
	KindAgent     NodeKind = "agent"
	KindCondition NodeKind = "condition"
	KindHitl      NodeKind = "hitl"
	KindOutput    NodeKind = "output"
	KindEnd       NodeKind = "end"
)

// Node 是工作流中的一个节点。
type Node struct {
	ID          string   `json:"id"`
	Kind        NodeKind `json:"kind"`
	Label       string   `json:"label,omitempty"`
	Instruction string   `json:"instruction,omitempty"` // agent/hitl 节点指令
	Tool        string   `json:"tool,omitempty"`        // tool 节点工具名
	Args        string   `json:"args,omitempty"`        // tool 节点参数 JSON 模板（支持 {{}}）
	Expression  string   `json:"expression,omitempty"`  // condition 节点表达式
	OutputKey   string   `json:"output_key,omitempty"`  // 写入 outputs 池的变量名
	Agent       string   `json:"agent,omitempty"`       // agent 节点指定 agent key
	Reviewer    string   `json:"reviewer,omitempty"`    // hitl 审批方 human|audit_agent
	Join        string   `json:"join,omitempty"`        // 多上游汇聚 all_merge|first_non_empty|fail_fast
}

// Edge 是一条有向边 from→to。
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Graph 是完整的工作流图。
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// NodeRun 是一次运行中单个节点的执行记录。
type NodeRun struct {
	NodeID string `json:"node_id"`
	Kind   NodeKind `json:"kind"`
	Status string `json:"status"` // pending|skipped|ok|error|paused
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// RunResult 是一次工作流运行的完整结果。
type RunResult struct {
	Status   string            `json:"status"` // running|completed|failed|paused
	Outputs  map[string]string `json:"outputs"`
	NodeRuns []NodeRun         `json:"node_runs"`
	Final    string            `json:"final,omitempty"`
}

// Validate 校验图结构（DAG、必需节点、边合法性）。返回错误列表。
func (g *Graph) Validate() []string {
	var errs []string
	if len(g.Nodes) == 0 {
		return []string{"图不能为空"}
	}
	byID := map[string]*Node{}
	hasStart, hasTerminal := false, false
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.ID == "" {
			errs = append(errs, "存在无 id 的节点")
			continue
		}
		if _, dup := byID[n.ID]; dup {
			errs = append(errs, "重复节点 id: "+n.ID)
			continue
		}
		byID[n.ID] = n
		switch n.Kind {
		case KindStart:
			hasStart = true
		case KindOutput, KindEnd:
			hasTerminal = true
		case KindTool:
			if strings.TrimSpace(n.Tool) == "" {
				errs = append(errs, fmt.Sprintf("节点 %s 是 tool 但未指定工具", n.ID))
			}
		case KindCondition:
			if strings.TrimSpace(n.Expression) == "" {
				errs = append(errs, fmt.Sprintf("节点 %s 是 condition 但表达式为空", n.ID))
			}
		case KindAgent:
			if strings.TrimSpace(n.Instruction) == "" && strings.TrimSpace(n.Agent) == "" {
				errs = append(errs, fmt.Sprintf("节点 %s 是 agent 但缺少指令或 agent", n.ID))
			}
		}
	}
	if !hasStart {
		errs = append(errs, "缺少 start 节点")
	}
	if !hasTerminal {
		errs = append(errs, "缺少 output 或 end 节点")
	}
	// 边合法性
	inDeg := map[string]int{}
	out := map[string][]string{}
	for i := range g.Nodes {
		inDeg[g.Nodes[i].ID] = 0
	}
	for _, e := range g.Edges {
		if _, ok := byID[e.From]; !ok {
			errs = append(errs, fmt.Sprintf("边引用不存在的节点: %s", e.From))
			continue
		}
		if _, ok := byID[e.To]; !ok {
			errs = append(errs, fmt.Sprintf("边引用不存在的节点: %s", e.To))
			continue
		}
		if e.From == e.To {
			errs = append(errs, fmt.Sprintf("自环: %s→%s", e.From, e.To))
			continue
		}
		inDeg[e.To]++
		out[e.From] = append(out[e.From], e.To)
	}
	// start 无入边；output/end 无出边
	for id, n := range byID {
		if n.Kind == KindStart && inDeg[id] > 0 {
			errs = append(errs, "start 节点不应有入边")
		}
		if (n.Kind == KindOutput || n.Kind == KindEnd) && len(out[id]) > 0 {
			errs = append(errs, "output/end 节点不应有出边")
		}
	}
	// DAG（拓扑排序判环）+ 从 start 可达性
	order, cycle := topoOrder(g, inDeg, out)
	if cycle {
		errs = append(errs, "图存在环（必须为 DAG）")
	}
	if len(order) < len(byID) {
		// 环内节点未入序，已被上面报错
	}
	// 可达性：所有节点需从某 start 可达
	startIDs := []string{}
	for id, n := range byID {
		if n.Kind == KindStart {
			startIDs = append(startIDs, id)
		}
	}
	reach := map[string]bool{}
	for _, s := range startIDs {
		dfs(s, out, reach)
	}
	for id := range byID {
		if !reach[id] {
			errs = append(errs, fmt.Sprintf("节点 %s 不可达（需从 start 出发可达）", id))
		}
	}
	return errs
}

// Topo 返回拓扑序（执行顺序）。
func (g *Graph) Topo() []string {
	inDeg := map[string]int{}
	out := map[string][]string{}
	for i := range g.Nodes {
		inDeg[g.Nodes[i].ID] = 0
	}
	for _, e := range g.Edges {
		inDeg[e.To]++
		out[e.From] = append(out[e.From], e.To)
	}
	order, _ := topoOrder(g, inDeg, out)
	return order
}

func topoOrder(g *Graph, inDeg map[string]int, out map[string][]string) ([]string, bool) {
	queue := []string{}
	for _, n := range g.Nodes {
		if inDeg[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}
	order := []string{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, t := range out[id] {
			inDeg[t]--
			if inDeg[t] == 0 {
				queue = append(queue, t)
			}
		}
	}
	return order, len(order) != len(g.Nodes)
}

func dfs(id string, out map[string][]string, reach map[string]bool) {
	if reach[id] {
		return
	}
	reach[id] = true
	for _, t := range out[id] {
		dfs(t, out, reach)
	}
}
