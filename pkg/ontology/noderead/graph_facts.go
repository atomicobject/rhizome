package noderead

import (
	"context"
	"strconv"
	"strings"
)

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
	for _, node := range graph.Nodes {
		if !req.IncludeEmbedded && (node.Kind == GraphEndpointEmbedded || node.Kind == GraphEndpointSection) {
			continue
		}
		nodesByID[node.ID] = node
	}

	edgesByKey := make(map[string]GraphReadEdge, len(graph.Edges))
	for _, edge := range graph.Edges {
		if graphFactEdgeIncluded(edge, req) {
			edgesByKey[graphReadEdgeKey(edge)] = edge
		}
	}
	nodes, selectedEdges := limitGraph(nodesByID, edgesByKey, graph.sourceIDs, req.Paths, req.NodeLimit, req.EdgeLimit)
	facts := make([]GraphFactEdge, 0, len(selectedEdges))
	for _, edge := range selectedEdges {
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
		b.WriteString(RefIdentityKey(ref))
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
