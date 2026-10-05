package unifiedsearch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	stdpath "path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/answer"
	answeradapt "github.com/atomicobject/rhizome/pkg/app/answer/adapt"
	"github.com/atomicobject/rhizome/pkg/app/presentation"
	"github.com/atomicobject/rhizome/pkg/app/searchengine"
	"github.com/atomicobject/rhizome/pkg/app/semanticops"
	searchapplication "github.com/atomicobject/rhizome/pkg/app/unifiedsearch/application"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeembsql "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
	searchintent "github.com/atomicobject/rhizome/pkg/search/intent"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	searchplanner "github.com/atomicobject/rhizome/pkg/search/planner"
	"github.com/atomicobject/rhizome/pkg/search/queryframe"
	"github.com/atomicobject/rhizome/pkg/search/retrieval"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

var ErrTimedOutBeforePlanning = errors.New("search timed out before planning completed")

type Options struct {
	Query       string
	Queries     []string
	QueryInputs []QueryInput
	Seeds       []string
	Files       []string
	IntentInput string
	Limit       int
	Pack        bool
	// DeferPack leaves body hydration to the application layer after it selects
	// the visible canonical page. Direct Run callers retain immediate packing.
	DeferPack   bool
	BudgetChars int
	MaxPerOwner int
	SeedLimit   int
	UseVector   bool
	UseIntel    bool
	UseGraph    bool
	UseRefs     bool
	UseFTSBody  bool
	Filters     search.Filters
	// PathKinds is caller-supplied configured ownership for raw seed paths.
	// Persisted ownership is merged in when the Intel store is available.
	PathKinds map[string]search.PathKind
	// DirectorySeedExpansion and GraphSource let indexed-only adapters forbid
	// filesystem fallback. Zero values keep the default permissive behavior.
	DirectorySeedExpansion searchplanner.DirectorySeedExpansionPolicy
	GraphSource            retrieval.GraphSourcePolicy
	VaultPath              string
	VaultDef               obsidian.VaultDefinition
	EmbCfg                 embeddings.Config
	ProviderAPIKey         string
	IntelStore             *semdb.Store
	NoteProvider           embeddings.Provider
	NoteProviderConfig     embeddings.ProviderConfig
	CodeProvider           embeddings.Provider
	CodeProviderConfig     embeddings.ProviderConfig
}

type QueryInput struct {
	Text string
	Mode string
}

type Result struct {
	VaultPath         string
	Intent            search.Intent
	IntelStore        *semdb.Store
	Results           []search.RankedResult
	Display           []DisplayItem
	Answer            answer.Response
	PackedText        string
	DeferredPacker    search.Packer
	PackSpec          search.QuerySpec
	Warnings          []search.Warning
	Lanes             []search.LaneStatus
	TargetStatus      search.TargetStatus
	TargetConfidence  float64
	TargetCandidates  []search.TargetCandidate
	ResolvedTarget    *search.TargetCandidate
	IndexGeneration   string
	VectorUnavailable bool
	Cleanup           func()
}

type DisplayItem struct {
	Primary       search.RankedResult
	NoteChunks    []search.RankedResult
	Anchor        *codeanchor.IntelAnchor
	ModuleExports []string
}

type cleanupStack struct {
	fns []func()
}

func (c *cleanupStack) Add(fn func()) {
	if fn == nil {
		return
	}
	c.fns = append(c.fns, fn)
}

