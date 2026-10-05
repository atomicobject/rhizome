package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/codepatterns"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/frontmatter"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/bmatcuk/doublestar/v4"
)

type ContextProfile string

const (
	ContextProfileAuto  ContextProfile = "auto"
	ContextProfileVault ContextProfile = "vault"
	ContextProfileCode  ContextProfile = "code"
)

type ContextFormat string

const (
	ContextFormatText ContextFormat = "text"
	ContextFormatJSON ContextFormat = "json"
)

const thinContextWarningCode = "thin_context"

type VaultContextRequestScope string

const (
	// VaultContextRequestScopeRich preserves the standalone vault-context contract.
	VaultContextRequestScopeRich VaultContextRequestScope = "rich"
	// VaultContextRequestScopeMinimalBootstrap identifies start requests that need
	// only bounded repository guidance. Phase 7 narrows the emitted packet.
	VaultContextRequestScopeMinimalBootstrap VaultContextRequestScope = "minimal_bootstrap"
	// VaultContextRequestScopeIndexedBootstrap is the explicit-rich agent-start
	// path. It appends bounded durable-index evidence to minimal guidance without
	// initializing the live discovery/runtime pipeline.
	VaultContextRequestScopeIndexedBootstrap VaultContextRequestScope = "indexed_bootstrap"
)

type VaultContextTextParams struct {
	Context      context.Context
	BudgetChars  int
	Profile      ContextProfile
	RequestScope VaultContextRequestScope
	// RequireOntology asks indexed bootstrap to require and render the persisted
	// ontology summary. It never authorizes live projection or repair.
	RequireOntology bool
	// IndexedUnavailable carries store-open authority into fail-soft rendering.
	// It is set only by one-shot startup orchestration; the renderer does not
	// duplicate migration/schema probing.
	IndexedUnavailable *IndexedContextFreshness

	SkipAnchors bool
	SkipEmbeds  bool
	IncludeTags bool
	// IncludeTagsSet indicates IncludeTags was explicitly set by the caller.
	IncludeTagsSet bool
	RecencyCascade bool
	// RecencyCascadeSet indicates RecencyCascade was explicitly set by the caller.
	RecencyCascadeSet bool

	ContextFiles   []string
	Files          []string
	KeyPatterns    []string
	SubmoduleDepth int

	IncludeComponents bool
	GraphSummary      bool

	// Optional: if provided, boosts authority for notes referenced from code.
	CodeRefsByNote map[string][]coderefs.CodeRef

	// Optional: if provided, preserves file-level coderef note linking for bundled file context.
	CodeRefsByFile map[string][]coderefs.CodeRef

	// Optional: when set, reuses an existing code anchor service for bundled file context.
	CodeAnchor *codeanchor.Service

	// Optional: when set, dedupes pieces already returned to a session.
	Dedupe DedupeTracker

	// Optional: when set, reuses an existing metadata/session store.
	SessionStore *semdb.Store
	NoteMetadata notemeta.Indexer
	// IndexedReadOnly uses only a current persisted projection for rich context.
	// It never materializes ontology state through the caller-managed reader.
	IndexedReadOnly bool
	// MetadataStoreFallback prevents nested graph/filter actions from opening a
	// writable store when the planned one-shot reader is unavailable.
	MetadataStoreFallback MetadataStoreFallbackPolicy

	// Optional: when set, shares one ontology freshness result across a
	// compound command's concurrent consumers.
	OntologyRuntimeProvider OntologyRuntimeProvider

	// Optional: intent describing what the caller is trying to accomplish.
	// Used for compression when enabled.
	Intent string

	// Optional: when set, enables intent-driven compression when over budget.
	Compressor contextpack.Compressor
}

// OntologyRuntimeProvider supplies a command-scoped, fresh ontology runtime.
// Implementations may memoize within one compound command; they must not cache
// across commands because Markdown remains the source of truth.
type OntologyRuntimeProvider interface {
	LoadOntologyRuntime(context.Context) (*ontology.Runtime, error)
}

type FileContextTextParams struct {
	Context     context.Context
	BudgetChars int
	Profile     ContextProfile
	// IndexedReadOnly restricts file-context enrichment to bounded reads from
	// SessionStore plus authored ontology schema. It never initializes live
	// graph/ontology state or performs repair/index writes.
	IndexedReadOnly bool
	// IndexedUnavailable carries store-open authority from one-shot bootstrap.
	// When set, indexed enrichment remains disabled and the result preserves the
	// exact missing/incompatible classification and remediation.
	IndexedUnavailable *IndexedContextFreshness

	SkipAnchors bool
	SkipEmbeds  bool
	AnchorKinds []codeanchor.AnchorKind

	Files []string

	// SubmoduleDepth controls how deep to search for module docs in subdirectories.
	// 0 (default): only immediate ancestor docs (current behavior)
	// 1: also include */CONTEXT.md
	// 2: also include */*/CONTEXT.md
	// Budget: 25000 chars dedicated to submodule docs.
	SubmoduleDepth int

	ExcludeNotePaths []string
	ExcludeDocPaths  []string

	// Optional: if provided, improves note relevance and code linking.
	CodeRefsByFile map[string][]coderefs.CodeRef

	// Optional: when set, enriches code files with code anchors.
	CodeAnchor *codeanchor.Service

	// Optional: when set, dedupes pieces already returned to a session.
	Dedupe DedupeTracker

	// Optional: when set, reuses an existing metadata/session store.
	SessionStore            *semdb.Store
	NoteMetadata            notemeta.Indexer
	MetadataStoreFallback   MetadataStoreFallbackPolicy
	OntologyRuntimeProvider OntologyRuntimeProvider
	EnsureLinkTarget        ontology.EnsureLinkTargetMode
	// ApplyLinkTargets must publish to the same live vault/store supplied here.
	// Commands compose it; read bundles and preview consumers leave it nil.
	ApplyLinkTargets func(context.Context, string, ontology.LinkTargetRequest) (ontology.LinkTargetResult, error)

	// Optional: intent describing what the caller is trying to accomplish.
	// Used for compression when enabled.
	Intent string

	// Optional: when set, enables intent-driven compression when over budget.
	Compressor contextpack.Compressor
}

func ParseEnsureLinkTargetMode(raw string) (ontology.EnsureLinkTargetMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(ontology.EnsureLinkTargetNever):
		return ontology.EnsureLinkTargetNever, nil
	case string(ontology.EnsureLinkTargetPlan):
		return ontology.EnsureLinkTargetPlan, nil
	case string(ontology.EnsureLinkTargetApply):
		return ontology.EnsureLinkTargetApply, nil
	default:
		return ontology.EnsureLinkTargetNever, fmt.Errorf("invalid ensure link target mode %q (expected never, plan, or apply)", raw)
	}
}

// KeyNoteMatch captures a key/MOC note and which pattern matched.
type KeyNoteMatch struct {
	Path    string
	Pattern string
}

