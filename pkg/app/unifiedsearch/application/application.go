// Package application owns transport-neutral search policy, source assessment,
// and answer assembly. Resource-owning adapters may prepare different runtime
// dependencies, but they must pass the same effective policy to the shared
// engine and consume these results without reinterpreting relevance.
package application

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/answer"
	answeradapt "github.com/atomicobject/rhizome/pkg/app/answer/adapt"
	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/queryframe"
)

type Profile string

const (
	ProfileInteractive Profile = "interactive"
	ProfileAgent       Profile = "agent"
)

type EffectivePolicy struct {
	Profile         Profile       `json:"profile"`
	VisibleLimit    int           `json:"visibleLimit"`
	CandidateWindow int           `json:"candidateWindow"`
	MaxPerOwner     int           `json:"maxPerOwner"`
	BudgetChars     int           `json:"budgetChars"`
	Deadline        time.Duration `json:"-"`
	DeadlineMillis  int64         `json:"deadlineMillis"`
}

func ResolveEffectivePolicy(profile Profile, intent search.Intent, explicitLimit, explicitBudget, explicitMaxPerOwner int) (EffectivePolicy, error) {
	switch profile {
	case "", ProfileInteractive:
		profile = ProfileInteractive
	case ProfileAgent:
	default:
		return EffectivePolicy{}, fmt.Errorf("unknown search profile %q", profile)
	}
	policy := EffectivePolicy{Profile: profile, VisibleLimit: 10, CandidateWindow: 100, MaxPerOwner: 2, BudgetChars: 8_000, Deadline: 3 * time.Second}
	if profile == ProfileAgent {
		policy.VisibleLimit = 20
		policy.CandidateWindow = 240
		policy.MaxPerOwner = 3
		policy.BudgetChars = 24_000
		policy.Deadline = 8 * time.Second
	}
	if search.IsPrecisionIntent(intent) {
		policy.MaxPerOwner = 1
		policy.CandidateWindow = min(policy.CandidateWindow, 100)
	}
	if explicitLimit > 0 {
		policy.VisibleLimit = explicitLimit
	}
	if explicitBudget > 0 {
		policy.BudgetChars = explicitBudget
	}
	if explicitMaxPerOwner > 0 {
		policy.MaxPerOwner = explicitMaxPerOwner
	}
	if policy.VisibleLimit > policy.CandidateWindow {
		return EffectivePolicy{}, errors.New("visible limit exceeds fixed candidate window")
	}
	policy.DeadlineMillis = policy.Deadline.Milliseconds()
	return policy, nil
}

type Availability string

const (
	AvailabilityComplete    Availability = "complete"
	AvailabilityPartial     Availability = "partial"
	AvailabilityUnavailable Availability = "unavailable"
)

type EvidenceEligibility string

const (
	EvidencePrimary    EvidenceEligibility = "primary"
	EvidenceSupporting EvidenceEligibility = "supporting"
)

type RelevanceLevel string

const (
	RelevanceStrong  RelevanceLevel = "strong"
	RelevanceUseful  RelevanceLevel = "useful"
	RelevanceWeak    RelevanceLevel = "weak"
	RelevanceUnknown RelevanceLevel = "unknown"
)

type SourceAssessment struct {
	Result         search.RankedResult `json:"result"`
	Eligibility    EvidenceEligibility `json:"eligibility"`
	Relationship   string              `json:"relationship,omitempty"`
	Relevance      RelevanceLevel      `json:"relevance"`
	RelevanceWhy   string              `json:"relevanceWhy,omitempty"`
	SupportedRoles []string            `json:"supportedRoles,omitempty"`
	FacetSupport   []FacetSupport      `json:"facetSupport,omitempty"`
}

// FacetSupport binds a source assessment to the specific query facet it
// supports. A source that is strong for one facet does not imply coverage of a
// different facet merely because both were present in the request.
type FacetSupport struct {
	Text              string              `json:"text"`
	Mode              search.Intent       `json:"mode"`
	Relevance         RelevanceLevel      `json:"relevance"`
	Eligibility       EvidenceEligibility `json:"eligibility,omitempty"`
	Relationship      string              `json:"relationship,omitempty"`
	TargetEstablished bool                `json:"targetEstablished,omitempty"`
	// LeadsFacet marks the facet's own top-ranked result, which is what
	// covers a facet when no source can be strong for a paraphrased question.
	LeadsFacet     bool     `json:"leadsFacet,omitempty"`
	SupportedRoles []string `json:"supportedRoles,omitempty"`
}

type RequiredFacet struct {
	Text string
	Mode search.Intent
}

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

type TargetResolution struct {
	Status     search.TargetStatus      `json:"status"`
	Confidence float64                  `json:"confidence,omitempty"`
	Candidates []search.TargetCandidate `json:"candidates,omitempty"`
	Selected   *search.TargetCandidate  `json:"selected,omitempty"`
}