func (c *cleanupStack) Close() {
	for i := len(c.fns) - 1; i >= 0; i-- {
		c.fns[i]()
	}
	c.fns = nil
}

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
	// Root search owns direct provider/store construction, not an agent JSON
	// runtime, so flags cannot become a second, implicit runtime-policy switch.
	// These are the capabilities oneshotruntime.UnifiedSearchPlan declares; they
	// stay inline because oneshotruntime and bootstrap import the MCP adapter,
	// which now calls Execute from this package.
	usesVectorProvider := opts.UseVector
	usesIntelStore := opts.UseIntel || opts.UseRefs || opts.UseVector

	cleanups := &cleanupStack{}
	fail := func(err error) (Result, error) {
		cleanups.Close()
		return Result{}, err
	}

	var (
		noteStore       *sqlite.Store
		codeStore       *codeembsql.Store
		noteProvider    = opts.NoteProvider
		noteCfg         = opts.NoteProviderConfig
		codeProvider    = opts.CodeProvider
		codeProviderCfg = opts.CodeProviderConfig
		intelStore      = opts.IntelStore
	)

	if usesVectorProvider && opts.EmbCfg.Enabled {
		provider, providerCfg, err := prepareProvider(opts.EmbCfg, opts.ProviderAPIKey)
		if err != nil {
			return fail(err)
		}
		noteProvider = provider
		noteCfg = providerCfg
		if closer, ok := provider.(io.Closer); ok {
			cleanups.Add(func() { _ = closer.Close() })
		}
		store, err := sqlite.OpenWithMetadata(ctx, opts.EmbCfg.IndexPath, provider, embeddings.MetadataForProvider(provider, providerCfg))
		if err != nil {
			var metaErr embeddings.MetadataError
			if errors.As(err, &metaErr) {
				return fail(fmt.Errorf("semantic index metadata mismatch at %s: %w", opts.EmbCfg.IndexPath, metaErr))
			}
			return fail(err)
		}
		noteStore = store
		cleanups.Add(func() { _ = noteStore.Close() })
	}

	codeEmbCfg, _, err := obsidian.EffectiveCodeEmbeddingsConfig(opts.VaultPath, opts.EmbCfg)
	if err != nil {
		return fail(err)
	}

	if codeProvider == nil && usesVectorProvider && codeEmbCfg.Enabled {
		if _, statErr := os.Stat(codeEmbCfg.IndexPath); statErr == nil {
			apiKey := strings.TrimSpace(opts.ProviderAPIKey)
			if apiKey == "" {
				apiKey = embeddings.ResolveAPIKeyForProvider(codeEmbCfg.Provider)
			}
			providerCfg := codeEmbCfg.ProviderCfg(apiKey)
			var provider embeddings.Provider
			if noteProvider != nil && providerConfigsMatch(providerCfg, noteCfg) {
				// Reuse the note provider when configs match so one search request
				// does not create duplicate provider clients/rate-limit buckets for
				// code and note embeddings.
				provider = noteProvider
				providerCfg = noteCfg
			} else {
				provider, providerCfg, err = prepareProvider(codeEmbCfg, apiKey)
				if err != nil {
					return fail(err)
				}
				if closer, ok := provider.(io.Closer); ok {
					cleanups.Add(func() { _ = closer.Close() })
				}
			}
			codeProvider = provider
			codeProviderCfg = providerCfg
			store, err := codeembsql.OpenWithMetadata(ctx, codeEmbCfg.IndexPath, codeProvider, embeddings.MetadataForProvider(codeProvider, providerCfg))
			if err != nil {
				var metaErr embeddings.MetadataError
				if errors.As(err, &metaErr) {
					return fail(fmt.Errorf("code embeddings metadata mismatch at %s: %w", codeEmbCfg.IndexPath, metaErr))
				}
				return fail(fmt.Errorf("open code embeddings index: %w", err))
			}
			codeStore = store
			cleanups.Add(func() { _ = codeStore.Close() })
		}
	}

	codeCfg, codeErr := obsidian.LoadCodeConfig(opts.VaultPath)
	if intelStore == nil && codeErr == nil && codeCfg.IndexPath != "" && usesIntelStore {
		requireEnabled := codeCfg.Enabled
		if opts.EmbCfg.Enabled || codeEmbCfg.Enabled {
			requireEnabled = false
		}
		if store, cleanup, err := obsidian.OpenIntelStoreFromConfigIfVaultPresent(opts.VaultPath, codeCfg, requireEnabled); err == nil && store != nil {
			intelStore = store
			cleanups.Add(cleanup)
		}
	}
	var generationBefore string
	generationUnavailable := false
	var generationUnavailableReason error
	if intelStore != nil || noteStore != nil || codeStore != nil {
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

	var pathKinds map[string]search.PathKind
	if opts.PathKinds != nil {
		pathKinds = mergeSearchPathKinds(nil, opts.PathKinds)
	}
	if needsPathKindHydration(seedTokens, pathKinds) {
		pathKinds, err = hydrateSearchPathKinds(ctx, intelStore, pathKinds)
		if err != nil {
			return fail(err)
		}
	}
	seeds, err := ResolveSeedHandlesWithPathKinds(ctx, seedTokens, pathKinds, intelStore, opts.SeedLimit)
	if err != nil {
		return fail(err)
	}
	pathKinds = mergeSearchPathKinds(pathKinds, search.PathKindsFromHandles(seeds))
	explicitSeedPaths := search.ExplicitSeedPathsFromHandles(seeds)
	hasNoteSeeds := false
	for _, s := range seeds {
		if s.Kind == knowledge.KindNote || s.Kind == knowledge.KindNoteChunk || s.Kind == knowledge.KindNodeChunk {
			hasNoteSeeds = true
			break
		}
	}

	intentInput := strings.TrimSpace(opts.IntentInput)
	intent := search.Intent(intentInput)
	if intentInput != "" {
		if err := searchplanner.ValidateIntent(intentInput); err != nil {
			detected, ok, derr := InferIntentFromText(ctx, intentInput, intelStore, noteProvider, noteCfg, codeProvider, codeProviderCfg)
			if derr != nil {
				return fail(derr)
			}
			if ok {
				intent = detected
			} else {
				intent = ""
			}
		}
	}
	if intent == "" {
		if len(seeds) > 0 && query == "" {
			intent = search.IntentRelatedToSeed
		} else {
			intent = search.IntentSearch
		}
	}
	if err := searchplanner.ValidateIntent(string(intent)); err != nil {
		return fail(err)
	}

	maxPerOwner := opts.MaxPerOwner
	if maxPerOwner <= 0 {
		maxPerOwner = 3
	}

	noteMgr := obsidian.NoteReader(&obsidian.Note{})

	searcher := semantic.Searcher{
		CodeProvider:   codeProvider,
		NoteProvider:   noteProvider,
		IntelStore:     intelStore,
		VaultDef:       opts.VaultDef,
		NoteReader:     noteMgr,
		OntologySchema: loadOntologySchemaBestEffort(opts.VaultPath),
	}

	var codeIndexReader presentation.ChunkBodyReader
	if codeStore != nil {
		codeIndexReader = presentation.AdaptCodeIndex(codeStore)
	}
	plannerDeps := searchplanner.Deps{
		Semantic:   &searcher,
		IntelStore: intelStore,
		CodeIndex:  codeIndexReader,
		VaultPath:  opts.VaultPath,
		VaultDef:   opts.VaultDef,
		NoteReader: noteMgr,
		DocPatterns: func() []string {
			fileCfg, _ := obsidian.FileContextConfigForVault(opts.VaultPath)
			return fileCfg.DocPatterns
		}(),
	}
	plannerOptions := searchplanner.Options{
		EnableVector: opts.UseVector,
		EnableIntel:  opts.UseIntel,
		EnableGraph:  opts.UseGraph && hasNoteSeeds,
		EnableRefs:   opts.UseRefs,
		UseFTSBody:   opts.UseFTSBody,
		MaxPerOwner:  maxPerOwner,

		DirectorySeedExpansion: opts.DirectorySeedExpansion,
		GraphSource:            opts.GraphSource,
	}

	runSearch := func(ctx context.Context, spec search.QuerySpec, intent search.Intent) (search.Response, error) {
		// Docs: [[unified-search-answer-architecture#^spec-0035-us3-ac1]].
		// WHY: planning and execution stay separate so target-resolution warnings,
		// timeout fallback, and ranked evidence remain observable to callers.
		response, err := searchengine.Execute(ctx, searchengine.Request{
			Dependencies: plannerDeps,
			Options:      plannerOptions,
			Spec:         spec,
			VaultPath:    opts.VaultPath,
			IntelStore:   intelStore,
			DeferPacking: opts.DeferPack || len(queryInputs) > 1,
		})
		if errors.Is(err, searchengine.ErrTimedOutBeforePlanning) {
			return search.Response{}, ErrTimedOutBeforePlanning
		}
		return response, err
	}

	buildSpec := func(q string, mode search.Intent) (search.QuerySpec, []search.Warning) {
		// Docs: [[search-answer-workflow#^spec-0034-us1-ac3]] and
		// [[search-diagnostics-explain-architecture#^spec-0043-us1-ac3]]
		// require ambiguous/unresolved targets and index warnings to stay visible.
		spec := search.QuerySpec{
			Text:              q,
			Seeds:             seeds,
			Intent:            mode,
			ExplicitSeedPaths: explicitSeedPaths,
			PathKinds:         pathKinds,
			HasExplicitSeeds:  len(explicitSeedPaths) > 0,
			Filters:           opts.Filters,
			Limits:            search.Limits{Total: opts.Limit},
		}
		if spec.Limits.Total <= 0 {
			spec.Limits.Total = 25
		}
		if opts.Pack {
			budget := opts.BudgetChars
			if budget <= 0 {
				budget = 6000
			}
			spec.Budget = search.Budget{Chars: budget}
		}
		spec, repairWarnings := search.RepairQuerySpec(opts.VaultPath, spec)
		spec, resolutionWarnings := search.ResolveQuerySpecTargets(ctx, opts.VaultPath, intelStore, spec)
		warnings := append([]search.Warning{}, repairWarnings...)
		warnings = append(warnings, resolutionWarnings...)
		return spec, warnings
	}

	if len(queryInputs) > 1 {
		result := Result{
			VaultPath:         opts.VaultPath,
			Intent:            intent,
			IntelStore:        intelStore,
			VectorUnavailable: opts.UseVector && intelStore == nil,
			Cleanup:           cleanups.Close,
		}
		responses, warnings, err := runMultiQuery(ctx, queryInputs, intent, buildSpec, runSearch)
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
		result.Answer = BuildAnswer(intent, query, search.TargetStatusNone, result.Warnings, result.Results)
		result.IndexGeneration, err = stableGeneration()
		if err != nil {
			return fail(err)
		}
		return result, nil
	}

	spec, warnings := buildSpec(query, intent)
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

	resp, err := runSearch(ctx, spec, intent)
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
	result.Display = CoalesceNoteResults(resp.Results)
	result.Answer = BuildAnswer(intent, query, spec.TargetStatus, result.Warnings, resp.Results)
	result.IndexGeneration, err = stableGeneration()
	if err != nil {
		return fail(err)
	}
	return result, nil
}

