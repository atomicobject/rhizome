package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"strings"
)

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
	graphStore := s.service.Store
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
	})
	if err != nil {
		return GraphResult{}, err
	}
	ontologyEdges, err := graphStore.GraphOntologyEdges(ctx, readmodel.GraphEdgeQuery{
		Paths:             paths,
		PathPrefixes:      req.PathPrefixes,
		EndpointSelectors: endpointSelectors,
		IncludeAmbient:    true,
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
		docEdges, err := graphDocEdges(ctx, graphStore, docPaths, req.PathPrefixes, 0, options.includeCode, options.includeCodeEdges)
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
	selectedNodes, selectedEdges := limitGraph(nodes, edges, sourceEndpointIDs, req.Paths, req.NodeLimit, req.EdgeLimit)
	result := GraphResult{Nodes: selectedNodes, Edges: selectedEdges, Diagnostics: diagnostics.resultPtr(), sourceIDs: sourceEndpointIDs}
	s.mu.Lock()
	if s.cacheGeneration == generation {
		s.graphByKey[cacheKey] = cloneGraphResult(result)
	}
	s.mu.Unlock()
	return result, nil
}
