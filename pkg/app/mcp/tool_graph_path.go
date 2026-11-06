package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/app/graphpresentation"
	"github.com/atomicobject/rhizome/pkg/graph/algorithms"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
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
			NewScope(noderead.ScopeOptions{}).
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
		minEdges, allPaths, canonicalPath := graphpresentation.PathInputsFromFacts(facts)

		fromPath, err := algorithms.ResolvePathFuzzy(allPaths, fromInput)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("graph_path: %s", err)), nil
		}
		toPath, err := algorithms.ResolvePathFuzzy(allPaths, toInput)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("graph_path: %s", err)), nil
		}

		fromPath = graphpresentation.CanonicalPath(canonicalPath, fromPath)
		toPath = graphpresentation.CanonicalPath(canonicalPath, toPath)

		result, err := algorithms.ShortestPath(minEdges, fromPath, toPath, maxHops)
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
