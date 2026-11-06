// Package graphpresentation owns the wire graph and its shared pure transforms.
package graphpresentation

import (
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"strings"
)

const ModuleCollapseThreshold = 250

// ProjectNode retains graph identity; callers choose display defaults and grouping.
func ProjectNode(node noderead.GraphEndpoint) GraphNode {
	result := GraphNode{
		ID: node.ID, Path: node.Path, NotePath: node.NotePath, NodeID: node.NodeID,
		Label: node.Label, Kind: string(node.Kind), SourceLocator: node.SourceLocator,
		ResolvedType: node.TypeName,
	}
	if !node.Ref.IsZero() {
		ref := node.Ref
		result.NodeRef = &ref
	}
	return result
}

// ProjectEdges drops edges whose endpoints were filtered out.
func ProjectEdges(nodes map[string]GraphNode, edges []noderead.GraphReadEdge) []GraphEdge {
	result := make([]GraphEdge, 0, len(edges))
	for _, edge := range edges {
		if _, ok := nodes[edge.Source]; !ok {
			continue
		}
		if _, ok := nodes[edge.Target]; !ok {
			continue
		}
		result = append(result, GraphEdge{
			Source: edge.Source, Target: edge.Target, Kind: edge.Kind, Weight: edge.Weight,
			RelationName: edge.RelationName, RelationLabel: edge.RelationLabel,
			Provenance: edge.Provenance, Structural: edge.Structural,
		})
	}
	return result
}

// Collapse groups large modules. The caller retains module naming, enrichment,
// and output ordering policy. Aggregated edges retain their kind and weight.
func Collapse(nodes map[string]GraphNode, edges []GraphEdge, limit int, moduleForPath func(string) string) ([]GraphNode, []GraphEdge, bool) {
	if len(nodes) <= limit {
		return nodeList(nodes), edges, false
	}
	byModule := map[string][]GraphNode{}
	for _, node := range nodes {
		byModule[node.Module] = append(byModule[node.Module], node)
	}
	collapsed := map[string]GraphNode{}
	for module, members := range byModule {
		if len(members) < ModuleCollapseThreshold {
			for _, node := range members {
				collapsed[node.ID] = node
			}
			continue
		}
		id := "module:" + module
		collapsed[id] = GraphNode{ID: id, Path: module, Label: module, Kind: "module", Collapsed: true, ChildCount: len(members)}
	}
	endpoint := func(id string) string {
		if _, ok := collapsed[id]; ok {
			return id
		}
		if strings.HasPrefix(id, "module:") {
			return id
		}
		var path string
		switch {
		case strings.HasPrefix(id, "note:"):
			path = strings.TrimPrefix(id, "note:")
		case strings.HasPrefix(id, "code:"):
			path = strings.TrimPrefix(id, "code:")
		default:
			return id
		}
		moduleID := "module:" + moduleForPath(path)
		if _, ok := collapsed[moduleID]; ok {
			return moduleID
		}
		return id
	}
	edgeMap := map[string]*GraphEdge{}
	for _, edge := range edges {
		source, target := endpoint(edge.Source), endpoint(edge.Target)
		key := source + "|" + target + "|" + edge.Kind
		if aggregate, ok := edgeMap[key]; ok {
			aggregate.Weight += edge.Weight
		} else {
			edgeMap[key] = &GraphEdge{Source: source, Target: target, Kind: edge.Kind, Weight: edge.Weight}
		}
	}
	result := make([]GraphEdge, 0, len(edgeMap))
	for _, edge := range edgeMap {
		result = append(result, *edge)
	}
	return nodeList(collapsed), result, true
}

func nodeList(nodes map[string]GraphNode) []GraphNode {
	result := make([]GraphNode, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, node)
	}
	return result
}
