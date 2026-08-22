package agent

import (
	"encoding/json"

	"github.com/Autumn-27/norma/llm"
	actool "github.com/Autumn-27/norma/tool"
)

// deferredSystem builds an agent's system-prompt segments and cache boundary from
// its DeferredInfo. Globally-available deferred tools render as a compact catalog
// (name, bounded description, schema-token estimate, and operational metadata)
// placed as the LAST system-prompt segment, with DynamicBoundary set so the whole
// (session-fixed) system prompt — including the block — is cached (design doc
// §2.1 / C1). Skill-gated MCP names are NOT in this block; they surface when their
// skill loads.
//
// P2.1: 即使没有 deferred 块，也返回 boundary=1 —— 让纯静态的 system prompt 始终被
// 作为单个 cache_control 块缓存（norma 对 boundary>=len 缓存整段；P1.1 已把动态内容
// 移出 system，所以这里的 sysText 是静态的，跨唤醒/跨 intent 可命中 Anthropic 前缀缓存）。
func deferredSystem(sysText string, def DeferredInfo) (system []string, boundary int) {
	block := actool.RenderToolCatalogBlock(def.GlobalCatalog)
	if block == "" {
		block = actool.RenderDeferredToolsBlock(def.GlobalNames)
	}
	if block == "" {
		return []string{sysText}, 1 // 缓存单段静态 system（P2.1）
	}
	system = []string{sysText, block}
	boundary = len(system) // b >= len → whole system prompt cached (SDK guard)
	return system, boundary
}

// seedUnlockFromHistory replays prior Skill() invocations in the conversation so
// their skill-gated MCPs are re-unlocked on a resumed session (design doc C2). The
// main agent builds a fresh session each turn; its in-memory unlock set would
// otherwise reset, leaving the model able to see a skill-revealed tool name yet
// unable to call it. No-op when unlockSkill is nil (no deferred tools).
func seedUnlockFromHistory(msgs []llm.Message, unlockSkill func(string)) {
	if unlockSkill == nil {
		return
	}
	for _, m := range msgs {
		for _, b := range m.ToolUses() {
			if b.Name != "Skill" {
				continue
			}
			var in struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(b.Input, &in) == nil && in.Name != "" {
				unlockSkill(in.Name)
			}
		}
	}
}
