package search

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// Service is the unified search pipeline executor.
// It runs a pre-built Plan: retrieval → ranking → rollup → packing.
//
// Construct a Plan via planner.Planner.Plan(), then call Service.Search().
// Service handles deadline-aware progressive retrieval and fallback ranking.
//
// Docs: [Search (Hub)](docs/hubs/Search (Hub).md)
type Service struct {
	Retrievers []Retriever // Ordered list of retrievers to run
	Ranker     Ranker      // Blends evidence into final scores
	Rollupper  Rollupper   // Optional: collapses fine-grained results
	Shaper     Shaper      // Optional: reorders results for task-shaped top windows
	Packer     Packer      // Optional: renders results into budgeted context
}

// Search executes the retrieval pipeline for the given QuerySpec.
//
// Pipeline stages:
//  1. Retrieval: runs Retrievers concurrently (or progressively with deadline)
//  2. Ranking: Ranker.Rank() blends evidence into FinalScore
//  3. Rollup: optional Rollupper.Rollup() collapses fine-grained results
//  4. Packing: optional Packer.Pack() renders results into budgeted context
//
// Deadline handling: if ctx has a deadline, retrieval runs progressively in
// priority order and returns partial results on timeout. Ranking/packing have
// fallback paths for timeouts. Keep those fallbacks aligned with ranker weights
// so timed-out search degrades in shape, not semantics.
//
// Defaults applied:
//   - spec.Limits.Total defaults to 25
//   - spec.Intent defaults to IntentSearch
func (s *Service) Search(ctx context.Context, spec QuerySpec) (response Response, resultErr error) {
	ctx, operation := beginSearchOperation(ctx)
	defer func() {
		panicked := recover()
		operation.finish(ctx, response, resultErr, panicked != nil)
		if panicked != nil {
			panic(panicked)
		}
	}()
	ctx, warningSink := withWarningSink(ctx)
	ctx, laneCollector := withLaneCollector(ctx)
	timings := TimingsFromContext(ctx)

	if strings.TrimSpace(spec.Text) == "" && len(spec.Seeds) == 0 {
		return Response{}, errors.New("query requires text or seeds")
	}
	if spec.Limits.Total <= 0 {
		spec.Limits.Total = 25
	}
	if spec.Intent == "" {
		spec.Intent = IntentSearch
	}

	if len(s.Retrievers) == 0 {
		return Response{}, errors.New("search service missing retrievers")
	}
	if s.Ranker == nil {
		return Response{}, errors.New("search service missing ranker")
	}

	startRetrieve := time.Now()
	candidates, err := s.retrieve(ctx, spec)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			if ctx.Err() == nil && !IsBroadIntent(spec.Intent) {
				return Response{}, err
			}
		} else {
			return Response{}, err
		}
	}
	if timings != nil {
		timings.Add(TimingEvent{
			Name:     "retrieve",
			Kind:     "stage",
			Started:  startRetrieve,
			Duration: time.Since(startRetrieve),
			Status:   statusFromErr(err),
			Err:      errString(err),
		})
	}

	indexingperf.AddCount(ctx, "search.candidates.retrieved", int64(len(candidates)))
	candidates = filterSupportOnlyCandidates(candidates)
	candidates = filterCandidatesByQuery(candidates, spec.Filters)
	indexingperf.AddCount(ctx, "search.candidates.filtered", int64(len(candidates)))
	candidates = coalesceNoteCandidates(candidates)

	var results []RankedResult
	approxWeights, approxMaxPerOwner := ApproxScoringConfig(spec, s.Ranker)
	if ctx.Err() != nil {
		recordSearchFallback(ctx, "rank", ctx.Err())
		candidates = classifyCandidates(candidates)
		results = approxRankCandidates(spec, candidates, approxWeights, approxMaxPerOwner)
	} else {
		candidates = classifyCandidates(candidates)
		startRank := time.Now()
		ranked, rankErr := s.Ranker.Rank(ctx, spec, candidates)
		if rankErr != nil && (errors.Is(rankErr, context.DeadlineExceeded) || errors.Is(rankErr, context.Canceled)) {
			results = approxRankCandidates(spec, candidates, approxWeights, approxMaxPerOwner)
			recordSearchFallback(ctx, "rank", rankErr)
			if timings != nil {
				timings.Add(TimingEvent{
					Name:     "rank",
					Kind:     "stage",
					Started:  startRank,
					Duration: time.Since(startRank),
					Status:   "timeout",
					Err:      errString(rankErr),
				})
				timings.Add(TimingEvent{
					Name:     "rank_fallback",
					Kind:     "stage",
					Started:  time.Now(),
					Duration: 0,
					Status:   "partial",
				})
			}
		} else if rankErr != nil {
			return Response{}, rankErr
		} else {
			results = ranked
			if timings != nil {
				timings.Add(TimingEvent{
					Name:     "rank",
					Kind:     "stage",
					Started:  startRank,
					Duration: time.Since(startRank),
					Status:   "ok",
				})
			}
		}
	}

	if s.Rollupper != nil && ctx.Err() == nil {
		startRollup := time.Now()
		rolled, err := s.Rollupper.Rollup(ctx, spec, results)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				rolled = results
				recordSearchFallback(ctx, "rollup", err)
				if timings != nil {
					timings.Add(TimingEvent{
						Name:     "rollup",
						Kind:     "stage",
						Started:  startRollup,
						Duration: time.Since(startRollup),
						Status:   "timeout",
						Err:      errString(err),
					})
				}
			} else {
				return Response{}, err
			}
		}
		results = rolled
		if timings != nil && err == nil {
			timings.Add(TimingEvent{
				Name:     "rollup",
				Kind:     "stage",
				Started:  startRollup,
				Duration: time.Since(startRollup),
				Status:   "ok",
			})
		}
	}
	// Rankers own the final order, including eligibility tiers such as exact
	// navigation and relationship-qualified precision results. Rollup may create
	// new representatives, so restore those evidence-backed tiers before the
	// numeric score and stable identity tie-break.
	if len(results) > 1 {
		sort.SliceStable(results, func(i, j int) bool {
			leftExact := !IsPrecisionIntent(spec.Intent) && hasAnyEvidence(results[i].Evidence, "note_title_exact", "path_exact")
			rightExact := !IsPrecisionIntent(spec.Intent) && hasAnyEvidence(results[j].Evidence, "note_title_exact", "path_exact")
			if leftExact != rightExact {
				return leftExact
			}
			leftPrimary := HasPrimaryEvidence(spec.Intent, results[i].Evidence)
			rightPrimary := HasPrimaryEvidence(spec.Intent, results[j].Evidence)
			if leftPrimary != rightPrimary {
				return leftPrimary
			}
			if results[i].FinalScore != results[j].FinalScore {
				return results[i].FinalScore > results[j].FinalScore
			}
			return rankedResultKey(results[i]) < rankedResultKey(results[j])
		})
	}
	if s.Shaper != nil && ctx.Err() == nil {
		startShape := time.Now()
		shaped, err := s.Shaper.Shape(ctx, spec, results)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				recordSearchFallback(ctx, "shaping", err)
				if timings != nil {
					timings.Add(TimingEvent{
						Name:     "shape",
						Kind:     "stage",
						Started:  startShape,
						Duration: time.Since(startShape),
						Status:   "timeout",
						Err:      errString(err),
					})
				}
			} else {
				return Response{}, err
			}
		} else {
			results = shaped
			if timings != nil {
				timings.Add(TimingEvent{
					Name:     "shape",
					Kind:     "stage",
					Started:  startShape,
					Duration: time.Since(startShape),
					Status:   "ok",
				})
			}
		}
	}

	indexingperf.AddCount(ctx, "search.results", int64(len(results)))
	resp := Response{Query: spec, Results: results}
	if s.Packer != nil && spec.Budget.Chars != 0 {
		packed, packErr := PackRankedResults(ctx, s.Packer, spec, results)
		if packErr != nil {
			return Response{}, packErr
		}
		resp.Packed = &packed
	}
	resp.Warnings = append(resp.Warnings, warningSink.snapshot()...)
	resp.Warnings = append(resp.Warnings, DetectWarnings(spec, results)...)
	for _, warning := range resp.Warnings {
		indexingperf.AddCount(ctx, "search.warning."+warning.Code, 1)
	}
	resp.Lanes = BuildLaneStatuses(s.Retrievers, laneCollector.snapshot(), results)
	return resp, nil
}

