package graphalg

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// AdjEntry is one neighbor in the adjacency map.
type AdjEntry struct {
	Path            string
	EdgeKind        string
	Confidence      string
	ConfidenceScore float64
}

// PathHop is one edge in a path result.
type PathHop struct {
	FromPath        string
	ToPath          string
	EdgeKind        string
	Confidence      string
	ConfidenceScore float64
}

// PathResult is the result of a shortest path query.
type PathResult struct {
	From string    // resolved source path
	To   string    // resolved target path
	Hops int       // number of edges
	Path []PathHop // ordered hops
}

// GraphDocEdgeMinimal is the minimal edge data needed for path-finding.
// It corresponds to graph_doc_edges columns (src_path, dst_path, kind, confidence, confidence_score).
type GraphDocEdgeMinimal struct {
	SrcPath         string
	DstPath         string
	Kind            string
	Confidence      string
	ConfidenceScore float64
}

type weightedPathState struct {
	Path     string
	PrevPath string
	PrevHops int
	Edge     AdjEntry
	Hops     int
	Cost     float64
	PathKey  string
}

const pathStateEpsilon = 1e-9

// ShortestPath finds the best path between fromPath and toPath in the undirected graph.
// Paths are bounded by maxHops, but among admissible routes we prefer lower total
// confidence cost, then fewer hops, then a deterministic lexical path key.
func ShortestPath(edges []GraphDocEdgeMinimal, fromPath, toPath string, maxHops int) (*PathResult, error) {
	if maxHops < 0 {
		return nil, fmt.Errorf("maxHops must be >= 0")
	}
	if len(edges) == 0 {
		return nil, fmt.Errorf("no path found: graph is empty")
	}
	if fromPath == toPath {
		return &PathResult{From: fromPath, To: toPath, Hops: 0, Path: nil}, nil
	}

	// Build undirected adjacency map.
	adj := make(map[string][]AdjEntry, len(edges)*2)
	for _, e := range edges {
		adj[e.SrcPath] = append(adj[e.SrcPath], AdjEntry{
			Path:            e.DstPath,
			EdgeKind:        e.Kind,
			Confidence:      e.Confidence,
			ConfidenceScore: e.ConfidenceScore,
		})
		adj[e.DstPath] = append(adj[e.DstPath], AdjEntry{
			Path:            e.SrcPath,
			EdgeKind:        e.Kind,
			Confidence:      e.Confidence,
			ConfidenceScore: e.ConfidenceScore,
		})
	}

	layers := make([]map[string]weightedPathState, maxHops+1)
	start := weightedPathState{
		Path:    fromPath,
		Hops:    0,
		Cost:    0,
		PathKey: fromPath,
	}
	layers[0] = map[string]weightedPathState{fromPath: start}

	for hops := 0; hops < maxHops; hops++ {
		currentLayer := layers[hops]
		if len(currentLayer) == 0 {
			continue
		}

		nextLayer := layers[hops+1]
		if nextLayer == nil {
			nextLayer = make(map[string]weightedPathState)
			layers[hops+1] = nextLayer
		}

		for _, current := range currentLayer {
			for _, neighbor := range adj[current.Path] {
				next := weightedPathState{
					Path:     neighbor.Path,
					PrevPath: current.Path,
					PrevHops: hops,
					Edge:     neighbor,
					Hops:     hops + 1,
					Cost:     current.Cost + ConfidenceCost(neighbor.Confidence, neighbor.ConfidenceScore),
					PathKey:  current.PathKey + "\x00" + neighbor.Path,
				}
				if existing, ok := nextLayer[next.Path]; !ok || betterWeightedPath(next, existing) {
					nextLayer[next.Path] = next
				}
			}
		}
	}

	var best weightedPathState
	found := false
	for hops := 1; hops <= maxHops; hops++ {
		layer := layers[hops]
		if len(layer) == 0 {
			continue
		}
		if candidate, ok := layer[toPath]; ok {
			if !found || betterWeightedPath(candidate, best) {
				best = candidate
				found = true
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("no path found within %d hops", maxHops)
	}

	return reconstructWeightedPath(layers, fromPath, best), nil
}

// reconstructWeightedPath walks the dynamic-programming layers backwards from the selected end state.
func reconstructWeightedPath(layers []map[string]weightedPathState, fromPath string, end weightedPathState) *PathResult {
	var hops []PathHop
	cur := end
	for cur.Hops > 0 {
		hops = append(hops, PathHop{
			FromPath:        cur.PrevPath,
			ToPath:          cur.Path,
			EdgeKind:        cur.Edge.EdgeKind,
			Confidence:      cur.Edge.Confidence,
			ConfidenceScore: cur.Edge.ConfidenceScore,
		})
		cur = layers[cur.PrevHops][cur.PrevPath]
	}
	// Reverse hops to get from→to order.
	for i, j := 0, len(hops)-1; i < j; i, j = i+1, j-1 {
		hops[i], hops[j] = hops[j], hops[i]
	}
	return &PathResult{
		From: fromPath,
		To:   end.Path,
		Hops: len(hops),
		Path: hops,
	}
}

func betterWeightedPath(a, b weightedPathState) bool {
	if a.Cost < b.Cost-pathStateEpsilon {
		return true
	}
	if a.Cost > b.Cost+pathStateEpsilon {
		return false
	}
	if a.Hops != b.Hops {
		return a.Hops < b.Hops
	}
	return a.PathKey < b.PathKey
}

// ResolvePathFuzzy matches an input string to a path in allPaths using:
//  1. Exact match (case-insensitive)
//  2. Basename match (without extension)
//  3. Substring scoring (terms in input scored against path)
//
// Returns an error if no match or if ambiguous (top 2 have equal score).
func ResolvePathFuzzy(allPaths []string, input string) (string, error) {
	lower := strings.ToLower(input)

	// 1. Exact match (case-insensitive).
	for _, p := range allPaths {
		if strings.ToLower(p) == lower {
			return p, nil
		}
	}

	// 2. Basename match (without extension).
	var basenameMatches []string
	for _, p := range allPaths {
		base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		if strings.ToLower(base) == lower {
			basenameMatches = append(basenameMatches, p)
		}
	}
	if len(basenameMatches) == 1 {
		return basenameMatches[0], nil
	}
	if len(basenameMatches) > 1 {
		sort.Strings(basenameMatches)
		return "", ambiguousPathError(input, basenameMatches)
	}

	// 3. Substring scoring: split input into terms, score each path.
	terms := strings.Fields(lower)
	if len(terms) == 0 {
		return "", fmt.Errorf("no node matching %q found", input)
	}

	type scored struct {
		path  string
		score int
	}
	var candidates []scored
	for _, p := range allPaths {
		lp := strings.ToLower(p)
		s := 0
		for _, t := range terms {
			if strings.Contains(lp, t) {
				s++
			}
		}
		if s > 0 {
			candidates = append(candidates, scored{p, s})
		}
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no node matching %q found", input)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	if len(candidates) > 1 && candidates[0].score == candidates[1].score {
		top := candidates
		if len(top) > 5 {
			top = top[:5]
		}
		paths := make([]string, len(top))
		for i, c := range top {
			paths[i] = c.path
		}
		sort.Strings(paths)
		return "", ambiguousPathError(input, paths)
	}

	return candidates[0].path, nil
}

func ambiguousPathError(input string, paths []string) error {
	return fmt.Errorf("ambiguous match for %q: did you mean one of?\n  %s", input, strings.Join(paths, "\n  "))
}
