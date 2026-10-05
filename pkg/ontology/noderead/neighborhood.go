package noderead

import (
	"context"
	"fmt"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
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
	sourceRefs := normalizeNeighborhoodSources(req.Sources, req.SourceKinds)
	if len(sourceRefs) == 0 {
		return emptyNeighborhood(nil), nil
	}
	sourcePaths := make([]string, 0, len(sourceRefs))
	sourceByPath := make(map[string][]ontology.NodeRef, len(sourceRefs))
	sourceByEndpoint := make(map[string][]ontology.NodeRef, len(sourceRefs))
	sourceSet := make(map[string]struct{}, len(sourceRefs))
	for _, ref := range sourceRefs {
		sourcePaths = append(sourcePaths, ref.NotePath)
		sourceByPath[ref.NotePath] = append(sourceByPath[ref.NotePath], ref)
		sourceByEndpoint[nodeEndpointKey(ref.NotePath, ref.NodeID)] = append(sourceByEndpoint[nodeEndpointKey(ref.NotePath, ref.NodeID)], ref)
		sourceSet[ref.NotePath] = struct{}{}
	}
	sourcePaths = normalizeStrings(sourcePaths)
	relationSet := stringSet(req.RelationNames)
	targetTypeSet := stringSet(req.TargetTypes)
	targetKindSet := nodeKindSet(req.TargetKinds)

	allRows := make([]NeighborhoodEdge, 0)
	for _, relation := range relationNamesForQuery(req.RelationNames) {
		rows, err := s.service.Store.OntologyEdgesForPaths(ctx, sourcePaths, true, relation, 0)
		if err != nil {
			return NeighborhoodResult{}, err
		}
		s.diagnostics.EdgeLoads++
		endpointPaths := make([]string, 0, len(rows))
		for _, row := range rows {
			if (req.Direction == TraversalDirectionInbound || req.Direction == TraversalDirectionBoth) && hasPath(sourceSet, row.DstPath) && row.SrcNodeID == "" {
				endpointPaths = append(endpointPaths, row.SrcPath)
			}
			if (req.Direction == TraversalDirectionOutbound || req.Direction == TraversalDirectionBoth) && hasPath(sourceSet, row.SrcPath) && row.DstType == "" && (len(targetTypeSet) > 0 || len(req.TargetInterfaces) > 0) {
				endpointPaths = append(endpointPaths, row.DstPath)
			}
		}
		endpointTypes, err := s.typesByPathsLocked(ctx, endpointPaths)
		if err != nil {
			return NeighborhoodResult{}, err
		}
		inboundNodes := map[string]ontology.NodeRef{}
		for _, row := range rows {
			if row.SrcNodeID != "" && (req.Direction == TraversalDirectionInbound || req.Direction == TraversalDirectionBoth) && hasPath(sourceSet, row.DstPath) {
				inboundNodes[row.SrcNodeID] = ontology.NodeRef{NotePath: row.SrcPath, Kind: ontology.NodeKindEmbedded, NodeID: row.SrcNodeID}
			}
		}
		inboundTypes := map[string]string{}
		if len(inboundNodes) > 0 {
			if catalog, ok := s.service.Store.(CatalogStore); ok {
				// Persisted edges use the projection's locator-form NodeID,
				// not the separate hashed catalog primary key.
				locators := make([]string, 0, len(inboundNodes))
				for locator := range inboundNodes {
					locators = append(locators, locator)
				}
				nodes, err := catalog.OntologyNodesBySourceLocators(ctx, normalizeStrings(locators))
				if err != nil {
					return NeighborhoodResult{}, err
				}
				for id, node := range nodes {
					inboundTypes[id] = node.TypeName
				}
			}
			if len(targetTypeSet) > 0 || len(req.TargetInterfaces) > 0 {
				missing := make([]ontology.NodeRef, 0)
				for id, ref := range inboundNodes {
					if inboundTypes[id] == "" {
						missing = append(missing, ref)
					}
				}
				if len(missing) > 0 {
					records, err := s.hydrateLocked(ctx, missing, HydrateOptions{Profile: HydrateSummary})
					if err != nil {
						return NeighborhoodResult{}, err
					}
					resolved := indexLoadedRecordsForRequests(missing, records)
					for _, ref := range missing {
						if record, ok := resolved[nodeRefIdentityKey(ref)]; ok {
							inboundTypes[ref.NodeID] = record.TypeName
						}
					}
				}
			}
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
			if row.Structural && !req.IncludeStructural {
				continue
			}
			if !row.Structural && !req.IncludeAmbient {
				continue
			}
			if (req.Direction == TraversalDirectionOutbound || req.Direction == TraversalDirectionBoth) && hasPath(sourceSet, row.SrcPath) {
				if !endpointKindAllowed(targetKindSet, row.DstNodeID) {
					continue
				}
				targetType := row.DstType
				if targetType == "" {
					targetType = endpointTypes[row.DstPath].TypeName
				}
				allowed, err := s.neighborhoodTargetAllowed(ctx, row.DstPath, targetType, targetTypeSet, req.TargetInterfaces)
				if err != nil {
					return NeighborhoodResult{}, err
				}
				if allowed {
					allRows = append(allRows, s.neighborhoodEdgesForSourceRefs(sourceRefsForEndpoint(sourceByPath, sourceByEndpoint, row.SrcPath, row.SrcNodeID), row, TraversalDirectionOutbound, row.DstPath, row.DstNodeID, targetType)...)
				}
			}
			if (req.Direction == TraversalDirectionInbound || req.Direction == TraversalDirectionBoth) && hasPath(sourceSet, row.DstPath) {
				if !endpointKindAllowed(targetKindSet, row.SrcNodeID) {
					continue
				}
				targetType := endpointTypes[row.SrcPath].TypeName
				if row.SrcNodeID != "" {
					targetType = inboundTypes[row.SrcNodeID]
					if targetType == "" && (len(targetTypeSet) > 0 || len(req.TargetInterfaces) > 0) {
						return NeighborhoodResult{}, fmt.Errorf("embedded relation source %s in %s has no resolvable type", row.SrcNodeID, row.SrcPath)
					}
				}
				allowed, err := s.neighborhoodTargetAllowed(ctx, row.SrcPath, targetType, targetTypeSet, req.TargetInterfaces)
				if err != nil {
					return NeighborhoodResult{}, err
				}
				if allowed {
					allRows = append(allRows, s.neighborhoodEdgesForSourceRefs(sourceRefsForEndpoint(sourceByPath, sourceByEndpoint, row.DstPath, row.DstNodeID), row, TraversalDirectionInbound, row.SrcPath, row.SrcNodeID, targetType)...)
				}
			}
		}
	}
	if err := s.canonicalizeNeighborhoodEdgeRefsLocked(ctx, allRows); err != nil {
		return NeighborhoodResult{}, err
	}
	canonicalSources, err := s.canonicalizeNodeRefsLocked(ctx, sourceRefs)
	if err != nil {
		return NeighborhoodResult{}, err
	}
	sourceRefs = canonicalSources
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

func (s *Scope) hydrateLocked(ctx context.Context, refs []ontology.NodeRef, opts HydrateOptions) ([]NodeRecord, error) {
	// IMPORTANT: Hydrate takes the same mutex. Release it here so Neighborhood can
	// reuse the public hydration path without deadlocking or bypassing its caches.
	s.mu.Unlock()
	records, err := s.Hydrate(ctx, refs, opts)
	s.mu.Lock()
	return records, err
}

func (s *Scope) neighborhoodEdgesForSourceRefs(sources []ontology.NodeRef, row semdb.OntologyEdgeRow, direction TraversalDirection, targetPath string, targetNodeID string, targetType string) []NeighborhoodEdge {
	out := make([]NeighborhoodEdge, 0, len(sources))
	for _, source := range sources {
		target := ontology.NodeRef{NotePath: targetPath, Kind: ontology.NodeKindNote, TypeName: targetType}
		if targetNodeID != "" {
			target.NodeID = targetNodeID
			target.Kind = ontology.NodeKindEmbedded
		}
		out = append(out, NeighborhoodEdge{
			Source:       source,
			Target:       target,
			Edge:         row,
			Direction:    direction,
			RelationName: row.RelationName,
			Provenance:   row.Provenance,
			Structural:   row.Structural,
			TargetType:   targetType,
			Depth:        1,
		})
	}
	return out
}

func (s *Scope) canonicalizeNeighborhoodEdgeRefsLocked(ctx context.Context, edges []NeighborhoodEdge) error {
	if len(edges) == 0 {
		return nil
	}
	refs := make([]ontology.NodeRef, 0, len(edges)*2)
	seen := make(map[string]struct{}, len(edges)*2)
	add := func(ref ontology.NodeRef) {
		ref = normalizeNodeRef(ref)
		if ref.IsZero() || ref.Kind != ontology.NodeKindEmbedded {
			return
		}
		key := nodeRefIdentityKey(ref)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		refs = append(refs, ref)
	}
	for _, edge := range edges {
		add(edge.Source)
		add(edge.Target)
	}
	locators, err := s.locatorsLocked(ctx, refs)
	if err != nil {
		return err
	}
	for i := range edges {
		edges[i].Source = canonicalNodeRefFromLocator(edges[i].Source, locators[nodeRefIdentityKey(edges[i].Source)])
		edges[i].Target = canonicalNodeRefFromLocator(edges[i].Target, locators[nodeRefIdentityKey(edges[i].Target)])
	}
	return nil
}

func (s *Scope) canonicalizeNodeRefsLocked(ctx context.Context, refs []ontology.NodeRef) ([]ontology.NodeRef, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	// Only embedded refs gain a block fragment from their locator; a note
	// ref's locator is the ref itself, and building it lists every vault file.
	embedded := make([]ontology.NodeRef, 0, len(refs))
	for _, ref := range refs {
		if normalizeNodeRef(ref).Kind == ontology.NodeKindEmbedded {
			embedded = append(embedded, ref)
		}
	}
	locators, err := s.locatorsLocked(ctx, embedded)
	if err != nil {
		return nil, err
	}
	out := make([]ontology.NodeRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, canonicalNodeRefFromLocator(ref, locators[nodeRefIdentityKey(ref)]))
	}
	return out, nil
}