type queryRunResult struct {
	resp     search.Response
	warnings []search.Warning
	err      error
}

func runMultiQuery(ctx context.Context, inputs []QueryInput, fallbackIntent search.Intent, buildSpec func(string, search.Intent) (search.QuerySpec, []search.Warning), runSearch func(context.Context, search.QuerySpec, search.Intent) (search.Response, error)) ([]search.Response, []search.Warning, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	concurrency := min(len(inputs), min(runtime.GOMAXPROCS(0), 4))
	if concurrency < 1 {
		concurrency = 1
	}
	// Each facet is a full search run, so cap fan-out below GOMAXPROCS to avoid
	// multiplying retriever/provider concurrency for one user request.
	sem := make(chan struct{}, concurrency)
	results := make([]queryRunResult, len(inputs))
	var wg sync.WaitGroup
	for i, input := range inputs {
		i, input := i, input
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i].err = ctx.Err()
				return
			}
			defer func() { <-sem }()
			intent := fallbackIntent
			if strings.TrimSpace(input.Mode) != "" {
				intent = search.Intent(strings.TrimSpace(input.Mode))
			}
			spec, warnings := buildSpec(input.Text, intent)
			if search.ShouldBlockPrecisionFallback(spec) {
				results[i].resp = search.Response{Query: spec, Lanes: []search.LaneStatus{{Lane: "target_resolution", Status: search.LaneStateDegraded, Reason: "exact target resolution unavailable"}}}
				results[i].warnings = warnings
				return
			}
			resp, err := runSearch(ctx, spec, spec.Intent)
			if err != nil {
				results[i].err = err
				cancel()
				return
			}
			results[i].resp = resp
			results[i].warnings = append(warnings, resp.Warnings...)
		}()
	}
	wg.Wait()
	responses := make([]search.Response, 0, len(inputs))
	var warnings []search.Warning
	for _, result := range results {
		if result.err != nil {
			return nil, nil, result.err
		}
		responses = append(responses, result.resp)
		warnings = append(warnings, result.warnings...)
	}
	return responses, dedupeWarnings(warnings), nil
}

