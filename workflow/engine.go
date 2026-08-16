package workflow

import (
	"context"
	"fmt"
	"strings"
)

// Engine 执行一个工作流图。tool / agent / hitl 节点的实际行为由注入的回调提供，
// 使引擎可与平台工具、LLM agent、拦截审批解耦。
type Engine struct {
	// RunTool 执行一个 tool 节点。tool 为工具名，args 为已渲染的参数 JSON。
	RunTool func(ctx context.Context, tool, args string) (string, error)
	// RunAgent 执行一个 agent 节点（指令化单轮）。
	RunAgent func(ctx context.Context, agent, instruction string) (string, error)
	// Hitl 处理一个人工审批节点，返回是否通过。
	Hitl func(ctx context.Context, node Node) (approved bool, comment string, err error)
	// DryRun 时 tool/agent 不真正执行。
	Dry bool
}

// Run 按拓扑序执行图。
func (e *Engine) Run(ctx context.Context, g *Graph, inputs map[string]string) (*RunResult, error) {
	res := &RunResult{Status: "running", Outputs: map[string]string{}}
	if errs := g.Validate(); len(errs) > 0 {
		res.Status = "failed"
		return res, fmt.Errorf("图校验失败: %s", strings.Join(errs, "; "))
	}
	if inputs == nil {
		inputs = map[string]string{}
	}
	nodeOut := map[string]string{}
	vars := func(n *Node, prev string) map[string]string {
		m := map[string]string{}
		for k, v := range inputs {
			m["inputs."+k] = v
		}
		m["previous.output"] = prev
		for k, v := range res.Outputs {
			m["outputs."+k] = v
		}
		for id, v := range nodeOut {
			m[id+".output"] = v
		}
		return m
	}

	order := g.Topo()
	prev := ""
	completed := false
	for _, id := range order {
		if completed {
			break
		}
		var n *Node
		for i := range g.Nodes {
			if g.Nodes[i].ID == id {
				n = &g.Nodes[i]
				break
			}
		}
		if n == nil {
			continue
		}
		run := NodeRun{NodeID: id, Kind: n.Kind}
		switch n.Kind {
		case KindStart:
			run.Status, run.Output = "ok", "started"
		case KindTool:
			args := Resolve(n.Args, vars(n, prev))
			if e.Dry {
				run.Status, run.Output = "ok", "[dry-run] tool call skipped"
			} else if e.RunTool == nil {
				run.Status, run.Error = "error", "未配置工具执行器"
			} else {
				out, err := e.RunTool(ctx, n.Tool, args)
				if err != nil {
					run.Status, run.Error = "error", err.Error()
				} else {
					run.Status, run.Output = "ok", out
				}
			}
		case KindAgent:
			inst := Resolve(n.Instruction, vars(n, prev))
			if e.Dry {
				run.Status, run.Output = "ok", "[dry-run] agent call skipped"
			} else if e.RunAgent == nil {
				run.Status, run.Error = "error", "未配置 agent 执行器"
			} else {
				out, err := e.RunAgent(ctx, n.Agent, inst)
				if err != nil {
					run.Status, run.Error = "error", err.Error()
				} else {
					run.Status, run.Output = "ok", out
				}
			}
		case KindCondition:
			ok, err := EvalCondition(n.Expression, vars(n, prev))
			if err != nil {
				run.Status, run.Error = "error", err.Error()
			} else {
				run.Status, run.Output = "ok", fmt.Sprintf("%t", ok)
			}
		case KindHitl:
			if e.Dry {
				run.Status, run.Output = "ok", "approved"
			} else if e.Hitl == nil {
				run.Status, run.Error = "error", "未配置审批执行器"
			} else {
				ok, comment, err := e.Hitl(ctx, *n)
				if err != nil {
					run.Status, run.Error = "error", err.Error()
				} else {
					run.Output = fmt.Sprintf("approved=%t comment=%s", ok, comment)
					if ok {
						run.Status = "ok"
					} else {
						run.Status = "paused"
						res.Status = "paused"
					}
				}
			}
		case KindOutput:
			run.Status, run.Output = "ok", Resolve(n.Instruction, vars(n, prev))
			res.Final = run.Output
			res.Status = "completed"
			completed = true
		case KindEnd:
			run.Status, run.Output = "ok", "end"
			if res.Status == "running" {
				res.Status = "completed"
			}
			completed = true
		}
		if run.Status == "error" {
			res.Status = "failed"
		}
		nodeOut[id] = run.Output
		if n.OutputKey != "" && run.Output != "" {
			res.Outputs[n.OutputKey] = run.Output
		}
		res.NodeRuns = append(res.NodeRuns, run)
		prev = run.Output
		if res.Status != "running" {
			completed = true
		}
	}
	return res, nil
}