func BuildVaultContextText(vault obsidian.VaultManager, noteMgr obsidian.NoteReader, params VaultContextTextParams) (string, error) {
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

func BuildVaultContextTextResult(vault obsidian.VaultManager, noteMgr obsidian.NoteReader, params VaultContextTextParams) (VaultContextTextResult, error) {
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

func buildVaultContextTextRich(vault obsidian.VaultManager, noteMgr obsidian.NoteReader, params VaultContextTextParams) (string, error) {
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
			ontoCtx = loadIndexedOntologyTextContext(ctx, vaultDef, noteMgr, params.SessionStore)
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

	var analysis *obsidian.GraphAnalysis
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
			obsidian.RefreshGraphAnalysisDerived(analysis)
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
		norm := string(paths.NormalizeNote(f))
		if filepath.IsAbs(f) && vaultPaths.Root() != "" {
			if rel, err := vaultPaths.RelNoteStrict(f); err == nil && rel.String() != "" {
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
func buildMinimalBootstrapContext(vault obsidian.VaultManager, params VaultContextTextParams, budget int) (string, error) {
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

type FileContextTextResult = VaultContextTextResult

func BuildFileContextText(vault obsidian.VaultManager, noteMgr obsidian.NoteReader, params FileContextTextParams) (string, error) {
	result, err := BuildFileContextTextResult(vault, noteMgr, params)
	return result.Text, err
}

func BuildFileContextTextResult(vault obsidian.VaultManager, noteMgr obsidian.NoteReader, params FileContextTextParams) (FileContextTextResult, error) {
	result, delivery, err := buildFileContextTextResultPending(vault, noteMgr, params)
	defer delivery.release()
	if err != nil {
		return result, err
	}
	if err := delivery.finish(params.Context); err != nil {
		return result, err
	}
	return result, nil
}

func buildFileContextTextResultPending(vault obsidian.VaultManager, noteMgr obsidian.NoteReader, params FileContextTextParams) (FileContextTextResult, *contextDelivery, error) {
	result := FileContextTextResult{}
	renderParams := params
	if params.IndexedReadOnly {
		freshness, reads, warning, err := indexedFileContextFreshness(vault, params)
		if err != nil {
			return result, nil, err
		}
		result.IndexedStatus = freshness.State
		result.IndexedReads = reads
		if warning != nil {
			result.Warnings = append(result.Warnings, *warning)
			// Preserve the bounded filesystem response while preventing stale or
			// unavailable persisted state from contributing any enrichment.
			renderParams.SessionStore = nil
			renderParams.CodeAnchor = nil
			renderParams.OntologyRuntimeProvider = nil
			renderParams.CodeRefsByFile = nil
		}
	}

	text, delivery, err := buildFileContextText(vault, noteMgr, renderParams)
	if err != nil {
		return result, nil, err
	}
	if len(result.Warnings) > 0 {
		var retained int
		text, retained = appendIndexedBootstrapDetailed(text, "", result.Warnings, effectiveContextBudget(params.BudgetChars))
		delivery.clip(retained)
	}
	result.Text = text
	return result, delivery, nil
}

func indexedFileContextFreshness(vault obsidian.VaultManager, params FileContextTextParams) (IndexedContextFreshness, int, *IndexedContextWarning, error) {
	if params.IndexedUnavailable != nil {
		freshness := *params.IndexedUnavailable
		warning := indexedContextWarning(freshness, "indexed enrichment is unavailable because the unified index could not be opened")
		return freshness, 0, &warning, nil
	}
	if params.SessionStore == nil {
		freshness := IndexedContextFreshness{
			State:       IndexedContextMissing,
			WarningCode: "indexed-context-missing",
			Remediation: "rzm index",
		}
		warning := indexedContextWarning(freshness, "indexed enrichment is unavailable because the unified index could not be opened")
		return freshness, 0, &warning, nil
	}

	ctx := params.Context
	if ctx == nil {
		ctx = context.Background()
	}
	inputs, err := resolveContextInputs(vault, params.Profile)
	if err != nil {
		return IndexedContextFreshness{}, 0, nil, err
	}
	expectedScopeHash := ""
	if inputs.LocalCfg != nil && inputs.LocalCfg.cfg != nil {
		expectedScopeHash = inputs.LocalCfg.cfg.ScopeConfigHash()
	}
	snapshot, snapshotErr := params.SessionStore.CodeIndexPrerequisiteSnapshot(ctx)
	if snapshotErr != nil {
		if ctx.Err() != nil {
			return IndexedContextFreshness{}, 1, nil, ctx.Err()
		}
		freshness := IndexedContextFreshness{
			State:       IndexedContextMissing,
			WarningCode: "indexed-context-missing",
			Remediation: "rzm index",
		}
		warning := indexedContextWarning(freshness, fmt.Sprintf("indexed enrichment metadata is unavailable: %v", snapshotErr))
		return freshness, 1, &warning, nil
	}
	freshness := EvaluateIndexedContextFreshness(IndexedContextFreshnessEvidence{
		SchemaCompatible:  true,
		HasIndexerVersion: snapshot.IndexerVersionPresent,
		IndexerVersion:    snapshot.IndexerVersion,
		ExpectedVersion:   codeanchor.IndexerVersion,
		HasScopeHash:      snapshot.ScopeConfigHashPresent,
		ScopeHash:         snapshot.ScopeConfigHash,
		ExpectedScopeHash: expectedScopeHash,
	})
	if freshness.State != IndexedContextAvailable {
		warning := indexedContextWarning(freshness, "indexed enrichment is unavailable or does not match the current index contract")
		return freshness, 1, &warning, nil
	}

	fileReads, warning, err := indexedRequestedFileFreshness(ctx, params.SessionStore, inputs, params.NoteMetadata, params.Files)
	if err != nil {
		return IndexedContextFreshness{}, 1 + fileReads, nil, err
	}
	if warning != nil {
		return IndexedContextFreshness{
			State:       IndexedContextStale,
			WarningCode: warning.Code,
			Remediation: warning.Remediation,
		}, 1 + fileReads, warning, nil
	}
	return freshness, 1 + fileReads, nil, nil
}

func indexedRequestedFileFreshness(
	ctx context.Context,
	store *semdb.Store,
	inputs resolvedContextInputs,
	noteMetadata notemeta.Indexer,
	requested []string,
) (int, *IndexedContextWarning, error) {
	vaultPaths, err := paths.NewVaultPaths(inputs.VaultDef.BasePath())
	if err != nil {
		return 0, nil, err
	}
	seen := make(map[string]struct{}, len(requested))
	reads := 0
	for _, target := range requested {
		if err := ctx.Err(); err != nil {
			return reads, nil, err
		}
		abs := target
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(inputs.ProjectRoot, abs)
		}
		if resolved := paths.ResolveSymlinks(abs).String(); resolved != "" {
			abs = resolved
		}
		info, statErr := os.Stat(abs)
		if statErr != nil {
			return reads, staleRequestedFileWarning(target, "the requested file cannot be read"), nil
		}
		if !info.Mode().IsRegular() {
			continue
		}
		rel, relErr := vaultPaths.RelStrict(abs)
		if relErr != nil || rel.String() == "" {
			return reads, staleRequestedFileWarning(target, "the requested file is outside the indexed vault"), nil
		}
		relPath := rel.String()
		if _, ok := seen[relPath]; ok {
			continue
		}
		seen[relPath] = struct{}{}

		content, readErr := os.ReadFile(abs)
		if readErr != nil {
			return reads, staleRequestedFileWarning(relPath, "the requested file cannot be hashed"), nil
		}
		sum := sha256.Sum256(content)
		currentHash := hex.EncodeToString(sum[:])

		classification, classifyErr := classifyConfiguredPath(inputs.VaultDef, abs, noteMetadata)
		if classifyErr != nil {
			return reads, staleRequestedFileWarning(relPath, "the requested file cannot be classified"), nil
		}
		if classification.Owner == notediscovery.Note {
			indexedHash, indexerVersion, _, ok, metaErr := store.NoteIndexMeta(ctx, string(paths.NormalizeNote(relPath)))
			reads++
			if metaErr != nil {
				if ctx.Err() != nil {
					return reads, nil, ctx.Err()
				}
				return reads, staleRequestedFileWarning(relPath, "indexed note metadata could not be read"), nil
			}
			if !ok || indexedHash == "" || indexerVersion != codeanchor.NoteIndexerVersion {
				return reads, staleRequestedFileWarning(relPath, "indexed note metadata is missing or outdated"), nil
			}
			if indexedHash != currentHash {
				return reads, staleRequestedFileWarning(relPath, "the requested note changed since indexing"), nil
			}
			continue
		}

		indexedHash, indexerVersion, parseStatus, ok, metaErr := store.FileHash(ctx, string(paths.NormalizeCode(relPath)))
		reads++
		if metaErr != nil {
			if ctx.Err() != nil {
				return reads, nil, ctx.Err()
			}
			return reads, staleRequestedFileWarning(relPath, "indexed code metadata could not be read"), nil
		}
		if !ok || indexedHash == "" || indexerVersion != codeanchor.IndexerVersion || !parseStatus.TrustedForIndexing() {
			return reads, staleRequestedFileWarning(relPath, "indexed code metadata is missing or outdated"), nil
		}
		if indexedHash != currentHash {
			return reads, staleRequestedFileWarning(relPath, "the requested code file changed since indexing"), nil
		}
	}
	return reads, nil, nil
}

func staleRequestedFileWarning(path, reason string) *IndexedContextWarning {
	return &IndexedContextWarning{
		Code:        "indexed-context-stale",
		Message:     fmt.Sprintf("indexed enrichment is unavailable for %q because %s", path, reason),
		Remediation: "rzm index",
	}
}

func effectiveContextBudget(budget int) int {
	if budget > 0 {
		return budget
	}
	return contextpack.DefaultBudgetChars
}

func buildFileContextText(vault obsidian.VaultManager, noteMgr obsidian.NoteReader, params FileContextTextParams) (string, *contextDelivery, error) {
	ctx := params.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if params.IndexedReadOnly {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
	}
	budget := effectiveContextBudget(params.BudgetChars)
	if len(params.Files) == 0 {
		return "", nil, fmt.Errorf("files required")
	}

	inputs, err := resolveContextInputs(vault, params.Profile)
	if err != nil {
		return "", nil, err
	}
	vaultDef := inputs.VaultDef
	vaultPath := inputs.VaultPath
	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	projectRoot := inputs.ProjectRoot
	fileCfg := inputs.FileCfg
	profile := inputs.Profile
	projectRootPaths := inputs.ProjectRootPaths

	codeAnchor := params.CodeAnchor
	var codeAnchorCleanup func()
	if codeAnchor == nil && !params.IndexedReadOnly {
		codeAnchor, codeAnchorCleanup, err = loadCodeAnchorForContext(vaultPath, params.SessionStore)
		if err != nil {
			return "", nil, err
		}
	}
	if codeAnchorCleanup != nil {
		defer codeAnchorCleanup()
	}

	opts := defaultContextGraphOptions(params.SkipAnchors, params.SkipEmbeds)

	// Reserve header budget; pack per-target blocks as pieces.
	const headerReserve = 420
	bodyBudget := budget - headerReserve
	if bodyBudget < 800 {
		bodyBudget = budget
	}

	type fileTarget struct {
		abs         string
		display     string
		kind        configuredPathKind
		isDirectory bool
	}
	targets := make([]fileTarget, 0, len(params.Files))
	needsGraph := false
	for _, f := range params.Files {
		abs := f
		if !filepath.IsAbs(f) {
			abs = filepath.Join(projectRoot, f)
		}
		abs = paths.ResolveSymlinks(abs).String()
		if abs == "" {
			abs = filepath.Clean(abs)
		}
		target := fileTarget{abs: abs, display: f, kind: configuredPathKind{Owner: notediscovery.Unowned}}
		if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
			target.isDirectory = true
		} else {
			target.kind, err = classifyConfiguredPath(vaultDef, abs, params.NoteMetadata)
			if err != nil {
				return "", nil, fmt.Errorf("classify file context target %q: %w", f, err)
			}
		}
		if target.kind.Owner == notediscovery.Note && target.kind.Projectable {
			needsGraph = true
		}
		if rel, err := projectRootPaths.RelStrict(abs); err == nil && rel.String() != "" {
			target.display = rel.String()
		}
		targets = append(targets, target)
	}

	var analysis *obsidian.GraphAnalysis
	var reverseNeighbors map[string][]string
	var ontoCtx *ontologyTextContext
	var nodeScope *noderead.Scope
	var ontoCleanup func()
	defer func() {
		if ontoCleanup != nil {
			ontoCleanup()
		}
	}()
	if needsGraph {
		if params.IndexedReadOnly {
			// Persisted graph edges do not retain orthogonal anchor/embed
			// eligibility for mixed link forms. When either exclusion is
			// requested, omit indexed graph enrichment rather than returning
			// links the caller explicitly suppressed.
			if !params.SkipAnchors && !params.SkipEmbeds {
				notePaths := make([]string, 0, len(targets))
				for _, target := range targets {
					if target.kind.Owner == notediscovery.Note && target.kind.Projectable {
						notePaths = append(notePaths, normalizeAbsNotePath(target.abs, vaultPaths))
					}
				}
				analysis, reverseNeighbors, err = loadIndexedNoteGraphContext(ctx, params.SessionStore, notePaths)
				if err != nil {
					return "", nil, err
				}
			}
		} else {
			analysis, err = GraphAnalysis(vault, noteMgr, GraphAnalysisParams{
				UseConfig:             true,
				Options:               opts,
				SessionStore:          params.SessionStore,
				NoteMetadata:          params.NoteMetadata,
				MetadataStoreFallback: params.MetadataStoreFallback,
			})
			if err != nil {
				return "", nil, err
			}
			reverseNeighbors = buildReverseNeighborsForText(analysis.Nodes)
		}
	}
	var ontologyLoadErr error
	if params.IndexedReadOnly {
		ontoCtx = loadIndexedOntologyTextContext(ctx, vaultDef, noteMgr, params.SessionStore)
	} else if params.OntologyRuntimeProvider != nil {
		ontoCtx, ontologyLoadErr = loadOntologyTextContextFromProvider(ctx, vaultDef, noteMgr, params.OntologyRuntimeProvider)
	} else if params.NoteMetadata.Validate() == nil {
		ontoCtx, ontoCleanup, ontologyLoadErr = loadOntologyTextContext(vaultDef, noteMgr, params.NoteMetadata, params.SessionStore)
	}
	if params.EnsureLinkTarget == ontology.EnsureLinkTargetApply && ontologyLoadErr != nil {
		return "", nil, ontologyLoadErr
	}
	if ontoCtx != nil && ontoCtx.Store != nil && ontoCtx.Schema != nil {
		liveReader := noteMgr
		if params.EnsureLinkTarget == ontology.EnsureLinkTargetApply {
			liveReader = &obsidian.Note{}
		}
		service := noderead.NewService(vaultDef, liveReader, ontoCtx.Store, ontoCtx.Schema)
		if params.EnsureLinkTarget == ontology.EnsureLinkTargetApply {
			service.ApplyLinkTargets = func(applyCtx context.Context, request ontology.LinkTargetRequest) (ontology.LinkTargetResult, error) {
				if params.IndexedReadOnly || params.ApplyLinkTargets == nil {
					return ontology.LinkTargetResult{}, fmt.Errorf("ensure-link apply requires an explicit live writer")
				}
				if err := params.NoteMetadata.Validate(); err != nil {
					return ontology.LinkTargetResult{}, fmt.Errorf("ensure-link apply metadata indexer: %w", err)
				}
				return params.ApplyLinkTargets(applyCtx, ontoCtx.Schema.Hash, request)
			}
		}
		nodeScope = service.NewScope(ctx, noderead.ScopeOptions{})
	}

	// Split budget across targets to avoid single huge payloads.
	perTarget := int(math.Max(1200, float64(bodyBudget)/float64(len(targets))))

	type cachedFileContext struct {
		context FileContext
		err     error
	}
	var pieces []contextpack.Piece
	nestedDedupe := make(map[string][]DedupeItem)
	incompleteKeys := make(map[string]bool)
	fileContexts := make(map[string]cachedFileContext)
	sourceTruncated := false
	for i, target := range targets {
		abs, disp := target.abs, target.display
		classification := target.kind
		orderScore := float64(len(targets) - i)
		if classification.Owner == notediscovery.Note {
			if !classification.Projectable {
				pieces = append(pieces, contextpack.Piece{
					Key:      fmt.Sprintf("note-unavailable:%s", disp),
					Priority: 80,
					Score:    orderScore,
					Text:     fmt.Sprintf("### %s (note)\n\nUnsupported projection: %s", disp, unsupportedProjectionMessage(disp, classification.Provider, "file-context")),
				})
				continue
			}
			norm := normalizeAbsNotePath(abs, vaultPaths)
			block := renderNoteSummaryBlockWithContext(ctx, noteMgr, analysis, reverseNeighbors, norm, ontoCtx)
			if params.IndexedReadOnly && strings.TrimSpace(block) == "" {
				block = renderNoteSummaryBlockNoGraphWithContext(ctx, noteMgr, norm, ontoCtx)
			}
			isError := strings.TrimSpace(block) == ""
			if isError {
				block = fmt.Sprintf("### %s (note)\n\nError: not found in graph", disp)
			}
			var ctxDedupe []DedupeItem
			if !isError {
				ctxBudget := int(float64(perTarget) * 0.55)
				if ctxBudget < 1000 {
					ctxBudget = perTarget
				}
				ctxBlocks, dedupe := renderNoteContextIncludesWithContext(ctx, noteMgr, vaultDef, norm, ontoCtx, ctxBudget, params.Dedupe)
				if ctxBlocks != "" {
					block += ctxBlocks
				}
				ctxDedupe = dedupe
			}
			pieceKey := fmt.Sprintf("note:%s", norm)
			trimmed := contextpack.TrimToBudget(block, perTarget)
			incompleteKeys[pieceKey] = trimmed != strings.TrimSpace(block)
			block = trimmed
			pieces = append(pieces, contextpack.Piece{
				Key:      pieceKey,
				Priority: 80,
				Score:    orderScore,
				Text:     block,
			})
			if len(ctxDedupe) > 0 {
				nestedDedupe[pieceKey] = append(nestedDedupe[pieceKey], ctxDedupe...)
			}
			continue
		}
		if !target.isDirectory && classification.Owner != notediscovery.Code {
			pieces = append(pieces, contextpack.Piece{
				Key:      fmt.Sprintf("unowned:%s", disp),
				Priority: 80,
				Score:    orderScore,
				Text:     fmt.Sprintf("### %s\n\nError: file is not owned by a configured note or code provider", disp),
			})
			continue
		}

		codeBudget := min(fileCfg.ContextBudget, int(float64(perTarget)*0.55))
		if codeBudget < 800 {
			codeBudget = min(fileCfg.ContextBudget, perTarget)
		}

		cached, ok := fileContexts[abs]
		if !ok {
			codeCtx, buildErr := BuildFileContext(abs, FileContextParams{
				Context:              ctx,
				IndexedReadOnly:      params.IndexedReadOnly,
				VaultDef:             vaultDef,
				ProjectRoot:          projectRoot,
				DocPatterns:          fileCfg.DocPatterns,
				MaxEmptyLevels:       fileCfg.MaxEmptyLevels,
				ContextBudget:        codeBudget,
				ExpandNoteLinks:      fileCfg.ExpandNoteLinks,
				ExpandNoteLinksLimit: fileCfg.ExpandNoteLinksLimit,
				CodeRefsByFile:       params.CodeRefsByFile,
				NoteReader:           noteMgr,
				ExcludeNotePaths:     params.ExcludeNotePaths,
				ExcludeDocPaths:      params.ExcludeDocPaths,
				CodeAnchor:           codeAnchor,
				SessionStore:         params.SessionStore,
				NodeReadScope:        nodeScope,
				EnsureLinkTarget:     params.EnsureLinkTarget,
				SkipAnchorRefresh:    params.IndexedReadOnly,
				AnchorKinds:          params.AnchorKinds,
				SubmoduleDepth:       params.SubmoduleDepth,
			})
			cached = cachedFileContext{context: codeCtx, err: buildErr}
			fileContexts[abs] = cached
		}
		codeCtx, err := cached.context, cached.err
		sourceTruncated = sourceTruncated || codeCtx.Truncated
		block, dedupeEntries := renderCodeFileContextBlock(noteMgr, vaultDef, disp, codeCtx, perTarget, profile, params.Dedupe)
		if err != nil {
			if params.EnsureLinkTarget == ontology.EnsureLinkTargetApply {
				return "", nil, err
			}
			block = fmt.Sprintf("### %s (code)\n\nError: %s", disp, err.Error())
			dedupeEntries = nil
		}
		pieceKey := fmt.Sprintf("code:%s", disp)
		trimmed := contextpack.TrimToBudget(block, perTarget)
		incompleteKeys[pieceKey] = trimmed != strings.TrimSpace(block)
		pieces = append(pieces, contextpack.Piece{
			Key:      pieceKey,
			Priority: 80,
			Score:    orderScore,
			Text:     trimmed,
		})
		if len(dedupeEntries) > 0 {
			nestedDedupe[pieceKey] = dedupeEntries
		}
	}

	result := packContext(packContextOptions{
		Context:        ctx,
		Pieces:         pieces,
		Budget:         bodyBudget,
		Tracker:        params.Dedupe,
		NestedDedupe:   nestedDedupe,
		IncompleteKeys: incompleteKeys,
		Intent:         params.Intent,
		Compressor:     params.Compressor,
	})

	header := renderHeader("file_context", vaultDef.Name, vaultPath, string(profile), budget, len(result.Text), result.Meta.Trimmed || sourceTruncated)
	final := strings.TrimSpace(header + "\n\n" + result.Text)
	result.Delivery.shift(len(header) + 2)
	final, retained := contextpack.TrimToBudgetDetailed(final, budget)
	result.Delivery.clip(retained)
	return final, result.Delivery, nil
}

type localCfgInfo struct {
	dir string
	cfg *obsidian.LocalConfig
}

type resolvedContextInputs struct {
	VaultDef         obsidian.VaultDefinition
	VaultPath        string
	ProjectRoot      string
	ProjectRootPaths paths.VaultPaths
	FileCfg          obsidian.LocalFileContextConfig
	LocalCfg         *localCfgInfo
	Profile          ContextProfile
}

func loadLocalConfigBestEffort(vaultPath string) (*localCfgInfo, error) {
	if vaultPath == "" {
		return nil, obsidian.ErrNoLocalConfig
	}
	cfgDir, cfg, err := obsidian.FindLocalConfig(vaultPath)
	if err != nil || cfg == nil {
		return nil, err
	}
	return &localCfgInfo{dir: cfgDir, cfg: cfg}, nil
}

func resolveContextInputs(vault obsidian.VaultManager, profile ContextProfile) (resolvedContextInputs, error) {
	vaultDef, err := vault.Definition()
	if err != nil {
		return resolvedContextInputs{}, err
	}
	vaultPath := vaultDef.BasePath()

	projectRoot := vaultPath
	fileCfg := obsidian.FileContextConfigDefaults
	localCfg, localCfgErr := loadLocalConfigBestEffort(vaultPath)
	if localCfgErr != nil && !errors.Is(localCfgErr, obsidian.ErrNoLocalConfig) {
		return resolvedContextInputs{}, localCfgErr
	}
	if localCfg != nil {
		projectRoot = localCfg.dir
		fileCfg = obsidian.FileContextConfigFromLocalOrDefault(localCfg.cfg)
	}
	if projectRoot == "" {
		projectRoot = vaultPath
	}

	projectRootPaths, _ := paths.NewVaultPaths(projectRoot)
	return resolvedContextInputs{
		VaultDef:         vaultDef,
		VaultPath:        vaultPath,
		ProjectRoot:      projectRoot,
		ProjectRootPaths: projectRootPaths,
		FileCfg:          fileCfg,
		LocalCfg:         localCfg,
		Profile:          resolveProfile(profile, localCfg, projectRoot),
	}, nil
}

func defaultContextGraphOptions(skipAnchors, skipEmbeds bool) obsidian.GraphAnalysisOptions {
	return obsidian.GraphAnalysisOptions{
		WikilinkOptions: obsidian.WikilinkOptions{
			SkipAnchors: skipAnchors,
			SkipEmbeds:  skipEmbeds,
		},
		IncludeTags:       true,
		RecencyCascade:    true,
		RecencyCascadeSet: true,
	}
}

func resolveProfile(want ContextProfile, localCfg *localCfgInfo, projectRoot string) ContextProfile {
	switch want {
	case ContextProfileVault, ContextProfileCode:
		return want
	default:
	}
	// Auto: treat as code if code scanning is configured OR common code markers exist.
	if localCfg != nil && localCfg.cfg != nil {
		if len(localCfg.cfg.Code.Scan) > 0 || localCfg.cfg.Code.Go != nil || localCfg.cfg.Code.Python != nil || localCfg.cfg.Code.TypeScript != nil || localCfg.cfg.Code.JavaScript != nil {
			return ContextProfileCode
		}
	}
	for _, marker := range []string{"go.mod", "package.json", "pyproject.toml", "Cargo.toml"} {
		if _, err := os.Stat(filepath.Join(projectRoot, marker)); err == nil {
			return ContextProfileCode
		}
	}
	return ContextProfileVault
}

// loadCodeAnchorForContext returns a best-effort codeanchor service for the vault.
// Caller is responsible for closing the returned cleanup func when non-nil.
func loadCodeAnchorForContext(vaultPath string, store *semdb.Store) (*codeanchor.Service, func(), error) {
	basePath := openableVaultBasePath(vaultPath)
	if basePath == "" {
		return nil, nil, nil
	}
	var cleanup func()
	if store == nil {
		cfg, err := obsidian.LoadCodeConfig(basePath)
		if err != nil {
			return nil, nil, err
		}
		opened, closeFn, err := obsidian.OpenIntelStoreFromConfig(basePath, cfg, true)
		if err != nil {
			return nil, nil, err
		}
		if opened == nil {
			return nil, nil, nil
		}
		store = opened
		cleanup = closeFn
	}
	svc := codeanchor.NewServiceWithOptions(
		store,
		nil,
		codeanchor.WithBasePath(basePath),
		codeanchor.WithoutWarmCache(),
	)

	timeout := 5 * time.Second
	if cfg, err := obsidian.LoadCodeConfig(basePath); err == nil {
		if candidate := cfg.EffectiveRecomputeTimeout(); candidate > 0 {
			timeout = candidate
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = svc.RecomputeAnchorScopes(ctx) // best-effort; fall back to existing scopes on error

	return svc, cleanup, nil
}

func renderHeader(toolName, vaultName, vaultPath, profile string, budget int, bodyLen int, trimmed bool) string {
	used := bodyLen
	// Include a conservative join overhead for header separator.
	used += 200
	if used > budget {
		used = budget
	}
	trimStr := "false"
	if trimmed {
		trimStr = "true"
	}
	var b strings.Builder
	b.WriteString("# Rhizome context\n\n")
	b.WriteString(fmt.Sprintf("- tool: %s\n", toolName))
	if vaultName != "" {
		b.WriteString(fmt.Sprintf("- vault: %s\n", vaultName))
	}
	if vaultPath != "" {
		b.WriteString(fmt.Sprintf("- vaultPath: %s\n", vaultPath))
	}
	if profile != "" {
		b.WriteString(fmt.Sprintf("- profile: %s\n", profile))
	}
	b.WriteString(fmt.Sprintf("- budgetChars: %d\n", budget))
	b.WriteString(fmt.Sprintf("- trimmed: %s\n", trimStr))
	return strings.TrimSpace(b.String())
}

func renderVaultStatsBlock(analysis *obsidian.GraphAnalysis) string {
	if analysis == nil {
		return "## Stats\n\nError: graph analysis unavailable"
	}
	return fmt.Sprintf("## Stats\n\n- notes: %d\n- links: %d\n- orphans: %d", analysis.Stats.NodeCount, analysis.Stats.EdgeCount, len(analysis.Orphans))
}

func renderOntologyStatsBlock(summary *OntologyVaultSummary) string {
	if summary == nil {
		return "## Stats\n\nError: ontology summary unavailable"
	}
	return fmt.Sprintf(
		"## Stats\n\n- notes: %d\n- typed: %d\n- untyped: %d\n- ontologyTypes: %d\n- ontologyIssues: %d",
		summary.TotalNotes,
		summary.TypedNotes,
		summary.UntypedNotes,
		len(summary.TypeCounts),
		summary.IssueCount,
	)
}

func renderOntologyTypeCountsBlock(summary *OntologyVaultSummary, limit int) string {
	if summary == nil || len(summary.TypeCounts) == 0 {
		return ""
	}
	counts := append([]OntologyTypeCount(nil), summary.TypeCounts...)
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Count == counts[j].Count {
			return counts[i].TypeName < counts[j].TypeName
		}
		return counts[i].Count > counts[j].Count
	})
	if limit > 0 && len(counts) > limit {
		counts = counts[:limit]
	}

	var b strings.Builder
	b.WriteString("## Ontology types\n")
	for _, entry := range counts {
		b.WriteString(fmt.Sprintf("\n- %s (%d)", entry.TypeName, entry.Count))
		if len(entry.Examples) > 0 {
			b.WriteString(fmt.Sprintf(": %s", strings.Join(entry.Examples, ", ")))
		}
	}
	return b.String()
}

func renderOntologyIssuesBlock(summary *OntologyVaultSummary, limit int) string {
	if summary == nil || len(summary.Issues) == 0 {
		return ""
	}
	issues := append([]obsidian.OntologyIssue(nil), summary.Issues...)
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		if issues[i].NotePath != issues[j].NotePath {
			return issues[i].NotePath < issues[j].NotePath
		}
		return issues[i].FieldName < issues[j].FieldName
	})
	total := len(issues)
	if limit > 0 && len(issues) > limit {
		issues = issues[:limit]
	}

	var b strings.Builder
	b.WriteString("## Ontology issues\n")
	for _, issue := range issues {
		location := issue.NotePath
		if strings.TrimSpace(location) == "" {
			location = issue.TypeName
		}
		if strings.TrimSpace(location) == "" {
			location = "<schema>"
		}
		b.WriteString(fmt.Sprintf("\n- [%s] %s", issue.Code, location))
		if issue.FieldName != "" {
			b.WriteString(fmt.Sprintf(" field=%s", issue.FieldName))
		}
		if issue.Line > 0 {
			b.WriteString(fmt.Sprintf(" line=%d", issue.Line))
		}
		if issue.Message != "" {
			b.WriteString(fmt.Sprintf(": %s", issue.Message))
		}
	}
	if total > len(issues) {
		b.WriteString(fmt.Sprintf("\n- ... %d more", total-len(issues)))
	}
	return b.String()
}

func renderTopList(title string, items []string) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## ")
	b.WriteString(title)
	b.WriteString("\n")
	for _, it := range items {
		b.WriteString("\n- ")
		b.WriteString(it)
	}
	return b.String()
}

func topAuthorityPaths(nodes map[string]obsidian.GraphNode, limit int) []string {
	if len(nodes) == 0 || limit == 0 {
		return nil
	}
	type pr struct {
		path string
		val  float64
	}
	list := make([]pr, 0, len(nodes))
	for p, n := range nodes {
		list = append(list, pr{path: p, val: n.Authority})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].val == list[j].val {
			return list[i].path < list[j].path
		}
		return list[i].val > list[j].val
	})
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, item.path)
	}
	return out
}

