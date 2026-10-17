package unifiedsearch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/presentation"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	searchplanner "github.com/atomicobject/rhizome/pkg/search/planner"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func Run(ctx context.Context, opts Options) (Result, error) {
	// Docs: [[unified-search-answer-architecture#^spec-0035-us1-ac2]] owns this boundary:
	// normalize inputs, prepare stores/providers, plan retrieval, then adapt ranked
	// candidates into answer packets without moving retrieval into the answer layer.
	queryInputs := opts.QueryInputs
	if len(queryInputs) == 0 {
		queryInputs = NormalizeQueryInputs(opts.Query, opts.Queries, opts.IntentInput)
	}
	queryInputs = NormalizeExplicitQueryInputs(queryInputs, opts.IntentInput)
	query := JoinQueryInputs(queryInputs)
	seedTokens := MergeSeedTokens(opts.Seeds, opts.Files)
	if query == "" && len(seedTokens) == 0 {
		return Result{}, errors.New("provide a query and/or one or more --seed values")
	}
	formats := opts.NoteFormats
	if len(formats.Registry().IDs()) == 0 {
		return Result{}, errors.New("search requires an explicit note format runtime")
	}

	resources, err := openSearchResources(ctx, opts)
	if err != nil {
		return Result{}, err
	}
	cleanups := resources.cleanups
	fail := func(err error) (Result, error) { cleanups.Close(); return Result{}, err }
	codeStore, noteProvider, codeProvider, intelStore := resources.codeStore, resources.noteProvider, resources.codeProvider, resources.intelStore
	resolved := resources.config
	var generationBefore string
	generationUnavailable := false
	var generationUnavailableReason error
	if intelStore != nil || codeStore != nil {
		generationBefore, err = IndexGeneration(ctx, intelStore, nil, nil)
		if err != nil {
			if errors.Is(err, ErrIndexGenerationUnavailable) {
				generationUnavailable = true
				generationUnavailableReason = err
				generationBefore = ""
			} else {
				return fail(err)
			}
		}
	}
	generationWarning := func(warnings []search.Warning) []search.Warning {
		if !generationUnavailable {
			return warnings
		}
		message := "The visible index has no committed generation; continuation is unavailable for this response."
		if generationUnavailableReason != nil {
			message += " " + generationUnavailableReason.Error()
		}
		return append(warnings, search.Warning{Code: "index_generation_unavailable", Kind: "index_state", Source: "unifiedsearch", Message: message})
	}
	stableGeneration := func() (string, error) {
		if generationBefore == "" {
			return "", nil
		}
		freshnessCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		after, err := IndexGeneration(freshnessCtx, intelStore, nil, nil)
		if err != nil {
			return "", err
		}
		if after != generationBefore {
			return "", fmt.Errorf("%w: index changed during search", ErrCursorStale)
		}
		return after, nil
	}

	seedTokens, err = canonicalizeRawSeedPaths(seedTokens, opts.VaultPath)
	if err != nil {
		return fail(err)
	}

	intentInput := strings.TrimSpace(opts.IntentInput)
	intent := search.Intent(intentInput)
	if intentInput != "" && searchplanner.ValidateIntent(intentInput) != nil {
		intent = InferDefaultIntent(intentInput, seedTokens).Intent
	}
	if intent == "" {
		if len(seedTokens) > 0 && query == "" {
			intent = search.IntentRelatedToSeed
		} else {
			intent = search.IntentSearch
		}
	}
	if err := searchplanner.ValidateIntent(string(intent)); err != nil {
		return fail(err)
	}

	corpus, err := loadRequestCorpus(ctx, opts, queryInputs, seedTokens, intent, intelStore)
	if err != nil {
		return fail(fmt.Errorf("load search corpus: %w", err))
	}
	pathKinds := opts.PathKinds
	if corpus != nil {
		pathKinds = search.MergePathKinds(pathKinds, corpus.PathKinds)
	}
	seeds, err := resolveSeedHandlesWithCorpus(ctx, seedTokens, pathKinds, intelStore, opts.SeedLimit, corpus)
	if err != nil {
		return fail(err)
	}
	pathKinds = search.MergePathKinds(pathKinds, search.PathKindsFromHandles(seeds))

	explicitSeedPaths := search.ExplicitSeedPathsFromHandles(seeds)
	hasNoteSeeds := false
	for _, s := range seeds {
		if s.Kind == knowledge.KindNote || s.Kind == knowledge.KindNoteChunk || s.Kind == knowledge.KindNodeChunk {
			hasNoteSeeds = true
			break
		}
	}

	maxPerOwner := opts.MaxPerOwner
	if maxPerOwner <= 0 {
		maxPerOwner = 3
	}

	noteMgr := obsidian.NoteReader(&obsidian.Note{})

	searcher := semantic.Searcher{
		CodeProvider: codeProvider,
		NoteProvider: noteProvider,
		IntelStore:   intelStore,
		VaultDef:     opts.VaultDef,
		NoteReader:   noteMgr,
	}

	var codeIndexReader presentation.ChunkBodyReader
	if codeStore != nil {
		codeIndexReader = presentation.AdaptCodeIndex(codeStore)
	}
	plannerDeps := searchplanner.Deps{
		NoteFormats: formats,
		Corpus:      corpus,
		Semantic:    &searcher,
		IntelStore:  intelStore,

		VaultPath:      opts.VaultPath,
		VaultDef:       opts.VaultDef,
		NoteReader:     noteMgr,
		OntologySchema: loadOntologySchemaBestEffort(opts.VaultPath),
		DocPatterns:    resolved.FileContext.DocPatterns,
	}
	plannerDeps.Packer = &presentation.DefaultPacker{
		VaultPath: opts.VaultPath, CodeRoot: opts.VaultPath, Intel: intelStore, CodeIndex: codeIndexReader,
		NoteStore: intelStore, NoteFormats: formats,
		UseFTSBody: opts.UseFTSBody, VaultDef: opts.VaultDef, NoteReader: noteMgr, OntologySchema: plannerDeps.OntologySchema,
	}

	plannerOptions := searchplanner.Options{
		EnableVector: opts.UseVector,
		EnableIntel:  opts.UseIntel,
		EnableGraph:  opts.UseGraph && hasNoteSeeds,
		EnableRefs:   opts.UseRefs,
		MaxPerOwner:  maxPerOwner,

		DirectorySeedExpansion: opts.DirectorySeedExpansion,
		GraphSource:            opts.GraphSource,
	}

	request := requestSearch{
		Planner:   searchplanner.Planner{Deps: plannerDeps, Options: plannerOptions},
		VaultPath: opts.VaultPath, DeferPacking: opts.DeferPack || len(queryInputs) > 1,
		Spec: search.QuerySpec{Seeds: seeds, ExplicitSeedPaths: explicitSeedPaths, PathKinds: pathKinds, HasExplicitSeeds: len(explicitSeedPaths) > 0, Filters: opts.Filters, Limits: search.Limits{Total: opts.Limit}},
	}
	if request.Spec.Limits.Total <= 0 {
		request.Spec.Limits.Total = 25
	}
	if opts.Pack {
		budget := opts.BudgetChars
		if budget <= 0 {
			budget = 6000
		}
		request.Spec.Budget = search.Budget{Chars: budget}
	}

	if len(queryInputs) > 1 {
		result := Result{
			VaultPath:         opts.VaultPath,
			Intent:            intent,
			IntelStore:        intelStore,
			VectorUnavailable: opts.UseVector && intelStore == nil,
			Cleanup:           cleanups.Close,
		}
		responses, warnings, err := runMultiQuery(ctx, queryInputs, intent, request.buildSpec, request.search)
		if err != nil {
			return fail(err)
		}
		result.Warnings = generationWarning(warnings)
		result.Results, err = filterResultsByNoteType(ctx, intelStore, searchapplication.AggregateFacets(responses, opts.Limit), opts.Filters.NoteTypes)
		if err != nil {
			return fail(err)
		}
		result.Lanes = aggregateMultiQueryLanes(responses)
		if opts.Pack {
			for _, response := range responses {
				if response.DeferredPacker != nil {
					result.DeferredPacker = response.DeferredPacker
					result.PackSpec = response.Query
					break
				}
			}
			if !opts.DeferPack && result.DeferredPacker != nil {
				packed, packErr := search.PackRankedResults(ctx, result.DeferredPacker, result.PackSpec, result.Results)
				if packErr != nil {
					return fail(packErr)
				}
				result.PackedText = packed.Text
				result.DeferredPacker = nil
			}
		}
		result.Display = CoalesceNoteResults(result.Results)
		result.IndexGeneration, err = stableGeneration()
		if err != nil {
			return fail(err)
		}
		return result, nil
	}

	spec, warnings := request.buildSpec(ctx, query, intent)
	intent = spec.Intent
	if search.ShouldBlockPrecisionFallback(spec) {
		generation, generationErr := stableGeneration()
		if generationErr != nil {
			return fail(generationErr)
		}
		return Result{
			VaultPath:         opts.VaultPath,
			Intent:            intent,
			IntelStore:        intelStore,
			Results:           targetCandidateResults(ctx, intelStore, opts.Filters, spec.TargetCandidates),
			Warnings:          generationWarning(warnings),
			TargetStatus:      spec.TargetStatus,
			TargetConfidence:  spec.ResolutionConfidence,
			TargetCandidates:  append([]search.TargetCandidate(nil), spec.TargetCandidates...),
			ResolvedTarget:    cloneTargetCandidate(spec.ResolvedTarget),
			IndexGeneration:   generation,
			VectorUnavailable: opts.UseVector && intelStore == nil,
			Cleanup:           cleanups.Close,
		}, nil
	}

	resp, err := request.search(ctx, spec)
	if err != nil {
		return fail(err)
	}

	result := Result{
		VaultPath:         opts.VaultPath,
		Intent:            intent,
		IntelStore:        intelStore,
		Warnings:          generationWarning(append(warnings, resp.Warnings...)),
		Lanes:             resp.Lanes,
		TargetStatus:      spec.TargetStatus,
		TargetConfidence:  spec.ResolutionConfidence,
		TargetCandidates:  append([]search.TargetCandidate(nil), spec.TargetCandidates...),
		ResolvedTarget:    cloneTargetCandidate(spec.ResolvedTarget),
		VectorUnavailable: opts.UseVector && intelStore == nil,
		Cleanup:           cleanups.Close,
	}

	if opts.Pack && resp.Packed != nil {
		result.PackedText = resp.Packed.Text
	}
	if opts.Pack && resp.DeferredPacker != nil {
		result.DeferredPacker = resp.DeferredPacker
		result.PackSpec = resp.Query
	}

	result.Results, err = filterResultsByNoteType(ctx, intelStore, resp.Results, opts.Filters.NoteTypes)
	if err != nil {
		return fail(err)
	}
	result.Display = CoalesceNoteResults(result.Results)
	result.IndexGeneration, err = stableGeneration()
	if err != nil {
		return fail(err)
	}
	return result, nil
}