// PackRankedResults renders exactly the selected source population while
// preserving the service's deadline fallback and timing contract. The
// application layer uses this after canonical pagination so candidate-window
// hydration cannot consume the body budget or perform unnecessary reads.
func PackRankedResults(ctx context.Context, packer Packer, spec QuerySpec, results []RankedResult) (PackedContext, error) {
	if packer == nil || spec.Budget.Chars == 0 {
		return PackedContext{}, nil
	}
	ctx = withMetricsTimings(ctx)
	timings := TimingsFromContext(ctx)
	started := time.Now()
	packed, err := packer.Pack(ctx, spec, results)
	if err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
		recordSearchFallback(ctx, "pack", err)
		fallback := fallbackPack(spec, results)
		if timings != nil {
			timings.Add(TimingEvent{Name: "pack", Kind: "stage", Started: started, Duration: time.Since(started), Status: "timeout", Err: errString(err)})
			timings.Add(TimingEvent{Name: "pack_fallback", Kind: "stage", Started: time.Now(), Status: "partial"})
		}
		return fallback, nil
	}
	if err != nil {
		return PackedContext{}, err
	}
	if timings != nil {
		timings.Add(TimingEvent{Name: "pack", Kind: "stage", Started: started, Duration: time.Since(started), Status: "ok"})
	}
	return packed, nil
}

