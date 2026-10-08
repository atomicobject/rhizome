package search

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type EvidenceChannel string

const (
	EvidenceChannelNone               EvidenceChannel = "none"
	EvidenceChannelSemantic           EvidenceChannel = "semantic"
	EvidenceChannelLexical            EvidenceChannel = "lexical"
	EvidenceChannelGraph              EvidenceChannel = "graph"
	EvidenceChannelRefs               EvidenceChannel = "refs"
	EvidenceChannelFusion             EvidenceChannel = "fusion"
	EvidenceChannelOntologyStructural EvidenceChannel = "ontology_structural"
	EvidenceChannelOntologyAmbient    EvidenceChannel = "ontology_ambient"
	EvidenceChannelRecency            EvidenceChannel = "recency"
	EvidenceChannelSeedLocality       EvidenceChannel = "seed_locality"
	EvidenceChannelSpecificity        EvidenceChannel = "specificity"
)

type EvidenceSpec struct {
	Channel   EvidenceChannel
	Normalize func(float64) float64
	Rankable  bool
}

var evidenceSpecs = map[string]EvidenceSpec{
	"note_vector_similarity":       {Channel: EvidenceChannelSemantic, Normalize: clampUnit, Rankable: true},
	"code_vector_similarity":       {Channel: EvidenceChannelSemantic, Normalize: clampUnit, Rankable: true},
	"anchor_vector_similarity":     {Channel: EvidenceChannelSemantic, Normalize: clampUnit, Rankable: true},
	"intel_fts_match":              {Channel: EvidenceChannelLexical, Normalize: clampUnit, Rankable: true},
	"intel_doc_match":              {Channel: EvidenceChannelLexical, Normalize: clampUnit, Rankable: true},
	"rationale_fts_match":          {Channel: EvidenceChannelLexical, Normalize: clampUnit, Rankable: true},
	"note_title_match":             {Channel: EvidenceChannelLexical, Normalize: clampUnit, Rankable: true},
	"note_title_exact":             {Channel: EvidenceChannelLexical, Normalize: clampUnit, Rankable: true},
	"path_exact":                   {Channel: EvidenceChannelLexical, Normalize: clampUnit, Rankable: true},
	"typo_title_match":             {Channel: EvidenceChannelLexical, Normalize: clampUnit, Rankable: true},
	"link_text_match":              {Channel: EvidenceChannelLexical, Normalize: clampUnit, Rankable: true},
	"tests_path":                   {Channel: EvidenceChannelLexical, Normalize: clampUnit, Rankable: true},
	"tests_package":                {Channel: EvidenceChannelLexical, Normalize: clampUnit, Rankable: true},
	"graph_proximity":              {Channel: EvidenceChannelGraph, Normalize: clampUnit, Rankable: true},
	"same_community":               {Channel: EvidenceChannelGraph, Normalize: clampUnit, Rankable: true},
	"graph_hits_authority":         {Channel: EvidenceChannelGraph, Normalize: clampUnit, Rankable: true},
	"graph_hits_hub":               {Channel: EvidenceChannelGraph, Normalize: clampUnit, Rankable: true},
	"graph_ppr":                    {Channel: EvidenceChannelGraph, Normalize: clampUnit, Rankable: true},
	"graph_edge_confidence":        {Channel: EvidenceChannelGraph, Normalize: clampUnit, Rankable: true},
	"graph_anchor_pagerank":        {Channel: EvidenceChannelGraph, Normalize: clampUnit, Rankable: true},
	"direct_link_out":              {Channel: EvidenceChannelGraph, Normalize: clampUnit, Rankable: true},
	"ontology_relation_structural": {Channel: EvidenceChannelOntologyStructural, Normalize: clampUnit, Rankable: true},
	"ontology_relation_ambient":    {Channel: EvidenceChannelOntologyAmbient, Normalize: clampUnit, Rankable: true},
	"doc_link":                     {Channel: EvidenceChannelRefs, Normalize: clampUnit, Rankable: true},
	"code_ref":                     {Channel: EvidenceChannelRefs, Normalize: clampUnit, Rankable: true},
	"anchor_graph_edge":            {Channel: EvidenceChannelRefs, Normalize: clampUnit, Rankable: true},
	"call_edge":                    {Channel: EvidenceChannelRefs, Normalize: clampUnit, Rankable: true},
	"implements_edge":              {Channel: EvidenceChannelRefs, Normalize: clampUnit, Rankable: true},
	"definition_anchor":            {Channel: EvidenceChannelRefs, Normalize: clampUnit, Rankable: true},
	"symbol_exact":                 {Channel: EvidenceChannelRefs, Normalize: clampUnit, Rankable: true},
	"symbol_match":                 {Channel: EvidenceChannelRefs, Normalize: clampUnit, Rankable: true},
	"code_anchor":                  {Channel: EvidenceChannelRefs, Normalize: clampUnit, Rankable: true},
	"rank_fusion":                  {Channel: EvidenceChannelFusion, Normalize: clampUnit, Rankable: true},
	"recently_modified":            {Channel: EvidenceChannelRecency, Normalize: clampUnit, Rankable: true},
	"explicit_seed":                {Channel: EvidenceChannelSeedLocality, Normalize: clampUnit, Rankable: true},
	"module_doc":                   {Channel: EvidenceChannelSeedLocality, Normalize: clampUnit, Rankable: true},
	"submodule_doc":                {Channel: EvidenceChannelSeedLocality, Normalize: clampUnit, Rankable: true},
	"query_specificity":            {Channel: EvidenceChannelSpecificity, Normalize: clampUnit, Rankable: true},
	"query_match":                  {Channel: EvidenceChannelNone, Normalize: clampUnit, Rankable: false},
	"auto_seed":                    {Channel: EvidenceChannelNone, Normalize: clampZero, Rankable: false},
	"overview_shaped":              {Channel: EvidenceChannelNone, Normalize: clampZero, Rankable: false},
}

