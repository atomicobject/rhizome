package retrieval

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/atomicobject/rhizome/pkg/search/queryframe"
)

// AutoExpandRetriever runs a first-stage retrieval, then uses the top results
// as implicit seeds to run seed-driven retrievers (graph/refs). This improves
// recall and cross-domain bridging without requiring explicit seeds.
type AutoExpandRetriever struct {
	Base []search.Retriever

	Graph    search.Retriever
	Refs     search.Retriever
	Ontology search.Retriever
	// Diffusion runs query-time graph diffusion over a bounded induced doc graph.
	// It is expected to be seeded (explicitly or via derived seeds).
	Diffusion search.Retriever

	MaxSeedsTotal   int
	MaxSeedsPerKind int
	ApproxWeights   map[search.EvidenceChannel]float64
	// MinSeedSpecificity requires a direct query match before broad no-seed auto expansion.
	MinSeedSpecificity float64
}

func (r *AutoExpandRetriever) Name() string { return "auto_expand" }

func (r *AutoExpandRetriever) NestedRetrievers() []search.Retriever {
	out := make([]search.Retriever, 0, len(r.Base)+4)
	for _, retriever := range r.Base {
		if retriever != nil {
			out = append(out, retriever)
		}
	}
	for _, retriever := range []search.Retriever{r.Graph, r.Refs, r.Ontology, r.Diffusion} {
		if retriever != nil {
			out = append(out, retriever)
		}
	}
	return out
}

func (r *AutoExpandRetriever) Retrieve(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	if strings.TrimSpace(spec.Text) == "" || len(r.Base) == 0 {
		return nil, nil
	}

	started := time.Now()
	baseCandidates, err := runRetrievers(ctx, r.Base, spec, r.ApproxWeights)
	addTiming(ctx, search.TimingEvent{
		Name:     "auto_expand.base",
		Kind:     "retriever",
		Started:  started,
		Duration: time.Since(started),
		Status:   timingStatus(err),
		Err:      timingErr(err),
	})
	if err != nil {
		return nil, err
	}
	if len(baseCandidates) == 0 {
		return nil, nil
	}
	frame := queryframe.Extract(spec.Text)
	for i := range baseCandidates {
		baseCandidates[i] = queryframe.EnrichCandidate(frame, baseCandidates[i])
	}

	// IMPORTANT: no-seed auto expansion only earns graph/refs fanout from base
	// hits with direct query specificity or strong evidence. Otherwise a broad
	// vector hit can turn into a noisy graph walk that looks more confident than
	// the original evidence supports.
	seeds := deriveSeeds(baseCandidates, r.MaxSeedsTotal, r.MaxSeedsPerKind, r.ApproxWeights, r.MinSeedSpecificity)
	if len(seeds) == 0 {
		return baseCandidates, nil
	}

	expSpec := spec
	expSpec.Seeds = seeds

	expanded, err := r.expandInParallel(ctx, expSpec)
	if err != nil {
		return nil, err
	}

	return append(baseCandidates, expanded...), nil
}

type expansionStage struct {
	key        string
	timingName string
	errPrefix  string
	retriever  search.Retriever
}

