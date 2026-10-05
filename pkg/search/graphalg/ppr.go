// Docs: [Indexing pipeline - Graph signals (doc scores + anchor PageRank)](docs/reference/analysis/Indexing pipeline - Graph signals (doc scores + anchor PageRank).md)
package graphalg

import (
	"math"
	"sort"
	"strings"
)

// PPROptions controls PersonalizedPageRank behavior.
type PPROptions struct {
	// Damping is the probability of following an edge (alpha). Typical: 0.85.
	Damping float64
	// Iterations is the number of power iterations to run. Typical: 10-30.
	Iterations int
}

// DefaultPPROptions returns tuned defaults for query-time diffusion over small graphs.
func DefaultPPROptions() PPROptions {
	return PPROptions{Damping: 0.85, Iterations: 15}
}

// PersonalizedPageRank computes a personalized PageRank (random-walk-with-restarts) score over a directed graph.
// Key Aliases: PPR, Personalize PageRank
//
// The teleport distribution is defined by seeds (node -> weight). Weights are normalized to sum to 1.
//
// Update rule (power iteration):
//
//	r_{t+1} = (1 - d) * p + d * (M * r_t) + d * dangling_mass * p
//
// Where:
// - d is damping (follow-edge probability)
// - p is the teleport distribution over seed nodes
// - M is the row-normalized transition matrix implied by adjacency (outgoing neighbors)
// - dangling_mass is the total probability mass on nodes with no outgoing edges
//
// Returns a map from node id to score. Scores sum to ~1 (floating error) and are comparable within a run.
//
// Notes:
// - Nodes present in seeds but missing from adjacency are ignored.
// - If seeds are empty (after normalization/filtering), returns empty scores.
func PersonalizedPageRank(adjacency map[string]map[string]float64, seeds map[string]float64, opts PPROptions) map[string]float64 {
	if len(adjacency) == 0 || len(seeds) == 0 {
		return map[string]float64{}
	}
	if opts == (PPROptions{}) {
		opts = DefaultPPROptions()
	}
	d := opts.Damping
	if d <= 0 || d >= 1 || math.IsNaN(d) {
		d = 0.85
	}
	iters := opts.Iterations
	if iters <= 0 {
		iters = 15
	}

	nodes := make([]string, 0, len(adjacency))
	for n := range adjacency {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	// Normalize seeds into teleport distribution p.
	p := make(map[string]float64, len(seeds))
	var sum float64
	for node, w := range seeds {
		node = strings.TrimSpace(node)
		if node == "" {
			continue
		}
		if _, ok := adjacency[node]; !ok {
			continue
		}
		if w <= 0 || math.IsNaN(w) || math.IsInf(w, 0) {
			continue
		}
		p[node] += w
		sum += w
	}
	if sum <= 0 {
		return map[string]float64{}
	}
	for k, v := range p {
		p[k] = v / sum
	}

	// Initialize rank to teleport distribution (better than uniform for PPR).
	rank := make(map[string]float64, len(adjacency))
	for _, n := range nodes {
		if pv, ok := p[n]; ok {
			rank[n] = pv
		} else {
			rank[n] = 0
		}
	}

	// Precompute outgoing weight sums for normalization.
	outSum := make(map[string]float64, len(adjacency))
	for _, src := range nodes {
		var s float64
		for _, w := range adjacency[src] {
			if w <= 0 || math.IsNaN(w) || math.IsInf(w, 0) {
				continue
			}
			s += w
		}
		outSum[src] = s
	}

	for i := 0; i < iters; i++ {
		newRank := make(map[string]float64, len(adjacency))

		// Distribute rank mass along outgoing edges.
		var dangling float64
		for _, src := range nodes {
			rs := rank[src]
			if rs == 0 {
				continue
			}
			den := outSum[src]
			if den <= 0 {
				dangling += rs
				continue
			}
			for dst, w := range adjacency[src] {
				if w <= 0 || math.IsNaN(w) || math.IsInf(w, 0) {
					continue
				}
				newRank[dst] += d * rs * (w / den)
			}
		}

		// Teleport + distribute dangling mass according to teleport distribution.
		for _, n := range nodes {
			base := (1 - d) * p[n]
			base += d * dangling * p[n]
			newRank[n] += base
		}
		rank = newRank
	}

	return rank
}
