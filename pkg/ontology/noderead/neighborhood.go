package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

// Neighborhood returns one-hop ontology relations around many source refs.
//
// It preserves embedded-node source identity, applies relation/type/kind
// filters, and can hydrate target nodes through the same Scope.
func (s *Scope) Neighborhood(ctx context.Context, req NeighborhoodRequest) (NeighborhoodResult, error) {
	if err := ctx.Err(); err != nil {
		return NeighborhoodResult{}, err
	}
	if s == nil || s.service == nil || s.service.Store == nil {
		return emptyNeighborhood(req.Sources), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	req = normalizeNeighborhoodRequest(req)
	sourceRefs := normalizeNeighborhoodSources(req.Sources, nil)
	if len(sourceRefs) == 0 {
		return emptyNeighborhood(nil), nil
	}
	canonicalSources, err := s.canonicalizeNodeRefsLocked(ctx, sourceRefs)
	if err != nil {
		return NeighborhoodResult{}, err
	}
	sourceRefs = normalizeNeighborhoodSources(canonicalSources, req.SourceKinds)
	sourceByPath := make(map[string][]ontology.NodeRef, len(sourceRefs))
	sourceByEndpoint := make(map[string][]ontology.NodeRef, len(sourceRefs))
	sourceSet := make(map[string]struct{}, len(sourceRefs))
	for _, ref := range sourceRefs {
		sourceByPath[ref.NotePath] = append(sourceByPath[ref.NotePath], ref)
		sourceByEndpoint[nodeEndpointKey(ref.NotePath, ref.NodeID)] = append(sourceByEndpoint[nodeEndpointKey(ref.NotePath, ref.NodeID)], ref)
		sourceSet[ref.NotePath] = struct{}{}
	}
	relationSet := stringSet(req.RelationNames)

	allRows := make([]NeighborhoodEdge, 0)
	query := readmodel.NeighborhoodQuery{Direction: string(req.Direction), Relations: req.RelationNames, IncludeStructural: req.IncludeStructural, IncludeAmbient: req.IncludeAmbient}
	for value := range req.Provenance {
		query.Provenance = append(query.Provenance, value)
	}
	for _, ref := range sourceRefs {
		nodeID := ref.NodeID
		if ref.Kind == ontology.NodeKindNote {
			nodeID = ""
		}
		query.Sources = append(query.Sources, readmodel.EdgeEndpoint{Path: ref.NotePath, NodeID: nodeID})
	}
	// Target filters must see every candidate before the per-source limit.
	if len(req.TargetTypes) == 0 && len(req.TargetInterfaces) == 0 && len(req.TargetKinds) == 0 && req.Direction != TraversalDirectionBoth && req.FirstPerSource > 0 {
		query.PerSourceLimit = req.FirstPerSource + 1
	}
	rows, err := s.neighborhoodRowsLocked(ctx, query)
	if err != nil {
		return NeighborhoodResult{}, err
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return NeighborhoodResult{}, err
		}
		if len(relationSet) > 0 {
			if _, ok := relationSet[row.RelationName]; !ok {
				continue
			}
		}
		if len(req.Provenance) > 0 {
			if _, ok := req.Provenance[row.Provenance]; !ok {
				continue
			}
		}
		if row.Structural && !req.IncludeStructural || !row.Structural && !req.IncludeAmbient {
			continue
		}
		if (req.Direction == TraversalDirectionOutbound || req.Direction == TraversalDirectionBoth) && hasPath(sourceSet, row.SrcPath) {
			allRows = append(allRows, s.neighborhoodEdgesForSourceRefs(sourceRefsForEndpoint(sourceByPath, sourceByEndpoint, row.SrcPath, row.SrcNodeID), row, TraversalDirectionOutbound, row.DstPath, row.DstNodeID, row.DstType)...)
		}
		if (req.Direction == TraversalDirectionInbound || req.Direction == TraversalDirectionBoth) && hasPath(sourceSet, row.DstPath) {
			allRows = append(allRows, s.neighborhoodEdgesForSourceRefs(sourceRefsForEndpoint(sourceByPath, sourceByEndpoint, row.DstPath, row.DstNodeID), row, TraversalDirectionInbound, row.SrcPath, row.SrcNodeID, "")...)
		}
	}
	if err := s.canonicalizeNeighborhoodEdgeRefsLocked(ctx, allRows); err != nil {
		return NeighborhoodResult{}, err
	}
	allRows, err = s.filterNeighborhoodTargetsLocked(ctx, allRows, req)
	if err != nil {
		return NeighborhoodResult{}, err
	}
	sortNeighborhoodEdges(allRows)
	result := groupNeighborhoodEdges(sourceRefs, allRows, req.FirstPerSource, req.FirstTotal)
	if req.Hydrate.Profile != "" {
		targetRefs := uniqueNeighborhoodTargets(result.Edges)
		nodes, err := s.hydrateLocked(ctx, targetRefs, req.Hydrate)
		if err != nil {
			return NeighborhoodResult{}, err
		}
		result.Nodes = nodes
	}
	return result, nil
}

// Edges is a Neighborhood shorthand that skips target hydration.
func (s *Scope) Edges(ctx context.Context, req NeighborhoodRequest) (NeighborhoodResult, error) {
	req.Hydrate = HydrateOptions{}
	return s.Neighborhood(ctx, req)
}
