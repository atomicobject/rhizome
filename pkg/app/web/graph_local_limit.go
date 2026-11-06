package web

// limitLocalGraph reserves the focus and admits neighbours in edge priority order.
func limitLocalGraph(nodes map[string]GraphNode, edges []GraphEdge, center string, limit int) ([]GraphNode, []GraphEdge, int, int) {
	retained := map[string]GraphNode{}
	if node, ok := nodes[center]; ok {
		retained[center] = node
	}
	orderedEdges := sortedEdgeSlice(edges)
	for _, edge := range orderedEdges {
		needed := 0
		if _, ok := retained[edge.Source]; !ok {
			needed++
		}
		if _, ok := retained[edge.Target]; !ok {
			needed++
		}
		if len(retained)+needed > limit {
			continue
		}
		retained[edge.Source] = nodes[edge.Source]
		retained[edge.Target] = nodes[edge.Target]
	}
	for _, node := range sortedNodes(nodes) {
		if len(retained) >= limit {
			break
		}
		retained[node.ID] = node
	}
	outEdges := make([]GraphEdge, 0, len(edges))
	for _, edge := range orderedEdges {
		_, sourceOK := retained[edge.Source]
		_, targetOK := retained[edge.Target]
		if sourceOK && targetOK {
			outEdges = append(outEdges, edge)
		}
	}
	return sortedNodes(retained), outEdges, len(nodes) - len(retained), len(edges) - len(outEdges)
}
