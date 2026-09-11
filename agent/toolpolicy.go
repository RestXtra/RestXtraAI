package agent

import (
	"context"
	"strings"

	actool "github.com/Autumn-27/norma/tool"
)

type allowedToolsContextKey struct{}

// WithAllowedTools attaches a child-task capability whitelist. An empty list
// means the ordinary task policy and preserves existing behavior.
func WithAllowedTools(ctx context.Context, names []string) context.Context {
	if len(names) == 0 {
		return ctx
	}
	copyNames := append([]string(nil), names...)
	return context.WithValue(ctx, allowedToolsContextKey{}, copyNames)
}

func allowedToolsFromContext(ctx context.Context) []string {
	names, _ := ctx.Value(allowedToolsContextKey{}).([]string)
	return names
}

// capabilityAllowed gates SDK capabilities that are configured by booleans
// rather than represented in the Tools slice (notably WebFetch/WebSearch).
func capabilityAllowed(ctx context.Context, name string) bool {
	policy := allowedToolsFromContext(ctx)
	if len(policy) == 0 {
		return true
	}
	for _, allowed := range policy {
		if strings.EqualFold(strings.TrimSpace(allowed), name) {
			return true
		}
	}
	return false
}

// applyToolPolicy keeps the role's graph/control tools and filters every other
// capability to the delegation whitelist. Meta-tools remain available only as
// routing infrastructure; the underlying callable tool registry is still
// filtered, so they cannot bypass the whitelist.
func applyToolPolicy(ctx context.Context, tools []actool.CoreTool, def *DeferredInfo, required []actool.CoreTool) []actool.CoreTool {
	policy := allowedToolsFromContext(ctx)
	if len(policy) == 0 {
		return tools
	}
	keep := map[string]bool{
		"skill": true, "searchextratools": true, "executeextratool": true,
	}
	for _, name := range policy {
		keep[strings.ToLower(strings.TrimSpace(name))] = true
	}
	for _, tool := range required {
		keep[strings.ToLower(tool.Name())] = true
	}
	out := make([]actool.CoreTool, 0, len(tools))
	keptExact := map[string]bool{}
	for _, tool := range tools {
		if keep[strings.ToLower(tool.Name())] {
			out = append(out, tool)
			keptExact[tool.Name()] = true
		}
	}
	filterNames := func(names []string) []string {
		filtered := make([]string, 0, len(names))
		for _, name := range names {
			if keptExact[name] {
				filtered = append(filtered, name)
			}
		}
		return filtered
	}
	def.Deferred = filterNames(def.Deferred)
	def.GlobalNames = filterNames(def.GlobalNames)
	catalog := def.GlobalCatalog[:0]
	for _, entry := range def.GlobalCatalog {
		if keptExact[entry.Name] {
			catalog = append(catalog, entry)
		}
	}
	def.GlobalCatalog = catalog
	return out
}

// ToolAllowlistFor, if set by the server, returns a per-agent base-tool allowlist
// (by tool name). nil/empty means no restriction (unchanged behavior). It is used
// to strip execution tools from orchestrator agents (e.g. red_team_lead) so they
// can only delegate: they keep orchestration/read/report tools and lose the
// SDK defaults (Bash/Read/Write/Edit/LS/Glob/Grep) and WebFetch.
var ToolAllowlistFor func(agentKey string) []string

// AgentHasToolAllowlist reports whether a non-empty allowlist is configured for
// agentKey (i.e. the agent is tool-restricted).
func AgentHasToolAllowlist(agentKey string) bool {
	if ToolAllowlistFor == nil {
		return false
	}
	return len(ToolAllowlistFor(agentKey)) > 0
}

// AgentToolAllowed reports whether name is permitted for agentKey under the
// allowlist. No allowlist configured → everything allowed.
func AgentToolAllowed(agentKey, name string) bool {
	if ToolAllowlistFor == nil {
		return true
	}
	allow := ToolAllowlistFor(agentKey)
	if len(allow) == 0 {
		return true
	}
	for _, n := range allow {
		if strings.EqualFold(strings.TrimSpace(n), name) {
			return true
		}
	}
	return false
}

// ApplyAgentToolAllowlist filters an assembled chat tool set down to the agent's
// allowlist and prunes the deferred wiring, so meta-tools cannot reach a filtered
// capability. No allowlist → tools returned unchanged.
func ApplyAgentToolAllowlist(agentKey string, tools []actool.CoreTool, def *DeferredInfo) []actool.CoreTool {
	if ToolAllowlistFor == nil {
		return tools
	}
	allow := ToolAllowlistFor(agentKey)
	if len(allow) == 0 {
		return tools
	}
	keep := map[string]bool{}
	for _, n := range allow {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			keep[n] = true
		}
	}
	out := make([]actool.CoreTool, 0, len(tools))
	kept := map[string]bool{}
	for _, tool := range tools {
		if keep[strings.ToLower(tool.Name())] {
			out = append(out, tool)
			kept[tool.Name()] = true
		}
	}
	if def != nil {
		filterNames := func(names []string) []string {
			f := make([]string, 0, len(names))
			for _, n := range names {
				if kept[n] {
					f = append(f, n)
				}
			}
			return f
		}
		def.Deferred = filterNames(def.Deferred)
		def.GlobalNames = filterNames(def.GlobalNames)
		catalog := def.GlobalCatalog[:0]
		for _, e := range def.GlobalCatalog {
			if kept[e.Name] {
				catalog = append(catalog, e)
			}
		}
		def.GlobalCatalog = catalog
	}
	return out
}