func NormalizeEvidence(ev Evidence) (Evidence, error) {
	key := normalizeEvidenceKey(ev.Type)
	spec, ok := evidenceSpecForKey(key)
	if !ok {
		return ev, fmt.Errorf("unmapped evidence type %q", ev.Type)
	}
	ev.Channel = spec.Channel
	ev.Score = spec.Normalize(rawOrNormalizedScore(ev))
	return ev, nil
}

func MustNormalizeEvidence(ev Evidence) Evidence {
	out, err := NormalizeEvidence(ev)
	if err != nil {
		panic(err)
	}
	return out
}

func NormalizeEvidenceList(evidence []Evidence) ([]Evidence, error) {
	if len(evidence) == 0 {
		return evidence, nil
	}
	out := make([]Evidence, len(evidence))
	for i, ev := range evidence {
		norm, err := NormalizeEvidence(ev)
		if err != nil {
			return nil, err
		}
		out[i] = norm
	}
	return out, nil
}

// LinkTextAliasDetail names the link_text_match detail holding a label that
// several linking notes use for the target.
const LinkTextAliasDetail = "alias"

func EvidenceScore(ev Evidence) float64 {
	if ev.Score > 0 {
		return ev.Score
	}
	norm, err := NormalizeEvidence(ev)
	if err != nil {
		return 0
	}
	return norm.Score
}

func rankableEvidence(ev Evidence) bool {
	spec, ok := evidenceSpecForKey(normalizeEvidenceKey(ev.Type))
	return ok && spec.Rankable
}

func evidenceSpecForKey(key string) (EvidenceSpec, bool) {
	if spec, ok := evidenceSpecs[key]; ok {
		return spec, true
	}
	if strings.HasPrefix(key, "retriever_rank:") {
		return EvidenceSpec{Channel: EvidenceChannelNone, Normalize: clampZero, Rankable: false}, true
	}
	return EvidenceSpec{}, false
}

func AggregateEvidenceScoresForRanking(evidence []Evidence) map[EvidenceChannel]float64 {
	buckets := make(map[EvidenceChannel][]float64)
	for _, ev := range evidence {
		if !rankableEvidence(ev) {
			continue
		}
		score := EvidenceScore(ev)
		if score <= 0 {
			continue
		}
		channel := ev.Channel
		if channel == "" {
			norm, err := NormalizeEvidence(ev)
			if err != nil {
				continue
			}
			channel = norm.Channel
		}
		buckets[channel] = append(buckets[channel], score)
	}
	out := make(map[EvidenceChannel]float64, len(buckets))
	for channel, vals := range buckets {
		sort.Slice(vals, func(i, j int) bool { return vals[i] > vals[j] })
		sum := 0.0
		decay := 1.0
		for _, v := range vals {
			sum += v * decay
			decay *= 0.55
		}
		out[channel] = clampUnit(sum)
	}
	return out
}