func filterCandidatesByQuery(candidates []Candidate, filters Filters) []Candidate {
	if len(candidates) == 0 || (len(filters.Types) == 0 && len(filters.PathPrefixes) == 0 && len(filters.NoteTypes) == 0 && len(filters.ExactSymbols) == 0 && !filters.TestsOnly && !filters.ExcludeTests) {
		return candidates
	}
	out := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !filters.AllowsCandidateType(candidate.Type) || !filters.AllowsPath(candidate.Path) || !filters.AllowsTestPath(candidate.Path) || !filters.AllowsSymbol(candidate.Symbol, candidate.FQN) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func filterSupportOnlyCandidates(candidates []Candidate) []Candidate {
	if len(candidates) == 0 {
		return candidates
	}
	out := candidates[:0]
	for _, c := range candidates {
		if c.SupportOnly {
			continue
		}
		out = append(out, c)
	}
	return out
}

func classifyCandidates(candidates []Candidate) []Candidate {
	if len(candidates) == 0 {
		return candidates
	}
	out := make([]Candidate, len(candidates))
	for i, c := range candidates {
		out[i] = ClassifyCandidate(c)
	}
	return out
}

func rankedResultKey(r RankedResult) string {
	if h := r.Handle.String(); h != "" {
		return h
	}
	if r.Path != "" {
		return r.Path
	}
	if r.AnchorID != "" {
		return r.AnchorID
	}
	if r.FQN != "" {
		return r.FQN
	}
	if r.NoteID != "" {
		return r.NoteID
	}
	if r.Title != "" {
		return r.Title
	}
	return ""
}

func hasAnyEvidence(evidence []Evidence, types ...string) bool {
	for _, item := range evidence {
		for _, typ := range types {
			if item.Type == typ {
				return true
			}
		}
	}
	return false
}

func (s *Service) retrieve(ctx context.Context, spec QuerySpec) ([]Candidate, error) {
	if len(s.Retrievers) == 0 {
		return nil, nil
	}

	// If the caller provided a deadline, prefer progressive retrieval: do the most
	// relevant/efficient work first and return partial results if we run out of time.
	if _, ok := ctx.Deadline(); ok {
		return s.retrieveProgressive(ctx, spec)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	mergeCap := spec.Limits.Total * len(s.Retrievers)
	if mergeCap < spec.Limits.Total {
		mergeCap = spec.Limits.Total
	}
	// Merge capacity scales with retriever count because each retriever may find
	// a different evidence surface for the same top-K request. The map still
	// collapses to stable handles before ranking.
	lanes := make([]map[string]Candidate, len(s.Retrievers))

	concurrency := calcRetrieverConcurrency(s.Retrievers)
	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	errCh := make(chan error, len(s.Retrievers))
	for i, retriever := range s.Retrievers {
		r := retriever
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			started := time.Now()
			items, err := r.Retrieve(ctx, spec)
			if timings := TimingsFromContext(ctx); timings != nil {
				timings.Add(TimingEvent{Name: r.Name(), Kind: "retriever", Started: started, Duration: time.Since(started), Status: statusFromErr(err)})
			}
			RecordRetrieverLane(ctx, r.Name(), len(items), err)
			if err != nil {
				if IsBroadIntent(spec.Intent) {
					// Broad/explanatory modes should surface degraded retrievers as
					// warnings and continue with partial evidence. Precision modes
					// fail closed so users do not mistake fallback search for a
					// deterministic definition/call/test lookup.
					AddRuntimeWarning(ctx, Warning{
						Code:    "retriever_degraded",
						Kind:    "retrieval_error",
						Source:  r.Name(),
						Message: fmt.Sprintf("%s degraded: %v", r.Name(), err),
					})
					return
				}
				cancel()
				errCh <- fmt.Errorf("%s retriever: %w", r.Name(), err)
				return
			}
			lanes[i] = mergeRetrieverItems(r.Name(), items)
		}()
	}

	wg.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		return nil, err
	}

	// First-non-empty metadata follows configured precedence, never completion
	// order. Each goroutine owns one lane until the wait above completes.
	merged := make(map[string]Candidate, mergeCap)
	for _, lane := range lanes {
		for key, candidate := range lane {
			if existing, ok := merged[key]; ok {
				merged[key] = MergeCandidate(existing, candidate)
			} else {
				merged[key] = candidate
			}
		}
	}

	out := make([]Candidate, 0, len(merged))
	for _, c := range merged {
		out = append(out, c)
	}
	return out, nil
}

