package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	actool "github.com/Autumn-27/norma/tool"
)

// PlaybookSearch 由服务端接线（检索攻击模式库），返回格式化文本。
// 供 search_playbook 工具调用；nil 时返回"攻击模式库未启用"。
var PlaybookSearch func(ctx context.Context, cve, technique, keywords string, limit int) (string, error)

// PlaybookSearchTool 让 worker/领域 agent 在任务中按 CVE / ATT&CK 技术 / 关键词检索攻击模式库，
// 命中后按 execution_steps 在目标上复用（自动复现的 playbook 优先）。
func (ts *ToolSet) PlaybookSearchTool() actool.CoreTool {
	return readTool("search_playbook",
		"检索平台攻击模式库（按 CVE 编号 / ATT&CK 技术 / 关键词）。返回匹配的攻击模式：标题、CVE、标签、验证状态与可复现的执行步骤。用于：遇到目标特征（技术栈/版本/CVE）时复用已验证的漏洞利用流程。",
		map[string]any{
			"type":     "object",
			"properties": map[string]any{
				"cve":       str("CVE 编号，如 CVE-2021-44228"),
				"technique": str("ATT&CK 技术 id，如 T1190"),
				"keywords":  str("关键词，如 log4j / rce / 反序列化"),
				"limit":     map[string]any{"type": "integer", "description": "返回条数（默认 5）"},
			},
		},
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if PlaybookSearch == nil {
				return actool.Text("攻击模式库未启用"), nil
			}
			var a struct {
				CVE       string `json:"cve"`
				Technique string `json:"technique"`
				Keywords  string `json:"keywords"`
				Limit     int    `json:"limit"`
			}
			_ = json.Unmarshal(in, &a)
			out, err := PlaybookSearch(ctx, strings.TrimSpace(a.CVE), strings.TrimSpace(a.Technique), strings.TrimSpace(a.Keywords), a.Limit)
			if err != nil {
				return actool.Errorf("攻击模式库检索失败: " + err.Error()), nil
			}
			if out == "" {
				return actool.Text("攻击模式库无匹配结果"), nil
			}
			return actool.Text(out), nil
		})
}

// FmtPlaybookResults 把检索结果格式化为 agent 可读文本（服务端回调用）。
func FmtPlaybookResults(patterns []struct {
	Title, CveID, Tags, Verification, ExecutionSteps string
}) string {
	var b strings.Builder
	for i, p := range patterns {
		fmt.Fprintf(&b, "%d. %s", i+1, p.Title)
		meta := []string{}
		if p.CveID != "" {
			meta = append(meta, p.CveID)
		}
		if p.Tags != "" {
			meta = append(meta, p.Tags)
		}
		meta = append(meta, "验证="+p.Verification)
		if len(meta) > 0 {
			fmt.Fprintf(&b, " [%s]", strings.Join(meta, ", "))
		}
		b.WriteString("\n")
		if p.ExecutionSteps != "" {
			b.WriteString("  复现步骤：\n" + indentBlock(p.ExecutionSteps, "    ") + "\n")
		}
	}
	return b.String()
}

func indentBlock(s, pad string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}
