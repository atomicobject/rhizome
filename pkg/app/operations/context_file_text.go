package operations

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	vaultgraph "github.com/atomicobject/rhizome/pkg/graph"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

type FileContextTextResult = VaultContextTextResult

func BuildFileContextText(vault repoconfig.VaultManager, noteMgr obsidian.NoteReader, params FileContextTextParams) (string, error) {
	result, err := BuildFileContextTextResult(vault, noteMgr, params)
	return result.Text, err
}

func BuildFileContextTextResult(vault repoconfig.VaultManager, noteMgr obsidian.NoteReader, params FileContextTextParams) (FileContextTextResult, error) {
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

func buildFileContextTextResultPending(vault repoconfig.VaultManager, noteMgr obsidian.NoteReader, params FileContextTextParams) (FileContextTextResult, *contextDelivery, error) {
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

func effectiveContextBudget(budget int) int {
	if budget > 0 {
		return budget
	}
	return repoconfig.DefaultBudgetChars
}

func buildFileContextText(vault repoconfig.VaultManager, noteMgr obsidian.NoteReader, params FileContextTextParams) (string, *contextDelivery, error) {
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

	var analysis *vaultgraph.GraphAnalysis
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
		ontoCtx = loadIndexedOntologyTextContext(ctx, vaultDef, noteMgr, params.NoteMetadata, params.SessionStore)
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
		nodeScope = service.NewScope(noderead.ScopeOptions{})
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
				shown := noteContextShownPaths(ctxDedupe, params.ExcludeNotePaths, vaultPaths)
				if anchored := anchoredNotesForNote(ctx, codeAnchor, abs, norm, vaultDef, vaultPaths, params.AnchorKinds, params.IndexedReadOnly, shown); len(anchored) > 0 {
					anchored = expandLinkedNoteFrontmatter(anchored, noteMgr)
					limit := 25
					if profile == ContextProfileCode {
						limit = 18
					}
					anchorDedupe := renderedNoteDedupe{vaultPaths: vaultPaths, tracker: params.Dedupe}
					var anchoredBlock strings.Builder
					renderLinkedNotesSection(&anchoredBlock, "Anchored notes", noteMgr, vaultDef, anchored, limit, perTarget-len(block), &anchorDedupe)
					block += anchoredBlock.String()
					ctxDedupe = append(ctxDedupe, anchorDedupe.items...)
				}
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
