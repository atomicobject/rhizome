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
	EvidenceChannelOntologyStructural EvidenceChannel = "ontology_structural"
	EvidenceChannelOntologyAmbient    EvidenceChannel = "ontology_ambient"
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

// LinkTextAliasDetail names the link_text_match detail holding a label that
// several linking notes use for the target and that names every query concept.
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

func DefaultChannelWeights() map[EvidenceChannel]float64 {
	return (&WeightedRanker{}).ChannelWeights()
}

func CandidateScore(c Candidate, weights map[EvidenceChannel]float64) float64 {
	return ScoreCandidate(c, Weights{
		Semantic: weights[EvidenceChannelSemantic], Lexical: weights[EvidenceChannelLexical],
		Graph: weights[EvidenceChannelGraph], Refs: weights[EvidenceChannelRefs],
		OntologyStructural: weights[EvidenceChannelOntologyStructural], OntologyAmbient: weights[EvidenceChannelOntologyAmbient],
		SeedLocality: weights[EvidenceChannelSeedLocality], Specificity: weights[EvidenceChannelSpecificity],
	})
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
