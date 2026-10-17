package noderead

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	"sort"
	"strings"
)

func normalizeNeighborhoodRequest(req NeighborhoodRequest) NeighborhoodRequest {
	if req.Direction == "" {
		req.Direction = TraversalDirectionOutbound
	}
	if req.Direction != TraversalDirectionInbound && req.Direction != TraversalDirectionBoth {
		req.Direction = TraversalDirectionOutbound
	}
	if !req.IncludeStructural && !req.IncludeAmbient {
		req.IncludeStructural = true
		req.IncludeAmbient = true
	}
	req.RelationNames = normalizeStrings(req.RelationNames)
	req.TargetTypes = normalizeStrings(req.TargetTypes)
	req.TargetInterfaces = normalizeStrings(req.TargetInterfaces)
	return req
}

func normalizeNeighborhoodSources(refs []ontology.NodeRef, kinds []ontology.NodeKind) []ontology.NodeRef {
	kindSet := nodeKindSet(kinds)
	out := make([]ontology.NodeRef, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		ref = NormalizeRef(ref)
		if ref.IsZero() || strings.TrimSpace(ref.NotePath) == "" {
			continue
		}
		if ref.Kind == "" {
			ref.Kind = ontology.NodeKindNote
		}
		if len(kindSet) > 0 {
			if _, ok := kindSet[ref.Kind]; !ok {
				continue
			}
		}
		key := nodeRefResultKey(ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func groupNeighborhoodEdges(sources []ontology.NodeRef, edges []NeighborhoodEdge, firstPerSource, firstTotal int) NeighborhoodResult {
	result := NeighborhoodResult{
		Sources:  make([]NeighborhoodSourceResult, 0, len(sources)),
		BySource: make(map[string]NeighborhoodSourceResult, len(sources)),
	}
	total := 0
	bySource := make(map[string][]NeighborhoodEdge, len(sources))
	truncatedBySource := make(map[string]bool, len(sources))
	for _, edge := range edges {
		key := nodeRefResultKey(edge.Source)
		if firstTotal > 0 && total >= firstTotal {
			truncatedBySource[key] = true
			continue
		}
		if firstPerSource > 0 && len(bySource[key]) >= firstPerSource {
			truncatedBySource[key] = true
			continue
		}
		bySource[key] = append(bySource[key], edge)
		result.Edges = append(result.Edges, edge)
		total++
	}
	for _, source := range sources {
		key := nodeRefResultKey(source)
		item := NeighborhoodSourceResult{
			Source:    source,
			Edges:     append([]NeighborhoodEdge(nil), bySource[key]...),
			Truncated: truncatedBySource[key],
		}
		result.Sources = append(result.Sources, item)
		result.BySource[key] = item
		if alias := source.String(); alias != "" {
			if _, exists := result.BySource[alias]; !exists {
				result.BySource[alias] = item
			}
		}
	}
	return result
}

func sortNeighborhoodEdges(edges []NeighborhoodEdge) {
	sort.SliceStable(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		valuesA := []string{nodeRefResultKey(a.Source), string(a.Direction), boolSortValue(a.Structural), a.Provenance, a.RelationName, nodeRefResultKey(a.Target)}
		valuesB := []string{nodeRefResultKey(b.Source), string(b.Direction), boolSortValue(b.Structural), b.Provenance, b.RelationName, nodeRefResultKey(b.Target)}
		for idx := range valuesA {
			if valuesA[idx] != valuesB[idx] {
				return valuesA[idx] < valuesB[idx]
			}
		}
		return false
	})
}

func boolSortValue(v bool) string {
	if v {
		return "0"
	}
	return "1"
}

func uniqueNeighborhoodTargets(edges []NeighborhoodEdge) []ontology.NodeRef {
	seen := make(map[string]struct{}, len(edges))
	out := make([]ontology.NodeRef, 0, len(edges))
	for _, edge := range edges {
		key := nodeRefResultKey(edge.Target)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, edge.Target)
	}
	return out
}

func emptyNeighborhood(sources []ontology.NodeRef) NeighborhoodResult {
	sourceRefs := normalizeNeighborhoodSources(sources, nil)
	return groupNeighborhoodEdges(sourceRefs, nil, 0, 0)
}