func renderKeyNotes(noteMgr obsidian.NoteReader, paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Key notes\n")
	for _, p := range paths {
		title := titleFromPath(p)
		summary := blessedSummary(noteMgr, p)
		b.WriteString("\n- ")
		b.WriteString(title)
		b.WriteString(" (")
		b.WriteString(p)
		b.WriteString(")")
		if summary != "" {
			b.WriteString(" — ")
			b.WriteString(summary)
		}
	}
	return b.String()
}

func blessedSummary(noteMgr obsidian.NoteReader, notePath string) string {
	if noteMgr == nil || notePath == "" {
		return ""
	}
	fact, ok := NoteFactsFromReader(noteMgr).LookupFact(notePath)
	if !ok || len(fact.Frontmatter) == 0 {
		return ""
	}
	blessed := frontmatter.FilterBlessed(fact.Frontmatter)
	if len(blessed) == 0 {
		return ""
	}
	for _, k := range []string{"summary", "synopsis", "about", "description", "desc", "doc"} {
		if v, ok := blessed[k]; ok {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" {
				return s
			}
		}
	}
	return ""
}

type ontologyTextContext struct {
	Schema          *ontology.Schema
	Store           *semdb.Store
	Scope           *noderead.Scope
	Issues          []ontology.ValidationIssue
	IndexedReadOnly bool
}

