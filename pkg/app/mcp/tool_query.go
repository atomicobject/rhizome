package mcp

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/mark3labs/mcp-go/mcp"
)

// ListTagsTool implements the list_tags MCP tool.
func ListTagsTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if config.Debug {
			log.Printf("MCP list_tags called")
		}

		args := request.GetArguments()
		inputs, err := parseMatchPatterns(args["match"])
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		var scanNotes []string
		if len(inputs) > 0 {
			parsed, expr, err := actions.ParseInputsWithExpression(inputs)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error parsing inputs: %s", err)), nil
			}
			note := withProjectedNoteFacts(ctx, config, resolveNoteReader(config))
			matchingFiles, err := actions.ListFiles(config.Vault, note, actions.ListParams{
				Inputs:                parsed,
				Expression:            expr,
				MaxDepth:              0,
				SkipAnchors:           false,
				SkipEmbeds:            false,
				AbsolutePaths:         false,
				SuppressedTags:        []string{},
				SessionStore:          config.GetIntelStore(),
				MetadataStoreFallback: metadataStoreFallbackPolicy(config),
			})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error filtering files: %s", err)), nil
			}
			scanNotes = matchingFiles
		}

		note := withProjectedNoteFacts(ctx, config, resolveNoteReader(config))
		tagSummaries, err := actions.Tags(config.Vault, note, actions.TagsOptions{Notes: scanNotes})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error listing tags: %s", err)), nil
		}

		payload := TagListResponse{Tags: tagSummaries}
		return respondJSON(payload, "Error marshaling tag list")
	}
}

// ListPropertiesTool implements the list_properties MCP tool.
func ListPropertiesTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if config.Debug {
			log.Printf("MCP list_properties called")
		}

		args := request.GetArguments()
		excludeTags, _ := args["excludeTags"].(bool)
		sourceArg, _ := args["source"].(string)
		var source actions.PropertySource
		switch sourceArg {
		case "", "all":
			source = actions.PropertySourceAll
		case "frontmatter":
			source = actions.PropertySourceFrontmatter
		case "inline":
			source = actions.PropertySourceInline
		default:
			return mcp.NewToolResultError(fmt.Sprintf("invalid source value %q: must be all, frontmatter, or inline", sourceArg)), nil
		}
		inputs, err := parseMatchPatterns(args["match"])
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		onlyProps := make([]string, 0)
		if raw, ok := args["only"]; ok {
			switch v := raw.(type) {
			case []interface{}:
				for _, item := range v {
					if s, ok := item.(string); ok {
						onlyProps = append(onlyProps, s)
					}
				}
			case []string:
				onlyProps = v
			case string:
				onlyProps = append(onlyProps, v)
			}
		}

		valueLimit := 25
		valueLimitSet := false
		if v, ok := args["valueLimit"].(float64); ok {
			valueLimit = int(v)
			valueLimitSet = true
		}

		maxValues := 500
		if v, ok := args["maxValues"].(float64); ok {
			maxValues = int(v)
		}
		if maxValues <= 0 {
			maxValues = 500
		}

		includeValueCounts := true
		if v, ok := args["valueCounts"].(bool); ok {
			includeValueCounts = v
		}

		forceEnumMixed := false
		if v, ok := args["verbose"].(bool); ok && v {
			forceEnumMixed = true
			if valueLimit < 50 {
				valueLimit = 50
			}
		}
		if len(onlyProps) > 0 && !valueLimitSet {
			if maxValues > 1 {
				valueLimit = maxValues - 1
			} else {
				valueLimit = maxValues
			}
		}
		if maxValues < valueLimit+1 {
			maxValues = valueLimit + 1
		}

		var scanNotes []string
		if len(inputs) > 0 {
			parsed, expr, err := actions.ParseInputsWithExpression(inputs)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error parsing inputs: %s", err)), nil
			}
			note := resolveNoteReader(config)
			matchingFiles, err := actions.ListFiles(config.Vault, note, actions.ListParams{
				Inputs:                parsed,
				Expression:            expr,
				MaxDepth:              0,
				SkipAnchors:           false,
				SkipEmbeds:            false,
				AbsolutePaths:         false,
				SuppressedTags:        []string{},
				SessionStore:          config.GetIntelStore(),
				MetadataStoreFallback: metadataStoreFallbackPolicy(config),
			})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error filtering files: %s", err)), nil
			}
			scanNotes = matchingFiles
		}

		note := resolveNoteReader(config)
		summaries, err := actions.Properties(config.Vault, note, actions.PropertiesOptions{
			ExcludeTags:           excludeTags,
			Source:                source,
			ValueLimit:            valueLimit,
			MaxValues:             maxValues,
			Notes:                 scanNotes,
			Only:                  onlyProps,
			ForceEnumMixed:        forceEnumMixed,
			IncludeValueCounts:    includeValueCounts,
			SessionStore:          config.GetIntelStore(),
			MetadataStoreFallback: metadataStoreFallbackPolicy(config),
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error listing properties: %s", err)), nil
		}

		payload := PropertyListResponse{Properties: summaries}
		return respondJSON(payload, "Error marshaling property list")
	}
}

