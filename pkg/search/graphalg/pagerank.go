// Docs: [Indexing pipeline - Graph signals (doc scores + anchor PageRank)](docs/reference/analysis/Indexing pipeline - Graph signals (doc scores + anchor PageRank).md)
package graphalg

// PageRank computes PageRank over a directed graph (adjacency lists).
//
// Algorithm parameters:
// - Damping factor: 0.85 (standard value; controls teleport probability)
// - Iterations: 30 (fixed; sufficient for convergence on typical graphs)
//
// The algorithm handles dangling nodes (no outgoing edges) by redistributing their rank
// via the teleport mechanism. All nodes receive a base teleport score plus a share of
// dangling mass, ensuring rank conservation.
//
// Returns a map from node ID to PageRank score (not normalized; relative magnitudes matter).
// Used by ComputeAnchorPageRank to assign importance scores to code anchors based on call topology.
func PageRank(adjacency map[string]map[string]struct{}) map[string]float64 {
	const (
		damping    = 0.85
		iterations = 30
	)
	n := len(adjacency)
	if n == 0 {
		return map[string]float64{}
	}
	nodes := make([]string, 0, n)
	for node := range adjacency {
		nodes = append(nodes, node)
	}

	rank := make(map[string]float64, n)
	for _, node := range nodes {
		rank[node] = 1.0 / float64(n)
	}

	for i := 0; i < iterations; i++ {
		newRank := make(map[string]float64, n)
		for _, node := range nodes {
			out := len(adjacency[node])
			share := rank[node]
			if out > 0 {
				share = rank[node] / float64(out)
			}
			for dst := range adjacency[node] {
				newRank[dst] += damping * share
			}
		}
		// Teleport + dangling mass.
		var dangling float64
		for _, node := range nodes {
			if len(adjacency[node]) == 0 {
				dangling += rank[node]
			}
		}
		dangling = damping * dangling / float64(n)
		teleport := (1 - damping) / float64(n)

		for _, node := range nodes {
			newRank[node] += teleport + dangling
		}
		rank = newRank
	}
	return rank
}