func loadOntologyTextContextFromProvider(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, provider OntologyRuntimeProvider) (*ontologyTextContext, error) {
	runtime, err := provider.LoadOntologyRuntime(ctx)
	if runtime == nil || runtime.Store == nil {
		return nil, err
	}
	scope := noderead.NewService(vaultDef, noteMgr, runtime.Store, runtime.Schema).NewScope(ctx, noderead.ScopeOptions{})
	return &ontologyTextContext{
		Schema: runtime.Schema,
		Store:  runtime.Store,
		Scope:  scope,
		Issues: append([]ontology.ValidationIssue(nil), runtime.Issues...),
	}, err
}

func loadOntologyTextContext(vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, noteMetadata notemeta.Indexer, store *semdb.Store) (*ontologyTextContext, func(), error) {
	if err := noteMetadata.Validate(); err != nil {
		return nil, nil, err
	}
	if store != nil {
		runtime, err := ontology.EnsureRuntimeWithStore(context.Background(), noteMetadata, vaultDef, noteMgr, store)
		if runtime == nil || runtime.Store == nil {
			return nil, nil, err
		}
		scope := noderead.NewService(vaultDef, noteMgr, runtime.Store, runtime.Schema).NewScope(context.Background(), noderead.ScopeOptions{})
		return &ontologyTextContext{
			Schema: runtime.Schema,
			Store:  runtime.Store,
			Scope:  scope,
			Issues: append([]ontology.ValidationIssue(nil), runtime.Issues...),
		}, nil, err
	}
	runtime, cleanup, err := ontology.EnsureRuntime(context.Background(), noteMetadata, vaultDef, noteMgr)
	if runtime == nil || runtime.Store == nil {
		return nil, cleanup, err
	}
	scope := noderead.NewService(vaultDef, noteMgr, runtime.Store, runtime.Schema).NewScope(context.Background(), noderead.ScopeOptions{})
	return &ontologyTextContext{
		Schema: runtime.Schema,
		Store:  runtime.Store,
		Scope:  scope,
		Issues: append([]ontology.ValidationIssue(nil), runtime.Issues...),
	}, cleanup, err
}

// loadIndexedOntologyTextContext creates only a request-scoped reader over the
// existing index. Authored SDL is presentation metadata only when the persisted
// ontology snapshot is ready and was materialized from that exact schema.
// Missing, invalid, or stale state degrades to graph-only context.
func loadIndexedOntologyTextContext(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store *semdb.Store) *ontologyTextContext {
	if store == nil {
		return nil
	}
	schema, err := ontology.LoadSchema(vaultDef.BasePath())
	if err != nil || schema == nil {
		return nil
	}
	state, err := store.GetOntologySchemaState(ctx)
	if err != nil ||
		!state.Ready ||
		state.SchemaHash != schema.Hash ||
		state.MaterializationVersion != ontology.OntologyMaterializationVersion {
		return nil
	}
	scope := noderead.NewService(vaultDef, noteMgr, store, schema).NewScope(ctx, noderead.ScopeOptions{})
	return &ontologyTextContext{Schema: schema, Store: store, Scope: scope, IndexedReadOnly: true}
}

