package application

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/search"
)

// BindFacetSupport records a facet only when this source has independently
// useful evidence for that facet.
func BindFacetSupport(assessment SourceAssessment, text string, mode search.Intent) SourceAssessment {
	text = strings.TrimSpace(text)
	if text == "" || (assessment.Relevance != RelevanceStrong && assessment.Relevance != RelevanceUseful) {
		return assessment
	}
	assessment.FacetSupport = mergeFacetSupport(assessment.FacetSupport, []FacetSupport{{
		Text: text, Mode: mode, Relevance: assessment.Relevance, Eligibility: assessment.Eligibility, Relationship: assessment.Relationship, SupportedRoles: append([]string(nil), assessment.SupportedRoles...),
	}})
	return assessment
}

func mergeFacetSupport(left, right []FacetSupport) []FacetSupport {
	byKey := make(map[string]FacetSupport, len(left)+len(right))
	for _, support := range append(append([]FacetSupport(nil), left...), right...) {
		key := strings.TrimSpace(support.Text) + "\x00" + string(support.Mode)
		if key == "\x00" {
			continue
		}
		retained, ok := byKey[key]
		if !ok || relevanceRank(support.Relevance) > relevanceRank(retained.Relevance) || (relevanceRank(support.Relevance) == relevanceRank(retained.Relevance) && facetProofRank(support) > facetProofRank(retained)) {
			byKey[key] = support
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]FacetSupport, 0, len(keys))
	for _, key := range keys {
		out = append(out, byKey[key])
	}
	return out
}

func facetProofRank(support FacetSupport) int {
	rank := 0
	if support.Eligibility == EvidencePrimary {
		rank++
	}
	if strings.TrimSpace(support.Relationship) != "" {
		rank++
	}
	if support.TargetEstablished {
		rank++
	}
	return rank
}

func relevanceRank(level RelevanceLevel) int {
	switch level {
	case RelevanceStrong:
		return 3
	case RelevanceUseful:
		return 2
	case RelevanceWeak:
		return 1
	default:
		return 0
	}
}

type facetObservation struct {
	result   search.RankedResult
	facetKey string
	rank     int
}

type facetAggregate struct {
	key            string
	result         search.RankedResult
	bestRank       int
	bestScore      float64
	facetCount     int
	observedFacets map[string]struct{}
}

// facetRoleCue reads the role a facet asks for from its own words. Measured on
// the repository corpus, a documentation facet whose leader is a code file (or a
// test facet whose leader is not a test) hands useful-support coverage to the
// wrong result.
func facetRoleCue(text string) string {
	test := false
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) }) {
		switch word {
		case "documented", "documentation", "docs", "spec", "specification", "guide", "design", "decision", "rationale", "explain":
			return "documentation"
		case "test", "tests", "proves", "proof", "verify", "verifies", "coverage":
			test = true
		}
	}
	if test {
		return "test"
	}
	return ""
}

// facetLeaderIndex is the rank of the result that answers the facet in the role
// the facet asked for, falling back to the facet's own top-ranked result.
func facetLeaderIndex(response search.Response) int {
	cue := facetRoleCue(response.Query.Text)
	if cue == "" {
		return 0
	}
	for index, result := range response.Results {
		if roleForResult(result) == cue {
			return index
		}
	}
	return 0
}

