package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
	"github.com/RestXtra/RestXtraAI/metrics"
)

// toolFixHooks 是最内层的 PreToolUse 包装（P5.1 ToolCallFixer，借鉴 pentagi）：
// 模型偶发输出畸形 JSON 参数（尾逗号、多余闭合括号等）时，先用宽容修复尝试恢复，
// 只有修复后是合法 JSON 才替换输入（harness 会用返回的 updated 作为工具输入）。
// 修复失败则原样放行，让工具/SDK 按原有逻辑报错——绝不掩盖问题。
type toolFixHooks struct {
	inner harness.HookRunner
}

func (h toolFixHooks) PreToolUse(ctx context.Context, name string, input []byte) (bool, string, []byte) {
	up := repairToolInput(input)
	if string(up) != string(input) {
		metrics.M.Inc(&metrics.M.ToolFixes) // P5.4: 修复计数
	}
	if h.inner != nil {
		return h.inner.PreToolUse(ctx, name, up)
	}
	return false, "", up
}

func (h toolFixHooks) PostToolUse(ctx context.Context, name string, input, result []byte, isErr bool) {
	if h.inner != nil {
		h.inner.PostToolUse(ctx, name, input, result, isErr)
	}
}

func (h toolFixHooks) Stop(ctx context.Context, messages []llm.Message) (bool, []string, string) {
	if h.inner != nil {
		return h.inner.Stop(ctx, messages)
	}
	return false, nil, ""
}

// repairToolInput 尝试修复非法 JSON 工具参数。只接受"合法 JSON 且 != 原输入"的修复，
// 否则原样返回。修复是保守的，绝不破坏本可用的输入。
func repairToolInput(input []byte) []byte {
	s := strings.TrimSpace(string(input))
	if s == "" || json.Valid([]byte(s)) {
		return input
	}
	candidates := []string{
		reTrailingComma.ReplaceAllString(s, "$1"), // 去掉数组/对象里的尾逗号
		balanceBraces(s),                          // 去掉多余的闭合括号
	}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c != s && json.Valid([]byte(c)) {
			return []byte(c)
		}
	}
	return input
}

// reTrailingComma 匹配 `,` 后紧跟 `}` 或 `]`（含空白）——模型常见的尾逗号错误。
var reTrailingComma = regexp.MustCompile(`,\s*([}\]])`)

// balanceBraces 当右括号多于左括号时，从尾部删掉多余的 `}`/`]`（模型偶尔多打一个闭合）。
func balanceBraces(s string) string {
	open := strings.Count(s, "{") + strings.Count(s, "[")
	close := strings.Count(s, "}") + strings.Count(s, "]")
	diff := close - open
	if diff <= 0 {
		return s
	}
	b := []byte(s)
	for i := len(b) - 1; i >= 0 && diff > 0; i-- {
		if b[i] == '}' || b[i] == ']' {
			b = append(b[:i], b[i+1:]...)
			diff--
		}
	}
	return string(b)
}