// loadIndexedNoteGraphContext materializes only requested note rows and a
// bounded one-hop wikilink neighborhood. GraphDocScores owns degree/HITS data
// when present; SQL window counts preserve complete degrees for scoreless
// requested nodes without hydrating every incident edge.
func loadIndexedNoteGraphContext(ctx context.Context, store *semdb.Store, notePaths []string) (*obsidian.GraphAnalysis, map[string][]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if store == nil || len(notePaths) == 0 {
		return nil, nil, nil
	}
	notePaths = uniqueNormalizedStrings(notePaths)
	const neighborLimit = 5
	neighborhood, edgeErr := store.GraphDocNoteNeighborhoodForPaths(ctx, notePaths, neighborLimit)
	if edgeErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, ctxErr
		}
		neighborhood = semdb.GraphDocNoteNeighborhood{
			Edges:   []semdb.GraphDocEdge{},
			Degrees: map[string]semdb.GraphDocDegree{},
		}
	}
	edges := neighborhood.Edges

	scorePaths := append([]string(nil), notePaths...)
	for _, edge := range edges {
		scorePaths = append(scorePaths, edge.SrcPath, edge.DstPath)
	}
	scores, scoreErr := store.GraphDocScoresByPaths(ctx, uniqueNormalizedStrings(scorePaths))
	if scoreErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, ctxErr
		}
		scores = map[string]semdb.GraphDocScore{}
	}

	nodes := make(map[string]obsidian.GraphNode, len(scores))
	for path, score := range scores {
		kind := score.DocType
		if kind == "" {
			kind = "note"
		}
		nodes[path] = obsidian.GraphNode{
			Path:      path,
			Title:     titleFromPath(path),
			Kind:      kind,
			Inbound:   score.Inbound,
			Outbound:  score.Outbound,
			Hub:       score.Hub,
			Authority: score.Authority,
			Community: score.Community,
		}
	}
	ensureNode := func(path string) {
		path = strings.TrimSpace(filepath.ToSlash(path))
		if path == "" {
			return
		}
		if _, ok := nodes[path]; ok {
			return
		}
		nodes[path] = obsidian.GraphNode{
			Path:  path,
			Title: titleFromPath(path),
			Kind:  "note",
		}
	}
	for _, path := range notePaths {
		ensureNode(path)
	}
	applyIndexedGraphEdges(nodes, scores, edges, ensureNode)
	for path, degree := range neighborhood.Degrees {
		if _, hasScore := scores[path]; hasScore {
			continue
		}
		node := nodes[path]
		node.Inbound = degree.Inbound
		node.Outbound = degree.Outbound
		nodes[path] = node
	}
	if len(nodes) == 0 {
		return nil, nil, nil
	}
	analysis := &obsidian.GraphAnalysis{Nodes: nodes}
	return analysis, buildReverseNeighborsForText(nodes), nil
}

func applyIndexedGraphEdges(
	nodes map[string]obsidian.GraphNode,
	scores map[string]semdb.GraphDocScore,
	edges []semdb.GraphDocEdge,
	ensureNode func(string),
) {
	inbound := make(map[string]map[string]struct{})
	outbound := make(map[string]map[string]struct{})
	for _, edge := range edges {
		src := strings.TrimSpace(filepath.ToSlash(edge.SrcPath))
		dst := strings.TrimSpace(filepath.ToSlash(edge.DstPath))
		if src == "" || dst == "" || src == dst {
			continue
		}
		ensureNode(src)
		ensureNode(dst)
		if outbound[src] == nil {
			outbound[src] = make(map[string]struct{})
		}
		outbound[src][dst] = struct{}{}
		if inbound[dst] == nil {
			inbound[dst] = make(map[string]struct{})
		}
		inbound[dst][src] = struct{}{}
	}
	for path, node := range nodes {
		node.Neighbors = make([]string, 0, len(outbound[path]))
		for neighbor := range outbound[path] {
			node.Neighbors = append(node.Neighbors, neighbor)
		}
		sort.Strings(node.Neighbors)
		if _, hasScore := scores[path]; !hasScore {
			node.Inbound = len(inbound[path])
			node.Outbound = len(outbound[path])
		}
		nodes[path] = node
	}
}

func uniqueNormalizedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(filepath.ToSlash(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func renderNoteSummaryBlock(noteMgr obsidian.NoteReader, analysis *obsidian.GraphAnalysis, reverseNeighbors map[string][]string, notePath string, onto *ontologyTextContext) string {
	return renderNoteSummaryBlockWithContext(context.Background(), noteMgr, analysis, reverseNeighbors, notePath, onto)
}

func renderNoteSummaryBlockWithContext(ctx context.Context, noteMgr obsidian.NoteReader, analysis *obsidian.GraphAnalysis, reverseNeighbors map[string][]string, notePath string, onto *ontologyTextContext) string {
	if analysis == nil || len(analysis.Nodes) == 0 || notePath == "" {
		return ""
	}
	node, ok := analysis.Nodes[notePath]
	if !ok {
		return ""
	}

	var b strings.Builder
	title := strings.TrimSpace(node.Title)
	if title == "" {
		title = titleFromPath(notePath)
	}
	b.WriteString("## ")
	b.WriteString(title)
	b.WriteString(" (note)\n\n")
	b.WriteString(fmt.Sprintf("- path: %s\n", notePath))
	summary := blessedSummary(noteMgr, notePath)
	if summary != "" {
		b.WriteString(fmt.Sprintf("- summary: %s\n", summary))
	}
	ontoBlock := renderNoteOntologyBlockWithContext(ctx, notePath, onto)
	if ontoBlock != "" {
		b.WriteString(ontoBlock)
	}
	b.WriteString(fmt.Sprintf("- hub: %.3f\n", node.Hub))
	b.WriteString(fmt.Sprintf("- authority: %.3f\n", node.Authority))
	b.WriteString(fmt.Sprintf("- inbound: %d\n", node.Inbound))
	b.WriteString(fmt.Sprintf("- outbound: %d\n", node.Outbound))

	// Top inbound/outbound neighbors by authority (very small, no backlink parsing).
	inTop := topNeighborsByAuthority(reverseNeighbors[notePath], analysis.Nodes, 5)
	outTop := topNeighborsByAuthority(node.Neighbors, analysis.Nodes, 5)
	if len(inTop) > 0 {
		b.WriteString("\n### Top inbound\n")
		for _, p := range inTop {
			b.WriteString("\n- ")
			b.WriteString(p)
		}
	}
	if len(outTop) > 0 {
		b.WriteString("\n\n### Top outbound\n")
		for _, p := range outTop {
			b.WriteString("\n- ")
			b.WriteString(p)
		}
	}
	if isThinNoteContext(node, ontoBlock) {
		b.WriteString(renderThinContextWarning(notePath))
	}

	return strings.TrimSpace(b.String())
}

func renderNoteSummaryBlockNoGraph(noteMgr obsidian.NoteReader, notePath string, onto *ontologyTextContext) string {
	return renderNoteSummaryBlockNoGraphWithContext(context.Background(), noteMgr, notePath, onto)
}

func renderNoteSummaryBlockNoGraphWithContext(ctx context.Context, noteMgr obsidian.NoteReader, notePath string, onto *ontologyTextContext) string {
	if notePath == "" {
		return ""
	}
	title := titleFromPath(notePath)

	var b strings.Builder
	b.WriteString("## ")
	b.WriteString(title)
	b.WriteString(" (note)\n\n")
	b.WriteString(fmt.Sprintf("- path: %s\n", notePath))
	summary := blessedSummary(noteMgr, notePath)
	if summary != "" {
		b.WriteString(fmt.Sprintf("- summary: %s\n", summary))
	}
	ontoBlock := renderNoteOntologyBlockWithContext(ctx, notePath, onto)
	if ontoBlock != "" {
		b.WriteString(ontoBlock)
	}
	if ontoBlock == "" && summary == "" {
		b.WriteString(renderThinContextWarning(notePath))
	}
	return strings.TrimSpace(b.String())
}

func isThinNoteContext(node obsidian.GraphNode, ontologyBlock string) bool {
	return node.Inbound == 0 && node.Outbound == 0 && strings.TrimSpace(ontologyBlock) == ""
}

func isThinCodeContext(ctx FileContext) bool {
	return len(ctx.AncestorDocs) == 0 &&
		len(ctx.SubmoduleDocs) == 0 &&
		len(ctx.Rationale) == 0 &&
		len(ctx.OntologyNodes) == 0 &&
		len(ctx.NodeDiagnostics) == 0 &&
		len(ctx.LinkedNotes) == 0 &&
		len(ctx.ReturnedNotePaths) == 0 &&
		len(ctx.ReturnedDocPaths) == 0 &&
		len(ctx.AnchorMatches) == 0
}

func renderThinContextWarning(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "<path>"
	}
	quotedPath := shellQuoteArg(path)
	return fmt.Sprintf("\n\n#### Warnings\n- %s: insufficient contextual bindings for `%s`; this is not absence of docs. Next: `rzm agent files --include-content true --input %s`; for discovery use `rzm agent semantic-query --path %s --query \"<topic>\"`.", thinContextWarningCode, path, quotedPath, quotedPath)
}

func shellQuoteArg(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func renderNoteOntologyBlockWithContext(ctx context.Context, notePath string, onto *ontologyTextContext) string {
	if onto == nil || onto.Store == nil || onto.Schema == nil || onto.Scope == nil || notePath == "" {
		return ""
	}
	rows, err := onto.Scope.TypesByPaths(ctx, []string{notePath})
	if err != nil {
		return ""
	}
	row, ok := rows[notePath]
	if !ok {
		return ""
	}
	noteType := onto.Schema.Types[row.TypeName]
	if noteType == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n### Ontology\n")
	b.WriteString(fmt.Sprintf("\n- type: %s", row.TypeName))
	if noteType.Label != "" && noteType.Label != row.TypeName {
		b.WriteString(fmt.Sprintf("\n- label: %s", noteType.Label))
	}
	if noteType.KeyField != "" {
		b.WriteString(fmt.Sprintf("\n- keyField: %s", noteType.KeyField))
	}
	fields := make([]string, 0, len(noteType.Fields))
	for _, field := range noteType.Fields {
		name := field.Name
		if field.Required {
			name += "!"
		}
		if field.ContextInclude {
			name += "(ctx)"
		}
		fields = append(fields, name)
	}
	if len(fields) > 0 {
		b.WriteString(fmt.Sprintf("\n- fields: %s", strings.Join(fields, ", ")))
	}
	if desc := strings.TrimSpace(noteType.Description); desc != "" {
		b.WriteByte('\n')
		for _, line := range strings.Split(desc, "\n") {
			b.WriteString("\n> ")
			b.WriteString(line)
		}
	}

	edges := noteOntologyPresentationEdges(ctx, onto, notePath, nil, 24)
	if len(edges) == 0 {
		return b.String()
	}

	type groupKey struct {
		Relation   string
		Provenance string
	}
	grouped := make(map[groupKey][]string)
	keys := make([]groupKey, 0)
	for _, edge := range edges {
		key := groupKey{Relation: edge.RelationName, Provenance: edge.Provenance}
		if _, ok := grouped[key]; !ok {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], edge.Target.NotePath)
	}
	if len(keys) == 0 {
		return b.String()
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Relation != keys[j].Relation {
			return keys[i].Relation < keys[j].Relation
		}
		return keys[i].Provenance < keys[j].Provenance
	})
	b.WriteString("\n\n### Ontology neighbors\n")
	for _, key := range keys {
		neighbors := grouped[key]
		sort.Strings(neighbors)
		marker := ""
		if field := noteType.ByName[key.Relation]; field != nil && field.ContextInclude {
			marker = " (ctx)"
		}
		b.WriteString(fmt.Sprintf("\n- %s%s [%s]: %s", key.Relation, marker, key.Provenance, strings.Join(neighbors, ", ")))
	}
	return b.String()
}

func stringBoolMapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key, ok := range values {
		if ok && strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func renderNoteContextIncludesWithContext(ctx context.Context, noteMgr obsidian.NoteReader, vaultDef obsidian.VaultDefinition, notePath string, onto *ontologyTextContext, budget int, tracker DedupeTracker) (string, []DedupeItem) {
	if onto == nil || onto.Scope == nil || onto.Schema == nil || noteMgr == nil || notePath == "" || budget < 400 {
		return "", nil
	}
	rows, err := onto.Scope.TypesByPaths(ctx, []string{notePath})
	if err != nil {
		return "", nil
	}
	row, ok := rows[notePath]
	if !ok {
		return "", nil
	}
	noteType := onto.Schema.Types[row.TypeName]
	if noteType == nil {
		return "", nil
	}
	ctxFields := make(map[string]bool)
	for _, field := range noteType.Fields {
		if field.ContextInclude {
			ctxFields[field.Name] = true
		}
	}
	if len(ctxFields) == 0 {
		return "", nil
	}
	edges := noteOntologyPresentationEdges(ctx, onto, notePath, stringBoolMapKeys(ctxFields), 64)
	if len(edges) == 0 {
		return "", nil
	}

	type target struct {
		path     string
		relation string
	}
	seen := make(map[string]bool)
	var targets []target
	for _, edge := range edges {
		if !ctxFields[edge.RelationName] {
			continue
		}
		targetPath := edge.Target.NotePath
		if targetPath == "" || targetPath == notePath || seen[targetPath] {
			continue
		}
		seen[targetPath] = true
		targets = append(targets, target{path: targetPath, relation: edge.RelationName})
	}
	if len(targets) == 0 {
		return "", nil
	}

	vaultPaths, _ := paths.NewVaultPaths(vaultDef.BasePath())
	const perNoteCap = 8000
	remaining := budget
	blocks := make([]string, 0, len(targets))
	dedupe := renderedNoteDedupe{vaultPaths: vaultPaths, tracker: tracker, items: make([]DedupeItem, 0, len(targets))}
	for _, tgt := range targets {
		if remaining < 400 {
			break
		}
		raw, err := noteMgr.GetContents(vaultDef, tgt.path)
		if err != nil || strings.TrimSpace(raw) == "" {
			continue
		}
		content, truncated := contextpack.TrimMarkdown(raw, min(perNoteCap, remaining))
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		if !dedupe.include(tgt.path, content) {
			continue
		}
		source := "ontology:" + tgt.relation
		blocks = append(blocks, renderEmbeddedFileXMLWithMeta("note", tgt.path, "", source, content, truncated))
		remaining -= len(content) + 120
	}
	if len(blocks) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString("\n\n#### Ontology context (ctx)\n")
	for _, block := range blocks {
		b.WriteString("\n\n")
		b.WriteString(block)
	}
	return b.String(), dedupe.items
}

func noteOntologyPresentationEdges(ctx context.Context, onto *ontologyTextContext, notePath string, relationNames []string, limit int) []noderead.NeighborhoodEdge {
	if onto == nil || onto.Scope == nil || strings.TrimSpace(notePath) == "" {
		return nil
	}
	if !onto.IndexedReadOnly {
		result, err := onto.Scope.Edges(ctx, noderead.NeighborhoodRequest{
			Sources:           []ontology.NodeRef{{NotePath: notePath, Kind: ontology.NodeKindNote}},
			Direction:         noderead.TraversalDirectionOutbound,
			IncludeStructural: true,
			IncludeAmbient:    true,
			RelationNames:     relationNames,
			FirstPerSource:    limit,
		})
		if err != nil {
			return nil
		}
		return result.Edges
	}

	source := ontology.NodeRef{NotePath: notePath, Kind: ontology.NodeKindNote}
	edges := make([]noderead.NeighborhoodEdge, 0)
	relations := relationNames
	if len(relations) == 0 {
		relations = []string{""}
	}
	for _, structural := range []bool{true, false} {
		for _, relation := range relations {
			result, err := onto.Scope.Traverse(ctx, noderead.TraverseRequest{
				Sources:        []ontology.NodeRef{source},
				Relation:       relation,
				Structural:     structural,
				LimitPerSource: 0,
			})
			if err != nil {
				return nil
			}
			for _, row := range result.EdgesBySource[notePath] {
				if strings.TrimSpace(row.SrcNodeID) != "" {
					continue
				}
				edges = append(edges, noderead.NeighborhoodEdge{
					Source:       source,
					Target:       ontology.NodeRef{NotePath: row.DstPath, NodeID: row.DstNodeID, Kind: ontology.NodeKindNote, TypeName: row.DstType},
					Edge:         row,
					Direction:    noderead.TraversalDirectionOutbound,
					RelationName: row.RelationName,
					Provenance:   row.Provenance,
					Structural:   row.Structural,
					TargetType:   row.DstType,
					Depth:        1,
				})
			}
		}
	}
	sort.SliceStable(edges, func(i, j int) bool {
		left, right := edges[i], edges[j]
		if left.Structural != right.Structural {
			return left.Structural
		}
		if left.Provenance != right.Provenance {
			return left.Provenance < right.Provenance
		}
		if left.RelationName != right.RelationName {
			return left.RelationName < right.RelationName
		}
		if left.Target.NotePath != right.Target.NotePath {
			return left.Target.NotePath < right.Target.NotePath
		}
		return left.Target.NodeID < right.Target.NodeID
	})
	if limit > 0 && len(edges) > limit {
		edges = edges[:limit]
	}
	return edges
}

func buildReverseNeighborsForText(nodes map[string]obsidian.GraphNode) map[string][]string {
	if len(nodes) == 0 {
		return nil
	}
	reverse := make(map[string][]string, len(nodes))
	for path := range nodes {
		reverse[path] = nil
	}
	for src, node := range nodes {
		for _, dst := range node.Neighbors {
			reverse[dst] = append(reverse[dst], src)
		}
	}
	for p := range reverse {
		sort.Strings(reverse[p])
	}
	return reverse
}

func topNeighborsByAuthority(paths []string, nodes map[string]obsidian.GraphNode, limit int) []string {
	if len(paths) == 0 || limit == 0 {
		return nil
	}
	type pr struct {
		path string
		val  float64
	}
	list := make([]pr, 0, len(paths))
	for _, p := range paths {
		if n, ok := nodes[p]; ok {
			list = append(list, pr{path: p, val: n.Authority})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].val == list[j].val {
			return list[i].path < list[j].path
		}
		return list[i].val > list[j].val
	})
	if len(list) > limit {
		list = list[:limit]
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, item.path)
	}
	return out
}

func normalizeAbsNotePath(absPath string, vaultPaths paths.VaultPaths) string {
	if vaultPaths.Root() == "" {
		return string(paths.NormalizeNote(absPath))
	}
	if rel, err := vaultPaths.RelNoteStrict(absPath); err == nil && rel.String() != "" {
		return rel.String()
	}
	return string(paths.NormalizeNote(absPath))
}

func noteDedupeKey(path string, vaultPaths paths.VaultPaths) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	normalized := path
	if filepath.IsAbs(path) {
		normalized = normalizeAbsNotePath(path, vaultPaths)
	} else {
		normalized = string(paths.NormalizeNote(path))
	}
	return "note:" + normalized
}