func sourceRefsForEndpoint(byPath map[string][]ontology.NodeRef, byEndpoint map[string][]ontology.NodeRef, path string, nodeID string) []ontology.NodeRef {
	if strings.TrimSpace(nodeID) != "" {
		return byEndpoint[nodeEndpointKey(path, nodeID)]
	}
	refs := byPath[path]
	out := make([]ontology.NodeRef, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.NodeID) == "" && (ref.Kind == "" || ref.Kind == ontology.NodeKindNote) {
			out = append(out, ref)
		}
	}
	return out
}

func nodeEndpointKey(path string, nodeID string) string {
	return strings.TrimSpace(path) + "\x00" + strings.TrimSpace(nodeID)
}

func endpointKindAllowed(kindSet map[ontology.NodeKind]struct{}, nodeID string) bool {
	if len(kindSet) == 0 {
		return true
	}
	kind := ontology.NodeKindNote
	if strings.TrimSpace(nodeID) != "" {
		kind = ontology.NodeKindEmbedded
	}
	_, ok := kindSet[kind]
	return ok
}

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
		ref = normalizeNodeRef(ref)
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

func relationNamesForQuery(names []string) []string {
	names = normalizeStrings(names)
	if len(names) == 0 {
		return []string{""}
	}
	return names
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

func (s *Scope) neighborhoodTargetAllowed(ctx context.Context, path string, typeName string, targetTypeSet map[string]struct{}, targetInterfaces []string) (bool, error) {
	if len(targetTypeSet) > 0 {
		if typeName == "" {
			resolved, err := s.noteTypeForPathLocked(ctx, path)
			if err != nil {
				return false, err
			}
			typeName = resolved
		}
		if _, ok := targetTypeSet[typeName]; !ok {
			return false, nil
		}
	}
	if len(targetInterfaces) > 0 {
		if typeName == "" {
			resolved, err := s.noteTypeForPathLocked(ctx, path)
			if err != nil {
				return false, err
			}
			typeName = resolved
		}
		if !s.typeMatchesAnyInterface(typeName, targetInterfaces) {
			return false, nil
		}
	}
	return true, nil
}

func (s *Scope) noteTypeForPathLocked(ctx context.Context, path string) (string, error) {
	rows, err := s.typesByPathsLocked(ctx, []string{path})
	if err != nil {
		return "", err
	}
	return rows[path].TypeName, nil
}

func (s *Scope) typeMatchesAnyInterface(typeName string, interfaces []string) bool {
	typeName = strings.TrimSpace(typeName)
	if s == nil || s.service == nil || s.service.Schema == nil || typeName == "" {
		return false
	}
	doc := s.service.Schema.Types[typeName]
	if doc == nil {
		return false
	}
	interfaceSet := stringSet(interfaces)
	for _, name := range doc.Implements {
		if _, ok := interfaceSet[name]; ok {
			return true
		}
	}
	return false
}

// TargetFiltersForType routes schema interface names to TargetInterfaces and
// concrete type names to TargetTypes for Neighborhood and Expansion requests.
func TargetFiltersForType(schema *ontology.Schema, typeName string) ([]string, []string) {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return nil, nil
	}
	if schema != nil && schema.Interfaces[typeName] != nil {
		return nil, []string{typeName}
	}
	return []string{typeName}, nil
}

func stringSet(values []string) map[string]struct{} {
	values = normalizeStrings(values)
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func nodeKindSet(values []ontology.NodeKind) map[ontology.NodeKind]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[ontology.NodeKind]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}

func hasPath(paths map[string]struct{}, path string) bool {
	_, ok := paths[path]
	return ok
}
