package server

import "testing"

// TestIRWorkflowTemplatesValid verifies every built-in IR DAG template passes
// workflow.Validate (DAG, required nodes, reachability) — no DB required.
func TestIRWorkflowTemplatesValid(t *testing.T) {
	templates := irWorkflowTemplateGraphs()
	if len(templates) < 3 {
		t.Fatalf("expected >=3 IR templates, got %d", len(templates))
	}
	seen := map[string]bool{}
	for _, tmpl := range templates {
		if tmpl.Name == "" || tmpl.Graph.Validate() == nil {
			// name non-empty + valid
		}
		if errs := tmpl.Graph.Validate(); len(errs) > 0 {
			t.Errorf("template %q invalid: %v", tmpl.Name, errs)
		}
		if seen[tmpl.Name] {
			t.Errorf("duplicate template name %q", tmpl.Name)
		}
		seen[tmpl.Name] = true
	}
}
