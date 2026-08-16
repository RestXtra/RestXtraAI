package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	actool "github.com/Autumn-27/norma/tool"
)

// KnowledgeSearch 由服务端接线（查询平台知识库），返回格式化结果字符串。
// 供 search_knowledge 工具调用；nil 时工具返回"知识库未启用"。
var KnowledgeSearch func(ctx context.Context, q string, limit int) (string, error)

// KnowledgeSearchTool 让 worker/planner 在任务中按关键词检索平台知识库
// （漏洞手法 / playbook / 技能要点），补足 agent 的方法论文档。
func (ts *ToolSet) KnowledgeSearchTool() actool.CoreTool {
	return readTool("search_knowledge",
		"从平台知识库检索与关键词相关的文档（Web漏洞手法/攻击playbook/绕过技巧等）。返回标题与匹配片段。关键词可用：sqli、xss、ssrf、ssti、xxe、rce、反序列化、cloud、evasion 等。",
		map[string]any{
			"type":     "object",
			"properties": map[string]any{
				"q":     str("搜索关键词"),
				"limit": map[string]any{"type": "integer", "description": "返回条数（默认 5）"},
			},
			"required": []any{"q"},
		},
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if KnowledgeSearch == nil {
				return actool.Text("知识库未启用"), nil
			}
			var a struct {
				Q     string `json:"q"`
				Limit int    `json:"limit"`
			}
			_ = json.Unmarshal(in, &a)
			out, err := KnowledgeSearch(ctx, strings.TrimSpace(a.Q), a.Limit)
			if err != nil {
				return actool.Errorf("知识库检索失败: " + err.Error()), nil
			}
			if out == "" {
				return actool.Text("知识库无匹配结果"), nil
			}
			return actool.Text(out), nil
		})
}

// FmtKnowledgeResult 把检索结果格式化为 agent 可读文本（服务端回调用）。
func FmtKnowledgeResult(items []struct {
	Title, Content, Tags string
}) string {
	var b strings.Builder
	for i, it := range items {
		fmt.Fprintf(&b, "%d. %s", i+1, it.Title)
		if it.Tags != "" {
			fmt.Fprintf(&b, " [%s]", it.Tags)
		}
		b.WriteString("\n")
		content := strings.TrimSpace(it.Content)
		if len(content) > 400 {
			content = content[:400] + "…"
		}
		if content != "" {
			b.WriteString("   " + strings.ReplaceAll(content, "\n", " ") + "\n")
		}
	}
	return b.String()
}
