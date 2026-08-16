package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"

	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
)

// 证据闸门（反幻觉）：基于 AgentState + completion gate 双重要求。
//   - 每次工具调用结果进入 EvidenceStore（原文完整保留 + sha256 去重）。
//   - 完成前校验：FINAL 引用的证据 id 必须真实存在；目标含 flag 时，声称的
//     flag 必须【逐字出现在工具输出】里——杜绝模型编造结论/flag。
//   - 拒绝时把原因回注给模型，让它基于真实证据修正后重答（worker 最多重试 2 次）。

type EvidenceRecord struct {
	ID          int    `json:"id"`
	Tool        string `json:"tool"`
	Input       string `json:"input,omitempty"`
	Content     string `json:"content"` // 原始工具输出
	IsError     bool   `json:"is_error"`
	ContentHash string `json:"content_hash"`
	DuplicateOf int    `json:"duplicate_of,omitempty"` // 与另一条内容相同（same as eXXX）
}

// EvidenceStore 是单次 worker 运行内的证据记忆（跨多轮/重试累计）。
type EvidenceStore struct {
	mu   sync.Mutex
	next int
	recs []*EvidenceRecord
}

func NewEvidenceStore() *EvidenceStore {
	return &EvidenceStore{next: 1}
}

// Add 记录一次工具结果；相同内容去重（content_hash）。返回（可能被去重的）记录。
func (e *EvidenceStore) Add(tool, input, content string, isErr bool) *EvidenceRecord {
	content = strings.TrimSpace(content)
	hash := sha256Hex(content)
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.recs {
		if r.ContentHash == hash && hash != "" {
			return &EvidenceRecord{ID: r.ID, Tool: tool, Input: input, Content: content,
				IsError: isErr, ContentHash: hash, DuplicateOf: r.ID}
		}
	}
	rec := &EvidenceRecord{ID: e.next, Tool: tool, Input: input, Content: content, IsError: isErr, ContentHash: hash}
	e.next++
	e.recs = append(e.recs, rec)
	return rec
}

// AllText 拼接全部证据原文（供 flag 逐字校验）。
func (e *EvidenceStore) AllText() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var b strings.Builder
	for _, r := range e.recs {
		b.WriteString(r.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func (e *EvidenceStore) HasID(id int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.recs {
		if r.ID == id {
			return true
		}
	}
	return false
}

func (e *EvidenceStore) Records() []*EvidenceRecord {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]*EvidenceRecord, len(e.recs))
	copy(out, e.recs)
	return out
}

func sha256Hex(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

var (
	reEvidenceID = regexp.MustCompile(`\be(\d{3,})\b`)
	reFlag       = regexp.MustCompile(`(?:flag|ctf)\{[^}\s]{0,200}\}`)
)

// extractFlags 提取文本中形如 flag{...} / ctf{...} 的 flag。
func extractFlags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range reFlag.FindAllString(s, -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// CheckCompletion 校验最终回答：
//   - 引用未知证据 id → 拒绝
//   - goal 含 flag 语义时，回答里的 flag 必须逐字出现在证据里
func (e *EvidenceStore) CheckCompletion(finalText, goal string) (bool, string) {
	text := strings.TrimSpace(finalText)
	if text == "" {
		return false, "最终总结为空，请给出基于证据的结论"
	}
	for _, m := range reEvidenceID.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 {
			continue
		}
		var id int
		for _, ch := range m[1] {
			id = id*10 + int(ch-'0')
		}
		if !e.HasID(id) {
			return false, "引用了不存在的证据 id e" + m[1] + "（证据 id 必须是本轮工具输出的编号）"
		}
	}
	if goalWantsFlag(goal) {
		claimed := extractFlags(text)
		if len(claimed) == 0 {
			return false, "目标要求拿到 flag，但最终总结中没有 flag"
		}
		evidence := e.AllText()
		for _, f := range claimed {
			if !strings.Contains(evidence, f) {
				return false, "声称的 flag「" + f + "」未在真实工具输出中出现——flag 必须逐字来自工具结果"
			}
		}
	}
	return true, ""
}

// goalWantsFlag 判定目标是否期望 flag（宽松启发式）。
func goalWantsFlag(goal string) bool {
	g := strings.ToLower(goal)
	return strings.Contains(g, "flag") || strings.Contains(g, "ctf{") ||
		strings.Contains(g, "拿到") || strings.Contains(g, "获取 flag") || strings.Contains(g, "拿下")
}

// evidenceHooks 包裹原有 guard hooks，把每次工具调用结果收进证据库。
type evidenceHooks struct {
	inner harness.HookRunner
	ev    *EvidenceStore
}

func (h evidenceHooks) PreToolUse(ctx context.Context, name string, input []byte) (bool, string, []byte) {
	if h.inner != nil {
		return h.inner.PreToolUse(ctx, name, input)
	}
	return false, "", nil
}

func (h evidenceHooks) PostToolUse(ctx context.Context, name string, input, result []byte, isErr bool) {
	if h.ev != nil {
		h.ev.Add(name, string(input), string(result), isErr)
	}
	if h.inner != nil {
		h.inner.PostToolUse(ctx, name, input, result, isErr)
	}
}

func (h evidenceHooks) Stop(ctx context.Context, messages []llm.Message) (bool, []string, string) {
	if h.inner != nil {
		return h.inner.Stop(ctx, messages)
	}
	return false, nil, ""
}
