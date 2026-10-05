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
	if len(req.Selections) == 0 && len(req.TargetTypes) == 0 && len(req.TargetInterfaces) == 0 && len(req.TargetKinds) == 0 && len(req.Provenance) == 0 {
		return s.legacyRelationCounts(ctx, req)
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
	sources := normalizeNeighborhoodSources(req.Sources, req.SourceKinds)
	if len(sources) == 0 {
		for _, selection := range selections {
			sources = mergeNodeRefs(sources, selection.Sources)
		}
	}
	if len(sources) == 0 {
		return emptyRelationCounts(nil), nil
	}
	targetsBySource := make(map[string]map[string]struct{}, len(sources))
	for _, selection := range selections {
		if len(selection.Sources) == 0 {
			selection.Sources = sources
		}
		selection.Sources = normalizeNeighborhoodSources(selection.Sources, req.SourceKinds)
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
			sourceKey := nodeEndpointKey(edge.Source.NotePath, edge.Source.NodeID)
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
	for _, source := range sources {
		counts[nodeRefResultKey(source)] = counts[nodeEndpointKey(source.NotePath, source.NodeID)]
	}
	return relationCountsResult(sources, counts), nil
}

func (s *Scope) legacyRelationCounts(ctx context.Context, req RelationCountsRequest) (RelationCountsResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	direction := req.Direction
	if direction == "" || (direction != TraversalDirectionInbound && direction != TraversalDirectionOutbound && direction != TraversalDirectionBoth) {
		direction = TraversalDirectionBoth
	}
	if !req.IncludeStructural && !req.IncludeAmbient {
		req.IncludeStructural = true
		req.IncludeAmbient = true
	}
	sources := normalizeNeighborhoodSources(req.Sources, req.SourceKinds)
	sourcePaths := make([]string, 0, len(sources))
	byPath := make(map[string][]ontology.NodeRef, len(sources))
	byEndpoint := make(map[string][]ontology.NodeRef, len(sources))
	for _, source := range sources {
		sourcePaths = append(sourcePaths, source.NotePath)
		byPath[source.NotePath] = append(byPath[source.NotePath], source)
		byEndpoint[nodeEndpointKey(source.NotePath, source.NodeID)] = append(byEndpoint[nodeEndpointKey(source.NotePath, source.NodeID)], source)
	}
	counts := make(map[string]int, len(sources))
	for _, relation := range relationNamesForQuery(req.RelationNames) {
		rows, err := s.service.Store.OntologyEdgesForPaths(ctx, normalizeStrings(sourcePaths), true, relation, 0)
		if err != nil {
			return RelationCountsResult{}, err
		}
		s.diagnostics.EdgeLoads++
		for _, row := range rows {
			if (row.Structural && !req.IncludeStructural) || (!row.Structural && !req.IncludeAmbient) {
				continue
			}
			if direction == TraversalDirectionOutbound || direction == TraversalDirectionBoth {
				for _, source := range relationCountSourcesForEndpoint(byPath, byEndpoint, row.SrcPath, row.SrcNodeID) {
					counts[nodeRefResultKey(source)]++
				}
			}
			if row.DstPath != row.SrcPath || strings.TrimSpace(row.DstNodeID) != strings.TrimSpace(row.SrcNodeID) {
				if direction == TraversalDirectionInbound || direction == TraversalDirectionBoth {
					for _, source := range relationCountSourcesForEndpoint(byPath, byEndpoint, row.DstPath, row.DstNodeID) {
						counts[nodeRefResultKey(source)]++
					}
				}
			}
		}
	}
	return relationCountsResult(sources, counts), nil
}

func relationCountSourcesForEndpoint(byPath map[string][]ontology.NodeRef, byEndpoint map[string][]ontology.NodeRef, path string, nodeID string) []ontology.NodeRef {
	out := make([]ontology.NodeRef, 0)
	for _, source := range byPath[path] {
		if strings.TrimSpace(source.NodeID) == "" && (source.Kind == "" || source.Kind == ontology.NodeKindNote) {
			out = append(out, source)
		}
	}
	if strings.TrimSpace(nodeID) != "" {
		out = append(out, byEndpoint[nodeEndpointKey(path, nodeID)]...)
	}
	return out
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
