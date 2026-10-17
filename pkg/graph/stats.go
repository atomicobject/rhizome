package graph

import (
	"sort"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ComputeGraphStats scans the vault and returns degree counts and strongly connected components.
func ComputeGraphStats(vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, options markdownsyntax.WikilinkOptions) (*GraphStats, error) {
	snapshot, err := BuildGraphSnapshot(vaultDef, note, GraphAnalysisOptions{WikilinkOptions: options})
	if err != nil {
		return nil, err
	}
	return ComputeGraphStatsFromSnapshot(snapshot, options), nil
}

// ComputeGraphStatsFromSnapshot computes graph statistics from the canonical
// source-neutral graph input.
func ComputeGraphStatsFromSnapshot(snapshot *GraphSnapshot, linkOptions ...markdownsyntax.WikilinkOptions) *GraphStats {
	options := GraphAnalysisOptions{}
	if len(linkOptions) > 0 {
		options.WikilinkOptions = linkOptions[0]
	}
	adjacency, _, _ := graphSnapshotAdjacency(snapshot, options, false)

	nodes := make(map[string]NodeStats, len(adjacency))
	for node := range adjacency {
		nodes[node] = NodeStats{}
	}

	for src, targets := range adjacency {
		stats := nodes[src]
		stats.Outbound = len(targets)
		nodes[src] = stats

		for dst := range targets {
			stats := nodes[dst]
			stats.Inbound++
			nodes[dst] = stats
		}
	}

	components := stronglyConnectedComponents(adjacency)

	return &GraphStats{
		Nodes:      nodes,
		Components: components,
		Adjacency:  adjacency,
	}
}

// Communities returns communities using label propagation over the adjacency map.
func (g *GraphStats) Communities() []Community {
	if g == nil {
		return nil
	}

	labels := labelPropagation(g.Adjacency)

	grouped := make(map[string][]string)
	for node, label := range labels {
		grouped[label] = append(grouped[label], node)
	}

	var result []Community
	for id, nodes := range grouped {
		sort.Strings(nodes)
		result = append(result, Community{ID: id, Nodes: nodes})
	}

	sort.Slice(result, func(i, j int) bool {
		if len(result[i].Nodes) == len(result[j].Nodes) {
			return result[i].Nodes[0] < result[j].Nodes[0]
		}
		return len(result[i].Nodes) > len(result[j].Nodes)
	})

	return result
}

// Orphans returns notes with zero inbound and outbound links.
func (g *GraphStats) Orphans() []string {
	if g == nil {
		return nil
	}
	var orphans []string
	for path, stats := range g.Nodes {
		if stats.Inbound == 0 && stats.Outbound == 0 {
			orphans = append(orphans, path)
		}
	}
	sort.Strings(orphans)
	return orphans
}

func stronglyConnectedComponents(adjacency map[string]map[string]struct{}) [][]string {
	index := 0
	indexMap := make(map[string]int)
	lowlink := make(map[string]int)
	onStack := make(map[string]bool)
	var stack []string
	var components [][]string

	var visit func(v string)
	visit = func(v string) {
		indexMap[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for w := range adjacency[v] {
			if _, seen := indexMap[w]; !seen {
				visit(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] && indexMap[w] < lowlink[v] {
				lowlink[v] = indexMap[w]
			}
		}

		if lowlink[v] == indexMap[v] {
			var component []string
			for {
				n := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[n] = false
				component = append(component, n)
				if n == v {
					break
				}
			}
			sort.Strings(component)
			components = append(components, component)
		}
	}

	var nodes []string
	for node := range adjacency {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)

	for _, node := range nodes {
		if _, visited := indexMap[node]; !visited {
			visit(node)
		}
	}

	sort.Slice(components, func(i, j int) bool {
		if len(components[i]) == len(components[j]) {
			return components[i][0] < components[j][0]
		}
		return len(components[i]) > len(components[j])
	})

	return components
}
