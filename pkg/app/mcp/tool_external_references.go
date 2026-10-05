package mcp

import (
	"context"
	"fmt"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/mark3labs/mcp-go/mcp"
)

// ExternalReferencesTool returns explicit, bounded uses of one pathless target.
func ExternalReferencesTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if config.GetIntelStore() == nil && config.Runtime != nil {
			if err := config.Runtime.WaitForCodeIndex(ctx); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("code index unavailable: %v", err)), nil
			}
		}
		store, cleanup, err := codeToolStore(config)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if cleanup != nil {
			defer cleanup()
		}
		args := request.GetArguments()
		result, err := store.ExternalReferences(ctx, codeanchor.ExternalReferenceQuery{
			Handle:       stringArg(args, "handle"),
			Ecosystem:    codeanchor.ExternalEcosystem(stringArg(args, "ecosystem")),
			Module:       stringArg(args, "module"),
			SymbolPrefix: stringArg(args, "symbolPrefix"),
			Limit:        intArg(args, "limit", 20),
		})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		response := externalReferencesResponse(result)
		return respondJSON(response, "marshal external_references failed")
	}
}

func externalReferencesResponse(result codeanchor.ExternalReferenceQueryResult) ExternalReferencesResponse {
	response := ExternalReferencesResponse{Status: result.Status, Truncated: result.Truncated}
	if result.Target != nil {
		target := externalReferenceTargetPayload(*result.Target)
		response.Target = &target
	}
	for _, target := range result.Candidates {
		response.Candidates = append(response.Candidates, externalReferenceTargetPayload(target))
	}
	response.Calls = externalReferenceUsePayloads(result.Calls)
	response.Types = externalReferenceUsePayloads(result.Types)
	response.Members = externalReferenceUsePayloads(result.Members)
	response.Imports = externalReferenceUsePayloads(result.Imports)
	switch result.Status {
	case "ambiguous":
		response.Warnings = []string{"structured identity matched multiple canonical external targets; pass an exact handle"}
	case "not_found":
		response.Warnings = []string{"no canonical external target matched the exact lookup"}
	}
	return response
}

func externalReferenceTargetPayload(target codeanchor.ExternalReferenceTarget) ExternalReferenceTargetPayload {
	return ExternalReferenceTargetPayload{
		Handle: target.Handle, Ecosystem: string(target.Ecosystem), Module: target.Module,
		SymbolPath: target.SymbolPath, Kind: string(target.Kind), External: target.External,
		Pathless: target.Pathless, Indexed: target.Indexed, SourceBacked: target.SourceBacked, SourceAvailable: target.SourceAvailable,
	}
}

func externalReferenceUsePayloads(uses []codeanchor.ExternalReferenceUse) []ExternalReferenceUsePayload {
	result := make([]ExternalReferenceUsePayload, 0, len(uses))
	for _, use := range uses {
		result = append(result, ExternalReferenceUsePayload{
			OwnerFQN: use.OwnerFQN, Path: use.Path, Evidence: string(use.Evidence), Confidence: string(use.Confidence),
			ImportedName: use.ImportedName, LocalName: use.LocalName, ManifestPath: use.ManifestPath,
			DeclaredRange: use.DeclaredRange, VersionScope: string(use.VersionScope),
		})
	}
	return result
}