func DefaultApproxChannelWeights() map[EvidenceChannel]float64 {
	return map[EvidenceChannel]float64{
		EvidenceChannelSemantic:           1.0,
		EvidenceChannelLexical:            0.8,
		EvidenceChannelGraph:              0.45,
		EvidenceChannelRefs:               0.9,
		EvidenceChannelFusion:             0.75,
		EvidenceChannelOntologyStructural: 0.7,
		EvidenceChannelOntologyAmbient:    0.45,
		EvidenceChannelRecency:            0.1,
		EvidenceChannelSeedLocality:       0.5,
		EvidenceChannelSpecificity:        0.9,
	}
}

func ApproxChannelWeightsForIntent(intent Intent) map[EvidenceChannel]float64 {
	switch intent {
	case IntentRelatedToSeed:
		return map[EvidenceChannel]float64{
			EvidenceChannelSemantic:           0.4,
			EvidenceChannelLexical:            0.3,
			EvidenceChannelGraph:              1.0,
			EvidenceChannelRefs:               0.9,
			EvidenceChannelFusion:             0.6,
			EvidenceChannelOntologyStructural: 0.7,
			EvidenceChannelOntologyAmbient:    0.4,
			EvidenceChannelRecency:            0.2,
			EvidenceChannelSeedLocality:       0.4,
			EvidenceChannelSpecificity:        0.35,
		}
	case IntentDocsForCode:
		return map[EvidenceChannel]float64{
			EvidenceChannelSemantic:           0.5,
			EvidenceChannelLexical:            0.9,
			EvidenceChannelGraph:              0.15,
			EvidenceChannelRefs:               1.2,
			EvidenceChannelFusion:             0.85,
			EvidenceChannelOntologyStructural: 0.45,
			EvidenceChannelOntologyAmbient:    0.15,
			EvidenceChannelRecency:            0.2,
			EvidenceChannelSeedLocality:       0.55,
			EvidenceChannelSpecificity:        0.7,
		}
	case IntentCodeForDocs:
		return map[EvidenceChannel]float64{
			EvidenceChannelSemantic:           0.6,
			EvidenceChannelLexical:            0.6,
			EvidenceChannelGraph:              0.2,
			EvidenceChannelRefs:               1.2,
			EvidenceChannelFusion:             0.8,
			EvidenceChannelOntologyStructural: 0.35,
			EvidenceChannelOntologyAmbient:    0.12,
			EvidenceChannelRecency:            0.2,
			EvidenceChannelSeedLocality:       0.45,
			EvidenceChannelSpecificity:        0.75,
		}
	case IntentConsolidation:
		return map[EvidenceChannel]float64{
			EvidenceChannelSemantic:           0.4,
			EvidenceChannelLexical:            0.2,
			EvidenceChannelGraph:              1.0,
			EvidenceChannelRefs:               0.4,
			EvidenceChannelFusion:             0.6,
			EvidenceChannelOntologyStructural: 0.8,
			EvidenceChannelOntologyAmbient:    0.45,
			EvidenceChannelRecency:            0.2,
			EvidenceChannelSeedLocality:       0.25,
			EvidenceChannelSpecificity:        0.45,
		}
	case IntentOverview:
		return map[EvidenceChannel]float64{
			EvidenceChannelSemantic:           1.0,
			EvidenceChannelLexical:            0.3,
			EvidenceChannelGraph:              0.5,
			EvidenceChannelRefs:               0.6,
			EvidenceChannelFusion:             0.8,
			EvidenceChannelOntologyStructural: 0.55,
			EvidenceChannelOntologyAmbient:    0.25,
			EvidenceChannelRecency:            0.2,
			EvidenceChannelSeedLocality:       0.35,
			EvidenceChannelSpecificity:        1.0,
		}
	case IntentSubsystemOverview:
		return map[EvidenceChannel]float64{
			EvidenceChannelSemantic:           0.8,
			EvidenceChannelLexical:            0.5,
			EvidenceChannelGraph:              0.35,
			EvidenceChannelRefs:               1.0,
			EvidenceChannelFusion:             0.9,
			EvidenceChannelOntologyStructural: 0.65,
			EvidenceChannelOntologyAmbient:    0.25,
			EvidenceChannelRecency:            0.2,
			EvidenceChannelSeedLocality:       0.85,
			EvidenceChannelSpecificity:        0.8,
		}
	case IntentGoToDef:
		return map[EvidenceChannel]float64{
			EvidenceChannelSemantic:           0.0,
			EvidenceChannelLexical:            0.6,
			EvidenceChannelGraph:              0.0,
			EvidenceChannelRefs:               1.4,
			EvidenceChannelFusion:             0.0,
			EvidenceChannelOntologyStructural: 0.1,
			EvidenceChannelOntologyAmbient:    0.0,
			EvidenceChannelRecency:            0.1,
			EvidenceChannelSeedLocality:       0.2,
			EvidenceChannelSpecificity:        0.1,
		}
	case IntentFindUsages, IntentCallers, IntentCallees:
		return map[EvidenceChannel]float64{
			EvidenceChannelSemantic:           0.0,
			EvidenceChannelLexical:            0.5,
			EvidenceChannelGraph:              0.2,
			EvidenceChannelRefs:               1.2,
			EvidenceChannelFusion:             0.0,
			EvidenceChannelOntologyStructural: 0.15,
			EvidenceChannelOntologyAmbient:    0.0,
			EvidenceChannelRecency:            0.1,
			EvidenceChannelSeedLocality:       0.15,
			EvidenceChannelSpecificity:        0.15,
		}
	case IntentTestsForCode:
		return map[EvidenceChannel]float64{
			EvidenceChannelSemantic:           0.0,
			EvidenceChannelLexical:            1.2,
			EvidenceChannelGraph:              0.1,
			EvidenceChannelRefs:               0.6,
			EvidenceChannelFusion:             0.0,
			EvidenceChannelOntologyStructural: 0.0,
			EvidenceChannelOntologyAmbient:    0.0,
			EvidenceChannelRecency:            0.1,
			EvidenceChannelSeedLocality:       0.25,
			EvidenceChannelSpecificity:        0.35,
		}
	case IntentExplainSymbol:
		return map[EvidenceChannel]float64{
			EvidenceChannelSemantic:           0.7,
			EvidenceChannelLexical:            0.8,
			EvidenceChannelGraph:              0.2,
			EvidenceChannelRefs:               1.0,
			EvidenceChannelOntologyStructural: 0.25,
			EvidenceChannelOntologyAmbient:    0.0,
			EvidenceChannelRecency:            0.2,
			EvidenceChannelSeedLocality:       0.25,
			EvidenceChannelSpecificity:        0.8,
		}
	case IntentRefactorImpact, IntentSecurityAudit, IntentDataFlow, IntentImplementers, IntentOverrides, IntentImports:
		return map[EvidenceChannel]float64{
			EvidenceChannelSemantic:           0.2,
			EvidenceChannelLexical:            0.3,
			EvidenceChannelGraph:              1.0,
			EvidenceChannelRefs:               0.8,
			EvidenceChannelOntologyStructural: 0.65,
			EvidenceChannelOntologyAmbient:    0.2,
			EvidenceChannelRecency:            0.2,
			EvidenceChannelSeedLocality:       0.2,
			EvidenceChannelSpecificity:        0.35,
		}
	default:
		return DefaultApproxChannelWeights()
	}
}

