package obsidian

// RefreshGraphAnalysisDerived recomputes fields derived from node authority/community membership
// (community summaries, anchors, buckets, and bridge hints) after callers modify node scores.
//
// This is intentionally a best-effort refresh: it does not rerun HITS or label propagation.
// It uses GraphNode.Community labels and GraphNode.Neighbors edges already present in the analysis.
func RefreshGraphAnalysisDerived(analysis *GraphAnalysis) {
	if analysis == nil || len(analysis.Nodes) == 0 {
		return
	}

	// Rebuild adjacency from node neighbors.
	adjacency := make(map[string]map[string]struct{}, len(analysis.Nodes))
	labels := make(map[string]string, len(analysis.Nodes))
	tagMap := make(map[string][]string)

	for path, node := range analysis.Nodes {
		labels[path] = node.Community
		if len(node.Tags) > 0 {
			tagMap[path] = node.Tags
		}
		dest := make(map[string]struct{}, len(node.Neighbors))
		for _, n := range node.Neighbors {
			if _, ok := analysis.Nodes[n]; ok {
				dest[n] = struct{}{}
			}
		}
		adjacency[path] = dest
	}

	communities := summarizeCommunities(labels, analysis.Nodes, tagMap, analysis.EffectiveTimes)
	bridges := computeBridges(adjacency, analysis.Nodes, communities)
	attachBridges(communities, bridges)
	analysis.Communities = communities
}