// AggregateFacets combines independently ranked facet results without making
// request order decide which facet survives the result cap. The aggregate rank
// rewards coverage across facets, then the best within-facet rank, then the
// engine score. Canonical identity preserves distinct embedded and section
// nodes while coalescing whole-note chunks by path.
func AggregateFacets(responses []search.Response, limit int) []search.RankedResult {
	// Docs: [[search-answer-workflow#^spec-0034-us3-ac1]],
	// [[search-answer-workflow#^spec-0034-us3-ac2]], and
	// [[search-quality-evaluation-corpus#^spec-0041-us2-ac3]].
	// RATIONALE: multi-query facets should converge on one packet while preserving
	// evidence that a source matched more than one facet.
	observations := map[string][]facetObservation{}
	for responseIndex, response := range responses {
		facetKey := strings.TrimSpace(response.Query.Text) + "\x00" + string(response.Query.Intent)
		if facetKey == "\x00" {
			facetKey = fmt.Sprintf("anonymous-facet-%d", responseIndex)
		}
		facetAssessments := AssessQuery(response.Query.Text, response.Query.Intent, response.Results)
		leaderIndex := facetLeaderIndex(response)
		for rank, result := range response.Results {
			key := CanonicalSourceIdentity(result)
			if key == "" {
				continue
			}
			result.Evidence = append(result.Evidence, search.Evidence{Type: "query_match", Source: "multi_query", Details: map[string]string{
				"query":              response.Query.Text,
				"mode":               string(response.Query.Intent),
				"rank":               fmt.Sprintf("%d", rank),
				"leads_facet":        fmt.Sprintf("%t", rank == leaderIndex),
				"relevance":          string(facetAssessments[rank].Relevance),
				"eligibility":        string(facetAssessments[rank].Eligibility),
				"relationship":       facetAssessments[rank].Relationship,
				"target_established": fmt.Sprintf("%t", targetSpecEstablished(response.Query)),
			}})
			observations[key] = append(observations[key], facetObservation{result: result, facetKey: facetKey, rank: rank})
		}
	}

	aggregates := make([]facetAggregate, 0, len(observations))
	allFacets := map[string]struct{}{}
	for key, group := range observations {
		sort.Slice(group, func(i, j int) bool {
			if group[i].result.FinalScore != group[j].result.FinalScore {
				return group[i].result.FinalScore > group[j].result.FinalScore
			}
			return resultTieKey(group[i].result) < resultTieKey(group[j].result)
		})
		agg := facetAggregate{key: key, result: group[0].result, bestRank: group[0].rank, bestScore: group[0].result.FinalScore, observedFacets: map[string]struct{}{}}
		for index, observation := range group {
			if index > 0 {
				agg.result.Candidate = search.MergeCandidate(agg.result.Candidate, observation.result.Candidate)
			}
			if observation.rank < agg.bestRank {
				agg.bestRank = observation.rank
			}
			if observation.result.FinalScore > agg.bestScore {
				agg.bestScore = observation.result.FinalScore
			}
			agg.observedFacets[observation.facetKey] = struct{}{}
			allFacets[observation.facetKey] = struct{}{}
		}
		agg.facetCount = len(agg.observedFacets)
		agg.result.FinalScore = agg.bestScore
		aggregates = append(aggregates, agg)
	}
	sort.Slice(aggregates, func(i, j int) bool {
		return facetAggregateLess(aggregates[i], aggregates[j])
	})
	if limit > 0 && limit < len(aggregates) && len(allFacets) > 1 && limit >= len(allFacets) {
		reserved := map[string]facetAggregate{}
		for facet := range allFacets {
			for _, aggregate := range aggregates {
				if aggregate.facetCount != 1 {
					continue
				}
				if _, ok := aggregate.observedFacets[facet]; ok {
					reserved[aggregate.key] = aggregate
					break
				}
			}
		}
		if len(reserved) >= len(allFacets) {
			prioritized := make([]facetAggregate, 0, len(aggregates))
			for _, aggregate := range aggregates {
				if _, ok := reserved[aggregate.key]; ok {
					prioritized = append(prioritized, aggregate)
				}
			}
			for _, aggregate := range aggregates {
				if _, ok := reserved[aggregate.key]; !ok {
					prioritized = append(prioritized, aggregate)
				}
			}
			aggregates = prioritized
		}
	}
	if limit > 0 && len(aggregates) > limit {
		aggregates = aggregates[:limit]
	}
	out := make([]search.RankedResult, len(aggregates))
	for i := range aggregates {
		out[i] = aggregates[i].result
	}
	return out
}

func facetAggregateLess(left, right facetAggregate) bool {
	if left.facetCount != right.facetCount {
		return left.facetCount > right.facetCount
	}
	if left.bestRank != right.bestRank {
		return left.bestRank < right.bestRank
	}
	if left.bestScore != right.bestScore {
		return left.bestScore > right.bestScore
	}
	return left.key < right.key
}

func strongestFacetRelevance(support []FacetSupport) (RelevanceLevel, bool) {
	best := RelevanceWeak
	found := false
	for _, facet := range support {
		found = true
		if queryMatchRelevanceRank(facet.Relevance) > queryMatchRelevanceRank(best) {
			best = facet.Relevance
		}
	}
	return best, found
}

func facetSupportFromEvidence(result search.RankedResult, roles []string) []FacetSupport {
	byKey := map[string]FacetSupport{}
	for _, evidence := range result.Evidence {
		if !isValidatedQueryMatch(evidence) {
			continue
		}
		text := strings.TrimSpace(evidence.Details["query"])
		mode := search.Intent(strings.TrimSpace(evidence.Details["mode"]))
		relevance := RelevanceLevel(strings.TrimSpace(evidence.Details["relevance"]))
		if text == "" || (relevance != RelevanceStrong && relevance != RelevanceUseful) {
			continue
		}
		key := text + "\x00" + string(mode)
		candidate := FacetSupport{
			Text: text, Mode: mode, Relevance: relevance,
			Eligibility:       EvidenceEligibility(strings.TrimSpace(evidence.Details["eligibility"])),
			Relationship:      strings.TrimSpace(evidence.Details["relationship"]),
			TargetEstablished: strings.EqualFold(strings.TrimSpace(evidence.Details["target_established"]), "true"),
			LeadsFacet:        strings.TrimSpace(evidence.Details["leads_facet"]) == "true",
			SupportedRoles:    append([]string(nil), roles...),
		}
		current, exists := byKey[key]
		if !exists || facetSupportBetter(candidate, current) {
			byKey[key] = candidate
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]FacetSupport, 0, len(keys))
	for _, key := range keys {
		out = append(out, byKey[key])
	}
	return out
}