func renderCodeFileContextBlock(noteMgr obsidian.NoteReader, vaultDef obsidian.VaultDefinition, display string, ctx FileContext, perTargetBudget int, profile ContextProfile, tracker DedupeTracker) (string, []DedupeItem) {
	var b strings.Builder
	b.WriteString("### ")
	b.WriteString(display)
	b.WriteString(" (code)\n")

	vaultPaths, _ := paths.NewVaultPaths(vaultDef.BasePath())

	// Track rough budget while rendering (best-effort; final packing will trim).
	remaining := perTargetBudget
	nestedDedupe := renderedNoteDedupe{vaultPaths: vaultPaths, tracker: tracker, items: make([]DedupeItem, 0, len(ctx.LinkedNotes))}

	if len(ctx.AncestorDocs) > 0 {
		docFraction := 0.55
		if ctx.IsDir {
			docFraction = 0.45
		}
		remain := int(float64(perTargetBudget) * docFraction)
		if remain < 800 {
			remain = perTargetBudget
		}
		docBlocks := make([]string, 0, len(ctx.AncestorDocs))
		for _, d := range ctx.AncestorDocs {
			content, truncated := contextpack.TrimMarkdown(d.Content, min(8000, remain))
			content = strings.TrimSpace(content)
			if content == "" {
				continue
			}
			if !nestedDedupe.include(d.Path, content) {
				continue
			}
			remain -= len(content) + 80
			docBlocks = append(docBlocks, renderEmbeddedFileXML("ancestor-doc", d.Path, content, d.Truncated || truncated))
			if remain < 500 {
				break
			}
		}
		if len(docBlocks) > 0 {
			b.WriteString("\n\n#### Docs\n")
			for _, block := range docBlocks {
				b.WriteString("\n\n")
				b.WriteString(block)
			}
			remaining -= int(float64(perTargetBudget) * docFraction)
		}
	}

	// Render submodule docs (from subdirectories when SubmoduleDepth > 0)
	if len(ctx.SubmoduleDocs) > 0 {
		submoduleBudget := min(remaining, max(1000, int(float64(perTargetBudget)*0.22)))
		remain := submoduleBudget
		rendered := 0
		for _, d := range ctx.SubmoduleDocs {
			content, truncated := contextpack.TrimMarkdown(d.Content, min(2200, remain))
			content = strings.TrimSpace(content)
			if content == "" {
				continue
			}
			if !nestedDedupe.include(d.Path, content) {
				continue
			}
			if rendered == 0 {
				b.WriteString("\n\n#### Submodule docs\n")
			}
			b.WriteString("\n\n")
			b.WriteString(renderEmbeddedFileXMLWithDepth("submodule-doc", d.Path, content, d.Depth, d.Truncated || truncated))
			rendered++
			remain -= len(content) + 80
			if remain < 400 {
				break
			}
		}
		if rendered > 0 {
			remaining -= submoduleBudget - max(0, remain)
		}
	}

	if len(ctx.Rationale) > 0 {
		rationaleBudget := min(remaining, max(700, int(float64(perTargetBudget)*0.18)))
		remain := rationaleBudget
		rendered := 0
		for _, r := range ctx.Rationale {
			content := strings.TrimSpace(contextpack.TrimToBudget(r.Content, min(500, remain)))
			if content == "" {
				continue
			}
			if rendered == 0 {
				b.WriteString("\n\n#### Rationale\n")
			}
			b.WriteString("\n\n")
			b.WriteString(renderRationaleXML(ctx.Path, r, content, len(content) < len(strings.TrimSpace(r.Content))))
			rendered++
			remain -= len(content) + 80
			if remain < 180 {
				break
			}
		}
		if rendered > 0 {
			remaining -= rationaleBudget - max(0, remain)
		}
	}

	if len(ctx.OntologyNodes) > 0 || len(ctx.NodeDiagnostics) > 0 {
		b.WriteString("\n\n#### Ontology nodes\n")
		for _, node := range ctx.OntologyNodes {
			nodeBudget := min(2200, max(400, remaining/2))
			before := b.Len()
			b.WriteString("\n\n")
			b.WriteString(renderOntologyNodeContextXML(node, nodeBudget))
			remaining -= b.Len() - before
			if remaining < 400 {
				break
			}
		}
		if len(ctx.NodeDiagnostics) > 0 {
			b.WriteString("\n\n##### Node diagnostics\n")
			for _, diagnostic := range ctx.NodeDiagnostics {
				b.WriteString("\n- ")
				if diagnostic.Input != "" {
					b.WriteString(diagnostic.Input)
					b.WriteString(": ")
				}
				b.WriteString(diagnostic.Code)
				if diagnostic.Message != "" {
					b.WriteString(" — ")
					b.WriteString(diagnostic.Message)
				}
			}
		}
	}

	if len(ctx.LinkedNotes) > 0 {
		b.WriteString("\n\n#### Linked notes\n")
		notes := append([]LinkedNoteContext{}, ctx.LinkedNotes...)
		sort.SliceStable(notes, func(i, j int) bool {
			wi := linkedNoteKindWeight(notes[i].Kind)
			wj := linkedNoteKindWeight(notes[j].Kind)
			if wi != wj {
				return wi > wj
			}
			return notes[i].Path < notes[j].Path
		})
		limit := 25
		if profile == ContextProfileCode {
			limit = 18
		}
		if ctx.IsDir {
			limit = 10
		}
		if len(notes) > limit {
			notes = notes[:limit]
		}

		// Prefer embedding full note bodies. Only fall back to summaries when budget is tight.
		const minNoteBodyBudget = 800
		noteFraction := 0.40
		if ctx.IsDir {
			noteFraction = 0.25
		}
		noteBudget := min(int(float64(perTargetBudget)*noteFraction), remaining)

		type noteBlock struct {
			LinkedNoteContext
			content   string
			truncated bool
		}

		blocks := make([]noteBlock, len(notes))
		for i, note := range notes {
			blocks[i].LinkedNoteContext = note
		}

		embeddedCount := 0
		for i := range blocks {
			if noteMgr == nil || blocks[i].Path == "" {
				continue
			}
			if noteBudget < minNoteBodyBudget {
				break
			}
			raw, err := noteMgr.GetContents(vaultDef, blocks[i].Path)
			if err != nil || strings.TrimSpace(raw) == "" {
				continue
			}
			content, truncated := contextpack.TrimMarkdown(raw, min(16000, noteBudget))
			content = strings.TrimSpace(content)
			if content == "" {
				continue
			}
			if !nestedDedupe.include(blocks[i].Path, content) {
				continue
			}
			blocks[i].content = content
			blocks[i].truncated = truncated
			noteBudget -= len(content) + 120
			embeddedCount++
		}

		for _, nb := range blocks {
			if nb.content == "" {
				continue
			}
			b.WriteString("\n\n")
			b.WriteString(renderEmbeddedFileXMLWithMeta("note", nb.Path, nb.Title, nb.Kind, nb.content, nb.truncated))
		}

		// If we couldn't embed all notes, list remaining as stubs with summary.
		if embeddedCount < len(blocks) {
			b.WriteString("\n\n##### Note stubs\n")
			for _, nb := range blocks {
				if nb.content != "" {
					continue
				}
				b.WriteString("\n- ")
				b.WriteString(nb.Title)
				if nb.Path != "" {
					b.WriteString(" (")
					b.WriteString(nb.Path)
					b.WriteString(")")
				}
				if value, ok := nb.Frontmatter["summary"]; ok {
					if summary := strings.TrimSpace(fmt.Sprint(value)); summary != "" {
						b.WriteString(" — ")
						b.WriteString(summary)
					}
				}
				if nb.Kind != "" {
					b.WriteString(" [")
					b.WriteString(nb.Kind)
					b.WriteString("]")
				}
				if nb.Line > 0 {
					b.WriteString(fmt.Sprintf(":%d", nb.Line))
				}
			}
		}
	}

	if len(ctx.ReturnedNotePaths) > 0 || len(ctx.ReturnedDocPaths) > 0 {
		b.WriteString("\n\n#### Returned paths\n")
		if len(ctx.ReturnedDocPaths) > 0 {
			b.WriteString("\n- docs: ")
			b.WriteString(strings.Join(ctx.ReturnedDocPaths, ", "))
		}
		if len(ctx.ReturnedNotePaths) > 0 {
			b.WriteString("\n- notes: ")
			b.WriteString(strings.Join(ctx.ReturnedNotePaths, ", "))
		}
	}
	if isThinCodeContext(ctx) {
		b.WriteString(renderThinContextWarning(display))
	}

	return strings.TrimSpace(b.String()), nestedDedupe.items
}

func chooseVaultContextFileBudget(totalBudget int, fileCount int) int {
	if totalBudget <= 0 || fileCount <= 0 {
		return 0
	}
	available := totalBudget - 420
	if available < 2400 {
		return 0
	}
	minOverview := 2600
	if available <= minOverview {
		return 0
	}
	wanted := max(2200*fileCount, available/2)
	limit := available - minOverview
	if wanted > limit {
		wanted = limit
	}
	return max(0, wanted)
}

func stripRenderedContextHeader(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "# Rhizome context\n") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 3 && lines[0] == "# Rhizome context" && lines[1] == "" {
			sawBullet := false
			for i := 2; i < len(lines); i++ {
				switch {
				case strings.HasPrefix(lines[i], "- "):
					sawBullet = true
				case lines[i] == "" && sawBullet:
					return strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
				default:
					i = len(lines)
				}
			}
		}
	}

	candidates := []string{"\n\n### ", "\n\n## "}
	idx := -1
	for _, marker := range candidates {
		markerIdx := strings.Index(trimmed, marker)
		if markerIdx >= 0 && (idx == -1 || markerIdx < idx) {
			idx = markerIdx
		}
	}
	if idx < 0 {
		return trimmed
	}
	return strings.TrimSpace(trimmed[idx+2:])
}

func linkedNoteKindWeight(kind string) int {
	switch {
	case kind == "wikilink":
		return 100
	case kind == "mention":
		return 95
	case strings.HasPrefix(kind, "anchor"):
		return 90
	case strings.HasPrefix(kind, "linkedFrom"):
		return 60
	default:
		return 10
	}
}

func renderCommunitiesBlock(noteMgr obsidian.NoteReader, analysis *obsidian.GraphAnalysis, defaults VaultContextSummaryDefaults) string {
	if analysis == nil || len(analysis.Communities) == 0 {
		return ""
	}

	// Filter out tiny communities (≤2 notes) and test/fixture communities.
	const minCommunitySize = 3
	filtered := make([]obsidian.CommunitySummary, 0, len(analysis.Communities))
	for _, c := range analysis.Communities {
		if len(c.Nodes) < minCommunitySize {
			continue
		}
		// Skip communities whose anchor is in testdata/mocks/fixtures.
		anchorPath := c.Anchor
		if anchorPath == "" && len(c.TopAuthority) > 0 {
			anchorPath = c.TopAuthority[0].Path
		}
		if anchorPath != "" && isTestOrFixturePath(anchorPath) {
			continue
		}
		filtered = append(filtered, c)
	}
	if len(filtered) == 0 {
		return ""
	}

	maxComms := defaults.MaxCommunities
	if maxComms <= 0 || maxComms > len(filtered) {
		maxComms = len(filtered)
	}

	var b strings.Builder
	b.WriteString("## Communities\n")

	for i := 0; i < maxComms; i++ {
		c := filtered[i]
		b.WriteString("\n\n### ")
		if c.Anchor != "" {
			b.WriteString(c.Anchor)
		} else {
			b.WriteString(c.ID)
		}
		b.WriteString(fmt.Sprintf(" (%d notes)\n", len(c.Nodes)))

		// Compact one-line stats (density + authority range)
		var stats []string
		if c.Density > 0 {
			stats = append(stats, fmt.Sprintf("density=%.2f", c.Density))
		}
		if c.AuthorityStats != nil && c.AuthorityStats.Max > 0 {
			stats = append(stats, fmt.Sprintf("authority: %.2f–%.2f", c.AuthorityStats.Mean, c.AuthorityStats.Max))
		}
		if c.Recency != nil && c.Recency.RecentCount > 0 {
			stats = append(stats, fmt.Sprintf("recent(30d): %d", c.Recency.RecentCount))
		}
		if len(stats) > 0 {
			b.WriteString("_")
			b.WriteString(strings.Join(stats, " | "))
			b.WriteString("_\n")
		}

		// Top notes (inline, more compact)
		top := c.TopAuthority
		if defaults.CommunityTopNotes > 0 && len(top) > defaults.CommunityTopNotes {
			top = top[:defaults.CommunityTopNotes]
		}
		if len(top) > 0 {
			for _, n := range top {
				title := titleFromPath(n.Path)
				summary := blessedSummary(noteMgr, n.Path)
				b.WriteString("- ")
				b.WriteString(title)
				b.WriteString(" (")
				b.WriteString(n.Path)
				b.WriteString(")")
				if summary != "" {
					b.WriteString(" — ")
					// Truncate long summaries
					if len(summary) > 100 {
						summary = summary[:97] + "…"
					}
					b.WriteString(summary)
				}
				b.WriteString("\n")
			}
		}

		// Top tags (inline, compact)
		if len(c.TopTags) > 0 && defaults.CommunityTopTags != 0 {
			tags := c.TopTags
			if defaults.CommunityTopTags > 0 && len(tags) > defaults.CommunityTopTags {
				tags = tags[:defaults.CommunityTopTags]
			}
			if len(tags) > 0 {
				b.WriteString("Tags: ")
				for idx, t := range tags {
					if idx > 0 {
						b.WriteString(", ")
					}
					b.WriteString(t.Tag)
				}
				b.WriteString("\n")
			}
		}
	}

	return strings.TrimSpace(b.String())
}

