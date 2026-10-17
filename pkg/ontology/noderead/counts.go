package noderead

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// RelationCounts counts distinct matching relation targets for many sources
// without hydrating endpoint records.
func (s *Scope) RelationCounts(ctx context.Context, req RelationCountsRequest) (RelationCountsResult, error) {
	if err := ctx.Err(); err != nil {
		return RelationCountsResult{}, err
	}
	if s == nil || s.service == nil || s.service.Store == nil {
		return emptyRelationCounts(req.Sources), nil
	}
	if req.Direction == "" {
		req.Direction = TraversalDirectionBoth
	}
	selections := append([]RelationSelection(nil), req.Selections...)
	if len(selections) == 0 {
		selections = []RelationSelection{{
			Sources:           req.Sources,
			Direction:         req.Direction,
			RelationNames:     req.RelationNames,
			Provenance:        req.Provenance,
			IncludeStructural: req.IncludeStructural,
			IncludeAmbient:    req.IncludeAmbient,
			TargetTypes:       req.TargetTypes,
			TargetInterfaces:  req.TargetInterfaces,
		}}
	}
	sources := normalizeNeighborhoodSources(req.Sources, nil)
	if len(req.Sources) == 0 {
		for _, selection := range selections {
			sources = mergeNodeRefs(sources, selection.Sources)
		}
	}
	sources = normalizeNeighborhoodSources(sources, nil)
	if len(sources) == 0 {
		return emptyRelationCounts(nil), nil
	}
	s.mu.Lock()
	canonicalSources, err := s.canonicalizeNodeRefsLocked(ctx, sources)
	s.mu.Unlock()
	if err != nil {
		return RelationCountsResult{}, err
	}
	filteredSources := make([]ontology.NodeRef, 0, len(sources))
	filteredCanonical := make([]ontology.NodeRef, 0, len(sources))
	for i, source := range sources {
		if relationCountTargetKindAllowed(req.SourceKinds, canonicalSources[i].Kind) {
			filteredSources = append(filteredSources, source)
			filteredCanonical = append(filteredCanonical, canonicalSources[i])
		}
	}
	sources, canonicalSources = filteredSources, filteredCanonical
	if len(sources) == 0 {
		return emptyRelationCounts(nil), nil
	}
	targetsBySource := make(map[string]map[string]struct{}, len(sources))
	for _, selection := range selections {
		if len(selection.Sources) == 0 {
			selection.Sources = sources
		}
		s.mu.Lock()
		canonical, err := s.canonicalizeNodeRefsLocked(ctx, selection.Sources)
		s.mu.Unlock()
		if err != nil {
			return RelationCountsResult{}, err
		}
		selection.Sources = normalizeNeighborhoodSources(canonical, req.SourceKinds)
		if len(selection.Sources) == 0 {
			continue
		}
		result, err := s.executeSingleRelationSelection(ctx, selection, TraverseLimits{})
		if err != nil {
			return RelationCountsResult{}, err
		}
		for _, edge := range result.Edges {
			if !relationCountTargetKindAllowed(req.TargetKinds, edge.Target.Kind) {
				continue
			}
			sourceKey := RefIdentityKey(edge.Source)
			if targetsBySource[sourceKey] == nil {
				targetsBySource[sourceKey] = map[string]struct{}{}
			}
			targetsBySource[sourceKey][RefIdentityKey(edge.Target)] = struct{}{}
		}
	}
	counts := make(map[string]int, len(targetsBySource))
	for key, targets := range targetsBySource {
		counts[key] = len(targets)
	}
	for i, source := range sources {
		count := counts[RefIdentityKey(canonicalSources[i])]
		counts[nodeRefResultKey(source)] = count
		counts[RefIdentityKey(source)] = count
	}
	return relationCountsResult(sources, counts), nil
}

func relationCountTargetKindAllowed(kinds []ontology.NodeKind, kind ontology.NodeKind) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, candidate := range kinds {
		if candidate == kind {
			return true
		}
	}
	return false
}

func emptyRelationCounts(sources []ontology.NodeRef) RelationCountsResult {
	return relationCountsResult(normalizeNeighborhoodSources(sources, nil), nil)
}

func relationCountsResult(sources []ontology.NodeRef, counts map[string]int) RelationCountsResult {
	result := RelationCountsResult{
		Sources:  make([]RelationCountSourceResult, 0, len(sources)),
		BySource: make(map[string]RelationCountSourceResult, len(sources)),
	}
	for _, source := range sources {
		count, ok := counts[nodeRefResultKey(source)]
		if !ok {
			count = counts[RefIdentityKey(source)]
		}
		item := RelationCountSourceResult{Source: source, Count: count}
		result.Sources = append(result.Sources, item)
		result.BySource[nodeRefResultKey(source)] = item
		result.BySource[RefIdentityKey(source)] = item
		if alias := strings.TrimSpace(source.String()); alias != "" {
			if _, exists := result.BySource[alias]; !exists {
				result.BySource[alias] = item
			}
		}
	}
	return result
}
