package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/search/graphalg"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
)

// GraphPathTool finds the shortest path between two notes or code files in the knowledge graph.
func GraphPathTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if managedIndexUnavailable(config) {
			return managedIndexUnavailableResult(config, "graph_path"), nil
		}
		args := request.GetArguments()

		fromInput, _ := args["from"].(string)
		toInput, _ := args["to"].(string)
		if fromInput == "" || toInput == "" {
			return mcp.NewToolResultError("graph_path: 'from' and 'to' are required"), nil
		}

		maxHops := 8
		if v, ok := args["maxHops"].(float64); ok && int(v) > 0 {
			maxHops = int(v)
		}

		store := config.GetIntelStore()
		if store == nil {
			return mcp.NewToolResultError("graph_path: code index not available"), nil
		}

		vaultDef := config.VaultDef
		if vaultDef.Path == "" && config.Vault != nil {
			if def, defErr := config.Vault.Definition(); defErr == nil {
				vaultDef = def
			}
		}
		facts, err := noderead.NewService(vaultDef, &obsidian.Note{}, store, nil).
			NewScope(ctx, noderead.ScopeOptions{}).
			GraphFacts(ctx, noderead.GraphFactsRequest{
				NodeLimit:       1_000_000,
				EdgeLimit:       1_000_000,
				IncludeOntology: true,
				IncludeDocLinks: true,
				IncludeCode:     true,
				IncludeEmbedded: true,
			})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("graph_path: loading graph facts: %s", err)), nil
		}
		minEdges, allPaths, canonicalPath := graphPathInputsFromFacts(facts)

		fromPath, err := graphalg.ResolvePathFuzzy(allPaths, fromInput)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("graph_path: %s", err)), nil
		}
		toPath, err := graphalg.ResolvePathFuzzy(allPaths, toInput)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("graph_path: %s", err)), nil
		}

		fromPath = canonicalGraphPathInput(canonicalPath, fromPath)
		toPath = canonicalGraphPathInput(canonicalPath, toPath)

		result, err := graphalg.ShortestPath(minEdges, fromPath, toPath, maxHops)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("graph_path: %s", err)), nil
		}

		encoded, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("graph_path: marshal error: %s", err)), nil
		}
		return mcp.NewToolResultText(string(encoded)), nil
	}
}

func graphPathInputsFromFacts(facts noderead.GraphFactsResult) ([]graphalg.GraphDocEdgeMinimal, []string, map[string]string) {
	pathSet := make(map[string]struct{}, len(facts.Nodes)*2+len(facts.Edges)*2)
	canonical := make(map[string]string, len(facts.Nodes)*4)
	for _, node := range facts.Nodes {
		if node.Kind != noderead.GraphEndpointNote {
			addGraphPathInputAlias(pathSet, canonical, node.ID, node.ID)
		}
		addGraphPathInputAlias(pathSet, canonical, node.NotePath, node.ID)
		addGraphPathInputAlias(pathSet, canonical, node.Path, node.ID)
		addGraphPathInputAlias(pathSet, canonical, node.SourceLocator, node.ID)
	}
	minEdges := make([]graphalg.GraphDocEdgeMinimal, 0, len(facts.Edges))
	for _, edge := range facts.Edges {
		src := edge.Source
		dst := edge.Target
		if src == "" || dst == "" || src == dst {
			continue
		}
		addGraphPathInputEndpoint(pathSet, canonical, src)
		addGraphPathInputEndpoint(pathSet, canonical, dst)
		minEdges = append(minEdges, graphalg.GraphDocEdgeMinimal{
			SrcPath:         src,
			DstPath:         dst,
			Kind:            graphPathFactKind(edge),
			Confidence:      graphPathFactConfidence(edge),
			ConfidenceScore: edge.Confidence,
		})
	}
	allPaths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		allPaths = append(allPaths, path)
	}
	return minEdges, allPaths, canonical
}

func addGraphPathInputAlias(pathSet map[string]struct{}, canonical map[string]string, path, endpoint string) {
	path = strings.TrimSpace(path)
	if path != "" {
		pathSet[path] = struct{}{}
		canonical[path] = strings.TrimSpace(endpoint)
	}
}

func addGraphPathInputEndpoint(pathSet map[string]struct{}, canonical map[string]string, endpoint string) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return
	}
	canonical[endpoint] = endpoint
	if !strings.HasPrefix(endpoint, string(noderead.GraphEndpointNote)+":") {
		pathSet[endpoint] = struct{}{}
	}
}

func canonicalGraphPathInput(canonical map[string]string, path string) string {
	if endpoint := canonical[strings.TrimSpace(path)]; endpoint != "" {
		return endpoint
	}
	return path
}

func graphPathFactKind(edge noderead.GraphFactEdge) string {
	if edge.RelationName != "" && edge.Kind == "ontology" {
		return edge.RelationName
	}
	return edge.Kind
}

func graphPathFactConfidence(edge noderead.GraphFactEdge) string {
	if edge.Confidence > 0 && edge.Confidence < 1 {
		return semdb.EdgeConfidenceInferred
	}
	return semdb.EdgeConfidenceExtracted
}