func (s *Service) retrieveProgressive(ctx context.Context, spec QuerySpec) ([]Candidate, error) {
	timings := TimingsFromContext(ctx)

	retrievers := make([]Retriever, 0, len(s.Retrievers))
	retrievers = append(retrievers, s.Retrievers...)
	sort.SliceStable(retrievers, func(i, j int) bool {
		return retrieverPriorityForIntent(spec.Intent, retrievers[i]) < retrieverPriorityForIntent(spec.Intent, retrievers[j])
	})

	maxCandidates := spec.Limits.Total * 120
	if maxCandidates < 200 {
		maxCandidates = 200
	}
	if maxCandidates > 5000 {
		maxCandidates = 5000
	}
	// Progressive retrieval may collect high-fanout graph/vector candidates
	// before the final ranker runs. Prune with approximate ranker weights to
	// keep latency bounded while preserving the same intent-tuned shape.

	merged := make(map[string]Candidate, min(maxCandidates, 1024))
	approxWeights, _ := ApproxScoringConfig(spec, s.Ranker)
	for i, retriever := range retrievers {
		if ctx.Err() != nil {
			break
		}
		retrieverCtx, cancelRetriever := RetrieverStageContext(ctx, spec, retriever.Name(), i, len(retrievers))
		started := time.Now()
		type result struct {
			items []Candidate
			err   error
		}
		ch := make(chan result, 1)
		go func() {
			items, err := retriever.Retrieve(retrieverCtx, spec)
			ch <- result{items: items, err: err}
		}()

		var (
			items []Candidate
			err   error
			done  bool
		)
		select {
		case r := <-ch:
			items = r.items
			err = r.err
			done = true
		case <-retrieverCtx.Done():
			err = retrieverCtx.Err()
			done = false
		}
		cancelRetriever()
		RecordRetrieverLane(ctx, retriever.Name(), len(items), err)
		if timings != nil {
			timings.Add(TimingEvent{
				Name:     retriever.Name(),
				Kind:     "retriever",
				Started:  started,
				Duration: time.Since(started),
				Status:   statusFromErr(err),
				Err:      errString(err),
			})
		}
		if !done {
			if ctx.Err() == nil && IsBroadIntent(spec.Intent) {
				AddRuntimeWarning(ctx, Warning{
					Code:    "retriever_degraded",
					Kind:    "retrieval_error",
					Source:  retriever.Name(),
					Message: fmt.Sprintf("%s degraded: %v", retriever.Name(), err),
				})
				continue
			}
			if ctx.Err() == nil {
				return nil, fmt.Errorf("%s retriever: %w", retriever.Name(), err)
			}
			break
		}
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				if ctx.Err() == nil && IsBroadIntent(spec.Intent) {
					AddRuntimeWarning(ctx, Warning{
						Code:    "retriever_degraded",
						Kind:    "retrieval_error",
						Source:  retriever.Name(),
						Message: fmt.Sprintf("%s degraded: %v", retriever.Name(), err),
					})
					continue
				}
				if ctx.Err() == nil {
					return nil, fmt.Errorf("%s retriever: %w", retriever.Name(), err)
				}
				break
			}
			if IsBroadIntent(spec.Intent) {
				AddRuntimeWarning(ctx, Warning{
					Code:    "retriever_degraded",
					Kind:    "retrieval_error",
					Source:  retriever.Name(),
					Message: fmt.Sprintf("%s degraded: %v", retriever.Name(), err),
				})
				continue
			}
			return nil, fmt.Errorf("%s retriever: %w", retriever.Name(), err)
		}
		local := mergeRetrieverItems(retriever.Name(), items)
		for key, c := range local {
			if existing, ok := merged[key]; ok {
				merged[key] = MergeCandidate(existing, c)
				continue
			}
			merged[key] = c
		}
		if len(merged) > maxCandidates {
			merged = pruneCandidates(merged, maxCandidates, approxWeights)
		}
	}

	out := make([]Candidate, 0, len(merged))
	for _, c := range merged {
		out = append(out, c)
	}
	return out, ctx.Err()
}

