package agent

import (
	"encoding/json"
	"testing"
)

func BenchmarkAgentTurnPreparation(b *testing.B) {
	tmpl := "Goal: {{.Goal}}\nScope: {{.Scope}}\nAssets: {{.AssetSummary}}\nNow: {{.Now}}"
	vars := PlannerVars{
		Goal: "Map the authorized target and verify findings with evidence",
		Scope: "example.com and 10.0.0.0/24",
		AssetSummary: "42 services, 7 web applications, 3 confirmed findings",
		Now: "2026-08-21T00:00:00Z",
	}
	schema := map[string]any{"properties": map[string]any{
		"target": map[string]any{"type": "string", "default": "example.com"},
		"ports":  map[string]any{"type": "string", "default": "80,443"},
	}}
	input := json.RawMessage(`{"target":""}`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		prompt := renderSystem("planner", tmpl, vars)
		resolved := injectDefaults(input, schema)
		if prompt == "" || len(resolved) == 0 {
			b.Fatal("turn preparation returned empty output")
		}
	}
}
