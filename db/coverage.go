package db

import (
	"fmt"
	"net/url"
	"strings"
)

// CoverageGraphNode is a compact asset coverage view for one task. Status is
// discovered, verified, or vulnerable; it is intentionally separate from the
// asset's lifecycle state.
type CoverageGraphNode struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Label    string `json:"label"`
	ParentID string `json:"parent_id,omitempty"`
	Status   string `json:"status"`
	Tested   bool   `json:"tested"`
	Findings int    `json:"findings"`
}

type CoverageGraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type CoverageGraph struct {
	Nodes      []*CoverageGraphNode `json:"nodes"`
	Edges      []*CoverageGraphEdge `json:"edges"`
	Total      int                  `json:"total"`
	Tested     int                  `json:"tested"`
	Vulnerable int                  `json:"vulnerable"`
	Untested   int                  `json:"untested"`
}

type coverageAnchorState struct {
	probed, vulnerable bool
	findings           int
}

// CoverageGraph returns task assets and their root-domain relationships. A
// node is probed when any exploration node is anchored to it; a finding anchor
// upgrades it to vulnerable. The query is bounded to one task's assets.
func (s *AssetStore) CoverageGraph(taskID int64) (*CoverageGraph, error) {
	if taskID <= 0 {
		return &CoverageGraph{Nodes: []*CoverageGraphNode{}, Edges: []*CoverageGraphEdge{}}, nil
	}
	assets, err := s.QueryByTask(taskID, "", 10000)
	if err != nil {
		return nil, err
	}
	if assets == nil {
		assets = []*Asset{}
	}
	result := &CoverageGraph{
		Nodes: make([]*CoverageGraphNode, 0, len(assets)),
		Edges: make([]*CoverageGraphEdge, 0),
	}
	if len(assets) == 0 {
		return result, nil
	}
	ids := make([]any, len(assets))
	for i, asset := range assets {
		ids[i] = asset.ID
	}
	placeholders := make([]string, len(ids))
	for i := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	anchorSQL := `SELECT ea.asset_id,
		BOOL_OR(en.kind IN ('fact','intent')) AS probed,
		BOOL_OR(en.kind='finding') AS vulnerable,
		COUNT(*) FILTER (WHERE en.kind='finding') AS findings
		FROM exploration_anchors ea
		JOIN exploration_nodes en ON en.id=ea.node_id
		JOIN tasks t ON t.exploration_id=en.exploration_id AND t.id=$` + fmt.Sprint(len(ids)+1) + `
		WHERE ea.asset_id IN (` + strings.Join(placeholders, ",") + `)
		GROUP BY ea.asset_id`
	args := append(ids, taskID)
	rows, err := s.db.Query(anchorSQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := map[int64]coverageAnchorState{}
	for rows.Next() {
		var id int64
		var probed, vulnerable bool
		var findings int
		if err := rows.Scan(&id, &probed, &vulnerable, &findings); err != nil {
			return nil, err
		}
		states[id] = coverageAnchorState{probed: probed, vulnerable: vulnerable, findings: findings}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return buildCoverageGraph(assets, states), nil
}

func buildCoverageGraph(assets []*Asset, states map[int64]coverageAnchorState) *CoverageGraph {
	result := &CoverageGraph{
		Nodes: make([]*CoverageGraphNode, 0, len(assets)),
		Edges: make([]*CoverageGraphEdge, 0),
	}
	domainIDs := make(map[string]string)
	ipIDs := make(map[string]string)
	appIDs := make(map[string]string)
	for _, asset := range assets {
		id := fmt.Sprintf("asset-%d", asset.ID)
		if (asset.Type == "root_domain" || asset.Type == "subdomain") && asset.Domain != "" {
			domainIDs[strings.ToLower(asset.Domain)] = id
		}
		if asset.Type == "ip" && asset.IP != "" {
			ipIDs[asset.IP] = id
		}
		if asset.Type == "app" {
			if origin := coverageURLOrigin(asset.URL); origin != "" {
				appIDs[origin] = id
			}
		}
	}
	for _, asset := range assets {
		state := states[asset.ID]
		status := "discovered"
		if state.vulnerable {
			status = "vulnerable"
		} else if state.probed {
			status = "verified"
		}
		label := coverageAssetLabel(asset)
		if label == "" {
			label = fmt.Sprintf("%s #%d", asset.Type, asset.ID)
		}
		id := fmt.Sprintf("asset-%d", asset.ID)
		parent := coverageParent(asset, domainIDs, ipIDs, appIDs)
		if parent == fmt.Sprintf("asset-%d", asset.ID) {
			parent = ""
		}
		result.Nodes = append(result.Nodes, &CoverageGraphNode{
			ID: id, Type: asset.Type, Label: label,
			ParentID: parent, Status: status, Tested: state.probed || state.vulnerable, Findings: state.findings,
		})
		if parent != "" {
			result.Edges = append(result.Edges, &CoverageGraphEdge{Source: parent, Target: id})
		}
		result.Total++
		if state.probed || state.vulnerable {
			result.Tested++
		}
		if state.vulnerable {
			result.Vulnerable++
		}
	}
	result.Untested = result.Total - result.Tested
	return result
}

func coverageParent(asset *Asset, domainIDs, ipIDs, appIDs map[string]string) string {
	host := coverageURLHost(asset.URL)
	switch asset.Type {
	case "root_domain":
		return ""
	case "subdomain":
		return domainIDs[strings.ToLower(asset.RootDomain)]
	case "ip":
		for _, domain := range asset.BoundDomains {
			if parent := domainIDs[strings.ToLower(domain)]; parent != "" {
				return parent
			}
		}
	case "service":
		if parent := ipIDs[asset.IP]; parent != "" {
			return parent
		}
		return domainIDs[host]
	case "app":
		if parent := domainIDs[host]; parent != "" {
			return parent
		}
		return ipIDs[asset.IP]
	case "endpoint":
		if parent := appIDs[coverageURLOrigin(asset.URL)]; parent != "" {
			return parent
		}
		if parent := domainIDs[host]; parent != "" {
			return parent
		}
		return ipIDs[asset.IP]
	}
	return ""
}

func coverageAssetLabel(asset *Asset) string {
	switch asset.Type {
	case "service":
		address := asset.IP
		if address == "" {
			address = coverageURLHost(asset.URL)
		}
		if asset.Port != nil {
			address = fmt.Sprintf("%s:%d", address, *asset.Port)
		}
		if asset.ServiceName != "" && address != "" {
			return address + " · " + asset.ServiceName
		}
		if address != "" {
			return address
		}
		return asset.ServiceName
	case "app", "endpoint":
		return asset.URL
	}
	if asset.Domain != "" {
		return asset.Domain
	}
	return asset.IP
}

func coverageURLHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func coverageURLOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}
