package search

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// Service is the unified search pipeline executor.
// It runs a pre-built Plan: retrieval → ranking → rollup → packing.
//
// Construct a Plan via planner.Planner.Plan(), then call Service.Search().
// Service handles concurrent retrieval with bounded deadlines and fallback ranking.
//
// Docs: [Search (Hub)](docs/hubs/Search (Hub).md)
type Service struct {
	Retrievers []Retriever         // Ordered list of retrievers to run
	Scorer     *WeightedRanker     // Pure scoring policy shared with pruning and deadline fallback
	Ranker     Ranker              // Blends evidence into final scores
	Enrichers  []CandidateAdjuster // Optional I/O-bound ranking evidence
	Adjusters  []ResultAdjuster    // Optional final ranking passes
	Rollupper  Rollupper           // Optional: collapses fine-grained results
	Shaper     Shaper              // Optional: reorders results for task-shaped top windows
	Packer     Packer              // Optional: renders results into budgeted context
}

// Search executes the retrieval pipeline for the given QuerySpec.
//
// Pipeline stages:
//  1. Retrieval: runs Retrievers concurrently under a shared deadline
//  2. Ranking: Ranker.Rank() blends evidence into FinalScore
//  3. Rollup: optional Rollupper.Rollup() collapses fine-grained results
//  4. Packing: optional Packer.Pack() renders results into budgeted context
//
// Deadline handling: concurrent retrieval reserves time for ranking and packing
// and returns partial results on timeout. Ranking/packing have
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
			Status:   TimingStatus(err),
			Err:      TimingError(err),
		})
	}

	indexingperf.AddCount(ctx, indexingperf.SearchCandidatesRetrieved, int64(len(candidates)))
	candidates = filterSupportOnlyCandidates(candidates)
	candidates = filterCandidatesByQuery(candidates, spec.Filters)
	indexingperf.AddCount(ctx, indexingperf.SearchCandidatesFiltered, int64(len(candidates)))
	candidates = coalesceNoteCandidates(candidates)

	results, err := s.rankCandidates(ctx, spec, candidates)
	if err != nil {
		return Response{}, err
	}
	results, err = s.prepareResults(ctx, spec, results)
	if err != nil {
		return Response{}, err
	}
	indexingperf.AddCount(ctx, indexingperf.SearchResults, int64(len(results)))
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
		indexingperf.AddCount(ctx, indexingperf.SearchWarning.Select(warning.Code), 1)
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
			timings.Add(TimingEvent{Name: "pack", Kind: "stage", Started: started, Duration: time.Since(started), Status: "timeout", Err: TimingError(err)})
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

func (s *Service) retrieve(ctx context.Context, spec QuerySpec) ([]Candidate, error) {
	candidates, err := RunLanes(ctx, spec, s.Retrievers, "")
	maxCandidates := min(max(spec.Limits.Total*120, 200), 5000)
	if len(candidates) > maxCandidates {
		weights := s.scorer().ChannelWeights()
		pruned := pruneCandidates(mergeRetrieverItems(candidates), maxCandidates, weights)
		candidates = make([]Candidate, 0, len(pruned))
		for _, candidate := range pruned {
			candidates = append(candidates, candidate)
		}
	}
	return candidates, err
}

func candidateBaseScore(c Candidate, weights map[EvidenceChannel]float64) float64 {
	return CandidateScore(c, weights)
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
		if HasEvidence(c.Evidence, "symbol_exact", "symbol_match", "note_title_exact", "path_exact") {
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

func (s *Service) scorer() *WeightedRanker {
	if s.Scorer != nil {
		return s.Scorer
	}
	if scorer, ok := s.Ranker.(*WeightedRanker); ok {
		return scorer
	}
	return &WeightedRanker{}
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
