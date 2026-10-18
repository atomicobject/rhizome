package operations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func BuildVaultContextText(vault repoconfig.VaultManager, noteMgr obsidian.NoteReader, params VaultContextTextParams) (string, error) {
	result, err := BuildVaultContextTextResult(vault, noteMgr, params)
	return result.Text, err
}

type VaultContextTextResult struct {
	Text           string
	Warnings       []IndexedContextWarning
	IndexedStatus  IndexedContextState
	IndexedReads   int
	IndexedResults int
}

func BuildVaultContextTextResult(vault repoconfig.VaultManager, noteMgr obsidian.NoteReader, params VaultContextTextParams) (VaultContextTextResult, error) {
	if params.RequestScope == VaultContextRequestScopeMinimalBootstrap {
		budget := effectiveContextBudget(params.BudgetChars)
		text, err := buildMinimalBootstrapContext(vault, params, budget)
		return VaultContextTextResult{Text: text}, err
	}
	if params.RequestScope == VaultContextRequestScopeIndexedBootstrap {
		budget := effectiveContextBudget(params.BudgetChars)
		return buildIndexedBootstrapContext(vault, params, budget)
	}
	text, err := buildVaultContextTextRich(vault, noteMgr, params)
	return VaultContextTextResult{Text: text}, err
}