func findKeyNotesByPatterns(vault obsidian.VaultManager, noteMgr obsidian.NoteReader, patterns []string) ([]KeyNoteMatch, error) {
	var out []KeyNoteMatch
	seen := make(map[string]struct{})
	for _, pat := range patterns {
		args := strings.Fields(strings.TrimSpace(pat))
		if len(args) == 0 {
			continue
		}
		inputs, expr, err := ParseInputsWithExpression(args)
		if err != nil {
			return nil, err
		}
		matches, err := ListFiles(vault, noteMgr, ListParams{
			Inputs:      inputs,
			Expression:  expr,
			MaxDepth:    0,
			SkipAnchors: false,
			SkipEmbeds:  false,
		})
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			norm := string(paths.NormalizeNote(m))
			if _, ok := seen[norm]; ok {
				continue
			}
			seen[norm] = struct{}{}
			out = append(out, KeyNoteMatch{Path: norm, Pattern: pat})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Pattern < out[j].Pattern
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func renderMOCs(patterns []string, mocs []KeyNoteMatch) string {
	var b strings.Builder
	b.WriteString("## Key patterns\n\n")
	for _, p := range patterns {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("\n## Matches\n")
	for _, m := range mocs {
		b.WriteString("\n- ")
		b.WriteString(m.Path)
		if m.Pattern != "" {
			b.WriteString(" (")
			b.WriteString(m.Pattern)
			b.WriteString(")")
		}
	}
	return strings.TrimSpace(b.String())
}

// repoDocMaxTiersDefault controls how many tiers of CONTEXT.md files are included in vault_context
// when compression is disabled. Tier 0 = root CONTEXT.md, Tier 1 = first level where non-root
// CONTEXT.md appears, etc. Default 1 means: root + first code tier only.
const repoDocMaxTiersDefault = 1

// repoDocMaxTiersWithCompression is used when compression is enabled. Walk deeper into the tree
// to collect more module docs, which compression will synthesize into a dense overview.
const repoDocMaxTiersWithCompression = 3

// vaultContextIntent is the hardcoded intent for vault_context compression.
// This helps the LLM produce an excellent, token-dense repository overview.
const vaultContextIntent = "Produce a token-dense repository overview for an AI agent. " +
	"Prioritize: (1) key subsystems and their responsibilities, (2) filesystem organization and module boundaries, " +
	"(3) important patterns/conventions, (4) entry points and extension patterns. " +
	"Omit boilerplate, installation instructions, and content that doesn't help navigate or understand the codebase."

// isTestOrFixturePath returns true if the path looks like test/mock/fixture data.
func isTestOrFixturePath(rel string) bool {
	lower := strings.ToLower(rel)
	for _, marker := range []string{"testdata/", "test_data/", "mocks/", "mock/", "fixtures/", "fixture/", "__tests__/", "__mocks__/"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// pathDepth returns the number of path segments (slashes) in a relative path.
func pathDepth(rel string) int {
	if rel == "" || rel == "." {
		return 0
	}
	return strings.Count(filepath.ToSlash(rel), "/")
}

func buildRepoDocBlocks(ctx context.Context, projectRoot string, docPatterns []string, localCfg *localCfgInfo, vaultDef obsidian.VaultDefinition, tracker DedupeTracker, maxTiers int) ([]contextpack.Piece, map[string][]DedupeItem, error) {
	phaseCtx := indexingperf.WithPhase(ctx, indexingperf.AgentStartPhaseRepoDocInventory)
	done := indexingperf.StartSpan(phaseCtx, indexingperf.AgentStartPhaseRepoDocInventory)
	defer done(nil)
	if projectRoot == "" {
		return nil, nil, nil
	}
	docPatterns = obsidian.NormalizeDocPatterns(docPatterns)
	vaultPaths, _ := paths.NewVaultPaths(projectRoot)

	var allDocs []string
	var codeFileDirs []string // directories containing code files (for scoring)

	codeScan, codeIgnore := codeGlobs(localCfg)
	if len(codeScan) == 0 {
		// Reasonable fallback for repo docs ranking.
		codeScan = codepatterns.DefaultScanGlobs()
	}
	userExcludes := append([]string{}, codeIgnore...)
	if len(vaultDef.Excludes) > 0 {
		userExcludes = append(userExcludes, vaultDef.Excludes...)
	}
	matcher := ignore.LoadUnifiedMatcher(projectRoot, userExcludes)

	// Single walk collects both docs and code file directories.
	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		relPath, relErr := vaultPaths.RelStrict(path)
		if relErr != nil {
			return nil
		}
		rel := relPath.String()
		if d.IsDir() {
			if matcher.IsIgnoredShallow(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if matcher.IsIgnoredShallow(rel, false) {
			return nil
		}
		if obsidian.MatchDocPatternNormalized(rel, docPatterns) {
			allDocs = append(allDocs, path)
		}
		if matchesAny(rel, codeScan) {
			codeFileDirs = append(codeFileDirs, filepath.Clean(filepath.Dir(path)))
		}
		return nil
	})
	indexingperf.AddCount(phaseCtx, indexingperf.AgentStartOpRepoWalks, 1)
	if err != nil {
		return nil, nil, err
	}
	if len(allDocs) == 0 {
		return nil, nil, nil
	}

	// Filter docs by depth tier: include root (depth 0) and up to repoDocMaxTiers
	// levels beyond the first non-root tier where docs appear.
	// Also filter out testdata/mocks paths.
	var docs []string
	minNonRootDepth := -1
	for _, d := range allDocs {
		rel := ""
		if relPath, err := vaultPaths.RelStrict(d); err == nil {
			rel = relPath.String()
		}
		if isTestOrFixturePath(rel) {
			continue
		}
		depth := pathDepth(filepath.Dir(rel)) // depth of the directory containing the doc
		if depth > 0 && (minNonRootDepth < 0 || depth < minNonRootDepth) {
			minNonRootDepth = depth
		}
	}
	maxDepth := maxTiers
	if minNonRootDepth > 0 {
		maxDepth = minNonRootDepth + maxTiers - 1
	}
	for _, d := range allDocs {
		rel := ""
		if relPath, err := vaultPaths.RelStrict(d); err == nil {
			rel = relPath.String()
		}
		if isTestOrFixturePath(rel) {
			continue
		}
		depth := pathDepth(filepath.Dir(rel))
		if depth <= maxDepth {
			docs = append(docs, d)
		}
	}
	if len(docs) == 0 {
		return nil, nil, nil
	}

	docDirSet := make(map[string]struct{}, len(docs))
	for _, d := range docs {
		docDirSet[filepath.Clean(filepath.Dir(d))] = struct{}{}
	}

	// Compute code counts by iterating collected code file directories.
	codeCounts := make(map[string]int, len(docDirSet))
	for _, codeDir := range codeFileDirs {
		// Attribute this code file to any ancestor doc dir(s).
		dir := codeDir
		for {
			if _, ok := docDirSet[dir]; ok {
				codeCounts[dir]++
			}
			if dir == projectRoot || dir == filepath.Dir(dir) {
				break
			}
			dir = filepath.Dir(dir)
		}
	}

	type scored struct {
		path  string
		score float64
	}
	scoredDocs := make([]scored, 0, len(docs))
	for _, d := range docs {
		dir := filepath.Clean(filepath.Dir(d))
		score := math.Log1p(float64(codeCounts[dir]))
		// Prefer root docs slightly.
		if filepath.Clean(dir) == filepath.Clean(projectRoot) {
			score += 5
		}
		scoredDocs = append(scoredDocs, scored{path: d, score: score})
	}
	sort.Slice(scoredDocs, func(i, j int) bool {
		if scoredDocs[i].score == scoredDocs[j].score {
			return scoredDocs[i].path < scoredDocs[j].path
		}
		return scoredDocs[i].score > scoredDocs[j].score
	})

	var pieces []contextpack.Piece
	nestedDedupe := make(map[string][]DedupeItem)
	pieces = append(pieces, contextpack.Piece{
		Key:      "repo-docs-header",
		Priority: 91,
		Score:    math.MaxFloat64,
		Text:     "## Repo docs",
	})
	for _, s := range scoredDocs {
		body, err := os.ReadFile(s.path)
		if err != nil {
			continue
		}
		content, _ := contextpack.TrimMarkdown(string(body), 16000)
		rel := filepath.ToSlash(s.path)
		if relPath, err := vaultPaths.RelStrict(s.path); err == nil && relPath.String() != "" {
			rel = relPath.String()
		}
		content = strings.TrimSpace(content)
		dedupeKey := noteDedupeKey(rel, vaultPaths)
		fp := pieceFingerprint(content)
		if tracker != nil && dedupeKey != "" && fp != "" && tracker.Seen(dedupeKey, fp) {
			continue
		}
		text := renderEmbeddedFileXML("repo-doc", rel, content, false)
		pieceKey := "doc:" + rel
		pieces = append(pieces, contextpack.Piece{
			Key:      pieceKey,
			Priority: 90,
			Score:    s.score,
			Text:     text,
		})
		if dedupeKey != "" && fp != "" {
			nestedDedupe[pieceKey] = append(nestedDedupe[pieceKey], DedupeItem{Key: dedupeKey, Fingerprint: fp})
		}
	}
	return pieces, nestedDedupe, nil
}

func codeGlobs(localCfg *localCfgInfo) (scan []string, ignore []string) {
	if localCfg == nil || localCfg.cfg == nil {
		return nil, obsidian.DefaultCodeConfig.Ignore
	}
	scan, ignore = obsidian.NormalizeCodeRefPatterns(*localCfg.cfg)
	if len(ignore) == 0 {
		ignore = append(ignore, obsidian.DefaultCodeConfig.Ignore...)
	}
	return scan, ignore
}

func matchesAny(rel string, globs []string) bool {
	for _, pat := range globs {
		ok, _ := doublestar.Match(pat, rel)
		if ok {
			return true
		}
	}
	return false
}