func aggregateMultiQueryLanes(responses []search.Response) []search.LaneStatus {
	var out []search.LaneStatus
	for index, response := range responses {
		facet := strings.TrimSpace(response.Query.Text) + "\x00" + string(response.Query.Intent)
		if facet == "\x00" {
			facet = fmt.Sprintf("facet-%d", index+1)
		}
		for _, status := range response.Lanes {
			status.Facet = facet
			status.Retrievers = append([]string(nil), status.Retrievers...)
			sort.Strings(status.Retrievers)
			out = append(out, status)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Facet != out[j].Facet {
			return out[i].Facet < out[j].Facet
		}
		return out[i].Lane < out[j].Lane
	})
	return out
}

func dedupeWarnings(warnings []search.Warning) []search.Warning {
	seen := map[string]struct{}{}
	out := make([]search.Warning, 0, len(warnings))
	for _, warning := range warnings {
		key := strings.Join([]string{warning.Code, warning.Kind, warning.Source, warning.Message}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, warning)
	}
	return out
}

func BuildAnswer(intent search.Intent, query string, targetStatus search.TargetStatus, warnings []search.Warning, results []search.RankedResult) answer.Response {
	inputs := make([]answer.Input, 0, len(results))
	for _, r := range results {
		// Docs: [[unified-search-answer-architecture#^spec-0035-us2-ac2]].
		// IMPORTANT: keep NodeRef data as provenance on ranked candidates;
		// pkg/app/answer must not re-query ontology state.
		inputs = append(inputs, answer.Input{
			Type:   r.Type,
			Path:   r.Path,
			Title:  r.Title,
			Role:   roleForResult(r),
			Symbol: r.Symbol,
			FQN:    r.FQN,
			Score:  r.FinalScore,

			Specificity: queryframe.SpecificityScore(r.Evidence),
			Granularity: r.Granularity,
			NodeRef:     answeradapt.NodeRef(r.NodeRef),
		})
	}
	return answer.Build(intent, query, targetStatus, warnings, inputs)
}