// CapabilitiesTool implements the capabilities MCP tool.
func CapabilitiesTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ready := true
		if config.Runtime != nil {
			ready = config.Runtime.Snapshot().Semantic.Ready
		}

		noteEmbOn, _, _, _ := config.NoteEmbeddings()
		codeEmbOn, _, _ := config.CodeEmbeddingsState()
		codeAnchorOn, svc := config.CodeAnchorState()

		langs := []string{}
		if svc != nil {
			for _, lang := range svc.SupportedLangs() {
				langs = append(langs, string(lang))
			}
			sort.Strings(langs)
		}

		codeIndex := false
		if config.Runtime != nil {
			codeIndex = config.Runtime.Snapshot().IntelStore != nil
		} else if config.GetIntelStore() != nil {
			codeIndex = true
		} else if store, cleanup, _ := obsidian.OpenIntelStoreBestEffort(config.VaultPath, true); store != nil {
			codeIndex = true
			if cleanup != nil {
				cleanup()
			}
		}

		if config.ToolInventory == nil {
			return mcp.NewToolResultError("tool inventory unavailable"), nil
		}
		available := config.ToolInventory.AvailableToolNames(config.ReadWrite)
		mutating := config.ToolInventory.MutatingToolNames(config.ReadWrite)

		vaultName := ""
		if config.Vault != nil {
			vaultName = config.Vault.Name
		}

		resp := CapabilitiesResponse{
			Server: CapabilitiesServer{
				Version:   version.Version,
				ReadWrite: config.ReadWrite,
				Ready:     ready,
			},
			Vault: CapabilitiesVault{
				Name:         vaultName,
				Path:         config.VaultPath,
				IsCollection: config.VaultDef.IsCollection(),
			},
			Tools: CapabilitiesTools{
				Available: available,
				Mutating:  mutating,
			},
			Features: CapabilitiesFeatures{
				NoteEmbeddings: noteEmbOn,
				CodeEmbeddings: codeEmbOn,
				CodeAnchors:    codeAnchorOn,
				CodeIndex:      codeIndex,
			},
			Langs: langs,
			Limits: CapabilitiesLimits{
				BudgetChars: config.BudgetChars(),
			},
			Reports: CapabilitiesReports{
				Ops: []string{"doc_coverage", "complexity", "hotspots", "rationale_attention", "relatedness", "code_similarity"},
			},
		}

		return respondJSON(resp, "Error marshaling capabilities")
	}
}