func mergeRetrieverItems(name string, items []Candidate) map[string]Candidate {
	local := make(map[string]Candidate, len(items))
	ranks := make(map[string]int, len(items))
	for i, c := range items {
		var key string
		c, key = ensureCandidateKey(c)
		if key == "" {
			continue
		}
		if _, ok := ranks[key]; !ok {
			ranks[key] = i
		}
		if existing, ok := local[key]; ok {
			local[key] = MergeCandidate(existing, c)
			continue
		}
		local[key] = c
	}
	for key, c := range local {
		local[key] = annotateRetrieverRank(name, c, ranks[key])
	}
	return local
}

func annotateRetrieverRank(name string, candidate Candidate, rank int) Candidate {
	evidenceType := "retriever_rank:" + strings.TrimSpace(name)
	candidate.Evidence = append(candidate.Evidence, Evidence{
		Type:     evidenceType,
		RawScore: 0,
		Source:   name,
		Details: map[string]string{
			"retriever": name,
			"rank":      strconv.Itoa(rank),
		},
	})
	return candidate
}

func retrieverPriority(r Retriever) int {
	// Lower is earlier.
	switch strings.TrimSpace(r.Name()) {
	case "intel_lexical", "note_lexical", "rationale_fts":
		return 0
	case "definition", "tests_for_code", "call_edges":
		return 1
	case "auto_expand", "outgoing_links":
		return 1
	case "doc_links", "code_anchor_notes", "code_anchor_refs", "anchor_graph":
		return 2
	case "seed_vector", "vector":
		return 3
	case "graph":
		return 4
	default:
		// Unknown: keep relatively early, but after obvious fast paths.
		return 2
	}
}

func retrieverPriorityForIntent(intent Intent, r Retriever) int {
	if isFetchIntent(intent) {
		switch strings.TrimSpace(r.Name()) {
		case "definition", "tests_for_code", "call_edges":
			return 0
		case "intel_lexical", "note_lexical":
			return 1
		case "auto_expand", "outgoing_links":
			return 2
		case "doc_links", "code_anchor_notes", "code_anchor_refs", "anchor_graph":
			return 3
		case "seed_vector", "vector":
			return 4
		case "graph":
			return 5
		default:
			return 3
		}
	}
	return retrieverPriority(r)
}

func isFetchIntent(intent Intent) bool {
	switch intent {
	case IntentGoToDef,
		IntentFindUsages,
		IntentCallers,
		IntentCallees,
		IntentTestsForCode,
		IntentImplementers,
		IntentOverrides,
		IntentImports:
		return true
	default:
		return false
	}
}

func candidateBaseScore(c Candidate, weights map[EvidenceChannel]float64) float64 {
	return NormalizedCandidateBaseScore(c, weights)
}

func pruneCandidates(cands map[string]Candidate, keep int, weights map[EvidenceChannel]float64) map[string]Candidate {
	if keep <= 0 || len(cands) <= keep {
		return cands
	}
	type kv struct {
		key   string
		score float64
		// tier keeps exact identities first, then notes named by agreed
		// link labels, ahead of the approximate score.
		tier int
	}
	items := make([]kv, 0, len(cands))
	for k, c := range cands {
		tier := 0
		if candidateHasEvidence(c, "symbol_exact", "symbol_match", "note_title_exact", "path_exact") {
			tier = 2
		} else if hasLinkTextAlias(c) {
			tier = 1
		}
		items = append(items, kv{key: k, score: candidateBaseScore(c, weights), tier: tier})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].tier != items[j].tier {
			return items[i].tier > items[j].tier
		}
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].key < items[j].key
	})
	if len(items) > keep {
		items = items[:keep]
	}
	out := make(map[string]Candidate, len(items))
	for _, it := range items {
		out[it.key] = cands[it.key]
	}
	return out
}

