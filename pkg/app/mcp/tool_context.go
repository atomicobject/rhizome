package mcp

import (
	"context"
	"fmt"
	"strings"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/mark3labs/mcp-go/mcp"
)

// FileContextTool returns graph + docs context for notes and code files.
func FileContextTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return fileContextTool(config)
}

func fileContextTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		tracker, sessionID := resolveSessionTracker(ctx, args, config, true)
		files := extractStringArray(args["files"])
		if len(files) == 0 {
			return mcp.NewToolResultError("files is required (array of paths)"), nil
		}
		excludeNotePaths := extractStringArray(args["exclude_note_paths"])
		excludeDocPaths := extractStringArray(args["exclude_doc_paths"])
		anchorKinds, err := parseAnchorKinds(args["anchorKinds"])
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		profile2 := "auto"
		if v, ok := args["profile"].(string); ok && strings.TrimSpace(v) != "" {
			profile2 = v
		}
		skipAnchors2, _ := args["skipAnchors"].(bool)
		skipEmbeds2, _ := args["skipEmbeds"].(bool)
		intent, _ := args["intent"].(string)
		ensureLinkTargets, _ := args["ensureLinkTargets"].(string)
		ensureMode, err := actions.ParseEnsureLinkTargetMode(ensureLinkTargets)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if ensureMode == ontology.EnsureLinkTargetApply && !config.ReadWrite {
			return mcp.NewToolResultError("ensureLinkTargets=apply requires MCP read-write mode"), nil
		}
		submoduleDepth := 0
		if v, ok := args["submoduleDepth"].(float64); ok && int(v) >= 0 {
			submoduleDepth = int(v)
			if submoduleDepth > 3 {
				submoduleDepth = 3
			}
		}

		codeRefsByFile2 := codeRefsByFile(config)
		if config.IndexedReadOnlyFileContext {
			// A nil map tells the CLI action to hydrate only requested-file
			// coderefs from the persisted index. An empty non-nil map means the
			// caller intentionally supplied a live cache snapshot.
			codeRefsByFile2 = nil
		}

		nm2 := withProjectedNoteFacts(ctx, config, resolveNoteReader(config))
		_, codeAnchorSvc := config.CodeAnchorState()
		result, err := actions.BuildFileContextTextResult(config.Vault, nm2, actions.FileContextTextParams{
			Context:                 ctx,
			BudgetChars:             config.BudgetChars(),
			Profile:                 actions.ContextProfile(profile2),
			SkipAnchors:             skipAnchors2,
			SkipEmbeds:              skipEmbeds2,
			AnchorKinds:             anchorKinds,
			Files:                   files,
			SubmoduleDepth:          submoduleDepth,
			ExcludeNotePaths:        excludeNotePaths,
			ExcludeDocPaths:         excludeDocPaths,
			CodeRefsByFile:          codeRefsByFile2,
			CodeAnchor:              codeAnchorSvc,
			Dedupe:                  tracker,
			SessionStore:            config.GetIntelStore(),
			NoteMetadata:            config.NoteMetadata,
			OntologyRuntimeProvider: config.OntologyRuntimeProvider,
			IndexedReadOnly:         config.IndexedReadOnlyFileContext,
			IndexedUnavailable:      config.IndexedContextUnavailable,
			Intent:                  intent,
			EnsureLinkTarget:        ensureMode,
			ApplyLinkTargets:        config.ApplyLinkTargets,
			Compressor:              config.Compressor,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("file_context failed: %s", err)), nil
		}
		if config.IndexedReadOnlyFileContext {
			return respondContextTextResult(sessionID, tracker, result, true)
		}
		return respondContextText(sessionID, tracker, result.Text)
	}
}