func facetSupportBetter(left, right FacetSupport) bool {
	if leftRank, rightRank := queryMatchRelevanceRank(left.Relevance), queryMatchRelevanceRank(right.Relevance); leftRank != rightRank {
		return leftRank > rightRank
	}
	if left.LeadsFacet != right.LeadsFacet {
		return left.LeadsFacet
	}
	if leftProof, rightProof := facetSupportHasExactProof(left), facetSupportHasExactProof(right); leftProof != rightProof {
		return leftProof
	}
	if left.Eligibility != right.Eligibility {
		return left.Eligibility == EvidencePrimary
	}
	if left.TargetEstablished != right.TargetEstablished {
		return left.TargetEstablished
	}
	return strings.Join([]string{left.Relationship, string(left.Eligibility)}, "\x00") <
		strings.Join([]string{right.Relationship, string(right.Eligibility)}, "\x00")
}

func facetSupportHasExactProof(facet FacetSupport) bool {
	relationship := relationshipForIntent(facet.Mode)
	if relationship == "" {
		return false
	}
	return facet.Relevance == RelevanceStrong && facet.Eligibility == EvidencePrimary && facet.TargetEstablished && strings.EqualFold(facet.Relationship, relationship)
}

func applyRequiredFacetCoverage(intent search.Intent, facets []RequiredFacet, sources []SourceAssessment, packet answer.Response) answer.Response {
	required := make(map[string]RequiredFacet, len(facets))
	for _, facet := range facets {
		facet.Text = strings.TrimSpace(facet.Text)
		if facet.Text == "" {
			continue
		}
		if facet.Mode == "" {
			facet.Mode = intent
		}
		required[facet.Text+"\x00"+string(facet.Mode)] = facet
	}
	if len(required) < 2 {
		return packet
	}
	selected := make(map[string]struct{}, len(packet.MustRead))
	for _, item := range packet.MustRead {
		selected[itemIdentity(item)] = struct{}{}
	}
	supported := map[string]struct{}{}
	for _, source := range sources {
		if _, ok := selected[resultIdentity(source.Result)]; !ok {
			continue
		}
		for _, facet := range source.FacetSupport {
			if !facet.Covers() {
				continue
			}
			supported[strings.TrimSpace(facet.Text)+"\x00"+string(facet.Mode)] = struct{}{}
		}
	}
	keys := make([]string, 0, len(required))
	for key := range required {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	missing := make([]RequiredFacet, 0)
	for _, key := range keys {
		if _, ok := supported[key]; !ok {
			missing = append(missing, required[key])
		}
	}
	if len(missing) == 0 {
		return packet
	}
	for _, facet := range missing {
		label := "facet:" + facet.Text
		packet.Coverage.Missing = appendUniqueString(packet.Coverage.Missing, label)
		packet.Confidence.MissingSignals = appendUniqueString(packet.Confidence.MissingSignals, label)
		packet.NextQueries = append(packet.NextQueries, answer.SuggestedQuery{Mode: string(facet.Mode), Query: facet.Text, Reason: "requested facet lacks supported evidence"})
	}
	if packet.Confidence.Level == "high" {
		packet.Confidence.Level = "medium"
	}
	packet.Confidence.Reason = "relevant evidence does not cover every requested facet"
	return answer.RefreshSummary(intent, packet)
}

func singleOrdinaryFacet(intent search.Intent, query string, required map[string]RequiredFacet) bool {
	for _, facet := range required {
		return facet.Mode == intent && strings.TrimSpace(facet.Text) == strings.TrimSpace(query)
	}
	return false
}

func facetSupportQualifies(facet FacetSupport) bool {
	if !search.IsPrecisionIntent(facet.Mode) {
		return true
	}
	return facet.TargetEstablished && facet.Eligibility == EvidencePrimary && facet.Relationship == relationshipForIntent(facet.Mode)
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// Covers reports whether this facet support proves the facet for coverage and
// selection: strong support, or useful support from the facet's own top-ranked
// result, with precision facets still requiring the resolved relationship.
func (f FacetSupport) Covers() bool {
	if f.Relevance != RelevanceStrong && !(f.Relevance == RelevanceUseful && f.LeadsFacet) {
		return false
	}
	return facetSupportQualifies(f)
}
