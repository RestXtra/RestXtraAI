package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Autumn-27/norma/agentcore"
	acperm "github.com/Autumn-27/norma/permission"
)

// 攻击链 DAG 自动建模：
// 读取任务真实工具执行轨迹 → 一次 LLM 调用转成 attack-chain DAG
// （target/action/vulnerability 节点 + leads_to/discovers/enables 边 + risk_score）。
// 反幻觉：没有实际工具执行记录时返回空链（严禁杜撰）。

type attackNode struct {
	ID    int    `json:"id"`
	Type  string `json:"type"` // target|action|vulnerability
	Label string `json:"label"`
}

type attackEdge struct {
	From int    `json:"from"`
	To   int    `json:"to"`
	Type string `json:"type"` // leads_to|discovers|enables
}

type attackChain struct {
	Summary   string       `json:"summary"`
	RiskScore int          `json:"risk_score"`
	Nodes     []attackNode `json:"nodes"`
	Edges     []attackEdge `json:"edges"`
}

// buildAttackChainPrompt 组装 LLM 提示词。
func buildAttackChainPrompt(goal string, steps []string) string {
	joined := ""
	if len(steps) > 20 {
		joined = strings.Join(steps[:20], "\n")
	} else {
		joined = strings.Join(steps, "\n")
	}
	return fmt.Sprintf(`任务目标：%s

以下是该任务实际执行过的工具步骤（真实轨迹）：
%s

请把这些步骤归纳为一条攻击链 DAG，严格遵守：
1. 节点类型仅三种：target(目标) / action(攻击动作) / vulnerability(漏洞)。
2. 边类型仅三种：leads_to(动作→动作) / discovers(动作→漏洞) / enables(漏洞→漏洞/动作)。
3. 必须是有向无环图：所有边的 from 编号 < to 编号（按节点编号严格递增）。
4. 只基于上面列出的真实步骤归纳，严禁编造未出现的目标/动作/漏洞。
5. 给出 risk_score(0-100) 与一句话 summary。

只输出 JSON（不要 markdown 代码块），格式：
{"summary":"...","risk_score":85,"nodes":[{"id":1,"type":"target","label":"..."}],"edges":[{"from":1,"to":2,"type":"leads_to"}]}`, goal, joined)
}

// taskAttackChain 生成/返回一个任务的攻击链 DAG。
func (s *Server) taskAttackChain(w http.ResponseWriter, r *http.Request) {
	taskID, _ := pathInt(r, "id")
	t, ok := s.m.Task(fmt.Sprint(taskID))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	// 采集真实工具轨迹（tool_use + tool_result 摘要）。
	items, _, err := t.Store.ActivityList(nil, 0, 1000)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var steps []string
	for _, a := range items {
		if a.Kind == "tool_use" && a.Tool != "" {
			steps = append(steps, fmt.Sprintf("%s: %s", a.Tool, firstLine(a.Summary, 200)))
		}
	}
	if len(steps) == 0 {
		// 无实际工具执行 → 空链（防幻觉）。
		writeJSON(w, 200, map[string]any{"summary": "", "risk_score": 0, "nodes": []any{}, "edges": []any{}})
		return
	}
	cfg, ok := s.loadLLMConfig()
	if !ok {
		writeErr(w, 400, "LLM 未配置")
		return
	}
	prov, err := cfg.NewProvider()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	prompt := buildAttackChainPrompt(t.Goal, steps)
	out, err := agentcore.Run(ctx, agentcore.Options{
		Provider:       prov,
		SystemPrompt:   []string{"你是攻击链建模器。只输出符合约束的 JSON。"},
		PermissionMode: acperm.ModeBypass,
		MaxTurns:       1,
		MaxTokens:      2048,
	}, prompt)
	if err != nil {
		writeErr(w, 500, "LLM 调用失败: "+err.Error())
		return
	}
	chain, err := parseAttackChain(out)
	if err != nil {
		writeErr(w, 502, "解析攻击链失败: "+err.Error())
		return
	}
	writeJSON(w, 200, chain)
}

// parseAttackChain 宽容解析 LLM 输出（去 code fence、取首个 {）。
func parseAttackChain(out string) (*attackChain, error) {
	text := strings.TrimSpace(out)
	if i := strings.Index(text, "```"); i >= 0 {
		text = text[:i] + text[strings.Index(text[i+3:], "```")+i+6:]
	}
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return nil, fmt.Errorf("输出中未找到 JSON")
	}
	end := strings.LastIndexByte(text, '}')
	if end <= start {
		return nil, fmt.Errorf("JSON 不完整")
	}
	var chain attackChain
	if err := json.Unmarshal([]byte(text[start:end+1]), &chain); err != nil {
		return nil, err
	}
	// 校验 DAG：from < to
	for _, e := range chain.Edges {
		if e.From >= e.To {
			return nil, fmt.Errorf("攻击链违反 DAG 约束（边 %d→%d）", e.From, e.To)
		}
	}
	return &chain, nil
}
