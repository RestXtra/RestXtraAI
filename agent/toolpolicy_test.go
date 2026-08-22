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