// VaultContextTool provides a compact, high-signal snapshot of the vault.
func VaultContextTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		tracker, sessionID := resolveSessionTracker(ctx, args, config, true)
		contextFiles := extractStringArray(args["contextFiles"])
		files := extractStringArray(args["files"])
		requestScope := vaultContextRequestScope(args)

		profile2 := "auto"
		if v, ok := args["profile"].(string); ok && strings.TrimSpace(v) != "" {
			profile2 = v
		}
		skipAnchors2, _ := args["skipAnchors"].(bool)
		skipEmbeds2, _ := args["skipEmbeds"].(bool)
		intent2, _ := args["intent"].(string)
		includeTags2 := true
		includeTagsSet2 := false
		if v, ok := args["includeTags"].(bool); ok {
			includeTags2 = v
			includeTagsSet2 = true
		}
		recencyCascade2 := true
		recencyCascadeSet2 := false
		if v, ok := args["recencyCascade"].(bool); ok {
			recencyCascade2 = v
			recencyCascadeSet2 = true
		}
		graphSummary2, _ := args["graphSummary"].(bool)
		includeOntology2, _ := args["includeOntology"].(bool)

		keyPatterns2 := extractStringArray(args["keyPatterns"])
		submoduleDepth := 0
		if v, ok := args["submoduleDepth"].(float64); ok && int(v) >= 0 {
			submoduleDepth = int(v)
			if submoduleDepth > 3 {
				submoduleDepth = 3
			}
		}
		codeRefsByNote2 := codeRefsByNote(config)

		nm2 := withProjectedNoteFacts(ctx, config, resolveNoteReader(config))
		codeRefsByFile2 := codeRefsByFile(config)
		_, codeAnchorSvc := config.CodeAnchorState()
		result, err := actions.BuildVaultContextTextResult(config.Vault, nm2, actions.VaultContextTextParams{
			Context:                 ctx,
			BudgetChars:             config.BudgetChars(),
			Profile:                 actions.ContextProfile(profile2),
			RequestScope:            requestScope,
			RequireOntology:         includeOntology2 || strings.EqualFold(profile2, string(actions.ContextProfileVault)),
			IndexedUnavailable:      config.IndexedContextUnavailable,
			SkipAnchors:             skipAnchors2,
			SkipEmbeds:              skipEmbeds2,
			IncludeTags:             includeTags2,
			IncludeTagsSet:          includeTagsSet2,
			RecencyCascade:          recencyCascade2,
			RecencyCascadeSet:       recencyCascadeSet2,
			GraphSummary:            graphSummary2,
			ContextFiles:            contextFiles,
			Files:                   files,
			KeyPatterns:             keyPatterns2,
			SubmoduleDepth:          submoduleDepth,
			CodeRefsByNote:          codeRefsByNote2,
			CodeRefsByFile:          codeRefsByFile2,
			CodeAnchor:              codeAnchorSvc,
			Dedupe:                  tracker,
			SessionStore:            config.GetIntelStore(),
			NoteMetadata:            config.NoteMetadata,
			IndexedReadOnly:         config.IntelStorePolicy == IntelStoreManagedReadOnly,
			MetadataStoreFallback:   metadataStoreFallbackPolicy(config),
			OntologyRuntimeProvider: config.OntologyRuntimeProvider,
			Intent:                  intent2,
			Compressor:              config.Compressor,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("vault_context failed: %s", err)), nil
		}
		if requestScope == actions.VaultContextRequestScopeIndexedBootstrap {
			statusMetric := map[actions.IndexedContextState]string{
				actions.IndexedContextAvailable:    indexingperf.AgentStartOpIndexedStatusAvailable,
				actions.IndexedContextStale:        indexingperf.AgentStartOpIndexedStatusStale,
				actions.IndexedContextIncompatible: indexingperf.AgentStartOpIndexedStatusIncompatible,
				actions.IndexedContextMissing:      indexingperf.AgentStartOpIndexedStatusMissing,
			}[result.IndexedStatus]
			if statusMetric != "" {
				indexingperf.AddCount(ctx, statusMetric, 1)
			}
		}
		return respondContextTextResult(sessionID, tracker, result, requestScope == actions.VaultContextRequestScopeIndexedBootstrap)
	}
}

func vaultContextRequestScope(args map[string]any) actions.VaultContextRequestScope {
	if value, ok := args["requestScope"].(string); ok {
		switch actions.VaultContextRequestScope(value) {
		case actions.VaultContextRequestScopeMinimalBootstrap:
			return actions.VaultContextRequestScopeMinimalBootstrap
		case actions.VaultContextRequestScopeIndexedBootstrap:
			return actions.VaultContextRequestScopeIndexedBootstrap
		}
	}
	// Omission deliberately retains the standalone vault_context behavior.
	return actions.VaultContextRequestScopeRich
}
