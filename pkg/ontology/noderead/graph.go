package noderead

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

type graphOptions struct {
	includeEmbedded  bool
	includeUntyped   bool
	includeCode      bool
	includeCodeEdges bool
	includeDocEdges  bool
}

type graphDiagnosticsCollector struct {
	enabled bool
	result  GraphDiagnostics
}

func newGraphDiagnostics(enabled bool) *graphDiagnosticsCollector {
	if !enabled {
		return nil
	}
	return &graphDiagnosticsCollector{enabled: true}
}

func (d *graphDiagnosticsCollector) node(node GraphEndpoint, source, reason string) {
	if d == nil || !d.enabled {
		return
	}
	d.result.Nodes = append(d.result.Nodes, GraphNodeDiagnostic{
		ID:      node.ID,
		Source:  source,
		Reason:  reason,
		Ref:     node.Ref,
		Locator: node.SourceLocator,
	})
}

func (d *graphDiagnosticsCollector) edge(edge GraphReadEdge, reason string) {
	if d == nil || !d.enabled {
		return
	}
	d.result.Edges = append(d.result.Edges, GraphEdgeDiagnostic{
		Source:       edge.Source,
		Target:       edge.Target,
		Kind:         edge.Kind,
		RelationName: edge.RelationName,
		Provenance:   edge.Provenance,
		Reason:       reason,
	})
}

func (d *graphDiagnosticsCollector) skip(id, kind, reason string) {
	if d == nil || !d.enabled {
		return
	}
	d.result.Skips = append(d.result.Skips, GraphSkipDiagnostic{
		ID:     id,
		Kind:   kind,
		Reason: reason,
	})
}

func (d *graphDiagnosticsCollector) dedupe(edge GraphReadEdge, preferredKind, reason string) {
	if d == nil || !d.enabled {
		return
	}
	d.result.Dedupe = append(d.result.Dedupe, GraphDedupeDiagnostic{
		Source:         edge.Source,
		Target:         edge.Target,
		SuppressedKind: edge.Kind,
		PreferredKind:  preferredKind,
		Reason:         reason,
	})
}

func (d *graphDiagnosticsCollector) resultPtr() *GraphDiagnostics {
	if d == nil || !d.enabled {
		return nil
	}
	out := d.result
	return &out
}