func cloneTargetCandidate(candidate *search.TargetCandidate) *search.TargetCandidate {
	if candidate == nil {
		return nil
	}
	copy := *candidate
	return &copy
}

func roleForResult(r search.RankedResult) string {
	path := strings.ToLower(r.Path)
	switch {
	case strings.Contains(path, "_test.") || strings.Contains(path, ".test.") || strings.Contains(path, ".spec.") || strings.Contains(path, "/test/") || strings.Contains(path, "/tests/"):
		return "test"
	case r.Type == "note":
		return "doc"
	case strings.EqualFold(r.Kind, "module") || strings.EqualFold(r.Granularity, "module"):
		return "entry_point"
	case r.Type == "code" || r.Type == "anchor":
		return "impl"
	default:
		return "support"
	}
}

func CoalesceNoteResults(results []search.RankedResult) []DisplayItem {
	type group struct {
		itemIdx int
		best    search.RankedResult
		merged  search.Candidate
		chunks  []search.RankedResult
	}

	var out []DisplayItem
	byOwner := map[string]*group{}

	emitNonNote := func(r search.RankedResult) {
		out = append(out, DisplayItem{Primary: r})
	}

	for _, r := range results {
		if r.Type != "note" || r.Owner.String() == "" {
			emitNonNote(r)
			continue
		}
		key := r.Owner.String()
		g := byOwner[key]
		if g == nil {
			out = append(out, DisplayItem{Primary: r})
			g = &group{itemIdx: len(out) - 1, best: r, merged: r.Candidate}
			byOwner[key] = g
		} else {
			if r.FinalScore > g.best.FinalScore {
				g.best = r
			}
			g.merged = search.MergeCandidate(g.merged, r.Candidate)
		}
		g.chunks = append(g.chunks, r)
	}

	for _, g := range byOwner {
		sort.SliceStable(g.chunks, func(i, j int) bool {
			if g.chunks[i].FinalScore != g.chunks[j].FinalScore {
				return g.chunks[i].FinalScore > g.chunks[j].FinalScore
			}
			return g.chunks[i].Handle.String() < g.chunks[j].Handle.String()
		})
		best := g.best
		best.Candidate = g.merged
		best.FinalScore = g.best.FinalScore

		var withContext []search.RankedResult
		for _, r := range g.chunks {
			if strings.TrimSpace(r.Breadcrumb) == "" && strings.TrimSpace(r.Heading) == "" {
				continue
			}
			withContext = append(withContext, r)
		}
		if len(withContext) > 3 {
			withContext = withContext[:3]
		}

		out[g.itemIdx].Primary = best
		out[g.itemIdx].NoteChunks = withContext
	}

	return out
}

func JoinQueries(primary string, extras []string) string {
	return JoinQueryInputs(NormalizeQueryInputs(primary, extras, ""))
}

func NormalizeQueryInputs(primary string, extras []string, mode string) []QueryInput {
	items := make([]string, 0, 1+len(extras))
	seen := make(map[string]struct{}, 1+len(extras))

	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		items = append(items, s)
	}

	add(primary)
	for _, q := range extras {
		add(q)
	}
	out := make([]QueryInput, 0, len(items))
	for _, item := range items {
		out = append(out, QueryInput{Text: item, Mode: strings.TrimSpace(mode)})
	}
	return out
}

// NormalizeExplicitQueryInputs canonicalizes explicit facets before they are
// used for request identity, retrieval, or answer coverage. Empty modes inherit
// the effective request intent so equivalent facets share one lane.
func NormalizeExplicitQueryInputs(inputs []QueryInput, defaultMode string) []QueryInput {
	out := make([]QueryInput, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	defaultMode = strings.TrimSpace(defaultMode)
	if defaultMode == "" {
		defaultMode = string(search.IntentSearch)
	}
	for _, input := range inputs {
		input.Text = strings.TrimSpace(input.Text)
		input.Mode = strings.TrimSpace(input.Mode)
		if input.Text == "" {
			continue
		}
		if input.Mode == "" {
			input.Mode = defaultMode
		}
		key := input.Mode + "\x00" + input.Text
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, input)
	}
	return out
}

