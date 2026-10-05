package graphalg

import "sort"

// edgeBetweennessFallback returns top N edges by betweenness centrality.
// Uses a BFS-based Brandes algorithm. Limits to 5000 nodes and samples up to
// 500 source nodes on large graphs to bound runtime. Scores are normalized for
// an undirected graph, so stored edge orientation does not affect ranking.
//
// This is the fallback mode for FindSurprisingConnections when no community
// scores are available, using structural bridge detection instead.
func edgeBetweennessFallback(edges []GraphDocEdgeForSurprise, topN int) []SurprisingConnection {
	const maxNodes = 5000
	const maxSources = 500

	// Build node index and adjacency list.
	nodeIdx := make(map[string]int)
	var nodes []string
	addNode := func(p string) int {
		if i, ok := nodeIdx[p]; ok {
			return i
		}
		i := len(nodes)
		nodeIdx[p] = i
		nodes = append(nodes, p)
		return i
	}

	type adjEntry struct {
		to    int
		edgeI int // index into edges slice
	}
	adj := make([][]adjEntry, 0)

	// Deduplicate edges by (u,v) using the first occurrence for edge index.
	type edgeKey struct{ u, v int }
	seenEdge := make(map[edgeKey]int) // key -> index into edges

	ensureAdj := func(n int) {
		for len(adj) <= n {
			adj = append(adj, nil)
		}
	}

	for i, e := range edges {
		u := addNode(e.SrcPath)
		v := addNode(e.DstPath)
		ensureAdj(u)
		ensureAdj(v)

		k1 := edgeKey{u, v}
		k2 := edgeKey{v, u}
		if _, exists := seenEdge[k1]; !exists {
			seenEdge[k1] = i
			seenEdge[k2] = i
			adj[u] = append(adj[u], adjEntry{v, i})
			adj[v] = append(adj[v], adjEntry{u, i})
		}
	}

	n := len(nodes)
	if n == 0 {
		return nil
	}
	if n > maxNodes {
		n = maxNodes
	}

	betweenness := make([]float64, len(edges))

	// Determine source set.
	sources := make([]int, n)
	for i := range sources {
		sources[i] = i
	}
	if len(sources) > maxSources {
		sources = sources[:maxSources]
	}

	type predecessor struct {
		node  int
		edgeI int
	}

	// Brandes BFS for each source.
	dist := make([]int, len(nodes))
	sigma := make([]float64, len(nodes))
	delta := make([]float64, len(nodes))
	// pred[v] = list of predecessor nodes/edges on shortest paths to v.
	pred := make([][]predecessor, len(nodes))

	for _, s := range sources {
		// Reset.
		for i := range dist {
			dist[i] = -1
			sigma[i] = 0
			delta[i] = 0
			pred[i] = pred[i][:0]
		}
		dist[s] = 0
		sigma[s] = 1

		queue := []int{s}
		stack := []int{}

		for len(queue) > 0 {
			v := queue[0]
			queue = queue[1:]
			stack = append(stack, v)
			for _, nb := range adj[v] {
				w := nb.to
				if dist[w] < 0 {
					dist[w] = dist[v] + 1
					queue = append(queue, w)
				}
				if dist[w] == dist[v]+1 {
					sigma[w] += sigma[v]
					pred[w] = append(pred[w], predecessor{node: v, edgeI: nb.edgeI})
				}
			}
		}

		// Backward accumulation.
		for len(stack) > 0 {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, prev := range pred[w] {
				u := prev.node
				if sigma[w] > 0 {
					contribution := (sigma[u] / sigma[w]) * (1 + delta[w])
					delta[u] += contribution
					betweenness[prev.edgeI] += contribution
				}
			}
		}
	}

	// Undirected graphs count each pair twice across all sources.
	for i := range betweenness {
		betweenness[i] /= 2
	}

	// Normalize by total undirected pairs.
	denom := float64(n) * float64(n-1) / 2
	if denom > 0 {
		for i := range betweenness {
			betweenness[i] /= denom
		}
	}

	// Build sorted results.
	type scored struct {
		edgeI int
		score float64
	}
	var scored2 []scored
	for i, b := range betweenness {
		if b > 0 {
			e := edges[i]
			scored2 = append(scored2, scored{i, b + ConfidenceSurpriseMultiplier(e.Confidence, e.ConfidenceScore)})
		}
	}
	sort.Slice(scored2, func(i, j int) bool {
		if scored2[i].score != scored2[j].score {
			return scored2[i].score > scored2[j].score
		}
		left := edges[scored2[i].edgeI]
		right := edges[scored2[j].edgeI]
		if left.SrcPath != right.SrcPath {
			return left.SrcPath < right.SrcPath
		}
		if left.DstPath != right.DstPath {
			return left.DstPath < right.DstPath
		}
		return left.Kind < right.Kind
	})

	if topN > 0 && len(scored2) > topN {
		scored2 = scored2[:topN]
	}

	result := make([]SurprisingConnection, 0, len(scored2))
	for _, s := range scored2 {
		e := edges[s.edgeI]
		conf := e.Confidence
		if conf == "" {
			conf = "extracted"
		}
		result = append(result, SurprisingConnection{
			SrcPath:    e.SrcPath,
			DstPath:    e.DstPath,
			EdgeKind:   e.Kind,
			Score:      s.score,
			Confidence: conf,
			Reasons:    []string{"bridges graph structure (betweenness centrality)"},
		})
	}
	return result
}