// Graph assembles a profile-aware graph from indexed ontology nodes/edges plus
// permitted fallback doc/code graph evidence.
//
// Embedded and section sources are endpoint-scoped; if a source endpoint cannot
// be resolved, Graph must degrade narrowly rather than widening to the parent
// note's whole graph.
func (s *Scope) Graph(ctx context.Context, req GraphRequest) (GraphResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Docs: [[noderef-batch-traversal-read-api#^spec-0022-us8-ac8]] requires
	// graph output to preserve endpoint refs, provenance, and deterministic edge
	// shape for browser/search/MCP callers.
	if err := ctx.Err(); err != nil {
		return GraphResult{}, err
	}
	if s == nil || s.service == nil || s.service.Store == nil {
		return GraphResult{}, nil
	}
	// Docs: [[ontology-indexed-read-model-contract#^spec-0040-profile-ontology-native]]
	// defines the graph profile matrix, including explicit code-edge opt-in.
	graphStore, ok := s.service.Store.(GraphStore)
	if !ok {
		return GraphResult{}, nil
	}
	req = normalizeGraphRequest(req)
	cacheKey := graphRequestCacheKey(req)
	s.mu.Lock()
	if cached, ok := s.graphByKey[cacheKey]; ok {
		s.mu.Unlock()
		return cloneGraphResult(cached), nil
	}
	generation := s.cacheGeneration
	s.mu.Unlock()
	options := graphOptionsForProfile(req.Profile)
	// Docs: [[Ontology graph diagnostics and profile review]] is the review map
	// for profile flags, endpoint scope, diagnostics, and consumer expectations.
	if req.IncludeCodeEdges {
		// IMPORTANT: code-aware profile includes note-code evidence, but code-code
		// calls/imports/type edges require this explicit opt-in.
		options.includeCode = true
		options.includeCodeEdges = true
	}
	diagnostics := newGraphDiagnostics(req.Diagnostics)

	paths := graphRequestPaths(req)
	endpointScoped := graphHasEndpointScopedSources(req.Sources)
	endpointSelectors := graphEndpointSelectors(req.Sources)
	catalogRows, err := graphStore.GraphOntologyNodes(ctx, readmodel.GraphNodeQuery{
		Paths:             paths,
		PathPrefixes:      req.PathPrefixes,
		EndpointSelectors: endpointSelectors,
		Limit:             graphNodeQueryLimit(req),
	})
	if err != nil {
		return GraphResult{}, err
	}
	ontologyEdges, err := graphStore.GraphOntologyEdges(ctx, readmodel.GraphEdgeQuery{
		Paths:             paths,
		PathPrefixes:      req.PathPrefixes,
		EndpointSelectors: endpointSelectors,
		IncludeAmbient:    true,
		Limit:             graphEdgeQueryLimit(req),
	})
	if err != nil {
		return GraphResult{}, err
	}

	nodes := make(map[string]GraphEndpoint)
	edges := make(map[string]GraphReadEdge)
	var ontologyEndpointPairs map[graphEndpointPair]struct{}
	if options.includeDocEdges {
		ontologyEndpointPairs = make(map[graphEndpointPair]struct{})
	}
	ontologyNodesByID := graphNodeRowsByID(catalogRows)
	var indexedPaths []readmodel.GraphPathRow
	if options.includeDocEdges && len(paths) == 0 && len(req.PathPrefixes) == 0 {
		indexedPaths, err = graphStore.GraphIndexedPaths(ctx, options.includeCode)
		if err != nil {
			return GraphResult{}, err
		}
	}
	if len(paths) == 0 && len(req.PathPrefixes) == 0 && options.includeDocEdges {
		for _, row := range indexedPaths {
			kind := GraphEndpointKind(row.Kind)
			if kind == "" {
				kind = GraphEndpointNote
			}
			node := graphEndpointFromPath(kind, row.Path)
			addGraphEndpoint(nodes, node)
			diagnostics.node(node, "indexed_path", "indexed fallback node")
		}
	}
	visibleOntologyNodes := make(map[string]readmodel.GraphNodeRow)
	sourceEndpointIDs := graphSourceEndpointIDs(req.Sources, catalogRows)
	for _, row := range catalogRows {
		if strings.TrimSpace(row.NotePath) == "" {
			diagnostics.skip(row.NodeID, row.NodeKind, "ontology node has no note path")
			continue
		}
		if row.NodeKind == string(ontology.NodeKindNote) {
			noteNode := graphEndpointFromNote(row.NotePath, row.TypeName, row)
			addGraphEndpoint(nodes, noteNode)
			diagnostics.node(noteNode, "ontology_node", "typed note node")
			continue
		}
		noteNode := graphEndpointFromNote(row.NotePath, "", readmodel.GraphNodeRow{})
		addGraphEndpoint(nodes, noteNode)
		diagnostics.node(noteNode, "ontology_node", "parent note endpoint")
		if !graphOntologyNodeVisible(row, options, sourceEndpointIDs) {
			diagnostics.skip(row.NodeID, row.NodeKind, "non-visible ontology node kind")
			continue
		}
		visibleOntologyNodes[row.NodeID] = row
		if ref := nodeRefFromGraphNodeRow(row); strings.TrimSpace(ref.NodeID) != "" {
			visibleOntologyNodes[ref.NodeID] = row
		}
		embeddedNode := graphEndpointFromCatalogRow(row)
		addGraphEndpoint(nodes, embeddedNode)
		diagnostics.node(embeddedNode, "ontology_node", "ontology child node")
	}
	if options.includeEmbedded {
		for _, row := range uniqueGraphNodeRows(visibleOntologyNodes) {
			sourceID := graphEmbedsSourceID(row, visibleOntologyNodes, ontologyNodesByID)
			targetID := graphEndpointID(graphEndpointKindForOntologyNodeKind(row.NodeKind), row.NodeID)
			if endpointScoped && !graphEndpointInScope(sourceEndpointIDs, sourceID, targetID, endpointScoped) {
				continue
			}
			edge := GraphReadEdge{
				Source:        sourceID,
				Target:        targetID,
				Kind:          string(GraphEdgeKindEmbeds),
				RelationName:  string(GraphEdgeKindEmbeds),
				RelationLabel: relationLabel(string(GraphEdgeKindEmbeds)),
				Provenance:    "ontology_node",
				Structural:    true,
				Weight:        1,
			}
			addGraphEdge(edges, edge)
			diagnostics.edge(edge, "nearest visible parent embeds child")
		}
	}
	for _, row := range ontologyEdges {
		sourceID := graphEndpointID(GraphEndpointNote, row.SrcPath)
		if options.includeEmbedded && row.SrcNodeID != "" {
			if sourceNode, ok := visibleOntologyNodes[row.SrcNodeID]; ok {
				sourceID = graphEndpointID(graphEndpointKindForOntologyNodeKind(sourceNode.NodeKind), sourceNode.NodeID)
			}
		}
		targetID := graphEndpointID(GraphEndpointNote, row.DstPath)
		if options.includeEmbedded && row.DstNodeID != "" {
			if targetNode, ok := visibleOntologyNodes[row.DstNodeID]; ok {
				targetID = graphEndpointID(graphEndpointKindForOntologyNodeKind(targetNode.NodeKind), targetNode.NodeID)
			}
		}
		if endpointScoped && !graphEndpointInScope(sourceEndpointIDs, sourceID, targetID, endpointScoped) {
			diagnostics.skip(sourceID+"|"+targetID, "ontology_edge", "edge outside endpoint source scope")
			continue
		}
		if sourceID == "" || targetID == "" || sourceID == targetID {
			diagnostics.skip(sourceID+"|"+targetID, "ontology_edge", "edge has empty or self endpoint")
			continue
		}
		if _, ok := nodes[sourceID]; !ok {
			sourceNode := graphEndpointFromNote(row.SrcPath, "", readmodel.GraphNodeRow{})
			addGraphEndpoint(nodes, sourceNode)
			diagnostics.node(sourceNode, "ontology_edge", "implicit source endpoint")
		}
		if _, ok := nodes[targetID]; !ok {
			targetNode := graphEndpointFromNote(row.DstPath, row.DstType, readmodel.GraphNodeRow{})
			addGraphEndpoint(nodes, targetNode)
			diagnostics.node(targetNode, "ontology_edge", "implicit target endpoint")
		}
		edge := GraphReadEdge{
			Source:        sourceID,
			Target:        targetID,
			Kind:          string(GraphEdgeKindOntology),
			RelationName:  row.RelationName,
			RelationLabel: relationLabel(row.RelationName),
			Provenance:    row.Provenance,
			Structural:    row.Structural,
			Weight:        1,
		}
		addGraphEdge(edges, edge)
		if ontologyEndpointPairs != nil {
			ontologyEndpointPairs[newGraphEndpointPair(sourceID, targetID)] = struct{}{}
		}
		diagnostics.edge(edge, "typed ontology edge")
	}
	if options.includeDocEdges && !endpointScoped {
		// IMPORTANT: endpoint-scoped reads intentionally skip fallback doc edges.
		// A missing embedded catalog row should not broaden into the parent note's
		// ambient wikilink/code neighborhood.
		docPaths := paths
		docEdges, err := graphDocEdges(ctx, graphStore, docPaths, req.PathPrefixes, graphEdgeQueryLimit(req), options.includeCode, options.includeCodeEdges)
		if err != nil {
			return GraphResult{}, err
		}
		for _, row := range docEdges {
			if len(req.PathPrefixes) > 0 && !graphPathHasAnyPrefix(row.SrcPath, req.PathPrefixes) && !graphPathHasAnyPrefix(row.DstPath, req.PathPrefixes) {
				continue
			}
			srcKind, srcPath := graphDocRowEndpoint(row.SrcPath, row.SourceKind)
			dstKind, dstPath := graphDocRowEndpoint(row.DstPath, row.TargetKind)
			if srcPath == "" || dstPath == "" {
				diagnostics.skip(row.SrcPath+"|"+row.DstPath, "graph_doc_edge", "edge has empty endpoint")
				continue
			}
			if !options.includeCode && (srcKind == GraphEndpointCode || dstKind == GraphEndpointCode) {
				diagnostics.skip(row.SrcPath+"|"+row.DstPath, "graph_doc_edge", "code endpoint excluded by profile")
				continue
			}
			if !options.includeUntyped {
				if _, ok := nodes[graphEndpointID(srcKind, srcPath)]; !ok {
					diagnostics.skip(row.SrcPath+"|"+row.DstPath, "graph_doc_edge", "untyped source excluded by profile")
					continue
				}
				if _, ok := nodes[graphEndpointID(dstKind, dstPath)]; !ok {
					diagnostics.skip(row.SrcPath+"|"+row.DstPath, "graph_doc_edge", "untyped target excluded by profile")
					continue
				}
			}
			srcID := graphEndpointID(srcKind, srcPath)
			dstID := graphEndpointID(dstKind, dstPath)
			if _, ok := ontologyEndpointPairs[newGraphEndpointPair(srcID, dstID)]; ok {
				// Docs: [[ontology-indexed-read-model-contract]].
				diagnostics.dedupe(GraphReadEdge{Source: srcID, Target: dstID, Kind: row.Kind}, "ontology", "ontology edge supersedes doc link")
				continue
			}
			srcNode := graphEndpointFromPath(srcKind, srcPath)
			dstNode := graphEndpointFromPath(dstKind, dstPath)
			addGraphEndpoint(nodes, srcNode)
			addGraphEndpoint(nodes, dstNode)
			diagnostics.node(srcNode, "graph_doc_edge", "doc edge source endpoint")
			diagnostics.node(dstNode, "graph_doc_edge", "doc edge target endpoint")
			weight := row.Weight
			if weight <= 0 {
				weight = 1
			}
			confidence := row.ConfidenceScore
			if confidence <= 0 {
				confidence = 1
			}
			edge := GraphReadEdge{Source: srcID, Target: dstID, Kind: row.Kind, Weight: weight, Confidence: confidence}
			addGraphEdge(edges, edge)
			diagnostics.edge(edge, "fallback doc edge")
		}
	}
	result := GraphResult{Nodes: sortedGraphEndpoints(nodes, req.NodeLimit), Edges: sortedGraphEdges(edges, req.EdgeLimit), Diagnostics: diagnostics.resultPtr()}
	s.mu.Lock()
	if s.cacheGeneration == generation {
		s.graphByKey[cacheKey] = cloneGraphResult(result)
	}
	s.mu.Unlock()
	return result, nil
}

// GraphFacts returns consumer-friendly graph facts with denormalized endpoint
// metadata.
//
// It delegates graph assembly to Graph, then applies include flags and final
// facts-level limits after endpoint kind filtering.
func (s *Scope) GraphFacts(ctx context.Context, req GraphFactsRequest) (GraphFactsResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return GraphFactsResult{}, err
	}
	req = normalizeGraphFactsRequest(req)
	cacheKey := graphFactsRequestCacheKey(req)
	s.mu.Lock()
	if cached, ok := s.graphFactsByKey[cacheKey]; ok {
		s.mu.Unlock()
		return cloneGraphFactsResult(cached), nil
	}
	generation := s.cacheGeneration
	s.mu.Unlock()

	profile := GraphProfileNotesOnly
	if req.IncludeCode || req.IncludeCalls {
		profile = GraphProfileCodeAware
	}
	graph, err := s.Graph(ctx, GraphRequest{
		Sources:          req.Sources,
		Paths:            req.Paths,
		PathPrefixes:     req.PathPrefixes,
		Profile:          profile,
		NodeLimit:        -1,
		EdgeLimit:        -1,
		IncludeCodeEdges: req.IncludeCalls,
		Diagnostics:      req.Diagnostics,
	})
	if err != nil {
		return GraphFactsResult{}, err
	}

	nodesByID := make(map[string]GraphEndpoint, len(graph.Nodes))
	nodes := make([]GraphEndpoint, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if !req.IncludeEmbedded && (node.Kind == GraphEndpointEmbedded || node.Kind == GraphEndpointSection) {
			continue
		}
		nodesByID[node.ID] = node
		nodes = append(nodes, node)
	}

	facts := make([]GraphFactEdge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		if !graphFactEdgeIncluded(edge, req) {
			continue
		}
		source, sourceOK := nodesByID[edge.Source]
		target, targetOK := nodesByID[edge.Target]
		if !sourceOK || !targetOK {
			continue
		}
		weight := float64(edge.Weight)
		if weight <= 0 {
			weight = 1
		}
		confidence := edge.Confidence
		if confidence <= 0 {
			confidence = 1
		}
		facts = append(facts, GraphFactEdge{
			Source:         edge.Source,
			Target:         edge.Target,
			SourceRef:      source.Ref,
			TargetRef:      target.Ref,
			SourcePath:     firstNonEmptyString(source.NotePath, source.Path),
			TargetPath:     firstNonEmptyString(target.NotePath, target.Path),
			SourceKind:     source.Kind,
			TargetKind:     target.Kind,
			Kind:           edge.Kind,
			RelationName:   edge.RelationName,
			RelationLabel:  edge.RelationLabel,
			Provenance:     edge.Provenance,
			Structural:     edge.Structural,
			Weight:         weight,
			Confidence:     confidence,
			SourceLocator:  source.SourceLocator,
			TargetLocator:  target.SourceLocator,
			SourceNodeID:   source.NodeID,
			TargetNodeID:   target.NodeID,
			SourceTypeName: source.TypeName,
			TargetTypeName: target.TypeName,
		})
	}
	if req.NodeLimit > 0 && len(nodes) > req.NodeLimit {
		nodes = nodes[:req.NodeLimit]
		allowed := make(map[string]struct{}, len(nodes))
		for _, node := range nodes {
			allowed[node.ID] = struct{}{}
		}
		filtered := facts[:0]
		for _, fact := range facts {
			if _, ok := allowed[fact.Source]; !ok {
				continue
			}
			if _, ok := allowed[fact.Target]; !ok {
				continue
			}
			filtered = append(filtered, fact)
		}
		facts = filtered
	}
	if req.EdgeLimit > 0 && len(facts) > req.EdgeLimit {
		facts = facts[:req.EdgeLimit]
	}
	result := GraphFactsResult{Nodes: nodes, Edges: facts, Diagnostics: graph.Diagnostics}
	s.mu.Lock()
	if s.cacheGeneration == generation {
		s.graphFactsByKey[cacheKey] = cloneGraphFactsResult(result)
	}
	s.mu.Unlock()
	return result, nil
}

func normalizeGraphFactsRequest(req GraphFactsRequest) GraphFactsRequest {
	req.Sources = normalizeGraphSources(req.Sources)
	req.Paths = normalizeStrings(req.Paths)
	req.PathPrefixes = normalizeStrings(req.PathPrefixes)
	if req.NodeLimit == 0 {
		req.NodeLimit = 500
	}
	if req.EdgeLimit == 0 {
		req.EdgeLimit = 500
	}
	if !req.IncludeOntology && !req.IncludeDocLinks {
		req.IncludeOntology = true
		req.IncludeDocLinks = true
	}
	if req.IncludeCalls {
		req.IncludeCode = true
	}
	return req
}

func graphFactsRequestCacheKey(req GraphFactsRequest) string {
	var b strings.Builder
	b.WriteString("facts|nl=")
	b.WriteString(strconv.Itoa(req.NodeLimit))
	b.WriteString("|el=")
	b.WriteString(strconv.Itoa(req.EdgeLimit))
	b.WriteString("|diag=")
	b.WriteString(strconv.FormatBool(req.Diagnostics))
	b.WriteString("|ontology=")
	b.WriteString(strconv.FormatBool(req.IncludeOntology))
	b.WriteString("|doc=")
	b.WriteString(strconv.FormatBool(req.IncludeDocLinks))
	b.WriteString("|code=")
	b.WriteString(strconv.FormatBool(req.IncludeCode))
	b.WriteString("|calls=")
	b.WriteString(strconv.FormatBool(req.IncludeCalls))
	b.WriteString("|embedded=")
	b.WriteString(strconv.FormatBool(req.IncludeEmbedded))
	b.WriteString("|sources=")
	for _, ref := range req.Sources {
		b.WriteString(nodeRefIdentityKey(ref))
		b.WriteString(";")
	}
	b.WriteString("|paths=")
	for _, path := range req.Paths {
		b.WriteString(path)
		b.WriteString(";")
	}
	b.WriteString("|prefixes=")
	for _, prefix := range req.PathPrefixes {
		b.WriteString(prefix)
		b.WriteString(";")
	}
	return b.String()
}

func cloneGraphFactsResult(result GraphFactsResult) GraphFactsResult {
	out := result
	out.Nodes = append([]GraphEndpoint(nil), result.Nodes...)
	out.Edges = append([]GraphFactEdge(nil), result.Edges...)
	if result.Diagnostics != nil {
		diag := *result.Diagnostics
		diag.Nodes = append([]GraphNodeDiagnostic(nil), result.Diagnostics.Nodes...)
		diag.Edges = append([]GraphEdgeDiagnostic(nil), result.Diagnostics.Edges...)
		diag.Skips = append([]GraphSkipDiagnostic(nil), result.Diagnostics.Skips...)
		diag.Dedupe = append([]GraphDedupeDiagnostic(nil), result.Diagnostics.Dedupe...)
		out.Diagnostics = &diag
	}
	return out
}

func graphFactEdgeIncluded(edge GraphReadEdge, req GraphFactsRequest) bool {
	kind := strings.ToLower(strings.TrimSpace(edge.Kind))
	switch kind {
	case string(GraphEdgeKindOntology):
		return req.IncludeOntology
	case string(GraphEdgeKindEmbeds):
		return req.IncludeEmbedded
	default:
		if !req.IncludeDocLinks {
			return false
		}
		if graphReadEdgeIsCodeCode(edge) {
			return req.IncludeCode && req.IncludeCalls
		}
		if !req.IncludeCode && (strings.HasPrefix(edge.Source, string(GraphEndpointCode)+":") || strings.HasPrefix(edge.Target, string(GraphEndpointCode)+":")) {
			return false
		}
		return true
	}
}

func graphReadEdgeIsCodeCode(edge GraphReadEdge) bool {
	return strings.HasPrefix(edge.Source, string(GraphEndpointCode)+":") && strings.HasPrefix(edge.Target, string(GraphEndpointCode)+":")
}

func normalizeGraphRequest(req GraphRequest) GraphRequest {
	req.Sources = normalizeGraphSources(req.Sources)
	req.Paths = normalizeStrings(req.Paths)
	req.PathPrefixes = normalizeStrings(req.PathPrefixes)
	if req.Limit <= 0 {
		req.Limit = 500
	}
	if req.NodeLimit == 0 {
		req.NodeLimit = req.Limit
	}
	if req.EdgeLimit == 0 {
		req.EdgeLimit = req.Limit
	}
	if req.Profile == "" {
		req.Profile = GraphProfileOntologyNative
	}
	return req
}

func graphNodeQueryLimit(req GraphRequest) int {
	if req.NodeLimit <= 0 {
		return 0
	}
	return req.NodeLimit * 4
}

func graphEdgeQueryLimit(req GraphRequest) int {
	if req.EdgeLimit <= 0 {
		return 0
	}
	return req.EdgeLimit * 4
}

func graphRequestCacheKey(req GraphRequest) string {
	var b strings.Builder
	b.WriteString(string(req.Profile))
	b.WriteString("|nl=")
	b.WriteString(strconv.Itoa(req.NodeLimit))
	b.WriteString("|el=")
	b.WriteString(strconv.Itoa(req.EdgeLimit))
	b.WriteString("|diag=")
	b.WriteString(strconv.FormatBool(req.Diagnostics))
	b.WriteString("|codeEdges=")
	b.WriteString(strconv.FormatBool(req.IncludeCodeEdges))
	b.WriteString("|sources=")
	for _, ref := range req.Sources {
		b.WriteString(nodeRefIdentityKey(ref))
		b.WriteString(";")
	}
	b.WriteString("|paths=")
	for _, path := range req.Paths {
		b.WriteString(path)
		b.WriteString(";")
	}
	b.WriteString("|prefixes=")
	for _, prefix := range req.PathPrefixes {
		b.WriteString(prefix)
		b.WriteString(";")
	}
	return b.String()
}

func cloneGraphResult(result GraphResult) GraphResult {
	out := result
	out.Nodes = append([]GraphEndpoint(nil), result.Nodes...)
	out.Edges = append([]GraphReadEdge(nil), result.Edges...)
	if result.Diagnostics != nil {
		diag := *result.Diagnostics
		diag.Nodes = append([]GraphNodeDiagnostic(nil), result.Diagnostics.Nodes...)
		diag.Edges = append([]GraphEdgeDiagnostic(nil), result.Diagnostics.Edges...)
		diag.Skips = append([]GraphSkipDiagnostic(nil), result.Diagnostics.Skips...)
		diag.Dedupe = append([]GraphDedupeDiagnostic(nil), result.Diagnostics.Dedupe...)
		out.Diagnostics = &diag
	}
	return out
}

func graphOptionsForProfile(profile GraphProfile) graphOptions {
	switch profile {
	case GraphProfileNotesOnly:
		return graphOptions{includeEmbedded: true, includeUntyped: true, includeCode: false, includeCodeEdges: false, includeDocEdges: true}
	case GraphProfileCodeAware:
		return graphOptions{includeEmbedded: true, includeUntyped: true, includeCode: true, includeCodeEdges: false, includeDocEdges: true}
	default:
		return graphOptions{includeEmbedded: true, includeUntyped: false, includeCode: false, includeCodeEdges: false, includeDocEdges: false}
	}
}

func normalizeGraphSources(refs []ontology.NodeRef) []ontology.NodeRef {
	out := make([]ontology.NodeRef, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		ref = normalizeNodeRef(ref)
		if ref.IsZero() {
			continue
		}
		key := nodeRefIdentityKey(ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func graphSourcePaths(refs []ontology.NodeRef) []string {
	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.NotePath != "" {
			paths = append(paths, ref.NotePath)
		}
	}
	return normalizeStrings(paths)
}

func graphRequestPaths(req GraphRequest) []string {
	paths := make([]string, 0, len(req.Sources)+len(req.Paths))
	paths = append(paths, graphSourcePaths(req.Sources)...)
	paths = append(paths, req.Paths...)
	return normalizeStrings(paths)
}

func graphEndpointSelectors(refs []ontology.NodeRef) []readmodel.GraphEndpointSelector {
	selectors := make([]readmodel.GraphEndpointSelector, 0, len(refs))
	for _, ref := range refs {
		ref = normalizeNodeRef(ref)
		if ref.IsZero() || !graphRefIsEndpointScoped(ref) {
			continue
		}
		selectors = append(selectors, readmodel.GraphEndpointSelector{
			Path:          ref.NotePath,
			NodeID:        ref.NodeID,
			Kind:          string(ref.Kind),
			Fragment:      ref.Fragment,
			Structural:    ref.Structural,
			SourceLocator: ontology.NodeSourceLocator(ref),
		})
	}
	return selectors
}

func graphHasEndpointScopedSources(refs []ontology.NodeRef) bool {
	for _, ref := range refs {
		if graphRefIsEndpointScoped(ref) {
			return true
		}
	}
	return false
}

func graphRefIsEndpointScoped(ref ontology.NodeRef) bool {
	return ref.Kind == ontology.NodeKindEmbedded || ref.Kind == ontology.NodeKindSection || strings.TrimSpace(ref.NodeID) != "" || strings.TrimSpace(ref.Fragment) != "" || strings.TrimSpace(ref.Structural) != ""
}

func graphNodeRowsByID(rows []readmodel.GraphNodeRow) map[string]readmodel.GraphNodeRow {
	out := make(map[string]readmodel.GraphNodeRow, len(rows))
	for _, row := range rows {
		id := strings.TrimSpace(row.NodeID)
		if id == "" {
			continue
		}
		out[id] = row
		if ref := nodeRefFromGraphNodeRow(row); strings.TrimSpace(ref.NodeID) != "" {
			out[ref.NodeID] = row
		}
	}
	return out
}

func graphEmbedsSourceID(row readmodel.GraphNodeRow, visibleRows map[string]readmodel.GraphNodeRow, allRows map[string]readmodel.GraphNodeRow) string {
	for parentID := strings.TrimSpace(row.ParentNodeID); parentID != ""; {
		if parent, ok := visibleRows[parentID]; ok && ontology.NodeKind(parent.NodeKind) == ontology.NodeKindEmbedded {
			return graphEndpointID(GraphEndpointEmbedded, parent.NodeID)
		}
		parent, ok := allRows[parentID]
		if !ok {
			break
		}
		if ontology.NodeKind(parent.NodeKind) == ontology.NodeKindNote {
			return graphEndpointID(GraphEndpointNote, parent.NotePath)
		}
		parentID = strings.TrimSpace(parent.ParentNodeID)
	}
	return graphEndpointID(GraphEndpointNote, row.NotePath)
}

func graphSourceEndpointIDs(refs []ontology.NodeRef, rows []readmodel.GraphNodeRow) map[string]struct{} {
	out := map[string]struct{}{}
	for _, ref := range refs {
		ref = normalizeNodeRef(ref)
		if ref.IsZero() {
			continue
		}
		if ref.Kind != ontology.NodeKindEmbedded && ref.Kind != ontology.NodeKindSection && strings.TrimSpace(ref.NodeID) == "" && strings.TrimSpace(ref.Fragment) == "" && strings.TrimSpace(ref.Structural) == "" {
			out[graphEndpointID(GraphEndpointNote, ref.NotePath)] = struct{}{}
			continue
		}
		for _, row := range rows {
			if !graphSourceRefMatchesRow(ref, row) {
				continue
			}
			id := graphEndpointID(graphEndpointKindForOntologyNodeKind(row.NodeKind), row.NodeID)
			if id != "" {
				out[id] = struct{}{}
			}
		}
	}
	return out
}

func graphSourceRefMatchesRow(ref ontology.NodeRef, row readmodel.GraphNodeRow) bool {
	if strings.TrimSpace(ref.NotePath) != "" && strings.TrimSpace(ref.NotePath) != strings.TrimSpace(row.NotePath) {
		return false
	}
	if strings.TrimSpace(ref.NodeID) != "" {
		rowRef := nodeRefFromGraphNodeRow(row)
		if strings.TrimSpace(ref.NodeID) != strings.TrimSpace(row.NodeID) &&
			strings.TrimSpace(ref.NodeID) != strings.TrimSpace(row.BlockID) &&
			strings.TrimSpace(ref.NodeID) != strings.TrimSpace(row.Fragment) &&
			strings.TrimSpace(ref.NodeID) != strings.TrimSpace(rowRef.NodeID) {
			return false
		}
	}
	if ref.Kind != "" && row.NodeKind != "" && string(ref.Kind) != row.NodeKind {
		return false
	}
	fragment := strings.TrimPrefix(strings.TrimSpace(ref.Fragment), "#")
	if fragment != "" {
		rowFragment := strings.TrimPrefix(strings.TrimSpace(row.Fragment), "#")
		rowBlock := "^" + strings.TrimPrefix(strings.TrimSpace(row.BlockID), "^")
		if fragment != rowFragment && fragment != rowBlock && "#"+fragment != strings.TrimPrefix(strings.TrimSpace(row.SourceLocator), row.NotePath) {
			return false
		}
	}
	if strings.TrimSpace(ref.Structural) != "" && strings.TrimSpace(ref.Structural) != strings.TrimSpace(row.Fragment) && strings.TrimSpace(ref.Structural) != strings.TrimSpace(row.BlockID) {
		return false
	}
	return true
}

func graphEndpointInScope(sourceIDs map[string]struct{}, a, b string, endpointScoped bool) bool {
	if len(sourceIDs) == 0 {
		return !endpointScoped
	}
	_, aOK := sourceIDs[a]
	_, bOK := sourceIDs[b]
	return aOK || bOK
}

func graphOntologyNodeVisible(row readmodel.GraphNodeRow, options graphOptions, sourceIDs map[string]struct{}) bool {
	kind := ontology.NodeKind(row.NodeKind)
	switch kind {
	case ontology.NodeKindEmbedded:
		return options.includeEmbedded || graphEndpointInSourceSet(sourceIDs, GraphEndpointEmbedded, row.NodeID)
	case ontology.NodeKindSection:
		return graphEndpointInSourceSet(sourceIDs, GraphEndpointSection, row.NodeID)
	default:
		return false
	}
}

func graphEndpointInSourceSet(sourceIDs map[string]struct{}, kind GraphEndpointKind, nodeID string) bool {
	if len(sourceIDs) == 0 || strings.TrimSpace(nodeID) == "" {
		return false
	}
	_, ok := sourceIDs[graphEndpointID(kind, nodeID)]
	return ok
}

func graphDocEdges(ctx context.Context, store GraphStore, paths []string, prefixes []string, limit int, includeCode bool, includeCodeEdges bool) ([]readmodel.GraphDocEdgeRow, error) {
	return store.GraphDocEdges(ctx, readmodel.GraphDocEdgeQuery{
		Paths:            paths,
		PathPrefixes:     prefixes,
		IncludeCode:      includeCode,
		IncludeCodeEdges: includeCodeEdges,
		Limit:            limit,
	})
}

// CodeGraphLinks returns note-code or code-note graph links incident to source.
//
// It is a light summary API for callers that need related code paths without
// pulling the full graph result.
func (s *Scope) CodeGraphLinks(ctx context.Context, source ontology.NodeRef, limit int) ([]GraphCodeLink, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.service == nil || s.service.Store == nil {
		return nil, nil
	}
	graphStore, ok := s.service.Store.(GraphStore)
	if !ok {
		return nil, nil
	}
	source = normalizeNodeRef(source)
	if strings.TrimSpace(source.NotePath) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := graphDocEdges(ctx, graphStore, []string{source.NotePath}, nil, limit, true, false)
	if err != nil {
		return nil, err
	}
	links := make([]GraphCodeLink, 0, len(rows))
	for _, row := range rows {
		other, otherKind := "", ""
		switch source.NotePath {
		case row.SrcPath:
			other, otherKind = row.DstPath, row.TargetKind
		case row.DstPath:
			other, otherKind = row.SrcPath, row.SourceKind
		default:
			continue
		}
		kind, path := graphDocRowEndpoint(other, otherKind)
		if kind != GraphEndpointCode || path == "" {
			continue
		}
		weight := row.Weight
		if weight <= 0 {
			weight = 1
		}
		links = append(links, GraphCodeLink{
			Path:       path,
			Kind:       row.Kind,
			Provenance: row.Kind,
			Weight:     weight,
		})
	}
	return links, nil
}

func graphEndpointFromCatalogRow(row readmodel.GraphNodeRow) GraphEndpoint {
	ref := nodeRefFromGraphNodeRow(row)
	locator := graphPreferredSourceLocator(row)
	return GraphEndpoint{
		ID:            graphEndpointID(graphEndpointKindForOntologyNodeKind(row.NodeKind), row.NodeID),
		Ref:           ref,
		Kind:          graphEndpointKindForOntologyNodeKind(row.NodeKind),
		Path:          firstNonEmptyString(locator, row.SourceLocator, row.NotePath),
		NotePath:      row.NotePath,
		NodeID:        row.NodeID,
		TypeName:      row.TypeName,
		Label:         firstNonEmptyString(row.DisplayLabel, row.Title, locator, row.SourceLocator, row.NotePath),
		SourceLocator: locator,
		ParentID:      row.ParentNodeID,
	}
}

func graphEndpointKindForOntologyNodeKind(kind string) GraphEndpointKind {
	switch ontology.NodeKind(kind) {
	case ontology.NodeKindSection:
		return GraphEndpointSection
	case ontology.NodeKindEmbedded:
		return GraphEndpointEmbedded
	default:
		return GraphEndpointNote
	}
}

func uniqueGraphNodeRows(rows map[string]readmodel.GraphNodeRow) []readmodel.GraphNodeRow {
	seen := map[string]struct{}{}
	out := make([]readmodel.GraphNodeRow, 0, len(rows))
	for _, row := range rows {
		key := strings.TrimSpace(row.NodeID)
		if key == "" {
			key = strings.TrimSpace(row.SourceLocator)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return graphEndpointID(graphEndpointKindForOntologyNodeKind(out[i].NodeKind), out[i].NodeID) < graphEndpointID(graphEndpointKindForOntologyNodeKind(out[j].NodeKind), out[j].NodeID)
	})
	return out
}

func graphEndpointFromNote(path, typeName string, row readmodel.GraphNodeRow) GraphEndpoint {
	ref := ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote, TypeName: typeName}
	nodeID := ""
	sourceLocator := path
	label := noteTitle(path, nil)
	endpointTypeName := typeName
	if row.NodeRefJSON != "" {
		ref = nodeRefFromGraphNodeRow(row)
		ref.Kind = ontology.NodeKindNote
		ref.NotePath = path
		ref.Fragment = ""
		ref.NodeID = strings.TrimSpace(ref.NodeID)
	}
	nodeID = row.NodeID
	sourceLocator = firstNonEmptyString(row.SourceLocator, path)
	label = firstNonEmptyString(row.DisplayLabel, row.Title, noteTitle(path, nil))
	endpointTypeName = firstNonEmptyString(typeName, row.TypeName)
	return GraphEndpoint{
		ID:            graphEndpointID(GraphEndpointNote, path),
		Ref:           ref,
		Kind:          GraphEndpointNote,
		Path:          path,
		NotePath:      path,
		NodeID:        nodeID,
		TypeName:      endpointTypeName,
		Label:         label,
		SourceLocator: sourceLocator,
	}
}

func nodeRefFromGraphNodeRow(row readmodel.GraphNodeRow) ontology.NodeRef {
	if strings.TrimSpace(row.NodeRefJSON) != "" {
		var ref ontology.NodeRef
		if err := json.Unmarshal([]byte(row.NodeRefJSON), &ref); err == nil && !ref.IsZero() {
			return ref
		}
	}
	ref := ontology.NodeRef{
		NotePath: row.NotePath,
		NodeID:   row.NodeID,
		TypeName: row.TypeName,
		Kind:     ontology.NodeKind(row.NodeKind),
	}
	if ref.Kind == "" {
		ref.Kind = ontology.NodeKindNote
	}
	if strings.Contains(row.SourceLocator, "#") {
		parts := strings.SplitN(row.SourceLocator, "#", 2)
		if strings.TrimSpace(parts[0]) != "" {
			ref.NotePath = strings.TrimSpace(parts[0])
		}
		ref.Fragment = strings.TrimSpace(parts[1])
	}
	if ref.Fragment == "" && row.Fragment != "" {
		ref.Fragment = strings.TrimSpace(row.Fragment)
	}
	if strings.TrimSpace(row.BlockID) != "" {
		ref.Fragment = "^" + strings.TrimPrefix(strings.TrimSpace(row.BlockID), "^")
	}
	return ref
}

func graphPreferredSourceLocator(row readmodel.GraphNodeRow) string {
	if strings.TrimSpace(row.NotePath) != "" && strings.TrimSpace(row.BlockID) != "" {
		return strings.TrimSpace(row.NotePath) + "#^" + strings.TrimPrefix(strings.TrimSpace(row.BlockID), "^")
	}
	if strings.TrimSpace(row.NotePath) != "" && strings.TrimSpace(row.Fragment) != "" {
		return strings.TrimSpace(row.NotePath) + "#" + strings.TrimPrefix(strings.TrimSpace(row.Fragment), "#")
	}
	return strings.TrimSpace(row.SourceLocator)
}

func graphEndpointFromPath(kind GraphEndpointKind, path string) GraphEndpoint {
	ref := ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote}
	notePath := path
	if kind == GraphEndpointCode {
		ref = ontology.NodeRef{}
		notePath = ""
	}
	return GraphEndpoint{
		ID:            graphEndpointID(kind, path),
		Ref:           ref,
		Kind:          kind,
		Path:          path,
		NotePath:      notePath,
		Label:         noteTitle(path, nil),
		SourceLocator: path,
	}
}

func graphEndpointID(kind GraphEndpointKind, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	return string(kind) + ":" + key
}

func addGraphEndpoint(nodes map[string]GraphEndpoint, node GraphEndpoint) {
	if node.ID == "" {
		return
	}
	if existing, ok := nodes[node.ID]; ok {
		if existing.TypeName == "" {
			existing.TypeName = node.TypeName
		}
		if existing.NodeID == "" {
			existing.NodeID = node.NodeID
		}
		if existing.SourceLocator == "" {
			existing.SourceLocator = node.SourceLocator
		}
		if existing.Ref.IsZero() {
			existing.Ref = node.Ref
		}
		nodes[node.ID] = existing
		return
	}
	nodes[node.ID] = node
}

func addGraphEdge(edges map[string]GraphReadEdge, edge GraphReadEdge) {
	if edge.Source == "" || edge.Target == "" || edge.Source == edge.Target {
		return
	}
	if edge.Weight <= 0 {
		edge.Weight = 1
	}
	key := graphReadEdgeKey(edge)
	if existing, ok := edges[key]; ok {
		existing.Weight += edge.Weight
		edges[key] = existing
		return
	}
	edges[key] = edge
}

func graphReadEdgeKey(edge GraphReadEdge) string {
	return edge.Source + "|" + edge.Target + "|" + edge.Kind + "|" + edge.RelationName
}

type graphEndpointPair struct {
	first  string
	second string
}

func newGraphEndpointPair(first, second string) graphEndpointPair {
	if first > second {
		first, second = second, first
	}
	return graphEndpointPair{first: first, second: second}
}

func graphDocRowEndpoint(path, kind string) (GraphEndpointKind, string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ""
	}
	switch GraphEndpointKind(kind) {
	case GraphEndpointNote, GraphEndpointCode:
		return GraphEndpointKind(kind), path
	default:
		return "", ""
	}
}

func graphPathHasAnyPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/") {
			return true
		}
	}
	return false
}

func sortedGraphEndpoints(nodes map[string]GraphEndpoint, limit int) []GraphEndpoint {
	out := make([]GraphEndpoint, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func sortedGraphEdges(edges map[string]GraphReadEdge, limit int) []GraphReadEdge {
	out := make([]GraphReadEdge, 0, len(edges))
	for _, edge := range edges {
		out = append(out, edge)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].RelationName < out[j].RelationName
	})
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func relationLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var out []rune
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out = append(out, ' ')
		}
		if i == 0 && r >= 'a' && r <= 'z' {
			r = r - 'a' + 'A'
		}
		out = append(out, r)
	}
	return string(out)
}