func (r *AutoExpandRetriever) expandInParallel(ctx context.Context, spec search.QuerySpec) ([]search.Candidate, error) {
	stages := []expansionStage{
		{key: "graph", timingName: "auto_expand.graph", errPrefix: "graph expansion", retriever: r.Graph},
		{key: "refs", timingName: "auto_expand.refs", errPrefix: "refs expansion", retriever: r.Refs},
		{key: "ontology", timingName: "auto_expand.ontology", errPrefix: "ontology expansion", retriever: r.Ontology},
		{key: "diffusion", timingName: "auto_expand.diffusion", errPrefix: "diffusion expansion", retriever: r.Diffusion},
	}

	results := make([][]search.Candidate, len(stages))
	errs := make([]error, len(stages))
	var wg sync.WaitGroup

	for idx, stage := range stages {
		if stage.retriever == nil {
			continue
		}
		stageCtx, cancelStage := search.RetrieverStageContext(ctx, spec, stage.timingName, idx, len(stages))
		wg.Add(1)
		go func(idx int, stage expansionStage) {
			defer wg.Done()
			defer cancelStage()
			started := time.Now()
			items, err := stage.retriever.Retrieve(stageCtx, spec)
			search.RecordRetrieverLane(ctx, stage.timingName, len(items), err)
			addTiming(ctx, search.TimingEvent{
				Name:     stage.timingName,
				Kind:     "retriever",
				Started:  started,
				Duration: time.Since(started),
				Status:   timingStatus(err),
				Err:      timingErr(err),
			})
			if err != nil {
				if search.IsBroadIntent(spec.Intent) {
					// Expansion stages are recall helpers for broad modes. Report
					// degradation but keep base hits usable.
					search.AddRuntimeWarning(ctx, search.Warning{
						Code:    "retriever_degraded",
						Kind:    "retrieval_error",
						Source:  stage.timingName,
						Message: fmt.Sprintf("%s degraded: %v", stage.errPrefix, err),
					})
					return
				}
				errs[idx] = fmt.Errorf("%s: %w", stage.errPrefix, err)
				return
			}
			for i := range items {
				items[i].Evidence = append(items[i].Evidence, search.Evidence{
					Type:     "auto_seed",
					RawScore: 0.0,
					Source:   "auto_expand",
					Details:  map[string]string{"stage": stage.key},
				})
			}
			results[idx] = items
		}(idx, stage)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	var out []search.Candidate
	for _, items := range results {
		out = append(out, items...)
	}
	return out, nil
}

func runRetrievers(ctx context.Context, retrievers []search.Retriever, spec search.QuerySpec, weightOverrides ...map[search.EvidenceChannel]float64) ([]search.Candidate, error) {
	out, err := (&CompositeRetriever{Retrievers: retrievers, Label: "auto_expand.base"}).Retrieve(ctx, spec)
	if err != nil {
		return nil, err
	}
	var weights map[search.EvidenceChannel]float64
	if len(weightOverrides) > 0 {
		weights = weightOverrides[0]
	}
	if len(weights) == 0 {
		weights = search.ApproxChannelWeightsForIntent(spec.Intent)
	}
	sort.Slice(out, func(i, j int) bool {
		left := search.NormalizedCandidateBaseScore(out[i], weights)
		right := search.NormalizedCandidateBaseScore(out[j], weights)
		if left != right {
			return left > right
		}
		return out[i].Handle.String() < out[j].Handle.String()
	})
	return out, nil
}

// deriveSeeds extracts the best seeds from base candidates for graph/refs expansion.
//
// It deduplicates by handle, sorts by normalized blended candidate score, and
// caps per-kind to avoid over-representation of a single entity type. The
// specificity gate is the precision guard for text-only search: expansion should
// bridge from a credible local hit, not from any semantically adjacent chunk.
func deriveSeeds(cands []search.Candidate, maxTotal, maxPerKind int, weights map[search.EvidenceChannel]float64, minSpecificity float64) []knowledge.Handle {
	if maxTotal <= 0 {
		maxTotal = 8
	}
	if maxPerKind <= 0 {
		maxPerKind = 4
	}
	if len(weights) == 0 {
		weights = search.DefaultApproxChannelWeights()
	}

	type scoredSeed struct {
		h     knowledge.Handle
		score float64
	}
	byKey := make(map[string]scoredSeed)
	for _, c := range cands {
		if minSpecificity > 0 && queryframe.SpecificityScore(c.Evidence) < minSpecificity && !hasStrongDirectEvidence(c.Evidence) {
			continue
		}
		seed := bestSeedForCandidate(c)
		key := seed.String()
		if key == "" {
			continue
		}
		score := bestEvidenceScore(c.Evidence, weights)
		if existing, ok := byKey[key]; ok {
			if score > existing.score {
				byKey[key] = scoredSeed{h: seed, score: score}
			}
			continue
		}
		byKey[key] = scoredSeed{h: seed, score: score}
	}
	seeds := make([]scoredSeed, 0, len(byKey))
	for _, s := range byKey {
		seeds = append(seeds, s)
	}
	sort.SliceStable(seeds, func(i, j int) bool {
		if seeds[i].score != seeds[j].score {
			return seeds[i].score > seeds[j].score
		}
		return seeds[i].h.String() < seeds[j].h.String()
	})

	perKind := make(map[knowledge.Kind]int)
	var out []knowledge.Handle
	for _, s := range seeds {
		if len(out) >= maxTotal {
			break
		}
		if perKind[s.h.Kind] >= maxPerKind {
			continue
		}
		perKind[s.h.Kind]++
		out = append(out, s.h)
	}
	return out
}

func hasStrongDirectEvidence(evidence []search.Evidence) bool {
	for _, ev := range evidence {
		score := search.EvidenceScore(ev)
		switch strings.ToLower(strings.TrimSpace(ev.Type)) {
		case "intel_fts_match", "intel_doc_match", "note_title_match":
			if score >= 0.65 {
				return true
			}
		}
	}
	return false
}

func bestSeedForCandidate(c search.Candidate) knowledge.Handle {
	if c.Owner.String() != "" {
		return c.Owner
	}
	switch c.Handle.Kind {
	case knowledge.KindNoteChunk:
		return knowledge.NoteHandle(c.Handle.ID)
	case knowledge.KindCodeChunk:
		return knowledge.AnchorHandle(c.Handle.ID)
	default:
		return c.Handle
	}
}

func bestEvidenceScore(evidence []search.Evidence, weights map[search.EvidenceChannel]float64) float64 {
	return search.NormalizedCandidateBaseScore(search.Candidate{Evidence: evidence}, weights)
}