// hasLinkTextAlias reports whether linking notes agree on a label that names
// every query concept. Query specificity reads that label with the title only
// at ranking time, so pruning must not drop the note first.
func hasLinkTextAlias(c Candidate) bool {
	for _, ev := range c.Evidence {
		if ev.Type == "link_text_match" && ev.Details[LinkTextAliasDetail] != "" {
			return true
		}
	}
	return false
}

func candidateHasEvidence(c Candidate, evidenceTypes ...string) bool {
	if len(evidenceTypes) == 0 {
		return false
	}
	wanted := make(map[string]struct{}, len(evidenceTypes))
	for _, typ := range evidenceTypes {
		wanted[strings.TrimSpace(typ)] = struct{}{}
	}
	for _, ev := range c.Evidence {
		if _, ok := wanted[strings.TrimSpace(ev.Type)]; ok {
			return true
		}
	}
	return false
}

func approxRankCandidates(spec QuerySpec, candidates []Candidate, weights map[EvidenceChannel]float64, maxPerOwner int) []RankedResult {
	if len(candidates) == 0 {
		return nil
	}
	limit := spec.Limits.Total
	if limit <= 0 {
		limit = 25
	}

	type scored struct {
		c     Candidate
		score float64
		owner string
	}
	items := make([]scored, 0, len(candidates))
	maxScore := 0.0
	for _, c := range candidates {
		s := NormalizedCandidateBaseScore(c, weights)
		if s > maxScore {
			maxScore = s
		}
		items = append(items, scored{c: c, score: s, owner: c.Owner.String()})
	}
	if maxScore <= 0 {
		maxScore = 1
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].c.Handle.String() < items[j].c.Handle.String()
	})
	perOwner := make(map[string]int)
	if maxPerOwner <= 0 {
		maxPerOwner = 1
	}
	out := make([]RankedResult, 0, len(items))
	for _, it := range items {
		if perOwner[it.owner] >= maxPerOwner && it.owner != "" {
			continue
		}
		perOwner[it.owner]++
		out = append(out, RankedResult{
			Candidate:  it.c,
			FinalScore: it.score / maxScore,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func fallbackPack(spec QuerySpec, results []RankedResult) PackedContext {
	budget := spec.Budget.Chars
	if budget <= 0 {
		budget = 6000
	}
	var b strings.Builder
	b.WriteString("Search timed out; returning a minimal pack.\n\n")
	for i, r := range results {
		if i >= spec.Limits.Total && spec.Limits.Total > 0 {
			break
		}
		line := fmt.Sprintf("%2d. [%s] %s (%s)\n", i+1, firstNonEmpty(r.Type, "result"), firstNonEmpty(r.Title, r.Symbol, r.FQN, r.Path), firstNonEmpty(r.Path, r.NoteID, r.AnchorID, r.Handle.String()))
		if b.Len()+len(line) > budget {
			break
		}
		b.WriteString(line)
	}
	text := b.String()
	if len(text) > budget {
		text = text[:budget]
	}
	return PackedContext{Text: text}
}

func calcRetrieverConcurrency(retrievers []Retriever) int {
	if len(retrievers) == 0 {
		return 0
	}

	var hasCPUHeavy bool
	ioBound := 0
	for _, r := range retrievers {
		switch classifyRetriever(r) {
		case RetrieverCostCPUHeavy:
			hasCPUHeavy = true
		case RetrieverCostIOBound:
			ioBound++
		}
	}

	maxProcs := runtime.GOMAXPROCS(0)
	if maxProcs < 1 {
		maxProcs = 1
	}

	var limit int
	switch {
	case hasCPUHeavy:
		limit = maxProcs
	default:
		limit = maxProcs * 2
		if limit < 2 {
			limit = 2
		}
		if limit > 8 {
			limit = 8
		}
		if ioBound > len(retrievers) {
			ioBound = len(retrievers)
		}
		if ioBound > 0 && limit < ioBound {
			limit = ioBound
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > len(retrievers) {
		limit = len(retrievers)
	}
	return limit
}

func classifyRetriever(r Retriever) RetrieverCostClass {
	if costed, ok := r.(CostedRetriever); ok {
		return costed.CostClass()
	}
	return RetrieverCostUnknown
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func statusFromContext(ctx context.Context) string {
	if ctx == nil || ctx.Err() == nil {
		return "ok"
	}
	return statusFromErr(ctx.Err())
}

func statusFromErr(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "error"
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