func JoinQueryInputs(inputs []QueryInput) string {
	items := make([]string, 0, len(inputs))
	for _, input := range inputs {
		text := strings.TrimSpace(input.Text)
		if text != "" {
			items = append(items, text)
		}
	}
	return strings.Join(items, "\n\n")
}

func MergeSeedTokens(seeds, files []string) []string {
	out := make([]string, 0, len(seeds)+len(files))
	seen := make(map[string]struct{}, len(seeds)+len(files))
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range seeds {
		add(s)
	}
	for _, f := range files {
		add(f)
	}
	return out
}

func ResolveSeedHandles(ctx context.Context, raw []string, vaultPath string, intelStore *semdb.Store, seedLimit int) ([]knowledge.Handle, error) {
	canonicalRaw, err := canonicalizeRawSeedPaths(raw, vaultPath)
	if err != nil {
		return nil, err
	}
	pathKinds, err := hydrateSearchPathKinds(ctx, intelStore, nil)
	if err != nil {
		return nil, err
	}
	return ResolveSeedHandlesWithPathKinds(ctx, canonicalRaw, pathKinds, intelStore, seedLimit)
}

// ResolveSeedHandlesWithPathKinds preserves resolved handle identity. Raw
// paths need caller-supplied configured or persisted ownership; explicit
// Markdown remains the sole legacy raw-path compatibility boundary.
func ResolveSeedHandlesWithPathKinds(ctx context.Context, raw []string, pathKinds map[string]search.PathKind, intelStore *semdb.Store, seedLimit int) ([]knowledge.Handle, error) {
	out := make([]knowledge.Handle, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	if seedLimit <= 0 {
		seedLimit = 10
	}
	persistedNoteDirectories, err := persistedNoteDirectorySeeds(ctx, raw, intelStore, seedLimit)
	if err != nil {
		return nil, err
	}
	add := func(h knowledge.Handle) {
		if _, ok := seen[h.String()]; ok {
			return
		}
		seen[h.String()] = struct{}{}
		out = append(out, h)
	}
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if strings.HasPrefix(token, "fqn:") {
			if intelStore == nil {
				return nil, fmt.Errorf("seed %q requires code intel index", token)
			}
			fqn := strings.TrimSpace(strings.TrimPrefix(token, "fqn:"))
			ids, err := intelStore.IntelAnchorIDsByFQN(ctx, fqn, seedLimit)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				h := knowledge.AnchorHandle(id)
				if _, ok := seen[h.String()]; ok {
					continue
				}
				seen[h.String()] = struct{}{}
				out = append(out, h)
			}
			continue
		}
		if strings.HasPrefix(token, "symbol:") {
			if intelStore == nil {
				return nil, fmt.Errorf("seed %q requires code intel index", token)
			}
			sym := strings.TrimSpace(strings.TrimPrefix(token, "symbol:"))
			ids, err := intelStore.IntelAnchorIDsBySymbol(ctx, sym, seedLimit)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				h := knowledge.AnchorHandle(id)
				if _, ok := seen[h.String()]; ok {
					continue
				}
				seen[h.String()] = struct{}{}
				out = append(out, h)
			}
			continue
		}

		if strings.Contains(token, ":") {
			h, err := knowledge.ParseHandle(token)
			if err != nil {
				return nil, err
			}
			if _, ok := seen[h.String()]; ok {
				continue
			}
			seen[h.String()] = struct{}{}
			out = append(out, h)
			continue
		}

		path := search.NormalizeLocalityPath(token)
		if directory, ok := persistedNoteDirectories[path]; ok {
			if directory.hasCode {
				add(knowledge.FileHandle(path))
			}
			for _, notePath := range directory.notePaths {
				add(knowledge.NoteHandle(notePath))
			}
			continue
		}
		switch pathKinds[path] {
		case search.PathKindCode:
			add(knowledge.FileHandle(path))
			continue
		case search.PathKindNote:
			add(knowledge.NoteHandle(path))
			continue
		}

		if !isExplicitMarkdownRawSeedCompatibilityPath(path) {
			return nil, fmt.Errorf("seed %q requires configured or persisted path ownership", token)
		}
		add(knowledge.NoteHandle(path))
	}
	return out, nil
}

type persistedNoteDirectorySeed struct {
	notePaths []string
	hasCode   bool
}

