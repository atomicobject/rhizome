package graphpresentation

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/graph/algorithms"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// PathInputsFromFacts prepares ordered edges and their authored endpoint aliases.
func PathInputsFromFacts(facts noderead.GraphFactsResult) ([]algorithms.GraphDocEdgeMinimal, []string, map[string]string) {
	pathSet := make(map[string]struct{}, len(facts.Nodes)*2+len(facts.Edges)*2)
	canonical := make(map[string]string, len(facts.Nodes)*4)
	for _, node := range facts.Nodes {
		if node.Kind != noderead.GraphEndpointNote {
			addPathAlias(pathSet, canonical, node.ID, node.ID)
		}
		addPathAlias(pathSet, canonical, node.NotePath, node.ID)
		addPathAlias(pathSet, canonical, node.Path, node.ID)
		addPathAlias(pathSet, canonical, node.SourceLocator, node.ID)
	}
	minEdges := make([]algorithms.GraphDocEdgeMinimal, 0, len(facts.Edges))
	for _, edge := range facts.Edges {
		src := edge.Source
		dst := edge.Target
		if src == "" || dst == "" || src == dst {
			continue
		}
		addPathEndpoint(pathSet, canonical, src)
		addPathEndpoint(pathSet, canonical, dst)
		minEdges = append(minEdges, algorithms.GraphDocEdgeMinimal{
			SrcPath:         src,
			DstPath:         dst,
			Kind:            FactEdgeKind(edge),
			Confidence:      FactConfidenceLabel(edge),
			ConfidenceScore: edge.Confidence,
		})
	}
	allPaths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		allPaths = append(allPaths, p)
	}
	return minEdges, allPaths, canonical
}

func addPathAlias(pathSet map[string]struct{}, canonical map[string]string, path, endpoint string) {
	path = strings.TrimSpace(path)
	if path != "" {
		pathSet[path] = struct{}{}
		canonical[path] = strings.TrimSpace(endpoint)
	}
}

func addPathEndpoint(pathSet map[string]struct{}, canonical map[string]string, endpoint string) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return
	}
	canonical[endpoint] = endpoint
	if !strings.HasPrefix(endpoint, string(noderead.GraphEndpointNote)+":") {
		pathSet[endpoint] = struct{}{}
	}
}

// CanonicalPath resolves an authored alias to its graph endpoint.
func CanonicalPath(canonical map[string]string, path string) string {
	if endpoint := canonical[strings.TrimSpace(path)]; endpoint != "" {
		return endpoint
	}
	return path
}

// FactEdgeKind uses the named relation for ontology edges.
func FactEdgeKind(edge noderead.GraphFactEdge) string {
	if edge.RelationName != "" && edge.Kind == "ontology" {
		return edge.RelationName
	}
	return edge.Kind
}

// FactConfidenceLabel preserves the graph algorithms' inferred/extracted distinction.
func FactConfidenceLabel(edge noderead.GraphFactEdge) string {
	if edge.Confidence > 0 && edge.Confidence < 1 {
		return semdb.EdgeConfidenceInferred
	}
	return semdb.EdgeConfidenceExtracted
}
