// Package relevance implements the ranking stage of the search pipeline.
// It blends evidence from multiple retrievers (vector, lexical, graph, refs, recency)
// into final scores and enforces diversity via MaxPerOwner.
//
// Docs: [Search (Hub)](docs/hubs/Search (Hub).md), [Search - Intent and weight tuning](docs/reference/domain/Search - Intent and weight tuning.md)
package relevance

import (
	"context"
	"sort"

	"github.com/atomicobject/rhizome/pkg/search"
)

// Weights define blend coefficients for scoring: Vector (embedding similarity), Lexical
// (FTS/title match), Graph (link proximity), Refs (coderefs/anchors), Recency (modification time).
// See [Search - Intent and weight tuning](docs/reference/domain/Search - Intent and weight tuning.md) for intent-specific defaults.
type Weights struct {
	Semantic           float64
	Lexical            float64
	Graph              float64
	Refs               float64
	Fusion             float64
	OntologyStructural float64
	OntologyAmbient    float64
	Recency            float64
	SeedLocality       float64
	Specificity        float64
}

// DefaultWeights returns sensible defaults for general-purpose search. Vector is strongest,
// refs second. Use weightsForIntent for intent-specific tuning.
func DefaultWeights() Weights {
	return Weights{
		Semantic:           1.0,
		Lexical:            0.6,
		Graph:              0.4,
		Refs:               0.8,
		Fusion:             0.75,
		OntologyStructural: 0.7,
		OntologyAmbient:    0.45,
		Recency:            0.2,
		SeedLocality:       0.35,
		Specificity:        0.9,
	}
}

// Ranker scores and orders candidates based on their evidence. Implementations
// must be stateless with respect to candidate ordering (deterministic tie-breaking).
type Ranker interface {
	Rank(ctx context.Context, spec search.QuerySpec, candidates []search.Candidate) ([]search.RankedResult, error)
}

// WeightedRanker scores candidates by blending evidence channels per Weights,
// then enforces MaxPerOwner diversity.
//
// MaxPerOwner is applied after scoring so one large file/note cannot crowd out
// task coverage. Evidence should already be merged by Handle before this runs.
type WeightedRanker struct {
	Weights     Weights
	MaxPerOwner int
}

func (r *WeightedRanker) Rank(ctx context.Context, spec search.QuerySpec, candidates []search.Candidate) ([]search.RankedResult, error) {
	_ = ctx
	w := r.Weights
	if w == (Weights{}) {
		w = DefaultWeights()
	}
	maxPerOwner := r.MaxPerOwner
	if maxPerOwner <= 0 {
		maxPerOwner = 1
	}

	type scored struct {
		search.RankedResult
		ownerKey string
		primary  bool
		exactNav bool
	}

	scoredItems := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		final := scoreCandidate(c, w)
		scoredItems = append(scoredItems, scored{
			RankedResult: search.RankedResult{Candidate: c, FinalScore: final},
			ownerKey:     c.Owner.String(),
			primary:      search.HasPrimaryEvidence(spec.Intent, c.Evidence),
			exactNav:     !search.IsPrecisionIntent(spec.Intent) && hasEvidenceType(c.Evidence, "note_title_exact", "path_exact"),
		})
	}

	sort.SliceStable(scoredItems, func(i, j int) bool {
		if scoredItems[i].exactNav != scoredItems[j].exactNav {
			return scoredItems[i].exactNav
		}
		if scoredItems[i].primary != scoredItems[j].primary {
			return scoredItems[i].primary
		}
		if scoredItems[i].FinalScore != scoredItems[j].FinalScore {
			return scoredItems[i].FinalScore > scoredItems[j].FinalScore
		}
		return scoredItems[i].Handle.String() < scoredItems[j].Handle.String()
	})

	perOwner := make(map[string]int)
	out := make([]search.RankedResult, 0, len(scoredItems))
	for _, s := range scoredItems {
		// Empty owner keys intentionally share one bucket. Retrievers should set
		// Owner when they can; this guard makes missing owners visible as reduced
		// diversity instead of letting malformed candidates dominate.
		if perOwner[s.ownerKey] >= maxPerOwner {
			continue
		}
		perOwner[s.ownerKey]++
		out = append(out, s.RankedResult)
		if spec.Limits.Total > 0 && len(out) >= spec.Limits.Total {
			break
		}
	}
	return out, nil
}

func hasEvidenceType(evidence []search.Evidence, types ...string) bool {
	for _, item := range evidence {
		for _, typ := range types {
			if item.Type == typ {
				return true
			}
		}
	}
	return false
}

func (r *WeightedRanker) ApproxChannelWeights(_ search.QuerySpec) map[search.EvidenceChannel]float64 {
	w := r.Weights
	if w == (Weights{}) {
		w = DefaultWeights()
	}
	return map[search.EvidenceChannel]float64{
		search.EvidenceChannelSemantic:           w.Semantic,
		search.EvidenceChannelLexical:            w.Lexical,
		search.EvidenceChannelGraph:              w.Graph,
		search.EvidenceChannelRefs:               w.Refs,
		search.EvidenceChannelFusion:             w.Fusion,
		search.EvidenceChannelOntologyStructural: w.OntologyStructural,
		search.EvidenceChannelOntologyAmbient:    w.OntologyAmbient,
		search.EvidenceChannelRecency:            w.Recency,
		search.EvidenceChannelSeedLocality:       w.SeedLocality,
		search.EvidenceChannelSpecificity:        w.Specificity,
	}
}

func (r *WeightedRanker) ApproxMaxPerOwner(_ search.QuerySpec) int {
	if r.MaxPerOwner <= 0 {
		return 1
	}
	return r.MaxPerOwner
}

// scoreCandidate weights channel scores after evidence is aggregated with diminishing returns.
func scoreCandidate(c search.Candidate, w Weights) float64 {
	channels := search.AggregateEvidenceScoresForRanking(c.Evidence)
	return w.Semantic*channels[search.EvidenceChannelSemantic] +
		w.Lexical*channels[search.EvidenceChannelLexical] +
		w.Graph*channels[search.EvidenceChannelGraph] +
		w.Refs*channels[search.EvidenceChannelRefs] +
		w.Fusion*channels[search.EvidenceChannelFusion] +
		w.OntologyStructural*channels[search.EvidenceChannelOntologyStructural] +
		w.OntologyAmbient*channels[search.EvidenceChannelOntologyAmbient] +
		w.Recency*channels[search.EvidenceChannelRecency] +
		w.SeedLocality*channels[search.EvidenceChannelSeedLocality] +
		w.Specificity*channels[search.EvidenceChannelSpecificity]
}