func buildVaultContextTextRich(vault repoconfig.VaultManager, noteMgr obsidian.NoteReader, params VaultContextTextParams) (string, error) {
	ctx := params.Context
	if ctx == nil {
		ctx = context.Background()
	}
	budget := effectiveContextBudget(params.BudgetChars)
	inputs, err := resolveContextInputs(vault, params.Profile)
	if err != nil {
		return "", err
	}
	vaultDef := inputs.VaultDef
	vaultPath := inputs.VaultPath
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	projectRoot := inputs.ProjectRoot
	fileCfg := inputs.FileCfg
	localCfg := inputs.LocalCfg
	profile := inputs.Profile

	opts := defaultContextGraphOptions(params.SkipAnchors, params.SkipEmbeds)
	if params.IncludeTagsSet {
		opts.IncludeTags = params.IncludeTags
	}
	if params.RecencyCascadeSet {
		opts.RecencyCascade = params.RecencyCascade
		opts.RecencyCascadeSet = true
	}

	needOntology := profile == ContextProfileVault || len(params.ContextFiles) > 0

	var ontoCtx *ontologyTextContext
	var ontoCleanup func()
	var ontologySummary *OntologyVaultSummary
	if needOntology {
		if params.IndexedReadOnly {
			ontoCtx = loadIndexedOntologyTextContext(ctx, vaultDef, noteMgr, params.NoteMetadata, params.SessionStore)
		} else if params.OntologyRuntimeProvider != nil {
			ontoCtx, _ = loadOntologyTextContextFromProvider(ctx, vaultDef, noteMgr, params.OntologyRuntimeProvider)
		} else if params.NoteMetadata.Validate() == nil {
			ontoCtx, ontoCleanup, _ = loadOntologyTextContext(vaultDef, noteMgr, params.NoteMetadata, params.SessionStore)
		}
		if ontoCleanup != nil {
			defer ontoCleanup()
		}
		if profile == ContextProfileVault && ontoCtx != nil {
			ontologySummary, _ = BuildOntologyVaultSummary(context.Background(), vaultDef, noteMgr, ontoCtx.Store, ontoCtx.Schema, ontoCtx.Issues, 3)
		}
	}

	includeGraphSummary := true
	if profile == ContextProfileVault && ontologySummary != nil {
		includeGraphSummary = params.GraphSummary
	}
	needGraph := profile == ContextProfileCode || includeGraphSummary || len(params.ContextFiles) > 0

	var analysis *vaultgraph.GraphAnalysis
	var reverseNeighbors map[string][]string
	if needGraph {
		analysis, err = GraphAnalysis(vault, noteMgr, GraphAnalysisParams{
			UseConfig:             true,
			Options:               opts,
			SessionStore:          params.SessionStore,
			NoteMetadata:          params.NoteMetadata,
			MetadataStoreFallback: params.MetadataStoreFallback,
		})
		if err != nil {
			return "", err
		}
		if len(params.CodeRefsByNote) > 0 {
			ApplyCodeRefAuthorityBoost(analysis, params.CodeRefsByNote)
			vaultgraph.RefreshGraphAnalysisDerived(analysis)
		}
		reverseNeighbors = buildReverseNeighborsForText(analysis.Nodes)
	}

	// Build body pieces under a reserved header budget.
	const headerReserve = 420
	bodyBudget := budget - headerReserve
	if bodyBudget < 800 {
		bodyBudget = budget
	}

	var pieces []contextpack.Piece
	nestedDedupe := make(map[string][]DedupeItem)

	// Stats is always useful.
	statsBlock := renderVaultStatsBlock(analysis)
	if profile == ContextProfileVault && ontologySummary != nil && !includeGraphSummary {
		statsBlock = renderOntologyStatsBlock(ontologySummary)
	}
	pieces = append(pieces, contextpack.Piece{Key: "stats", Priority: 100, Score: 0, Text: statsBlock})

	defaults := DefaultVaultContextSummaryDefaults

	switch profile {
	case ContextProfileCode:
		// Explicit file targets already load root, ancestor, and submodule docs
		// through file_context. Context-file targets intentionally stay note-scoped.
		// In both cases, skip the unrelated repository-wide doc inventory.
		if len(params.Files) == 0 && len(params.ContextFiles) == 0 {
			// Walk deeper when compression is enabled to collect more docs for synthesis.
			docMaxTiers := repoDocMaxTiersDefault
			if params.Compressor != nil {
				docMaxTiers = repoDocMaxTiersWithCompression
			}
			docBlocks, docDedupe, docErr := buildRepoDocBlocks(ctx, projectRoot, fileCfg.DocPatterns, localCfg, vaultDef, params.Dedupe, docMaxTiers)
			if docErr == nil {
				pieces = append(pieces, docBlocks...)
				for key, items := range docDedupe {
					nestedDedupe[key] = append(nestedDedupe[key], items...)
				}
			}
		}

		// Code overview and key notes are skipped for code repos to reduce noise.
		// Use file_context on specific directories for module summaries.
		// Use semantic_query to discover relevant notes contextually.

	case ContextProfileVault:
		if ontologySummary != nil && !includeGraphSummary {
			if block := renderOntologyTypeCountsBlock(ontologySummary, 30); strings.TrimSpace(block) != "" {
				pieces = append(pieces, contextpack.Piece{
					Key:      "ontology-types",
					Priority: 75,
					Score:    0,
					Text:     block,
				})
			}
			if block := renderOntologyIssuesBlock(ontologySummary, 10); strings.TrimSpace(block) != "" {
				pieces = append(pieces, contextpack.Piece{
					Key:      "ontology-issues",
					Priority: 72,
					Score:    0,
					Text:     block,
				})
			}
		} else {
			// Key notes (authority-ranked).
			keyNotes := topAuthorityPaths(analysis.Nodes, defaults.TopGlobalAuthorityLimit)
			if len(keyNotes) > 0 {
				pieces = append(pieces, contextpack.Piece{
					Key:      "key-notes",
					Priority: 75,
					Score:    0,
					Text:     renderKeyNotes(noteMgr, keyNotes),
				})
			}

			// Communities.
			if len(analysis.Communities) > 0 {
				pieces = append(pieces, contextpack.Piece{
					Key:      "communities",
					Priority: 70,
					Score:    0,
					Text:     renderCommunitiesBlock(noteMgr, analysis, defaults),
				})
			}

			// Orphans (top N).
			if len(analysis.Orphans) > 0 {
				orphans := analysis.Orphans
				if len(orphans) > defaults.TopOrphansLimit {
					orphans = orphans[:defaults.TopOrphansLimit]
				}
				pieces = append(pieces, contextpack.Piece{
					Key:      "orphans",
					Priority: 40,
					Score:    0,
					Text:     renderTopList("Top orphans", orphans),
				})
			}
		}

		// MOCs / key note patterns.
		if len(params.KeyPatterns) > 0 {
			mocs, err := findKeyNotesByPatterns(vault, noteMgr, params.KeyPatterns)
			if err == nil && len(mocs) > 0 {
				pieces = append(pieces, contextpack.Piece{
					Key:      "mocs",
					Priority: 50,
					Score:    0,
					Text:     renderMOCs(params.KeyPatterns, mocs),
				})
			}
		}
	}

	// Embedded contextFiles (notes): compact per-note summaries.
	for _, f := range params.ContextFiles {
		norm := string(paths.EnsureNoteExtension(f, ".md"))
		if filepath.IsAbs(f) && vaultPaths.Root() != "" {
			if rel, err := vaultPaths.RelNotePathStrict(paths.EnsureNoteExtension(f, ".md").String()); err == nil && rel.String() != "" {
				norm = rel.String()
			}
		}
		block := renderNoteSummaryBlock(noteMgr, analysis, reverseNeighbors, norm, ontoCtx)
		if analysis == nil {
			block = renderNoteSummaryBlockNoGraph(noteMgr, norm, ontoCtx)
		}
		if strings.TrimSpace(block) == "" {
			continue
		}
		pieces = append(pieces, contextpack.Piece{
			Key:      fmt.Sprintf("note:%s", norm),
			Priority: 80,
			Score:    0,
			Text:     block,
		})
	}

	fileContextText := ""
	fileContextTrimmed := false
	var fileDelivery *contextDelivery
	if len(params.Files) > 0 {
		fileBudget := chooseVaultContextFileBudget(budget, len(params.Files))
		if fileBudget > 0 {
			phaseCtx := indexingperf.WithPhase(ctx, indexingperf.AgentStartPhaseTargetFileContext)
			done := indexingperf.StartSpan(phaseCtx, indexingperf.AgentStartPhaseTargetFileContext)
			fileResult, pending, fileErr := buildFileContextTextResultPending(vault, noteMgr, FileContextTextParams{
				Context:                 ctx,
				BudgetChars:             fileBudget,
				Profile:                 profile,
				IndexedReadOnly:         params.IndexedReadOnly,
				IndexedUnavailable:      params.IndexedUnavailable,
				SkipAnchors:             params.SkipAnchors,
				SkipEmbeds:              params.SkipEmbeds,
				Files:                   params.Files,
				SubmoduleDepth:          params.SubmoduleDepth,
				CodeRefsByFile:          params.CodeRefsByFile,
				CodeAnchor:              params.CodeAnchor,
				Dedupe:                  params.Dedupe,
				SessionStore:            params.SessionStore,
				NoteMetadata:            params.NoteMetadata,
				MetadataStoreFallback:   params.MetadataStoreFallback,
				OntologyRuntimeProvider: params.OntologyRuntimeProvider,
				Intent:                  params.Intent,
				Compressor:              params.Compressor,
			})
			done(fileErr)
			if fileErr != nil {
				return "", fileErr
			}
			fileText := fileResult.Text
			fileDelivery = pending
			defer fileDelivery.release()
			fileContextText = stripRenderedContextHeader(fileText)
			// The rendered child packet is trimmed and the header remover returns
			// its suffix. Move the known delivery ends with that suffix.
			fileDelivery.shift(len(fileContextText) - len(fileText))
			fileContextTrimmed = len(strings.TrimSpace(fileText)) > len(strings.TrimSpace(fileContextText))
			if fileContextText != "" {
				bodyBudget -= min(fileBudget, max(1600, len(fileContextText)+64))
				if bodyBudget < 800 {
					bodyBudget = 800
				}
			}
		}
	}

	// Always compress when a compressor is available.
	// For code repos, use a hardcoded intent optimized for vault overview.
	intent := params.Intent
	if profile == ContextProfileCode && params.Compressor != nil {
		intent = vaultContextIntent
	}

	result := packContext(packContextOptions{
		Context:      ctx,
		Pieces:       pieces,
		Budget:       bodyBudget,
		Tracker:      params.Dedupe,
		NestedDedupe: nestedDedupe,
		Intent:       intent,
		Compressor:   params.Compressor,
	})
	defer result.Delivery.release()

	bodyText := result.Text
	if strings.TrimSpace(fileContextText) != "" {
		var combined strings.Builder
		combined.WriteString(strings.TrimSpace(result.Text))
		combined.WriteString("\n\n## Target context\n\n")
		combined.WriteString(strings.TrimSpace(fileContextText))
		bodyText = strings.TrimSpace(combined.String())
		fileDelivery.shift(len(bodyText) - len(fileContextText))
		result.Delivery.groups = append(result.Delivery.groups, fileDelivery.groups...)
	}

	header := renderHeader("vault_context", vaultDef.Name, vaultPath, string(profile), budget, len(bodyText), result.Meta.Trimmed || fileContextTrimmed)
	final := strings.TrimSpace(header + "\n\n" + bodyText)
	result.Delivery.shift(len(header) + 2)
	final, retained := contextpack.TrimToBudgetDetailed(final, budget)
	result.Delivery.clip(retained)
	if err := result.Delivery.finish(ctx); err != nil {
		return "", err
	}

	return final, nil
}