type CountSummary struct {
	RetrievedCandidates int `json:"retrievedCandidates"`
	GroupedSources      int `json:"groupedSources"`
	ReturnedSources     int `json:"returnedSources"`
	OmittedBodies       int `json:"omittedBodies"`
	RemainingWindow     int `json:"remainingWindow"`
}

type Result struct {
	Sources      []SourceAssessment  `json:"sources"`
	Target       TargetResolution    `json:"target"`
	Answer       answer.Response     `json:"answer"`
	Warnings     []search.Warning    `json:"warnings,omitempty"`
	Lanes        []search.LaneStatus `json:"lanes,omitempty"`
	Availability Availability        `json:"availability"`
	Counts       CountSummary        `json:"counts"`
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

// DedupeCanonicalSources preserves the first ranked occurrence of each source
// and folds later evidence into it. Adapters therefore cannot expose multiple
// presentation rows for one canonical source identity.
func DedupeCanonicalSources(results []search.RankedResult) []search.RankedResult {
	out := make([]search.RankedResult, 0, len(results))
	indexes := make(map[string]int, len(results))
	for _, result := range results {
		key := CanonicalSourceIdentity(result)
		if index, ok := indexes[key]; ok {
			out[index].Candidate = search.MergeCandidate(out[index].Candidate, result.Candidate)
			continue
		}
		indexes[key] = len(out)
		out = append(out, result)
	}
	return out
}

// CanonicalizeSources coalesces note chunks by their owner and then removes
// duplicate canonical sources. It preserves the earliest rank while retaining
// the best note score and all deterministic evidence facts.
func CanonicalizeSources(results []search.RankedResult) []search.RankedResult {
	out := make([]search.RankedResult, 0, len(results))
	noteIndexes := make(map[string]int)
	for _, result := range results {
		if result.Type != "note" || result.Owner.String() == "" {
			out = append(out, result)
			continue
		}
		owner := result.Owner.String()
		if index, ok := noteIndexes[owner]; ok {
			bestScore := out[index].FinalScore
			out[index].Candidate = search.MergeCandidate(out[index].Candidate, result.Candidate)
			if result.FinalScore > bestScore {
				out[index].FinalScore = result.FinalScore
			}
			continue
		}
		noteIndexes[owner] = len(out)
		out = append(out, result)
	}
	return DedupeCanonicalSources(out)
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

func CanonicalSourceIdentity(result search.RankedResult) string {
	if result.NodeRef != nil && result.NodeRef.Kind != "" && result.NodeRef.Kind != "NOTE" {
		return strings.Join([]string{"node", result.NodeRef.NotePath, result.NodeRef.NodeID, result.NodeRef.Fragment, result.NodeRef.Structural}, "\x00")
	}
	if result.FQN != "" {
		return strings.Join([]string{"fqn", result.FQN, result.Path}, "\x00")
	}
	if result.Path != "" {
		return "path\x00" + result.Path
	}
	if handle := result.Handle.String(); handle != "" {
		return "handle\x00" + handle
	}
	return "title\x00" + result.Title
}

func resultTieKey(result search.RankedResult) string {
	return strings.Join([]string{CanonicalSourceIdentity(result), result.Symbol, result.Granularity, result.Handle.String()}, "\x00")
}

func Assess(intent search.Intent, results []search.RankedResult) []SourceAssessment {
	return AssessQuery("", intent, results)
}

// AssessQuery classifies ranked sources using the request text when it is
// available. Explicit language qualifiers constrain code results, while notes
// remain eligible to discuss code in any language.
func AssessQuery(query string, intent search.Intent, results []search.RankedResult) []SourceAssessment {
	queryLanguages := search.ExplicitLanguageQualifiers(query)
	concepts := queryConcepts(query)
	out := make([]SourceAssessment, 0, len(results))
	for _, result := range results {
		assessment := SourceAssessment{Result: result, Eligibility: EvidenceSupporting, Relevance: RelevanceWeak}
		if search.HasPrimaryEvidence(intent, result.Evidence) {
			assessment.Eligibility = EvidencePrimary
			assessment.Relationship = relationshipForIntent(intent)
			assessment.Relevance = RelevanceStrong
			assessment.RelevanceWhy = "requested structural relationship is present"
		} else {
			specificity := queryframe.SupportSpecificityScore(result.Evidence)
			channels := search.AggregateEvidenceScoresForRanking(result.Evidence)
			semanticScore := channels[search.EvidenceChannelSemantic]
			// A lexical lane corroborates only when it matched at least half the
			// query's meaningful concepts; one shared word cannot turn a nearest
			// neighbour into strong evidence. Standing alone as strong evidence
			// is a higher bar, applied below: every concept must be matched.
			lexicalCorroboration := sourceOwnedLexicalScore(result.Evidence)
			if bareTokenQuery(concepts) || queryTermCoverage(concepts, result.Evidence) < 0.5 {
				lexicalCorroboration = 0
			}
			corroboratingScore := max(lexicalCorroboration, channels[search.EvidenceChannelOntologyStructural])
			semantic := semanticScore >= 0.25 && !search.IsPrecisionIntent(intent)
			// A second lane is corroborating only when both observations carry
			// meaningful normalized support. Mere lane presence must not turn two
			// near-zero retrieval artifacts into strong evidence. These provisional
			// cutoffs are measured and revised against independently judged
			// development cases; held-out cases never tune them.
			corroborated := semanticScore >= 0.35 && corroboratingScore >= 0.25 && result.FinalScore >= 0.45
			switch {
			case hasAnyEvidence(result, "path_exact", "note_title_exact", "symbol_exact", "definition_anchor"):
				assessment.Relevance = RelevanceStrong
				assessment.RelevanceWhy = "canonical source identity matches the request"
			case specificity >= 0.7 && !bareTokenQuery(concepts) && queryTermCoverage(concepts, result.Evidence) >= 1:
				assessment.Relevance = RelevanceStrong
				assessment.RelevanceWhy = "source-owned identity or content strongly matches the query"
			case semantic && corroborated:
				assessment.Relevance = RelevanceStrong
				assessment.RelevanceWhy = "semantic relevance is corroborated by an independent evidence lane"
			case semantic:
				assessment.Relevance = RelevanceUseful
				assessment.RelevanceWhy = "semantic evidence supports a paraphrase match"
			case specificity >= 0.25 || channels[search.EvidenceChannelLexical] >= 0.25:
				assessment.Relevance = RelevanceUseful
				assessment.RelevanceWhy = "source text or identity partially matches the query"
			default:
				assessment.RelevanceWhy = "retrieved evidence lacks direct query support"
			}
		}
		assessment.SupportedRoles = []string{roleForResult(result)}
		if assessment.Relevance == RelevanceStrong && codeResultLanguageConflicts(result, queryLanguages) {
			assessment.Relevance = RelevanceUseful
			assessment.RelevanceWhy = "code language does not match the explicit query language"
		}
		if assessment.Relevance == RelevanceStrong && assessment.Eligibility != EvidencePrimary && explicitEntitySupportUnestablished(query, result) {
			assessment.Relevance = RelevanceUseful
			assessment.RelevanceWhy = "retrieved evidence does not establish an explicit query entity"
		}
		assessment.FacetSupport = facetSupportFromEvidence(result, assessment.SupportedRoles)
		if facetRelevance, ok := strongestFacetRelevance(assessment.FacetSupport); ok {
			assessment.Relevance = facetRelevance
			assessment.RelevanceWhy = "strongest query-local facet assessment"
		}
		out = append(out, assessment)
	}
	return out
}

// queryConcepts returns the deduplicated meaningful query concepts, each with
// the term variants queryframe scores against. This is the term inventory the
// `matched` evidence detail is drawn from: question words, stop words, and
// repeated tokens are not part of it and must not dilute coverage.
func queryConcepts(query string) [][]string {
	frame := queryframe.Extract(query)
	if len(frame.SupportTermGroups) > 0 {
		concepts := make([][]string, 0, len(frame.SupportTermGroups))
		for _, group := range frame.SupportTermGroups {
			// "explain" is the directive the frame already captured as its
			// action, not content the source has to contain.
			if len(group) == 1 && group[0] == "explain" {
				continue
			}
			concepts = append(concepts, group)
		}
		if len(concepts) == 0 {
			// The directive was the whole query, so it is all the caller gave us
			// to match; dropping it would leave no concept to cover and let
			// routing metadata alone read as strong.
			return frame.SupportTermGroups
		}
		return concepts
	}
	// Every token was a question or stop word, so scoring falls back to all
	// terms; coverage must use the same set.
	groups := make([][]string, 0, len(frame.Terms))
	for _, term := range frame.Terms {
		groups = append(groups, []string{term})
	}
	return groups
}

// bareTokenQuery reports a query with exactly one meaningful concept; an empty
// query carries no term evidence and is not gated. A single
// token that happens to appear in a file name is not identity evidence.
func bareTokenQuery(concepts [][]string) bool { return len(concepts) == 1 }

// IdentityMatch reports canonical identity evidence: exact path, exact note
// title, exact symbol, or a definition anchor.
func (a SourceAssessment) IdentityMatch() bool {
	return hasAnyEvidence(a.Result, "path_exact", "note_title_exact", "symbol_exact", "definition_anchor")
}

// StrongLanes counts distinct retrieval lanes whose evidence for this source
// carries meaningful support (normalized raw score >= 0.25).
func (a SourceAssessment) StrongLanes() int {
	lanes := map[string]struct{}{}
	for _, evidence := range a.Result.Evidence {
		if evidence.RawScore < 0.25 {
			continue
		}
		for _, lane := range search.LanesForEvidence(evidence.Type) {
			lanes[lane] = struct{}{}
		}
	}
	return len(lanes)
}

var quotedQueryEntityREs = []*regexp.Regexp{
	regexp.MustCompile("`([^`]+)`"),
	regexp.MustCompile(`"([^"]+)"`),
}

func explicitEntitySupportUnestablished(query string, result search.RankedResult) bool {
	entities := explicitQueryEntities(query)
	if len(entities) == 0 {
		return false
	}
	for _, entity := range entities {
		if anyTextContainsEntity(entity, result.Path, result.Title, result.Symbol, result.FQN, result.Breadcrumb, result.Heading) || evidenceContainsEntity(result.Evidence, entity) {
			continue
		}
		return true
	}
	return false
}

func anyTextContainsEntity(entity []string, values ...string) bool {
	for _, value := range values {
		if containsTermSequence(queryframe.PhraseTerms(value), entity) {
			return true
		}
	}
	return false
}

func evidenceContainsEntity(evidence []search.Evidence, entity []string) bool {
	for _, item := range evidence {
		if item.Details == nil || strings.EqualFold(strings.TrimSpace(item.Details["pathOnly"]), "true") || !sourceOwnedSnippetEvidence(item) {
			continue
		}
		if containsTermSequence(queryframe.PhraseTerms(item.Details["snippet"]), entity) {
			return true
		}
	}
	return false
}

func sourceOwnedSnippetEvidence(evidence search.Evidence) bool {
	switch strings.TrimSpace(evidence.Type) {
	case "intel_fts_match", "intel_doc_match":
		return strings.TrimSpace(evidence.Source) == "pkg/anchors/sqlite"
	case "rationale_fts_match":
		return strings.TrimSpace(evidence.Source) == "rationale_fts"
	default:
		return false
	}
}

func sourceOwnedLexicalScore(evidence []search.Evidence) float64 {
	owned := make([]search.Evidence, 0, len(evidence))
	for _, item := range evidence {
		if item.Details == nil || strings.EqualFold(strings.TrimSpace(item.Details["pathOnly"]), "true") || strings.TrimSpace(item.Details["snippet"]) == "" || !sourceOwnedSnippetEvidence(item) {
			continue
		}
		owned = append(owned, item)
	}
	return search.AggregateEvidenceScoresForRanking(owned)[search.EvidenceChannelLexical]
}

func containsTermSequence(text, sequence []string) bool {
	if len(sequence) == 0 || len(sequence) > len(text) {
		return false
	}
	for start := 0; start+len(sequence) <= len(text); start++ {
		matched := true
		for offset := range sequence {
			if !strings.EqualFold(text[start+offset], sequence[offset]) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func explicitQueryEntities(query string) [][]string {
	var entities [][]string
	seen := map[string]struct{}{}
	add := func(value string) {
		terms := queryframe.PhraseTerms(value)
		if len(terms) == 0 {
			return
		}
		key := strings.Join(terms, "\x00")
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		entities = append(entities, terms)
	}
	for _, pattern := range quotedQueryEntityREs {
		for _, match := range pattern.FindAllStringSubmatch(query, -1) {
			add(match[1])
		}
	}
	fields := strings.Fields(query)
	for index, field := range fields {
		if strings.ContainsAny(field, "`\"") {
			continue
		}
		token := strings.Trim(field, "`'\"()[]{}<>,:;.!?")
		if token == "" || isQueryLanguageName(token) || explicitlyQuotedInQuery(query, token) {
			continue
		}
		if codeFormEntity(token) || (startsUpper(token) && hasNamedEntityCue(fields, index)) {
			add(token)
		}
	}
	return entities
}

func hasNamedEntityCue(fields []string, index int) bool {
	if index <= 0 || index >= len(fields) {
		return false
	}
	previous := strings.ToLower(strings.Trim(fields[index-1], "`'\"()[]{}<>,:;.!?"))
	switch previous {
	case "by", "to", "for", "from", "with", "owner", "assignee":
		return true
	default:
		return false
	}
}

func isQueryLanguageName(token string) bool {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "python", "go", "golang", "typescript", "javascript", "csharp", "c#", "php":
		return true
	default:
		return false
	}
}

func explicitlyQuotedInQuery(query, token string) bool {
	for _, quote := range []string{"`", "\""} {
		if strings.Contains(query, quote+token+quote) {
			return true
		}
	}
	return false
}

func codeFormEntity(token string) bool {
	if strings.ContainsAny(token, ".:_/\\") {
		return true
	}
	for index, r := range []rune(token) {
		if index > 0 && (unicode.IsUpper(r) || unicode.IsDigit(r)) {
			return true
		}
	}
	return false
}

func startsUpper(token string) bool {
	for _, r := range token {
		return unicode.IsUpper(r)
	}
	return false
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

func isValidatedQueryMatch(evidence search.Evidence) bool {
	return evidence.Type == "query_match" && evidence.Source == "multi_query" && evidence.Details != nil &&
		strings.TrimSpace(evidence.Details["query"]) != "" && strings.TrimSpace(evidence.Details["mode"]) != ""
}

func queryMatchRelevanceRank(relevance RelevanceLevel) int {
	switch relevance {
	case RelevanceStrong:
		return 2
	case RelevanceUseful:
		return 1
	default:
		return 0
	}
}

func codeResultLanguageConflicts(result search.RankedResult, allowed []codeanchor.Lang) bool {
	if len(allowed) == 0 || !isCodeResult(result) {
		return false
	}
	actual := codeLanguageFromPath(result.Path)
	if actual == "" {
		return false
	}
	for _, language := range allowed {
		if actual == language {
			return false
		}
	}
	return true
}

func isCodeResult(result search.RankedResult) bool {
	if result.NodeRef != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(result.Type)) {
	case "code", "anchor", "code_chunk":
		return true
	}
	return strings.TrimSpace(result.FQN) != ""
}

func codeLanguageFromPath(path string) codeanchor.Lang {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(path)))
	if codefile.IsTypeScriptJavaScriptExtension(ext) {
		return codeanchor.LangTS
	}
	switch ext {
	case ".py":
		return codeanchor.LangPy
	case ".go":
		return codeanchor.LangGo
	case ".cs":
		return codeanchor.LangCs
	case ".php":
		return codeanchor.LangPhp
	default:
		return ""
	}
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

func targetSpecEstablished(spec search.QuerySpec) bool {
	return targetResolutionEstablished(spec.TargetStatus) && spec.ResolutionConfidence >= 0.9 && spec.ResolvedTarget != nil
}

func ResolveNavigationTarget(current TargetResolution, sources []SourceAssessment) TargetResolution {
	if targetResolutionEstablished(current.Status) || current.Status == search.TargetStatusAmbiguous {
		return current
	}
	var candidates []search.TargetCandidate
	for _, source := range sources {
		if !hasAnyEvidence(source.Result, "note_title_exact", "path_exact") {
			continue
		}
		result := source.Result
		candidates = append(candidates, search.TargetCandidate{Path: result.Path, Symbol: result.Symbol, FQN: result.FQN, Kind: result.Type, Reason: "exact_navigation_match"})
	}
	switch len(candidates) {
	case 0:
		return current
	case 1:
		selected := candidates[0]
		return TargetResolution{Status: search.TargetStatusInferredPath, Confidence: 1, Candidates: candidates, Selected: &selected}
	default:
		return TargetResolution{Status: search.TargetStatusAmbiguous, Confidence: 0.3, Candidates: candidates}
	}
}

// ResolvePrecisionEvidenceTarget binds an explicit file seed to the one target
// proved by the returned structural evidence. File seeds can name a module with
// several declarations, so conflicting or incomplete proof remains unresolved.
func ResolvePrecisionEvidenceTarget(intent search.Intent, current TargetResolution, sources []SourceAssessment) TargetResolution {
	if !search.IsPrecisionIntent(intent) || current.Selected != nil {
		return current
	}
	targets := map[string]search.TargetCandidate{}
	for _, source := range sources {
		if source.Eligibility != EvidencePrimary || source.Relationship != relationshipForIntent(intent) {
			continue
		}
		for _, evidence := range source.Result.Evidence {
			if !search.HasPrimaryEvidence(intent, []search.Evidence{evidence}) || evidence.Details == nil {
				continue
			}
			fqn := strings.TrimSpace(evidence.Details["target_fqn"])
			if fqn == "" {
				continue
			}
			targets[fqn] = search.TargetCandidate{FQN: fqn, Reason: "structural_evidence_target"}
		}
	}
	if len(targets) != 1 {
		return current
	}
	for _, selected := range targets {
		current.Selected = &selected
		current.Candidates = dedupeTargetCandidates(append(current.Candidates, selected))
	}
	return current
}

func dedupeTargetCandidates(candidates []search.TargetCandidate) []search.TargetCandidate {
	seen := map[string]struct{}{}
	out := make([]search.TargetCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		key := strings.Join([]string{candidate.FQN, candidate.Path, candidate.Symbol}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func AvailabilityFromLanes(lanes []search.LaneStatus) Availability {
	if len(lanes) == 0 {
		return AvailabilityUnavailable
	}
	available, unavailable := 0, 0
	for _, lane := range lanes {
		switch lane.Status {
		case search.LaneStateRan, search.LaneStateEmpty:
			available++
		case search.LaneStateCanceled, search.LaneStateTimedOut, search.LaneStateDegraded:
			unavailable++
		}
	}
	if available == 0 {
		return AvailabilityUnavailable
	}
	if unavailable > 0 {
		return AvailabilityPartial
	}
	return AvailabilityComplete
}

func BuildAssessedAnswer(intent search.Intent, query string, target TargetResolution, warnings []search.Warning, availability Availability, sources []SourceAssessment) answer.Response {
	return buildAssessedAnswer(intent, query, nil, target, warnings, availability, sources)
}

func buildAssessedAnswer(intent search.Intent, query string, facets []RequiredFacet, target TargetResolution, warnings []search.Warning, availability Availability, sources []SourceAssessment) answer.Response {
	inputs := make([]answer.Input, 0, len(sources))
	strong, useful := 0, 0
	for _, source := range sources {
		if source.Relevance == RelevanceWeak || source.Relevance == RelevanceUnknown {
			continue
		}
		if source.Relevance == RelevanceStrong {
			strong++
		} else {
			useful++
		}
		r := source.Result
		facetProofs := make([]answer.FacetProof, 0, len(source.FacetSupport))
		for _, facet := range source.FacetSupport {
			facetProofs = append(facetProofs, answer.FacetProof{
				Key: strings.TrimSpace(facet.Text) + "\x00" + string(facet.Mode), Mode: facet.Mode, Strong: facet.Covers(),
				Eligibility: string(facet.Eligibility), Relationship: facet.Relationship, TargetEstablished: facet.TargetEstablished,
			})
		}
		directSpecificity := queryframe.ScoreFields(queryframe.Extract(query), queryframe.Fields{Path: r.Path, Title: r.Title, Symbol: r.Symbol, FQN: r.FQN}).Value
		inputs = append(inputs, answer.Input{Type: r.Type, Path: r.Path, Title: r.Title, Symbol: r.Symbol, FQN: r.FQN, Score: r.FinalScore, Specificity: queryframe.SpecificityScore(r.Evidence), DirectSpecificity: directSpecificity, Granularity: r.Granularity, NodeRef: answeradapt.NodeRef(r.NodeRef), SupportingOnly: search.IsPrecisionIntent(intent) && source.Eligibility != EvidencePrimary, SupportedRoles: append([]string(nil), source.SupportedRoles...), Relevance: string(source.Relevance), Eligibility: string(source.Eligibility), Relationship: source.Relationship, FacetProofs: facetProofs, IdentityMatch: hasAnyEvidence(r, "path_exact", "note_title_exact", "symbol_exact", "definition_anchor")})
	}
	var packet answer.Response
	if len(facets) == 0 {
		packet = answer.Build(intent, query, target.Status, warnings, inputs)
	} else {
		keys := make([]string, 0, len(facets))
		for _, facet := range facets {
			mode := facet.Mode
			if mode == "" {
				mode = intent
			}
			keys = append(keys, strings.TrimSpace(facet.Text)+"\x00"+string(mode))
		}
		packet = answer.BuildForFacets(intent, query, target.Status, warnings, inputs, keys)
	}
	return finalizeAssessedConfidence(intent, target, availability, strong, useful, sources, packet)
}

func finalizeAssessedConfidence(intent search.Intent, target TargetResolution, availability Availability, strong, useful int, sources []SourceAssessment, packet answer.Response) answer.Response {
	strongByIdentity := make(map[string]bool, len(sources))
	byIdentity := make(map[string]SourceAssessment, len(sources))
	for _, source := range sources {
		identity := resultIdentity(source.Result)
		strongByIdentity[identity] = strongByIdentity[identity] || source.Relevance == RelevanceStrong
		if existing, ok := byIdentity[identity]; !ok || existing.Relevance != RelevanceStrong {
			byIdentity[identity] = source
		}
	}
	allSelectedStrong := len(packet.MustRead) > 0
	mustReadHasIdentityOrTwoLanes := false
	for _, item := range packet.MustRead {
		identity := itemIdentity(item)
		if !strongByIdentity[identity] {
			allSelectedStrong = false
		}
		if source, ok := byIdentity[identity]; ok && (source.IdentityMatch() || source.StrongLanes() >= 2) {
			mustReadHasIdentityOrTwoLanes = true
		}
	}
	switch {
	case target.Status == search.TargetStatusAmbiguous || target.Status == search.TargetStatusUnresolved:
		packet.Confidence = answer.ConfidenceReport{Level: "low", Reason: "target resolution is not unique"}
	case strong == 0 && useful == 0:
		packet.Confidence = answer.ConfidenceReport{Level: "low", Reason: "no source has direct relevance support"}
		packet.MustRead = nil
	case availability == AvailabilityUnavailable:
		packet.Confidence = answer.ConfidenceReport{Level: "low", Reason: "required retrieval evidence is unavailable"}
	case search.IsPrecisionIntent(intent) && (!targetResolutionEstablished(target.Status) || target.Confidence < 0.9 || target.Selected == nil):
		packet.Confidence = answer.ConfidenceReport{Level: "low", Reason: "exact target resolution is not established"}
	case strong == 0:
		packet.Confidence = answer.ConfidenceReport{Level: "low", Reason: "no source has identity-level or corroborated support"}
	case !allSelectedStrong || availability == AvailabilityPartial || len(packet.Coverage.Missing) > 0:
		packet.Confidence = answer.ConfidenceReport{Level: "medium", Reason: "relevant evidence is useful but incomplete", MissingSignals: append([]string(nil), packet.Coverage.Missing...)}
	case !mustReadHasIdentityOrTwoLanes:
		packet.Confidence = answer.ConfidenceReport{Level: "medium", Reason: "strong evidence rests on one lane without an identity match"}
	default:
		packet.Confidence = answer.ConfidenceReport{Level: "high", Reason: "strong relevant evidence covers the requested task"}
	}
	if packet.Confidence.Level == "low" && !search.IsPrecisionIntent(intent) && packet.Confidence.Reason == "no source has identity-level or corroborated support" {
		packet = demoteMustReadWithoutQueryAboutness(byIdentity, packet)
	}
	return answer.RefreshSummary(intent, packet)
}

// queryAboutnessEvidence lists the evidence types that show a source is about
// the query itself. Graph popularity (hub, authority, pagerank, anchor edges,
// seeds) only shows that a source is well connected. A body-term hit
// (intel_fts_match) counts only when the query-specificity support score says
// the source carries a real share of the query, which is what separates a
// code answer from a file that merely mentions one of the words.
var queryAboutnessEvidence = []string{"note_title_match", "intel_doc_match", "symbol_match", "query_match", "note_vector_similarity"}

const (
	// lowConfidenceMustReadFloor is the FinalScore below which a low-confidence
	// page keeps no must-read. Measured across the repository and fixture
	// development splits: no-answer controls score at most 0.84, and judged
	// answers almost never score below 0.87.
	lowConfidenceMustReadFloor = 0.85
	// bodyTermSupportFloor is the query-specificity support a body-term-only hit
	// needs before it counts as being about the query.
	bodyTermSupportFloor = 0.45
)

// sourceIsAboutQuery reports whether a source's own evidence ties it to the
// query.
func sourceIsAboutQuery(source SourceAssessment) bool {
	if source.IdentityMatch() {
		return true
	}
	support := 0.0
	bodyHit := false
	for _, evidence := range source.Result.Evidence {
		for _, aboutness := range queryAboutnessEvidence {
			if evidence.Type == aboutness {
				return true
			}
		}
		switch evidence.Type {
		case "intel_fts_match":
			bodyHit = true
		case "query_specificity":
			if evidence.Source == "queryframe" && evidence.Details != nil {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(evidence.Details["support_score"]), 64); err == nil && parsed > support {
					support = parsed
				}
			}
		}
	}
	return bodyHit && support >= bodyTermSupportFloor
}

// demoteMustReadWithoutQueryAboutness moves must-read items that no source
// shows to be about the query, or that score too low to carry a page on their
// own, to the front of supporting. A low-confidence page that keeps nothing
// says so instead of naming an unrelated source.
func demoteMustReadWithoutQueryAboutness(byIdentity map[string]SourceAssessment, packet answer.Response) answer.Response {
	kept := make([]answer.Item, 0, len(packet.MustRead))
	demoted := make([]answer.Item, 0, len(packet.MustRead))
	for _, item := range packet.MustRead {
		source, ok := byIdentity[itemIdentity(item)]
		if ok && source.Result.FinalScore >= lowConfidenceMustReadFloor && sourceIsAboutQuery(source) {
			kept = append(kept, item)
			continue
		}
		demoted = append(demoted, item)
	}
	if len(demoted) == 0 {
		return packet
	}
	seen := make(map[string]struct{}, len(packet.Supporting))
	for _, item := range packet.Supporting {
		seen[itemIdentity(item)] = struct{}{}
	}
	front := make([]answer.Item, 0, len(demoted))
	for _, item := range demoted {
		identity := itemIdentity(item)
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		front = append(front, item)
	}
	packet.Supporting = append(front, packet.Supporting...)
	if len(kept) == 0 {
		packet.MustRead = nil
		packet.Confidence.Reason = "no source is about the query"
		return packet
	}
	packet.MustRead = kept
	return packet
}

// BuildAssessedAnswerForFacets applies the same pure answer assembly while
// requiring independently supported coverage for every distinct query facet.
func BuildAssessedAnswerForFacets(intent search.Intent, query string, facets []RequiredFacet, target TargetResolution, warnings []search.Warning, availability Availability, sources []SourceAssessment) answer.Response {
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
	if len(required) == 0 || (len(required) == 1 && singleOrdinaryFacet(intent, query, required)) {
		return buildAssessedAnswer(intent, query, nil, target, warnings, availability, sources)
	}
	packet := buildAssessedAnswer(intent, query, facets, target, warnings, availability, sources)
	return applyRequiredFacetCoverage(intent, facets, sources, packet)
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

func resultIdentity(result search.RankedResult) string {
	if result.NodeRef != nil {
		return strings.Join([]string{"node", result.NodeRef.NotePath, result.NodeRef.NodeID, result.NodeRef.Fragment, result.NodeRef.Structural}, "\x00")
	}
	if result.FQN != "" {
		return strings.Join([]string{"fqn", result.FQN, result.Path}, "\x00")
	}
	if result.Path != "" {
		return "path\x00" + result.Path
	}
	if handle := result.Handle.String(); handle != "" {
		return "handle\x00" + handle
	}
	return ""
}

func itemIdentity(item answer.Item) string {
	if item.NodeRef != nil {
		return strings.Join([]string{"node", item.NodeRef.NotePath, item.NodeRef.NodeID, item.NodeRef.Fragment, item.NodeRef.StructuralFingerprint}, "\x00")
	}
	if item.FQN != "" {
		return strings.Join([]string{"fqn", item.FQN, item.Path}, "\x00")
	}
	return "path\x00" + item.Path
}

func hasAnyEvidence(result search.RankedResult, types ...string) bool {
	for _, evidence := range result.Evidence {
		for _, typ := range types {
			if evidence.Type == typ {
				return true
			}
		}
	}
	return false
}

func targetResolutionEstablished(status search.TargetStatus) bool {
	switch status {
	case search.TargetStatusExplicitPath, search.TargetStatusInferredPath, search.TargetStatusInferredSymbol:
		return true
	default:
		return false
	}
}

func relationshipForIntent(intent search.Intent) string {
	switch intent {
	case search.IntentGoToDef:
		return "definition"
	case search.IntentTestsForCode:
		return "tests"
	case search.IntentCallers:
		return "caller"
	case search.IntentCallees:
		return "callee"
	case search.IntentFindUsages:
		return "usage"
	case search.IntentImports:
		return "import"
	case search.IntentImplementers:
		return "implementation"
	case search.IntentOverrides:
		return "override"
	default:
		return ""
	}
}

func roleForResult(result search.RankedResult) string {
	if search.IsTestPath(result.Path) {
		return "test"
	}
	// Code anchor retrievers use Type "anchor". FQN distinguishes those
	// canonical code symbols from embedded note anchors, whose NodeRef remains
	// documentation evidence.
	if result.Type == "code" || (result.NodeRef == nil && (result.FQN != "" || strings.EqualFold(result.Kind, "module"))) {
		return "implementation"
	}
	return "documentation"
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

// queryTermCoverage is the share of the query's meaningful concepts that the
// ranking-time specificity evidence actually matched. One shared token in a
// long question is not strong support, however high its identity score, so
// source-owned identity or content is strong only at full coverage.
func queryTermCoverage(concepts [][]string, evidence []search.Evidence) float64 {
	if len(concepts) == 0 {
		return 1
	}
	matched := map[string]struct{}{}
	sawMatched := false
	for _, item := range evidence {
		if item.Type != "query_specificity" || item.Source != "queryframe" || item.Details == nil {
			continue
		}
		if _, ok := item.Details["matched"]; ok {
			sawMatched = true
		}
		for _, term := range strings.Split(item.Details["matched"], ",") {
			if term = strings.ToLower(strings.TrimSpace(term)); term != "" {
				matched[term] = struct{}{}
			}
		}
	}
	if !sawMatched {
		// No term inventory was recorded; do not block on unknown coverage.
		return 1
	}
	covered := 0
	for _, concept := range concepts {
		for _, term := range concept {
			if _, ok := matched[term]; ok {
				covered++
				break
			}
		}
	}
	return float64(covered) / float64(len(concepts))
}

// DemoteNonTargetDefinitions keeps only the resolved target's own declaration
// as primary definition evidence. Same-named members in the same file also
// carry definition anchors, but they are not the definition the caller asked
// for and must not count as primary results.
func DemoteNonTargetDefinitions(intent search.Intent, target TargetResolution, sources []SourceAssessment) []SourceAssessment {
	if intent != search.IntentGoToDef || target.Selected == nil || strings.TrimSpace(target.Selected.FQN) == "" {
		return sources
	}
	want := strings.TrimSpace(target.Selected.FQN)
	out := make([]SourceAssessment, len(sources))
	for i, source := range sources {
		if source.Eligibility == EvidencePrimary && strings.TrimSpace(source.Result.FQN) != want {
			source.Eligibility = EvidenceSupporting
			source.Relationship = ""
			if source.Relevance == RelevanceStrong {
				source.Relevance = RelevanceUseful
				source.RelevanceWhy = "same-named declaration is not the resolved definition"
			}
		}
		out[i] = source
	}
	return out
}