func NormalizedCandidateBaseScore(c Candidate, weights map[EvidenceChannel]float64) float64 {
	if len(c.Evidence) == 0 {
		return 0
	}
	channels := AggregateEvidenceScoresForRanking(c.Evidence)
	score := 0.0
	for channel, value := range channels {
		score += weights[channel] * value
	}
	return score
}

func ApproxCandidateScore(c Candidate) float64 {
	return NormalizedCandidateBaseScore(c, DefaultApproxChannelWeights())
}

func ApproxScoringConfig(spec QuerySpec, ranker Ranker) (map[EvidenceChannel]float64, int) {
	weights := ApproxChannelWeightsForIntent(spec.Intent)
	maxPerOwner := 1
	if provider, ok := ranker.(ApproxScoreProvider); ok {
		if approx := provider.ApproxChannelWeights(spec); len(approx) > 0 {
			weights = approx
		}
		if perOwner := provider.ApproxMaxPerOwner(spec); perOwner > 0 {
			maxPerOwner = perOwner
		}
	}
	return weights, maxPerOwner
}

func normalizeEvidenceKey(typ string) string {
	return strings.TrimSpace(strings.ToLower(typ))
}

func rawOrNormalizedScore(ev Evidence) float64 {
	if ev.Score > 0 {
		return ev.Score
	}
	return ev.RawScore
}

func clampUnit(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampZero(_ float64) float64 { return 0 }