// buildMinimalBootstrapContext intentionally stays below the vault, graph,
// ontology, code-index, and coderef layers. A start bootstrap needs only the
// bounded guidance files that govern the repository and explicit code targets.
func buildMinimalBootstrapContext(vault repoconfig.VaultManager, params VaultContextTextParams, budget int) (string, error) {
	inputs, err := resolveContextInputs(vault, params.Profile)
	if err != nil {
		return "", err
	}
	root := inputs.ProjectRoot
	rootPaths, err := paths.NewVaultPaths(root)
	if err != nil {
		return "", err
	}

	targets := append([]string(nil), params.Files...)
	if len(targets) == 0 {
		targets = []string{"."}
	}
	perTargetBudget := max(1200, (budget-420)/len(targets))
	seenDocs := make(map[string]struct{})
	var body strings.Builder
	body.WriteString("## Repository guidance\n")
	for _, target := range targets {
		display := strings.TrimSpace(target)
		if display == "" {
			display = "."
		}
		abs := display
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, abs)
		}
		abs = paths.ResolveSymlinks(abs).String()
		if abs == "" {
			abs = filepath.Clean(filepath.Join(root, display))
		}
		if _, containmentErr := rootPaths.RelStrict(abs); containmentErr != nil {
			return "", fmt.Errorf("minimal bootstrap target %q: %w", display, containmentErr)
		}
		isDir := false
		if info, statErr := os.Stat(abs); statErr == nil {
			isDir = info.IsDir()
		}

		body.WriteString("\n### Target: ")
		body.WriteString(display)
		if isDir {
			body.WriteString(" (directory)\n")
		} else {
			body.WriteString(" (file)\n")
		}

		docStart := abs
		if isDir {
			docStart = filepath.Join(abs, "__dir__")
		}
		ancestorDocs, _ := collectAncestorDocs(docStart, rootPaths, inputs.FileCfg.DocPatterns, inputs.FileCfg.MaxEmptyLevels, perTargetBudget)
		for _, doc := range ancestorDocs {
			if _, exists := seenDocs[doc.Path]; exists {
				continue
			}
			seenDocs[doc.Path] = struct{}{}
			content, truncated := contextpack.TrimMarkdown(doc.Content, min(8000, perTargetBudget))
			if strings.TrimSpace(content) == "" {
				continue
			}
			body.WriteString("\n")
			body.WriteString(renderEmbeddedFileXML("ancestor-doc", doc.Path, strings.TrimSpace(content), doc.Truncated || truncated))
			body.WriteString("\n")
		}

		if isDir && params.SubmoduleDepth > 0 {
			submoduleDocs, _ := collectSubmoduleDocs(abs, rootPaths, inputs.FileCfg.DocPatterns, params.SubmoduleDepth, min(SubmoduleDocsBudget, perTargetBudget))
			for _, doc := range submoduleDocs {
				if _, exists := seenDocs[doc.Path]; exists {
					continue
				}
				seenDocs[doc.Path] = struct{}{}
				content, truncated := contextpack.TrimMarkdown(doc.Content, min(2200, perTargetBudget))
				if strings.TrimSpace(content) == "" {
					continue
				}
				body.WriteString("\n")
				body.WriteString(renderEmbeddedFileXMLWithDepth("submodule-doc", doc.Path, strings.TrimSpace(content), doc.Depth, doc.Truncated || truncated))
				body.WriteString("\n")
			}
		}
	}

	bodyText := strings.TrimSpace(body.String())
	header := renderHeader("vault_context", inputs.VaultDef.Name, inputs.VaultPath, string(inputs.Profile), budget, len(bodyText), false)
	return contextpack.TrimToBudget(strings.TrimSpace(header+"\n\n"+bodyText), budget), nil
}
