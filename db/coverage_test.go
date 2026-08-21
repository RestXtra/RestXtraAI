package db

import "testing"

func TestBuildCoverageGraphClassifiesAndLinksAssets(t *testing.T) {
	port := 443
	assets := []*Asset{
		{ID: 1, Type: "root_domain", Domain: "example.com", RootDomain: "example.com"},
		{ID: 2, Type: "subdomain", Domain: "api.example.com", RootDomain: "example.com"},
		{ID: 3, Type: "ip", IP: "192.0.2.10", BoundDomains: []string{"api.example.com"}},
		{ID: 4, Type: "service", IP: "192.0.2.10", Port: &port, ServiceName: "HTTPS"},
		{ID: 5, Type: "app", URL: "https://api.example.com"},
		{ID: 6, Type: "endpoint", URL: "https://api.example.com/v1/users"},
	}
	states := map[int64]coverageAnchorState{
		2: {probed: true},
		6: {vulnerable: true, findings: 2},
	}

	graph := buildCoverageGraph(assets, states)
	if graph.Total != 6 || graph.Tested != 2 || graph.Vulnerable != 1 || graph.Untested != 4 {
		t.Fatalf("unexpected totals: %+v", graph)
	}

	byID := make(map[string]*CoverageGraphNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		byID[node.ID] = node
	}
	checks := map[string]string{
		"asset-2": "asset-1",
		"asset-3": "asset-2",
		"asset-4": "asset-3",
		"asset-5": "asset-2",
		"asset-6": "asset-5",
	}
	for id, parent := range checks {
		if got := byID[id].ParentID; got != parent {
			t.Errorf("%s parent = %q, want %q", id, got, parent)
		}
	}
	if byID["asset-2"].Status != "verified" || !byID["asset-2"].Tested {
		t.Errorf("verified node not classified: %+v", byID["asset-2"])
	}
	if byID["asset-6"].Status != "vulnerable" || byID["asset-6"].Findings != 2 {
		t.Errorf("vulnerable node not classified: %+v", byID["asset-6"])
	}
	if byID["asset-4"].Label != "192.0.2.10:443 · HTTPS" {
		t.Errorf("service label = %q", byID["asset-4"].Label)
	}
}
