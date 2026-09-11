package agent

import (
	"context"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

func policyTestTool(name string) actool.CoreTool {
	return actool.Build(actool.Spec{Name: name, Description: name})
}

func TestApplyToolPolicyKeepsCoreAndWhitelistedCapabilities(t *testing.T) {
	core := policyTestTool("record_fact")
	allowed := policyTestTool("nmap")
	denied := policyTestTool("Bash")
	meta := policyTestTool("ExecuteExtraTool")
	def := DeferredInfo{
		Deferred:    []string{"nmap", "Bash"},
		GlobalNames: []string{"nmap", "Bash"},
		GlobalCatalog: []actool.CatalogEntry{
			{Name: "nmap"}, {Name: "Bash"},
		},
	}
	ctx := WithAllowedTools(context.Background(), []string{"NMAP"})
	got := applyToolPolicy(ctx, []actool.CoreTool{core, allowed, denied, meta}, &def, []actool.CoreTool{core})
	names := map[string]bool{}
	for _, tool := range got {
		names[tool.Name()] = true
	}
	if !names["record_fact"] || !names["nmap"] || !names["ExecuteExtraTool"] {
		t.Fatalf("required/allowed tools missing: %v", names)
	}
	if names["Bash"] {
		t.Fatalf("denied capability survived policy: %v", names)
	}
	if len(def.Deferred) != 1 || def.Deferred[0] != "nmap" ||
		len(def.GlobalNames) != 1 || def.GlobalNames[0] != "nmap" ||
		len(def.GlobalCatalog) != 1 || def.GlobalCatalog[0].Name != "nmap" {
		t.Fatalf("deferred catalog not filtered: %+v", def)
	}
}

func TestApplyToolPolicyEmptyListPreservesOrdinaryTask(t *testing.T) {
	tools := []actool.CoreTool{policyTestTool("Bash"), policyTestTool("nmap")}
	def := DeferredInfo{Deferred: []string{"nmap"}}
	got := applyToolPolicy(context.Background(), tools, &def, nil)
	if len(got) != len(tools) || len(def.Deferred) != 1 {
		t.Fatalf("empty policy changed ordinary task: tools=%d def=%+v", len(got), def)
	}
}

func TestCapabilityAllowedHonorsDelegationPolicy(t *testing.T) {
	ordinary := context.Background()
	if !capabilityAllowed(ordinary, "WebFetch") {
		t.Fatal("ordinary task unexpectedly lost WebFetch")
	}
	limited := WithAllowedTools(ordinary, []string{"WebSearch"})
	if capabilityAllowed(limited, "WebFetch") || !capabilityAllowed(limited, "websearch") {
		t.Fatal("SDK capability policy not enforced")
	}
}

func TestAgentToolAllowlistFiltersExecutionTools(t *testing.T) {
	orig := ToolAllowlistFor
	defer func() { ToolAllowlistFor = orig }()
	ToolAllowlistFor = func(key string) []string {
		if key == "red_team_lead" {
			return []string{"spawn_task", "wait_task", "skill"}
		}
		return nil
	}
	tools := []actool.CoreTool{
		policyTestTool("spawn_task"), policyTestTool("wait_task"),
		policyTestTool("Skill"), policyTestTool("Bash"), policyTestTool("Read"),
		policyTestTool("ExecuteExtraTool"),
	}
	def := DeferredInfo{
		Deferred:      []string{"Bash"},
		GlobalNames:   []string{"Bash", "spawn_task"},
		GlobalCatalog: []actool.CatalogEntry{{Name: "Bash"}, {Name: "spawn_task"}},
	}
	got := ApplyAgentToolAllowlist("red_team_lead", tools, &def)
	names := map[string]bool{}
	for _, tl := range got {
		names[tl.Name()] = true
	}
	if !names["spawn_task"] || !names["wait_task"] || !names["Skill"] {
		t.Fatalf("allowlisted tools missing: %v", names)
	}
	if names["Bash"] || names["Read"] || names["ExecuteExtraTool"] {
		t.Fatalf("execution tools survived allowlist: %v", names)
	}
	if len(def.Deferred) != 0 || len(def.GlobalCatalog) != 1 || def.GlobalCatalog[0].Name != "spawn_task" {
		t.Fatalf("deferred wiring not pruned: %+v", def)
	}
	if AgentToolAllowed("red_team_lead", "Bash") || !AgentToolAllowed("red_team_lead", "spawn_task") {
		t.Fatal("AgentToolAllowed mismatch")
	}
	if !AgentToolAllowed("other_agent", "Bash") {
		t.Fatal("agent without allowlist must keep all tools")
	}
}

func TestApplyAgentToolAllowlistNoopWithoutConfig(t *testing.T) {
	orig := ToolAllowlistFor
	defer func() { ToolAllowlistFor = orig }()
	ToolAllowlistFor = nil
	tools := []actool.CoreTool{policyTestTool("Bash")}
	got := ApplyAgentToolAllowlist("red_team_lead", tools, nil)
	if len(got) != 1 {
		t.Fatalf("no-op expected, got %d tools", len(got))
	}
}
