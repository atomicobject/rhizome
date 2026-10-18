package noderead

// limitGraph reserves requested endpoints, then admits relevant edges only
// when both endpoints fit. This keeps every emitted edge endpoint-closed.
func limitGraph(nodes map[string]GraphEndpoint, edges map[string]GraphReadEdge, sourceIDs map[string]struct{}, paths []string, nodeLimit, edgeLimit int) ([]GraphEndpoint, []GraphReadEdge) {
	requested := make(map[string]struct{}, len(sourceIDs)+2*len(paths))
	for id := range sourceIDs {
		requested[id] = struct{}{}
	}
	for _, path := range paths {
		requested[graphEndpointID(GraphEndpointNote, path)] = struct{}{}
		requested[graphEndpointID(GraphEndpointCode, path)] = struct{}{}
	}
	retained := make(map[string]GraphEndpoint)
	orderedNodes := sortedGraphEndpoints(nodes, 0)
	for _, node := range orderedNodes {
		if _, ok := requested[node.ID]; ok && (nodeLimit <= 0 || len(retained) < nodeLimit) {
			retained[node.ID] = node
		}
	}
	selectedEdges := make([]GraphReadEdge, 0)
	for _, edge := range sortedGraphEdges(edges, 0) {
		if edgeLimit > 0 && len(selectedEdges) == edgeLimit {
			break
		}
		source, sourceOK := nodes[edge.Source]
		target, targetOK := nodes[edge.Target]
		if !sourceOK || !targetOK {
			continue
		}
		needed := 0
		if _, ok := retained[edge.Source]; !ok {
			needed++
		}
		if _, ok := retained[edge.Target]; !ok {
			needed++
		}
		if nodeLimit > 0 && len(retained)+needed > nodeLimit {
			continue
		}
		retained[edge.Source] = source
		retained[edge.Target] = target
		selectedEdges = append(selectedEdges, edge)
	}
	for _, node := range orderedNodes {
		if nodeLimit > 0 && len(retained) >= nodeLimit {
			break
		}
		retained[node.ID] = node
	}
	return sortedGraphEndpoints(retained, 0), selectedEdges
}

func graphEdgePriority(edge GraphReadEdge) int {
	switch GraphEdgeKind(edge.Kind) {
	case GraphEdgeKindOntology:
		return 0
	case GraphEdgeKindEmbeds:
		return 1
	default:
		return 2
	}
}
