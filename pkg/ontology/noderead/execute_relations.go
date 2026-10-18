package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

func (s *Scope) executeSingleRelationSelection(ctx context.Context, selection RelationSelection, limits TraverseLimits) (NeighborhoodResult, error) {
	if s == nil || s.service == nil || s.service.Store == nil {
		return emptyNeighborhood(selection.Sources), nil
	}
	if len(selection.SourceTypes) > 0 {
		s.mu.Lock()
		canonical, err := s.canonicalizeNodeRefsLocked(ctx, selection.Sources)
		if err == nil {
			var paths []string
			for _, ref := range canonical {
				if ref.Kind == ontology.NodeKindNote && ref.TypeName == "" {
					paths = append(paths, ref.NotePath)
				}
			}
			types, typeErr := s.typesByPathsLocked(ctx, paths)
			err = typeErr
			for i := range canonical {
				if canonical[i].Kind == ontology.NodeKindNote && canonical[i].TypeName == "" {
					canonical[i].TypeName = types[canonical[i].NotePath].TypeName
				}
			}
		}
		s.mu.Unlock()
		if err != nil {
			return NeighborhoodResult{}, err
		}
		selection.Sources = filterRelationSources(canonical, selection.SourceTypes)
		if len(selection.Sources) == 0 {
			return emptyNeighborhood(nil), nil
		}
	}
	if selection.NeighborScope == ontoschema.NeighborScopeSubtree {
		return s.executeSubtreeRelationSelection(ctx, selection, limits)
	}
	return s.Neighborhood(ctx, neighborhoodRequestForSelection(selection, limits))
}

func (s *Scope) executeSubtreeRelationSelection(ctx context.Context, selection RelationSelection, limits TraverseLimits) (NeighborhoodResult, error) {
	s.mu.Lock()
	canonical, err := s.canonicalizeNodeRefsLocked(ctx, selection.Sources)
	s.mu.Unlock()
	if err != nil {
		return NeighborhoodResult{}, err
	}
	selection.Sources = normalizeNeighborhoodSources(canonical, nil)
	direction := selection.Direction
	if direction == "" {
		direction = TraversalDirectionOutbound
	}
	results := make([]NeighborhoodResult, 0, 2)
	if direction == TraversalDirectionOutbound || direction == TraversalDirectionBoth {
		outbound := selection
		outbound.Direction = TraversalDirectionOutbound
		outbound.IncludeStructural = true
		outbound.IncludeAmbient = false
		outbound.Provenance = map[string]struct{}{"section_neighbor": {}}
		result, err := s.Neighborhood(ctx, neighborhoodRequestForSelection(outbound, TraverseLimits{}))
		if err != nil {
			return NeighborhoodResult{}, err
		}
		result, err = s.overlaySubtreeOutboundRelations(ctx, outbound, result)
		if err != nil {
			return NeighborhoodResult{}, err
		}
		results = append(results, result)
	}
	if direction == TraversalDirectionInbound || direction == TraversalDirectionBoth {
		inbound := selection
		inbound.Direction = TraversalDirectionInbound
		inbound.RelationNames = nil
		inbound.Provenance = nil
		inbound.IncludeStructural = true
		inbound.IncludeAmbient = false
		result, err := s.Neighborhood(ctx, neighborhoodRequestForSelection(inbound, TraverseLimits{}))
		if err != nil {
			return NeighborhoodResult{}, err
		}
		result, err = s.subtreeInboundRelations(ctx, inbound, result)
		if err != nil {
			return NeighborhoodResult{}, err
		}
		results = append(results, result)
	}
	edges := make([]NeighborhoodEdge, 0)
	seen := map[string]struct{}{}
	for _, result := range results {
		for _, edge := range result.Edges {
			key := nodeRefResultKey(edge.Source) + "\x00" + nodeRefResultKey(edge.Target)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			edges = append(edges, edge)
		}
	}
	sortNeighborhoodEdges(edges)
	return groupNeighborhoodEdges(selection.Sources, edges, limits.FirstPerSource, limits.FirstTotal), nil
}

func neighborhoodRequestForSelection(selection RelationSelection, limits TraverseLimits) NeighborhoodRequest {
	return NeighborhoodRequest{
		Sources:           selection.Sources,
		Direction:         selection.Direction,
		RelationNames:     selection.RelationNames,
		Provenance:        selection.Provenance,
		IncludeStructural: selection.IncludeStructural,
		IncludeAmbient:    selection.IncludeAmbient,
		TargetTypes:       selection.TargetTypes,
		TargetInterfaces:  selection.TargetInterfaces,
		TargetKinds:       nil,
		FirstPerSource:    limits.FirstPerSource,
		FirstTotal:        limits.FirstTotal,
		Hydrate:           selection.Hydrate,
	}
}

func regroupNeighborhood(result NeighborhoodResult, edges []NeighborhoodEdge, limits TraverseLimits) NeighborhoodResult {
	sources := make([]ontology.NodeRef, 0, len(result.Sources))
	for _, group := range result.Sources {
		sources = append(sources, group.Source)
	}
	return groupNeighborhoodEdges(sources, edges, limits.FirstPerSource, limits.FirstTotal)
}
