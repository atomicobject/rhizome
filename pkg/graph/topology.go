package graph

import (
	"fmt"

	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/graph/algorithms"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
)

func assignIDs(components [][]string, prefix string) map[string]string {
	ids := make(map[string]string)
	for idx, comp := range components {
		id := fmt.Sprintf("%s%d", prefix, idx)
		for _, node := range comp {
			ids[node] = id
		}
	}
	return ids
}

func weakComponents(adjacency map[string]map[string]struct{}) [][]string {
	visited := make(map[string]bool)
	var comps [][]string

	for node := range adjacency {
		if visited[node] {
			continue
		}
		var queue []string
		queue = append(queue, node)
		visited[node] = true
		var comp []string
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			comp = append(comp, cur)

			for neigh := range adjacency[cur] {
				if !visited[neigh] {
					visited[neigh] = true
					queue = append(queue, neigh)
				}
			}
			for neigh, targets := range adjacency {
				if _, ok := targets[cur]; ok && !visited[neigh] {
					visited[neigh] = true
					queue = append(queue, neigh)
				}
			}
		}
		sort.Strings(comp)
		comps = append(comps, comp)
	}

	sort.Slice(comps, func(i, j int) bool {
		if len(comps[i]) == len(comps[j]) {
			return comps[i][0] < comps[j][0]
		}
		return len(comps[i]) > len(comps[j])
	})
	return comps
}

func computeBridges(adjacency map[string]map[string]struct{}, nodes map[string]GraphNode, comms []CommunitySummary) map[string][]string {
	communityByNode := make(map[string]string)
	for _, c := range comms {
		for _, n := range c.Nodes {
			communityByNode[n] = c.ID
		}
	}

	// Betweenness approx: count cross-community edges.
	bridgeCount := make(map[string]int)
	for src, targets := range adjacency {
		for dst := range targets {
			if communityByNode[src] != "" && communityByNode[src] != communityByNode[dst] {
				bridgeCount[src]++
				bridgeCount[dst]++
			}
		}
	}

	result := make(map[string][]string)
	for _, c := range comms {
		var candidates []string
		for _, n := range c.Nodes {
			if bridgeCount[n] > 0 {
				candidates = append(candidates, n)
			}
		}
		sort.Slice(candidates, func(i, j int) bool {
			if bridgeCount[candidates[i]] == bridgeCount[candidates[j]] {
				return nodes[candidates[i]].Authority > nodes[candidates[j]].Authority
			}
			return bridgeCount[candidates[i]] > bridgeCount[candidates[j]]
		})
		if len(candidates) > 5 {
			candidates = candidates[:5]
		}
		result[c.ID] = candidates
	}
	return result
}

func attachBridges(comms []CommunitySummary, bridges map[string][]string) {
	for i := range comms {
		if bs, ok := bridges[comms[i].ID]; ok {
			comms[i].Bridges = bs
		}
	}
}

// HITSResult contains the hub and authority scores from HITS algorithm.
type HITSResult struct {
	Hubs        map[string]float64
	Authorities map[string]float64
}

// computeHITS computes HITS (Hyperlink-Induced Topic Search) scores.
// Hub score: measures how well a note curates/aggregates links to good authorities.
// Authority score: measures how often a note is referenced by good hubs.
func computeHITS(adjacency map[string]map[string]struct{}) HITSResult {
	res := algorithms.ComputeHITS(adjacency)
	return HITSResult{Hubs: res.Hubs, Authorities: res.Authorities}
}

func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func extractTags(content string) []string {
	tags := make(map[string]struct{})

	frontmatter, err := markdownsyntax.ExtractFrontmatter(content)
	if err == nil && frontmatter != nil {
		if raw, ok := frontmatter["tags"]; ok {
			if slice, ok := raw.([]string); ok {
				for _, t := range slice {
					normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(t), "#"))
					if normalized != "" {
						tags[normalized] = struct{}{}
					}
				}
			}
		}
	}

	for _, ht := range markdownsyntax.ExtractHashtags(content) {
		normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ht), "#"))
		if normalized != "" {
			tags[normalized] = struct{}{}
		}
	}

	if len(tags) == 0 {
		return nil
	}
	return sortedKeys(tags)
}

func filterMutualEdges(adjacency map[string]map[string]struct{}) map[string]map[string]struct{} {
	mutual := make(map[string]map[string]struct{}, len(adjacency))
	for src := range adjacency {
		mutual[src] = make(map[string]struct{})
	}
	for src, targets := range adjacency {
		for dst := range targets {
			if _, ok := adjacency[dst][src]; ok {
				mutual[src][dst] = struct{}{}
			}
		}
	}
	return mutual
}

func filterByMinDegree(adjacency map[string]map[string]struct{}, min int) map[string]map[string]struct{} {
	if min <= 0 {
		return adjacency
	}

	adj := adjacency
	for {
		inDeg := make(map[string]int)
		for src, targets := range adj {
			for dst := range targets {
				inDeg[dst]++
			}
			if _, ok := inDeg[src]; !ok {
				inDeg[src] = 0 // ensure key exists
			}
		}

		var toRemove []string
		for node, targets := range adj {
			deg := len(targets) + inDeg[node]
			if deg < min {
				toRemove = append(toRemove, node)
			}
		}

		if len(toRemove) == 0 {
			break
		}

		for _, n := range toRemove {
			delete(adj, n)
		}
		for _, targets := range adj {
			for dst := range targets {
				if _, ok := adj[dst]; !ok {
					delete(targets, dst)
				}
			}
		}
	}

	return adj
}

// labelPropagation performs synchronous label propagation on an undirected view of the graph.
func labelPropagation(adjacency map[string]map[string]struct{}) map[string]string {
	return algorithms.LabelPropagation(adjacency)
}
