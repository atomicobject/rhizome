package noderead

import (
	"context"
	"strconv"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

type NodeReadPlan struct {
	Roots       []NodeReadRoot
	Fields      FieldSelection
	Relations   []RelationSelection
	Hydrate     HydrateOptions
	Limits      TraverseLimits
	Budget      TraverseBudget
	Diagnostics bool
}

type NodeReadRoot struct {
	Ref      ontology.NodeRef
	TypeName string
}

type FieldSelection struct {
	Names           []string
	IncludeBuiltin  bool
	IncludeIssues   bool
	IncludeContent  bool
	IncludeLocators bool
}

type RelationSelection struct {
	Name              string
	SourceTypes       []string
	Sources           []ontology.NodeRef
	Direction         TraversalDirection
	RelationNames     []string
	Provenance        map[string]struct{}
	IncludeStructural bool
	IncludeAmbient    bool
	TargetTypes       []string
	TargetInterfaces  []string
	NeighborScope     ontology.NeighborScope
	FirstPerSource    int
	FirstTotal        int
	MaxDepth          int
	Fields            FieldSelection
	Hydrate           HydrateOptions
	Children          []RelationSelection
}

type NodeReadResult struct {
	Roots       []NodeView
	Nodes       []NodeView
	Groups      []RelationGroup
	Edges       []NeighborhoodEdge
	Diagnostics NodeReadDiagnostics
	Truncated   bool
}

type NodeView struct {
	Ref      ontology.NodeRef
	Record   NodeRecord
	TypeName string
}

type RelationGroup struct {
	Name      string
	Source    ontology.NodeRef
	Edges     []NeighborhoodEdge
	Truncated bool
}

type NodeReadDiagnostics struct {
	RecordLoads         int
	RecordHits          int
	ContentLoads        int
	ContentHits         int
	TypeLoads           int
	TypeHits            int
	AssessmentLoads     int
	AssessmentHits      int
	EdgeLoads           int
	EdgeHits            int
	ProjectionFallbacks int
	Truncations         int
}

type relationWork struct {
	selection RelationSelection
	sources   []ontology.NodeRef
	depth     int
}

// Execute is the high-level request/job scoped read plan boundary. It executes
// relation work breadth-first so every depth shares scope-level edge and
// hydration batches before GraphQL or another caller assembles its own result.
func (s *Scope) Execute(ctx context.Context, plan NodeReadPlan) (NodeReadResult, error) {
	if err := ctx.Err(); err != nil {
		return NodeReadResult{}, err
	}
	if s == nil {
		return NodeReadResult{}, nil
	}
	before := s.Diagnostics()
	result := NodeReadResult{}
	rootRefs := nodeReadRootRefs(plan.Roots)
	rootHydrate := hydrateOptionsForFieldSelection(plan.Hydrate, plan.Fields)
	if rootHydrate.Profile != "" && len(rootRefs) > 0 {
		records, err := s.Hydrate(ctx, rootRefs, rootHydrate)
		if err != nil {
			return NodeReadResult{}, err
		}
		result.Roots = nodeViewsFromRecords(records)
	}
	if plan.Fields.IncludeLocators && len(rootRefs) > 0 {
		if _, err := s.Locators(ctx, rootRefs); err != nil {
			return NodeReadResult{}, err
		}
	}

	current := make([]relationWork, 0, len(plan.Relations))
	for _, relation := range plan.Relations {
		sources := relation.Sources
		if len(sources) == 0 {
			sources = rootRefs
		}
		current = append(current, relationWork{selection: relation, sources: sources})
	}
	seenNodeRefs := make(map[string]struct{})
	for depth := 0; len(current) > 0; depth++ {
		if err := ctx.Err(); err != nil {
			return NodeReadResult{}, err
		}
		if maxDepth := plan.Limits.MaxDepth; maxDepth > 0 && depth >= maxDepth {
			result.Truncated = true
			result.Diagnostics.Truncations++
			break
		}
		level := coalesceRelationWork(current)
		next := make([]relationWork, 0)
		for _, work := range level {
			if work.selection.MaxDepth > 0 && depth >= work.selection.MaxDepth {
				result.Truncated = true
				result.Diagnostics.Truncations++
				continue
			}
			limits := relationLimits(work.selection, plan.Limits)
			remainingEdges := remainingNodeReadLimit(minPositiveLimit(plan.Limits.MaxEdges, plan.Budget.MaxEdges), len(result.Edges))
			if remainingEdges.exhausted {
				result.Truncated = true
				result.Diagnostics.Truncations++
				continue
			}
			if remainingEdges.remaining > 0 {
				limits.FirstTotal = minPositiveLimit(limits.FirstTotal, remainingEdges.remaining)
			}
			remainingNodes := remainingNodeReadLimit(minPositiveLimit(plan.Limits.MaxNodes, plan.Budget.MaxNodes), len(seenNodeRefs))
			if remainingNodes.exhausted {
				result.Truncated = true
				result.Diagnostics.Truncations++
				continue
			}

			work.selection.Sources = filterRelationSources(work.sources, work.selection.SourceTypes)
			work.selection.Sources = normalizeNeighborhoodSources(work.selection.Sources, nil)
			if len(work.selection.Sources) == 0 {
				continue
			}
			neighborhood, err := s.executeSingleRelationSelection(ctx, work.selection, limits)
			if err != nil {
				return NodeReadResult{}, err
			}
			if remainingNodes.remaining > 0 {
				trimmed, truncated := trimNeighborhoodEdgesByTargets(neighborhood.Edges, seenNodeRefs, remainingNodes.remaining)
				if truncated {
					neighborhood.Edges = trimmed
					neighborhood = regroupNeighborhood(work.selection.Sources, neighborhood.Edges, limits)
					result.Truncated = true
					result.Diagnostics.Truncations++
				}
			}
			if remainingEdges.remaining > 0 && len(neighborhood.Edges) > remainingEdges.remaining {
				neighborhood.Edges = neighborhood.Edges[:remainingEdges.remaining]
				neighborhood = regroupNeighborhood(work.selection.Sources, neighborhood.Edges, limits)
				result.Truncated = true
				result.Diagnostics.Truncations++
			}
			result.Edges = append(result.Edges, neighborhood.Edges...)
			for _, source := range neighborhood.Sources {
				result.Groups = append(result.Groups, RelationGroup{
					Name:      work.selection.Name,
					Source:    source.Source,
					Edges:     append([]NeighborhoodEdge(nil), source.Edges...),
					Truncated: source.Truncated,
				})
				if source.Truncated {
					result.Truncated = true
					result.Diagnostics.Truncations++
				}
			}

			targets := uniqueNeighborhoodTargets(neighborhood.Edges)
			targets = trimNewNodeRefs(targets, seenNodeRefs, remainingNodes.remaining)
			if remainingNodes.remaining > 0 && len(uniqueNeighborhoodTargets(neighborhood.Edges)) > len(targets) {
				result.Truncated = true
				result.Diagnostics.Truncations++
			}
			for _, target := range targets {
				seenNodeRefs[nodeRefResultKey(target)] = struct{}{}
			}
			hydrate := hydrateOptionsForFieldSelection(work.selection.Hydrate, work.selection.Fields)
			if hydrate.Profile != "" && len(targets) > 0 {
				records, err := s.Hydrate(ctx, targets, hydrate)
				if err != nil {
					return NodeReadResult{}, err
				}
				result.Nodes = append(result.Nodes, nodeViewsFromRecords(records)...)
			}
			if work.selection.Fields.IncludeLocators && len(targets) > 0 {
				if _, err := s.Locators(ctx, targets); err != nil {
					return NodeReadResult{}, err
				}
			}
			if work.selection.MaxDepth > 0 && depth+1 >= work.selection.MaxDepth {
				if len(work.selection.Children) > 0 && len(targets) > 0 {
					result.Truncated = true
					result.Diagnostics.Truncations++
				}
				continue
			}
			for _, child := range work.selection.Children {
				if child.MaxDepth > 0 && depth+1 >= child.MaxDepth {
					result.Truncated = true
					result.Diagnostics.Truncations++
					continue
				}
				next = append(next, relationWork{selection: child, sources: targets, depth: depth + 1})
			}
		}
		current = next
	}
	if plan.Diagnostics {
		result.Diagnostics = result.Diagnostics.add(nodeReadDiagnosticsDelta(before, s.Diagnostics()))
	}
	return result, nil
}

func (s *Scope) executeSingleRelationSelection(ctx context.Context, selection RelationSelection, limits TraverseLimits) (NeighborhoodResult, error) {
	if selection.NeighborScope == ontology.NeighborScopeSubtree {
		return s.executeSubtreeRelationSelection(ctx, selection, limits)
	}
	switch {
	case selection.Direction == "" || selection.Direction == TraversalDirectionOutbound:
		if selection.IncludeStructural && !selection.IncludeAmbient && len(selection.RelationNames) <= 1 && len(selection.TargetTypes) == 0 && len(selection.TargetInterfaces) == 0 {
			return s.traverseRelationSelection(ctx, selection, true, limits)
		}
		if selection.IncludeAmbient && !selection.IncludeStructural && len(selection.RelationNames) <= 1 && len(selection.TargetTypes) <= 1 && len(selection.TargetInterfaces) == 0 {
			return s.traverseRelationSelection(ctx, selection, false, limits)
		}
	}
	return s.Neighborhood(ctx, neighborhoodRequestForSelection(selection, limits))
}

func (s *Scope) executeSubtreeRelationSelection(ctx context.Context, selection RelationSelection, limits TraverseLimits) (NeighborhoodResult, error) {
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

func (s *Scope) traverseRelationSelection(ctx context.Context, selection RelationSelection, structural bool, limits TraverseLimits) (NeighborhoodResult, error) {
	relation := ""
	if len(selection.RelationNames) > 0 {
		relation = selection.RelationNames[0]
	}
	dstType := ""
	if len(selection.TargetTypes) > 0 {
		dstType = selection.TargetTypes[0]
	}
	limitPerSource := limits.FirstPerSource
	if limitPerSource > 0 {
		limitPerSource++
	}
	traversed, err := s.Traverse(ctx, TraverseRequest{
		Sources:        selection.Sources,
		Relation:       relation,
		Provenance:     selection.Provenance,
		DstType:        dstType,
		Structural:     structural,
		LimitPerSource: limitPerSource,
	})
	if err != nil {
		return NeighborhoodResult{}, err
	}
	edges := make([]NeighborhoodEdge, 0)
	for _, source := range selection.Sources {
		for _, row := range traversed.EdgesBySource[source.NotePath] {
			edges = append(edges, neighborhoodEdgeFromTraverseRow(source, row, structural))
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

func regroupNeighborhood(sources []ontology.NodeRef, edges []NeighborhoodEdge, limits TraverseLimits) NeighborhoodResult {
	return groupNeighborhoodEdges(sources, edges, limits.FirstPerSource, limits.FirstTotal)
}

func neighborhoodEdgeFromTraverseRow(source ontology.NodeRef, row semdb.OntologyEdgeRow, structural bool) NeighborhoodEdge {
	target := ontology.NodeRef{NotePath: row.DstPath, Kind: ontology.NodeKindNote, TypeName: row.DstType}
	if row.DstNodeID != "" {
		target.Kind = ontology.NodeKindEmbedded
		target.NodeID = row.DstNodeID
	}
	return NeighborhoodEdge{
		Source:       source,
		Target:       target,
		Edge:         row,
		Direction:    TraversalDirectionOutbound,
		RelationName: row.RelationName,
		Provenance:   row.Provenance,
		Structural:   structural,
		TargetType:   row.DstType,
		Depth:        1,
	}
}

func relationLimits(selection RelationSelection, parent TraverseLimits) TraverseLimits {
	limits := parent
	if selection.FirstPerSource > 0 {
		limits.FirstPerSource = selection.FirstPerSource
	}
	if selection.FirstTotal > 0 {
		limits.FirstTotal = selection.FirstTotal
	}
	return limits
}

func coalesceRelationWork(items []relationWork) []relationWork {
	byKey := make(map[string]relationWork, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		item.sources = normalizeNeighborhoodSources(item.sources, nil)
		if len(item.sources) == 0 {
			continue
		}
		key := relationWorkKey(item.selection)
		current, exists := byKey[key]
		if !exists {
			order = append(order, key)
			current = item
			current.sources = nil
			current.selection.Children = nil
		}
		current.sources = mergeNodeRefs(current.sources, item.sources)
		current.selection.Children = append(current.selection.Children, item.selection.Children...)
		byKey[key] = current
	}
	out := make([]relationWork, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}
	return out
}

func relationWorkKey(selection RelationSelection) string {
	return strings.Join([]string{
		selection.Name,
		strings.Join(normalizeStrings(selection.SourceTypes), ","),
		string(selection.Direction),
		strings.Join(normalizeStrings(selection.RelationNames), ","),
		provenanceCacheKey(selection.Provenance),
		boolString(selection.IncludeStructural),
		boolString(selection.IncludeAmbient),
		strings.Join(normalizeStrings(selection.TargetTypes), ","),
		strings.Join(normalizeStrings(selection.TargetInterfaces), ","),
		string(selection.NeighborScope),
		intString(selection.FirstPerSource),
		intString(selection.FirstTotal),
		fieldSelectionKey(selection.Fields),
		string(selection.Hydrate.Profile),
	}, "|")
}

func hydrateOptionsForFieldSelection(hydrate HydrateOptions, fields FieldSelection) HydrateOptions {
	if hydrate.Profile != "" {
		return hydrate
	}
	if fields.IncludeContent {
		hydrate.Profile = HydrateContent
		return hydrate
	}
	if fields.IncludeBuiltin || fields.IncludeIssues || fields.IncludeLocators || len(fields.Names) > 0 {
		hydrate.Profile = HydrateSummary
	}
	return hydrate
}

func fieldSelectionKey(fields FieldSelection) string {
	return strings.Join([]string{
		strings.Join(normalizeStrings(fields.Names), ","),
		boolString(fields.IncludeBuiltin),
		boolString(fields.IncludeIssues),
		boolString(fields.IncludeContent),
		boolString(fields.IncludeLocators),
	}, ",")
}

func mergeNodeRefs(existing, refs []ontology.NodeRef) []ontology.NodeRef {
	out := append([]ontology.NodeRef(nil), existing...)
	seen := make(map[string]struct{}, len(out)+len(refs))
	for _, ref := range out {
		seen[nodeRefResultKey(ref)] = struct{}{}
	}
	for _, ref := range refs {
		key := nodeRefResultKey(ref)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func filterRelationSources(sources []ontology.NodeRef, sourceTypes []string) []ontology.NodeRef {
	sourceTypes = normalizeStrings(sourceTypes)
	if len(sourceTypes) == 0 {
		return sources
	}
	allowed := make(map[string]struct{}, len(sourceTypes))
	for _, typeName := range sourceTypes {
		allowed[typeName] = struct{}{}
	}
	out := make([]ontology.NodeRef, 0, len(sources))
	for _, source := range sources {
		if _, ok := allowed[source.TypeName]; ok {
			out = append(out, source)
		}
	}
	return out
}

func trimNewNodeRefs(refs []ontology.NodeRef, seen map[string]struct{}, remaining int) []ontology.NodeRef {
	out := make([]ontology.NodeRef, 0, len(refs))
	newCount := 0
	for _, ref := range refs {
		key := nodeRefResultKey(ref)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			out = append(out, ref)
			continue
		}
		if remaining > 0 && newCount >= remaining {
			break
		}
		out = append(out, ref)
		newCount++
	}
	return out
}

func trimNeighborhoodEdgesByTargets(edges []NeighborhoodEdge, seen map[string]struct{}, remaining int) ([]NeighborhoodEdge, bool) {
	out := make([]NeighborhoodEdge, 0, len(edges))
	localSeen := make(map[string]struct{})
	newCount := 0
	truncated := false
	for _, edge := range edges {
		key := nodeRefResultKey(edge.Target)
		if key == "" {
			out = append(out, edge)
			continue
		}
		if _, ok := seen[key]; ok {
			out = append(out, edge)
			continue
		}
		if _, ok := localSeen[key]; ok {
			out = append(out, edge)
			continue
		}
		if newCount >= remaining {
			truncated = true
			continue
		}
		localSeen[key] = struct{}{}
		newCount++
		out = append(out, edge)
	}
	return out, truncated
}

type remainingNodeRead struct {
	remaining int
	exhausted bool
}

func remainingNodeReadLimit(limit, used int) remainingNodeRead {
	if limit <= 0 {
		return remainingNodeRead{}
	}
	if used >= limit {
		return remainingNodeRead{exhausted: true}
	}
	return remainingNodeRead{remaining: limit - used}
}

func nodeReadRootRefs(roots []NodeReadRoot) []ontology.NodeRef {
	out := make([]ontology.NodeRef, 0, len(roots))
	for _, root := range roots {
		ref := normalizeNodeRef(root.Ref)
		if ref.IsZero() {
			continue
		}
		if root.TypeName != "" {
			ref.TypeName = root.TypeName
		}
		out = append(out, ref)
	}
	return out
}

func nodeViewsFromRecords(records []NodeRecord) []NodeView {
	out := make([]NodeView, 0, len(records))
	for _, record := range records {
		out = append(out, NodeView{
			Ref:      record.Ref,
			Record:   cloneNodeRecord(record),
			TypeName: record.TypeName,
		})
	}
	return out
}

func nodeReadDiagnosticsDelta(before, after Diagnostics) NodeReadDiagnostics {
	return NodeReadDiagnostics{
		RecordLoads:         after.RecordLoads - before.RecordLoads,
		RecordHits:          after.RecordHits - before.RecordHits,
		ContentLoads:        after.ContentLoads - before.ContentLoads,
		ContentHits:         after.ContentHits - before.ContentHits,
		TypeLoads:           after.TypeLoads - before.TypeLoads,
		TypeHits:            after.TypeHits - before.TypeHits,
		AssessmentLoads:     after.AssessmentLoads - before.AssessmentLoads,
		AssessmentHits:      after.AssessmentHits - before.AssessmentHits,
		EdgeLoads:           after.EdgeLoads - before.EdgeLoads,
		EdgeHits:            after.EdgeHits - before.EdgeHits,
		ProjectionFallbacks: after.ProjectionFallbacks - before.ProjectionFallbacks,
	}
}

func (d NodeReadDiagnostics) add(other NodeReadDiagnostics) NodeReadDiagnostics {
	d.RecordLoads += other.RecordLoads
	d.RecordHits += other.RecordHits
	d.ContentLoads += other.ContentLoads
	d.ContentHits += other.ContentHits
	d.TypeLoads += other.TypeLoads
	d.TypeHits += other.TypeHits
	d.AssessmentLoads += other.AssessmentLoads
	d.AssessmentHits += other.AssessmentHits
	d.EdgeLoads += other.EdgeLoads
	d.EdgeHits += other.EdgeHits
	d.ProjectionFallbacks += other.ProjectionFallbacks
	d.Truncations += other.Truncations
	return d
}

func boolString(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func intString(v int) string {
	return strconv.Itoa(v)
}
