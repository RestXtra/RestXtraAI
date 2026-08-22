package tool

import (
	"encoding/json"
	"sort"
	"strings"
)

type CatalogTier string

const (
	TierCore       CatalogTier = "core"
	TierCatalog    CatalogTier = "catalog"
	TierPrivileged CatalogTier = "privileged"
)

// CatalogMetadata describes the operational cost of a tool without exposing its
// full schema. Zero values are normalized conservatively by MetadataFor.
type CatalogMetadata struct {
	TokenCostEstimate int    `json:"token_cost_estimate"`
	LatencyClass      string `json:"latency_class"`
	SideEffect        string `json:"side_effect"`
	ConcurrencyClass  string `json:"concurrency_class"`
	ArtifactPolicy    string `json:"artifact_policy"`
}

type metadataProvider interface {
	CatalogMetadata() CatalogMetadata
}

type CatalogEntry struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Tier        CatalogTier     `json:"tier"`
	Locked      bool            `json:"locked"`
	Metadata    CatalogMetadata `json:"metadata"`
}

func MetadataFor(t CoreTool) CatalogMetadata {
	var metadata CatalogMetadata
	if provider, ok := t.(metadataProvider); ok {
		metadata = provider.CatalogMetadata()
	}
	if metadata.TokenCostEstimate <= 0 {
		schema, _ := json.Marshal(t.InputSchema())
		metadata.TokenCostEstimate = (len(schema) + len(t.Description()) + len(t.Prompt()) + 3) / 4
		if metadata.TokenCostEstimate < 1 {
			metadata.TokenCostEstimate = 1
		}
	}
	if metadata.LatencyClass == "" {
		metadata.LatencyClass = "medium"
	}
	if metadata.SideEffect == "" {
		metadata.SideEffect = "write_or_execute"
		if t.IsReadOnly(json.RawMessage(`{}`)) {
			metadata.SideEffect = "read"
		}
	}
	if metadata.ConcurrencyClass == "" {
		metadata.ConcurrencyClass = "exclusive"
		if t.IsConcurrencySafe(json.RawMessage(`{}`)) {
			metadata.ConcurrencyClass = "parallel"
		}
	}
	if metadata.ArtifactPolicy == "" {
		metadata.ArtifactPolicy = "spill_if_large"
	}
	return metadata
}

func CatalogForNames(tools []CoreTool, names []string, tier CatalogTier, locked bool) []CatalogEntry {
	byName := make(map[string]CoreTool, len(tools))
	for _, t := range tools {
		byName[t.Name()] = t
	}
	seen := map[string]bool{}
	entries := make([]CatalogEntry, 0, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		t, ok := byName[name]
		if !ok {
			continue
		}
		seen[name] = true
		entries = append(entries, CatalogEntry{Name: name, Description: compactDescription(t.Description()),
			Tier: tier, Locked: locked, Metadata: MetadataFor(t)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// Catalog returns one normalized decision record for every registered tool.
// Deferred membership determines whether an entry is core, catalog, or privileged.
func (r *Registry) Catalog(deferred []string, unlock *UnlockSet) []CatalogEntry {
	deferredSet := make(map[string]bool, len(deferred))
	for _, name := range deferred {
		deferredSet[name] = true
	}
	entries := make([]CatalogEntry, 0, len(r.order))
	for _, t := range r.List() {
		tier, locked := TierCore, false
		if deferredSet[t.Name()] {
			tier = TierCatalog
			if unlock != nil {
				tier = unlock.Tier(t.Name())
				locked = !unlock.Has(t.Name())
			}
		}
		entries = append(entries, CatalogEntry{Name: t.Name(), Description: compactDescription(t.Description()),
			Tier: tier, Locked: locked, Metadata: MetadataFor(t)})
	}
	return entries
}

func compactDescription(description string) string {
	description = strings.Join(strings.Fields(description), " ")
	runes := []rune(description)
	if len(runes) > 96 {
		return string(runes[:96]) + "..."
	}
	return description
}