// persistedNoteDirectorySeeds expands a raw directory only to current persisted
// notes, sorted and capped to the caller's seed limit. Code
// directories retain their file handle so explicit code-directory retrieval
// continues to sample indexed code.
func persistedNoteDirectorySeeds(ctx context.Context, raw []string, store *semdb.Store, seedLimit int) (map[string]persistedNoteDirectorySeed, error) {
	if store == nil {
		return nil, nil
	}
	targets := make(map[string]struct{}, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" || strings.Contains(token, ":") {
			continue
		}
		if path := search.NormalizeLocalityPath(token); path != "" {
			targets[path] = struct{}{}
		}
	}
	if len(targets) == 0 {
		return nil, nil
	}
	noteRows, err := store.CurrentNoteMetadataRows(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]persistedNoteDirectorySeed)
	for _, note := range noteRows {
		if note.Projection.Status != semdb.NoteProjectionStatusCurrent {
			continue
		}
		notePath := search.NormalizeLocalityPath(note.Path)
		for directory := range targets {
			if !strings.HasPrefix(notePath, directory+"/") {
				continue
			}
			entry := out[directory]
			entry.notePaths = append(entry.notePaths, notePath)
			out[directory] = entry
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	codePaths, err := store.IndexedFilePaths(ctx)
	if err != nil {
		return nil, err
	}
	for _, codePath := range codePaths {
		codePath = search.NormalizeLocalityPath(codePath)
		for directory, entry := range out {
			if strings.HasPrefix(codePath, directory+"/") {
				entry.hasCode = true
				out[directory] = entry
			}
		}
	}
	for directory, entry := range out {
		sort.Strings(entry.notePaths)
		notePaths := entry.notePaths[:0]
		for _, notePath := range entry.notePaths {
			if len(notePaths) > 0 && notePath == notePaths[len(notePaths)-1] {
				continue
			}
			notePaths = append(notePaths, notePath)
		}
		if len(notePaths) > seedLimit {
			notePaths = notePaths[:seedLimit]
		}
		entry.notePaths = notePaths
		out[directory] = entry
	}
	return out, nil
}

// isExplicitMarkdownRawSeedCompatibilityPath is the sole raw-path extension
// compatibility rule. All other raw paths require configured or persisted
// ownership in PathKinds; parsed knowledge handles are already typed.
func isExplicitMarkdownRawSeedCompatibilityPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), ".md")
}

// canonicalizeRawSeedPaths converts untyped raw inputs into durable vault-relative
// keys. Typed handles and symbol lookups already carry their own identity.
func canonicalizeRawSeedPaths(raw []string, vaultPath string) ([]string, error) {
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" || (strings.Contains(token, ":") && !filepath.IsAbs(token)) {
			out = append(out, token)
			continue
		}
		rel, err := vaultPaths.RelStrict(token)
		if err != nil || rel == "" {
			if err == nil {
				err = paths.ErrOutsideVault
			}
			return nil, fmt.Errorf("seed %q must be within vault: %w", token, err)
		}
		out = append(out, rel.String())
	}
	return out, nil
}

func needsPathKindHydration(raw []string, pathKinds map[string]search.PathKind) bool {
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" || strings.Contains(token, ":") {
			continue
		}
		path := search.NormalizeLocalityPath(token)
		if pathKinds[path] == search.PathKindCode || pathKinds[path] == search.PathKindNote {
			continue
		}
		if !isExplicitMarkdownRawSeedCompatibilityPath(path) {
			return true
		}
	}
	return false
}

func hydrateSearchPathKinds(ctx context.Context, store *semdb.Store, existing map[string]search.PathKind) (map[string]search.PathKind, error) {
	out := mergeSearchPathKinds(nil, existing)
	if out == nil {
		out = make(map[string]search.PathKind)
	}
	if store == nil {
		return out, nil
	}
	noteRows, err := store.CurrentNoteMetadataRows(ctx)
	if err != nil {
		return nil, err
	}
	codePaths, err := store.IndexedFilePaths(ctx)
	if err != nil {
		return nil, err
	}
	for _, note := range noteRows {
		if note.Projection.Status != semdb.NoteProjectionStatusCurrent {
			continue
		}
		path := search.NormalizeLocalityPath(note.Path)
		out[path] = search.PathKindNote
		for dir := stdpath.Dir(path); dir != "." && dir != ""; dir = stdpath.Dir(dir) {
			out[dir] = search.PathKindNote
		}
	}
	for _, path := range codePaths {
		path = search.NormalizeLocalityPath(path)
		if path != "" {
			if _, isNote := out[path]; !isNote {
				out[path] = search.PathKindCode
			}
			for dir := stdpath.Dir(path); dir != "." && dir != ""; dir = stdpath.Dir(dir) {
				if _, isNote := out[dir]; !isNote {
					out[dir] = search.PathKindCode
				}
			}
		}
	}
	return out, nil
}

