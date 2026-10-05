// Docs: [Indexing pipeline - Graph signals (doc scores + anchor PageRank)](docs/reference/analysis/Indexing pipeline - Graph signals (doc scores + anchor PageRank).md)
package graphalg

import "sort"

// LabelPropagation performs synchronous label propagation on an undirected view of the graph.
//
// Algorithm:
// - Converts directed adjacency to undirected neighbor sets
// - Initializes each node with its own ID as label
// - Runs up to 20 iterations: each node adopts the most frequent label among neighbors
// - Ties broken by lexicographic order (deterministic)
// - Stops early if no labels change in an iteration
//
// The result is a community assignment: nodes with the same label form a community.
// This is useful for discovering clusters of related documents (e.g., notes about the same
// topic that link to each other).
//
// Used by ComputeDocScores to assign community labels to documents, enabling same-community
// boosting in graph-aware ranking.
func LabelPropagation(adjacency map[string]map[string]struct{}) map[string]string {
	// Build undirected neighbor sets.
	neighbors := make(map[string]map[string]struct{}, len(adjacency))
	for src, targets := range adjacency {
		if _, ok := neighbors[src]; !ok {
			neighbors[src] = make(map[string]struct{})
		}
		for dst := range targets {
			if src == dst {
				continue
			}
			neighbors[src][dst] = struct{}{}
			if _, ok := neighbors[dst]; !ok {
				neighbors[dst] = make(map[string]struct{})
			}
			neighbors[dst][src] = struct{}{}
		}
	}
	for node := range adjacency {
		if _, ok := neighbors[node]; !ok {
			neighbors[node] = make(map[string]struct{})
		}
	}

	labels := make(map[string]string, len(neighbors))
	for node := range neighbors {
		labels[node] = node
	}

	nodes := make([]string, 0, len(neighbors))
	for node := range neighbors {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	const maxIter = 20
	for iter := 0; iter < maxIter; iter++ {
		changed := false
		for _, node := range nodes {
			counts := make(map[string]int)
			for neigh := range neighbors[node] {
				counts[labels[neigh]]++
			}
			if len(counts) == 0 {
				continue
			}
			var bestLabel string
			bestCount := -1
			for label, count := range counts {
				if count > bestCount || (count == bestCount && label < bestLabel) {
					bestLabel = label
					bestCount = count
				}
			}
			if bestLabel != "" && bestLabel != labels[node] {
				labels[node] = bestLabel
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	return labels
}
