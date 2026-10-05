package mcp

import (
	"context"
	"fmt"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
)

// VaultHealthTool implements the vault_health MCP tool.
func VaultHealthTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()

		// Parse staleDays (default 90)
		staleDays := 90
		if v, ok := args["staleDays"].(float64); ok {
			staleDays = int(v)
		}

		// Parse include filter
		var include []string
		if incRaw, ok := args["include"].([]interface{}); ok {
			for _, v := range incRaw {
				if s, ok := v.(string); ok {
					include = append(include, s)
				}
			}
		}

		// Parse skip options
		skipAnchors, _ := args["skipAnchors"].(bool)
		skipEmbeds, _ := args["skipEmbeds"].(bool)
		includeImages, _ := args["includeImages"].(bool)

		// Parse limit (default 25)
		limit := 25
		if v, ok := args["limit"].(float64); ok {
			limit = int(v)
		}

		note := resolveNoteReader(config)

		params := actions.HealthParams{
			StaleDays:             staleDays,
			Include:               include,
			IncludeImages:         includeImages,
			SessionStore:          config.GetIntelStore(),
			NoteMetadata:          config.NoteMetadata,
			MetadataStoreFallback: metadataStoreFallbackPolicy(config),
			WikilinkOptions: obsidian.WikilinkOptions{
				SkipAnchors: skipAnchors,
				SkipEmbeds:  skipEmbeds,
			},
		}

		report, err := actions.VaultHealth(config.Vault, note, params)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error analyzing vault health: %s", err)), nil
		}

		// Compute code ref statistics if available
		notesWithCodeRef := 0
		trulyOrphanCount := 0
		if config.Cache != nil {
			codeRefsByNote := config.Cache.CodeRefsByNote()
			if len(codeRefsByNote) > 0 {
				notesWithCodeRef = len(codeRefsByNote)

				// Count truly orphaned notes (graph orphans with no code refs)
				// We need to get backlinks for all dead ends to determine true orphans
				note := resolveNoteReader(config)
				allNotes, _ := note.GetNotesList(config.VaultDef)
				var aliasesByPath map[string][]string
				if provider, ok := note.(obsidian.NoteEntriesProvider); ok {
					if entries, snapErr := provider.NoteEntriesSnapshot(context.Background()); snapErr == nil {
						aliasesByPath = obsidian.AliasesFromNoteEntries(entries)
					}
				}
				noteCache := obsidian.BuildNotePathCacheWithAliases(allNotes, aliasesByPath)

				// Get graph stats to find orphans
				stats, _ := obsidian.ComputeGraphStats(config.VaultDef, note, obsidian.DefaultWikilinkOptions)
				if stats != nil {
					for _, orphan := range stats.Orphans() {
						normalized := markdownGraphNotePath(orphan)
						// Check for wikilink resolution
						resolved, _ := noteCache.ResolveNote(orphan)
						if resolved != "" {
							normalized = markdownGraphNotePath(resolved)
						}
						// If no code refs, it's truly orphaned
						if _, hasCodeRefs := codeRefsByNote[normalized]; !hasCodeRefs {
							trulyOrphanCount++
						}
					}
				}
			}
		}

		// Apply limit to results
		response := VaultHealthResponse{
			Vault:            report.Vault,
			VaultPath:        report.VaultPath,
			AnalyzedAt:       report.AnalyzedAt,
			Stats:            report.Stats,
			NotesWithCodeRef: notesWithCodeRef,
			TrulyOrphanCount: trulyOrphanCount,
		}

		// Apply limit to each category
		if limit > 0 {
			if len(report.BrokenLinks) > limit {
				response.BrokenLinks = report.BrokenLinks[:limit]
			} else {
				response.BrokenLinks = report.BrokenLinks
			}
			if len(report.StaleNotes) > limit {
				response.StaleNotes = report.StaleNotes[:limit]
			} else {
				response.StaleNotes = report.StaleNotes
			}
			if len(report.DeadEnds) > limit {
				response.DeadEnds = report.DeadEnds[:limit]
			} else {
				response.DeadEnds = report.DeadEnds
			}
			if len(report.SuggestedMerges) > limit {
				response.SuggestedMerges = report.SuggestedMerges[:limit]
			} else {
				response.SuggestedMerges = report.SuggestedMerges
			}
			if len(report.SurprisingConnections) > limit {
				response.SurprisingConnections = report.SurprisingConnections[:limit]
			} else {
				response.SurprisingConnections = report.SurprisingConnections
			}
		} else {
			response.BrokenLinks = report.BrokenLinks
			response.StaleNotes = report.StaleNotes
			response.DeadEnds = report.DeadEnds
			response.SuggestedMerges = report.SuggestedMerges
			response.SurprisingConnections = report.SurprisingConnections
		}

		return respondJSON(response, "Error marshaling vault_health result")
	}
}

// markdownGraphNotePath keeps the Markdown compatibility rule at the legacy
// graph boundary. Vault health receives orphans from obsidian.ComputeGraphStats,
// which only scans Markdown links; descriptor-only formats are not graph input.
func markdownGraphNotePath(path string) string {
	return string(paths.NormalizeNote(path))
}
