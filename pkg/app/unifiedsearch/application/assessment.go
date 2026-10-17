package application

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/queryframe"
)

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
			case search.HasEvidence(result.Evidence, "path_exact", "note_title_exact", "symbol_exact", "definition_anchor"):
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
	return search.HasEvidence(a.Result.Evidence, "path_exact", "note_title_exact", "symbol_exact", "definition_anchor")
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
		return strings.TrimSpace(evidence.Source) == "pkg/store"
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