// ReportTool implements the report MCP tool.
func ReportTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		if managedIndexUnavailable(config) {
			return managedIndexUnavailableResult(config, "report"), nil
		}
		op, _ := args["op"].(string)
		op = strings.TrimSpace(op)
		if op == "" {
			return mcp.NewToolResultError("op is required"), nil
		}

		var pathsArg []string
		if raw, ok := args["paths"]; ok {
			var errMsg string
			pathsArg, errMsg = parseStringArray(raw, "paths")
			if errMsg != "" {
				return mcp.NewToolResultError(errMsg), nil
			}
		}

		limit := 0
		if v, ok := args["limit"].(float64); ok {
			limit = int(v)
		}
		minLines := 0
		if v, ok := args["minLines"].(float64); ok {
			minLines = int(v)
		}
		includeTests, _ := args["includeTests"].(bool)

		store := config.GetIntelStore()
		var cleanup func()
		if store == nil && config.IntelStorePolicy == IntelStoreFallbackAllowed {
			var err error
			store, cleanup, err = obsidian.OpenIntelStoreBestEffort(config.VaultPath, true)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("report open failed: %s", err)), nil
			}
		}
		if cleanup != nil {
			defer cleanup()
		}
		if store == nil {
			return mcp.NewToolResultError("code index unavailable (run `rzm index`)"), nil
		}

		normalizedPaths := normalizeReportPaths(config.VaultPath, pathsArg)
		vaultPaths, _ := paths.NewVaultPaths(config.VaultPath)

		var warnings []string
		var data interface{}

		switch op {
		case "doc_coverage", "doc-coverage":
			rows, err := store.DocCoverage(ctx, semdb.DocCoverageOptions{
				Limit:        limit,
				PathPrefixes: normalizedPaths,
			})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("doc_coverage failed: %s", err)), nil
			}
			out := make([]DocCoverageRowPayload, 0, len(rows))
			for _, r := range rows {
				out = append(out, DocCoverageRowPayload{
					Lang:     r.Lang,
					Kind:     r.Kind,
					FQN:      r.FQN,
					Path:     relCodePath(vaultPaths, r.Path),
					Calls:    r.Calls,
					Callers:  r.Callers,
					Mentions: r.Mentions,
					Links:    r.Links,
					Resolved: r.Resolved,
				})
			}
			if len(out) == 0 {
				warnings = append(warnings, "No call data available (index code first with `rzm index`).")
			}
			data = out
		case "complexity":
			rows, err := store.ComplexityHotspots(ctx, semdb.ComplexityOptions{
				Limit:        limit,
				PathPrefixes: normalizedPaths,
				MinLines:     minLines,
			})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("complexity failed: %s", err)), nil
			}
			out := make([]ComplexityRowPayload, 0, len(rows))
			for _, r := range rows {
				out = append(out, ComplexityRowPayload{
					Lang:      r.Lang,
					Kind:      r.Kind,
					FQN:       r.FQN,
					Path:      relCodePath(vaultPaths, r.Path),
					SpanLines: r.SpanLines,
					CallsIn:   r.CallsIn,
					Callers:   r.Callers,
					CallsOut:  r.CallsOut,
					Callees:   r.Callees,
					Score:     r.Score,
				})
			}
			if len(out) == 0 {
				warnings = append(warnings, "No complexity data available (index code first with `rzm index`).")
			}
			data = out
		case "rationale_attention", "rationale-attention":
			rows, err := store.RationaleAttention(ctx, semdb.RationaleAttentionOptions{
				Limit:        limit,
				PathPrefixes: normalizedPaths,
			})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("rationale_attention failed: %s", err)), nil
			}
			out := make([]RationaleAttentionRowPayload, 0, len(rows))
			for _, r := range rows {
				out = append(out, RationaleAttentionRowPayload{
					Path:      relCodePath(vaultPaths, r.Path),
					SymbolFQN: r.SymbolFQN,
					Kind:      r.Kind,
					Line:      r.Line,
					Content:   r.Content,
					Calls:     r.Calls,
					Callers:   r.Callers,
					Mentions:  r.Mentions,
					Links:     r.Links,
					Attention: r.Attention,
					Reasons:   r.Reasons,
				})
			}
			if len(out) == 0 {
				warnings = append(warnings, "No rationale attention findings available (index code first with `rzm index`).")
			}
			data = out
		case "hotspots":
			pkgs, err := store.HotspotPackages(ctx, semdb.HotspotPackagesOptions{Limit: limit})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("hotspots failed: %s", err)), nil
			}
			files, err := store.HotspotFiles(ctx, semdb.HotspotFilesOptions{Limit: limit})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("hotspots failed: %s", err)), nil
			}
			pkgOut := make([]HotspotPackageRowPayload, 0, len(pkgs))
			for _, p := range pkgs {
				pkgOut = append(pkgOut, HotspotPackageRowPayload{
					Lang:              p.Lang,
					Pkg:               p.Pkg,
					Calls:             p.Calls,
					Callers:           p.Callers,
					UsedSymbols:       p.UsedSymbols,
					DocumentedSymbols: p.DocumentedSymbols,
				})
			}
			fileOut := make([]HotspotFileRowPayload, 0, len(files))
			for _, f := range files {
				fileOut = append(fileOut, HotspotFileRowPayload{
					File:        relCodePath(vaultPaths, f.File),
					Calls:       f.Calls,
					Deps:        f.Deps,
					UsedSymbols: f.UsedSymbols,
				})
			}
			if len(pkgOut) == 0 && len(fileOut) == 0 {
				warnings = append(warnings, "No call data available (index code first with `rzm index`).")
			}
			data = map[string]interface{}{
				"packages": pkgOut,
				"files":    fileOut,
			}
		case "relatedness":
			roots := normalizedPaths
			if len(roots) == 0 {
				roots = []string{"."}
			}
			report, err := actions.CodeRelatedness(ctx, store, actions.CodeRelatednessOptions{
				VaultPath:        config.VaultPath,
				Roots:            roots,
				MaxRelated:       8,
				ClusterThreshold: 3,
				IncludeTests:     includeTests,
				CriticalLimit:    20,
				HotFilesLimit:    5,
			})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("relatedness failed: %s", err)), nil
			}
			data = report
		case "code_similarity", "code-similarity":
			roots := normalizedPaths
			if len(roots) == 0 {
				roots = []string{"."}
			}
			report, err := actions.CodeSimilarity(ctx, store, actions.CodeSimilarityOptions{
				VaultPath: config.VaultPath,
				Roots:     roots,
				Limit:     limit,
			})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("code_similarity failed: %s", err)), nil
			}
			warnings = append(warnings, report.Warnings...)
			data = report
		default:
			return mcp.NewToolResultError("op must be one of doc_coverage, complexity, hotspots, rationale_attention, relatedness, code_similarity"), nil
		}

		resp := ReportResponse{
			Op:          op,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			Data:        data,
			Warnings:    warnings,
		}
		return respondJSON(resp, "Error marshaling report")
	}
}