func mergeSearchPathKinds(base map[string]search.PathKind, groups ...map[string]search.PathKind) map[string]search.PathKind {
	out := make(map[string]search.PathKind)
	hasSnapshot := base != nil
	for path, kind := range base {
		if kind == search.PathKindNote || kind == search.PathKindCode {
			out[search.NormalizeLocalityPath(path)] = kind
		}
	}
	for _, group := range groups {
		if group != nil {
			hasSnapshot = true
		}
		for path, kind := range group {
			path = search.NormalizeLocalityPath(path)
			if path == "" || (kind != search.PathKindNote && kind != search.PathKindCode) {
				continue
			}
			if _, exists := out[path]; !exists {
				out[path] = kind
			}
		}
	}
	if !hasSnapshot && len(out) == 0 {
		return nil
	}
	return out
}

func SelectIntentProvider(noteProvider embeddings.Provider, noteCfg embeddings.ProviderConfig, codeProvider embeddings.Provider, codeCfg embeddings.ProviderConfig) (embeddings.Provider, embeddings.ProviderConfig) {
	if codeProvider != nil {
		return codeProvider, codeCfg
	}
	return noteProvider, noteCfg
}

func InferIntentFromText(ctx context.Context, text string, intelStore *semdb.Store, noteProvider embeddings.Provider, noteCfg embeddings.ProviderConfig, codeProvider embeddings.Provider, codeCfg embeddings.ProviderConfig) (search.Intent, bool, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false, nil
	}
	provider, providerCfg := SelectIntentProvider(noteProvider, noteCfg, codeProvider, codeCfg)
	if provider == nil {
		return "", false, nil
	}
	detector := searchintent.DetectorForProvider(provider, providerCfg)
	if detector == nil {
		return "", false, nil
	}
	detected, _, ok, err := detector.DetectWithStore(ctx, text, intelStore)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, nil
	}
	if !searchintent.KeywordMatch(detected, text) {
		return "", false, nil
	}
	return detected, true, nil
}

func prepareProvider(cfg embeddings.Config, apiKey string) (embeddings.Provider, embeddings.ProviderConfig, error) {
	return semanticops.PrepareProvider(cfg, apiKey)
}

func loadOntologySchemaBestEffort(vaultPath string) *ontology.Schema {
	schema, err := ontology.LoadSchema(vaultPath)
	if err != nil {
		return nil
	}
	return schema
}

func providerConfigsMatch(a, b embeddings.ProviderConfig) bool {
	return strings.EqualFold(strings.TrimSpace(a.Provider), strings.TrimSpace(b.Provider)) &&
		strings.TrimSpace(a.Model) == strings.TrimSpace(b.Model) &&
		strings.TrimSpace(a.Endpoint) == strings.TrimSpace(b.Endpoint) &&
		a.Dimensions == b.Dimensions &&
		strings.TrimSpace(a.APIKey) == strings.TrimSpace(b.APIKey)
}

// filterResultsByNoteType drops note results whose owning note type is not
// selected. Retrievers already honor NoteTypes; explicit seeds bypass them, so
// the owning-type check is applied once here for every lane.
func filterResultsByNoteType(ctx context.Context, store *semdb.Store, results []search.RankedResult, noteTypes []string) ([]search.RankedResult, error) {
	if store == nil || len(noteTypes) == 0 || len(results) == 0 {
		return results, nil
	}
	paths := make([]string, 0, len(results))
	for _, result := range results {
		if result.Type != "code" && strings.TrimSpace(result.Path) != "" {
			paths = append(paths, result.Path)
		}
	}
	rows, err := store.OntologyTypesByPaths(ctx, paths)
	if err != nil {
		// Fail closed: an unverifiable owning-note type must not widen a
		// noteType restriction.
		return nil, fmt.Errorf("resolve owning note types for noteType filter: %w", err)
	}
	out := make([]search.RankedResult, 0, len(results))
	for _, result := range results {
		if result.Type == "code" {
			out = append(out, result)
			continue
		}
		row, ok := rows[result.Path]
		if ok && slices.ContainsFunc(noteTypes, func(want string) bool { return strings.EqualFold(want, row.TypeName) }) {
			out = append(out, result)
		}
	}
	return out, nil
}
