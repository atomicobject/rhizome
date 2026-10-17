// Weighted scoring blends evidence channels and enforces owner diversity.
// It blends evidence from multiple retrievers (vector, lexical, graph, refs)
// into final scores and enforces diversity via MaxPerOwner.
//
// Docs: [Search (Hub)](docs/hubs/Search (Hub).md), [Search - Intent and weight tuning](docs/reference/domain/Search - Intent and weight tuning.md)
package search

import (
	"context"
	"sort"
)

// Weights define blend coefficients for scoring: Vector (embedding similarity), Lexical
// (FTS/title match), Graph (link proximity), and Refs (coderefs/anchors).
// See [Search - Intent and weight tuning](docs/reference/domain/Search - Intent and weight tuning.md) for intent-specific defaults.
type Weights struct {
	Semantic           float64
	Lexical            float64
	Graph              float64
	Refs               float64
	OntologyStructural float64
	OntologyAmbient    float64
	SeedLocality       float64
	Specificity        float64
}

// DefaultWeights returns sensible defaults for general-purpose  Vector is strongest,
// refs second. Use weightsForIntent for intent-specific tuning.
func DefaultWeights() Weights {
	return Weights{
		Semantic:           1.0,
		Lexical:            0.6,
		Graph:              0.4,
		Refs:               0.8,
		OntologyStructural: 0.7,
		OntologyAmbient:    0.45,
		SeedLocality:       0.35,
		Specificity:        0.9,
	}
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

func (r *WeightedRanker) Rank(ctx context.Context, spec QuerySpec, candidates []Candidate) ([]RankedResult, error) {
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
		RankedResult
		ownerKey string
		primary  bool
		exactNav bool
	}

	scoredItems := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		final := ScoreCandidate(c, w)
		scoredItems = append(scoredItems, scored{
			RankedResult: RankedResult{Candidate: c, FinalScore: final},
			ownerKey:     c.Owner.String(),
			primary:      HasPrimaryEvidence(spec.Intent, c.Evidence),
			exactNav:     !IsPrecisionIntent(spec.Intent) && HasEvidence(c.Evidence, "note_title_exact", "path_exact"),
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
	out := make([]RankedResult, 0, len(scoredItems))
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

func (r *WeightedRanker) ChannelWeights() map[EvidenceChannel]float64 {
	w := r.Weights
	if w == (Weights{}) {
		w = DefaultWeights()
	}
	return map[EvidenceChannel]float64{
		EvidenceChannelSemantic:           w.Semantic,
		EvidenceChannelLexical:            w.Lexical,
		EvidenceChannelGraph:              w.Graph,
		EvidenceChannelRefs:               w.Refs,
		EvidenceChannelOntologyStructural: w.OntologyStructural,
		EvidenceChannelOntologyAmbient:    w.OntologyAmbient,
		EvidenceChannelSeedLocality:       w.SeedLocality,
		EvidenceChannelSpecificity:        w.Specificity,
	}
}

// ScoreCandidate is the single weighted sum used by ranking and pruning.
func ScoreCandidate(c Candidate, w Weights) float64 {
	channels := AggregateEvidenceScoresForRanking(c.Evidence)
	return w.Semantic*channels[EvidenceChannelSemantic] +
		w.Lexical*channels[EvidenceChannelLexical] +
		w.Graph*channels[EvidenceChannelGraph] +
		w.Refs*channels[EvidenceChannelRefs] +
		w.OntologyStructural*channels[EvidenceChannelOntologyStructural] +
		w.OntologyAmbient*channels[EvidenceChannelOntologyAmbient] +
		w.SeedLocality*channels[EvidenceChannelSeedLocality] +
		w.Specificity*channels[EvidenceChannelSpecificity]
}
