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
