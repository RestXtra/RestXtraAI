package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Autumn-27/norma/compaction"
	"github.com/Autumn-27/norma/llm"
)

// DeterministicSummarizer 是 P7.3 的免 LLM 摘要器（借鉴 VulnClaw digest / pentagi ChainAST 思路）：
// 把消息链压缩成结构化摘要（角色 + 截断文本 / 工具名+参数 / 工具结果头），
// **不调用任何模型**——避免 compaction 触发时的额外 LLM 调用成本，也避免"用模型摘要模型"的递归膨胀。
// 保留最近工具轨迹要点（tool_call↔result 相邻出现），细节丢失可让模型用工具重新获取。
var DeterministicSummarizer compaction.Summarizer = func(_ context.Context, msgs []llm.Message) (string, error) {
	var b strings.Builder
	b.WriteString("[确定性上下文摘要 · 系统生成（非模型输出）]")
	for _, m := range msgs {
		role := string(m.Role)
		if role == "" {
			role = "?"
		}
		b.WriteString("\n[" + role + "] ")
		parts := summarizeBlocks(m.Content)
		if len(parts) == 0 {
			if t := strings.TrimSpace(m.Text()); t != "" {
				b.WriteString(truncStr(t, 200))
			} else {
				b.WriteString("(empty)")
			}
			continue
		}
		b.WriteString(strings.Join(parts, " | "))
	}
	b.WriteString("\n（以上为系统确定性摘要，保留最近工具轨迹要点；被截断的细节可用工具重新获取）")
	return b.String(), nil
}

// summarizeBlocks 把一条消息的 content 块压成要点串。
func summarizeBlocks(blocks []llm.ContentBlock) []string {
	var parts []string
	for _, blk := range blocks {
		switch blk.Type {
		case llm.BlockText:
			if t := strings.TrimSpace(blk.Text); t != "" {
				parts = append(parts, truncStr(t, 200))
			}
		case llm.BlockThinking:
			if t := strings.TrimSpace(blk.Thinking); t != "" {
				parts = append(parts, "[thinking: "+truncStr(t, 80)+"]")
			}
		case llm.BlockToolUse:
			in, _ := json.Marshal(blk.Input)
			parts = append(parts, "tool("+blk.Name+": "+truncStr(string(in), 120)+")")
		case llm.BlockToolResult:
			var t strings.Builder
			for _, c := range blk.Content {
				if c.Type == llm.BlockText {
					t.WriteString(c.Text)
				}
			}
			if t.Len() == 0 {
				t.WriteString("(no text)")
			}
			mark := ""
			if blk.IsError {
				mark = "err "
			}
			parts = append(parts, "result("+mark+truncStr(t.String(), 160)+")")
		}
	}
	return parts
}

func truncStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
