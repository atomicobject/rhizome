package queryframe

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/search"
)

const (
	ActionSearch  = "search"
	ActionExplain = "explain"
)

type Frame struct {
	Query             string
	Action            string
	Terms             []string
	CoreTerms         []string
	SupportTermGroups [][]string
	MentionsTests     bool
}

type Fields struct {
	Path          string
	Title         string
	Symbol        string
	FQN           string
	Breadcrumb    string
	Heading       string
	Snippet       string
	SourceSnippet string
}

type Score struct {
	// Value is identity-field match strength; answer assembly calibrates its
	// confidence thresholds against it.
	Value float64
	// RankValue scores each query concept once per field, capped at its
	// share, so one word repeated across fields cannot cover for unmatched
	// words. It is the query_specificity ranking evidence.
	RankValue        float64
	SupportValue     float64
	Matched          []string
	IdentityMatched  []string
	ContentMatched   []string
	ContentAvailable bool
}

var splitRe = regexp.MustCompile(`[\pL\pN]+`)

func Extract(query string) Frame {
	query = strings.TrimSpace(query)
	raw := tokenize(query)
	terms := orderedSet(raw)
	for _, tok := range raw {
		terms = appendTokenVariants(terms, tok)
	}
	terms = orderedSet(terms)

	core := make([]string, 0, len(terms))
	for _, tok := range terms {
		if isQuestionWord(tok) || isStopWord(tok) {
			continue
		}
		core = append(core, tok)
	}
	if len(core) == 0 {
		core = terms
	}

	action := ActionSearch
	for _, tok := range raw {
		if tok == "how" || tok == "what" || tok == "where" || tok == "why" || tok == "explain" {
			action = ActionExplain
			break
		}
	}

	return Frame{
		Query:             query,
		Action:            action,
		Terms:             terms,
		CoreTerms:         orderedSet(core),
		SupportTermGroups: supportTermGroups(raw),
		MentionsTests:     mentionsTests(terms),
	}
}

func (f Frame) IsExplain() bool {
	return f.Action == ActionExplain
}

func ScoreFields(frame Frame, fields Fields) Score {
	scoringTerms := frame.CoreTerms
	if len(scoringTerms) == 0 {
		scoringTerms = frame.Terms
	}
	if len(scoringTerms) == 0 {
		return Score{}
	}

	fieldWeights := []struct {
		text   string
		weight float64
	}{
		{fields.Path, 1.0},
		{fields.Title, 0.9},
		{fields.Symbol, 1.0},
		{fields.FQN, 1.0},
		{fields.Breadcrumb, 0.8},
		{fields.Heading, 0.8},
		{fields.Snippet, 0.7},
	}

	// For a short, title-like query (not an explanatory question), RankValue
	// scores query concepts (a term and its variants): a concept counts once
	// per field and earns at most its share of the score, so one word repeated
	// across path, title, breadcrumb, and heading cannot stand in for the
	// words a source does not match.
	concepts := conceptIndex(scoringTerms)
	conceptTotals := make([]float64, len(concepts.groups))
	matched := map[string]struct{}{}
	total := 0.0
	for _, field := range fieldWeights {
		raw := strings.ToLower(field.text)
		if raw == "" {
			continue
		}
		normalized := strings.Join(tokenize(raw), " ")
		compact := strings.ReplaceAll(strings.ReplaceAll(normalized, " ", ""), "-", "")
		fieldConcepts := map[int]struct{}{}
		for _, term := range scoringTerms {
			if term == "" {
				continue
			}
			if strings.Contains(raw, term) || strings.Contains(normalized, term) || strings.Contains(compact, strings.ReplaceAll(term, "_", "")) {
				matched[term] = struct{}{}
				total += field.weight
				fieldConcepts[concepts.of[term]] = struct{}{}
			}
		}
		for concept := range fieldConcepts {
			conceptTotals[concept] += field.weight
		}
	}
	if total == 0 {
		return Score{}
	}

	codeBonus := 0.0
	if hasCodeFormMatch(matched) {
		codeBonus = 0.2
	}
	termBonus := 0.0
	if len(matched) >= 2 {
		termBonus = 0.15
	}
	value := math.Min(1, total/math.Max(3, float64(len(scoringTerms)))+termBonus+codeBonus)
	out := make([]string, 0, len(matched))
	for term := range matched {
		out = append(out, term)
	}
	sort.Strings(out)
	identityText := strings.Join([]string{fields.Path, fields.Title, fields.Symbol, fields.FQN, fields.Breadcrumb, fields.Heading}, " ")
	identity := sortedTokenSet(exactMatchedTerms(frame.Terms, identityText))
	content := sortedTokenSet(exactMatchedTerms(frame.Terms, fields.SourceSnippet))
	support := scoreSupportingFields(frame.SupportTermGroups, fields)
	contentAvailable := strings.TrimSpace(fields.SourceSnippet) != ""

	if frame.IsExplain() || len(conceptTotals) > maxTitleConcepts {
		return Score{Value: value, RankValue: value, SupportValue: support, Matched: out, IdentityMatched: identity, ContentMatched: content, ContentAvailable: contentAvailable}
	}
	conceptCount := float64(len(conceptTotals))
	rankValue, matchedConcepts := 0.0, 0
	for _, conceptTotal := range conceptTotals {
		if conceptTotal > 0 {
			matchedConcepts++
		}
		rankValue += math.Min(1/conceptCount, conceptTotal/3)
	}
	if matchedConcepts >= 2 {
		rankValue += 0.15
	}
	rankValue = math.Min(1, rankValue+codeBonus)
	return Score{Value: value, RankValue: rankValue, SupportValue: support, Matched: out, IdentityMatched: identity, ContentMatched: content, ContentAvailable: contentAvailable}
}

// maxTitleConcepts bounds the queries whose concepts must all match to earn
// full ranking specificity. Longer queries and explanatory questions (how,
// what, where, why, explain) rarely name every concept in one title, and a
// strong identity match on their key word is the signal, so they keep Value.
// Short who, which, and when questions keep the cap; "who owns inventory
// reconciliation" measured better with it (nDCG@10 0.834 -> 1.0). Measured on 2026-10-08: the cap dropped
// decorators.py for a seven-concept decorator question (nDCG@10 0.984 ->
// 0.604) and the Container class for "how does Container work?" (1.0 -> 0.71).
const maxTitleConcepts = 3

type conceptGroups struct {
	groups [][]string
	of     map[string]int
}

// conceptIndex groups scoring terms that are variants of one another. A term
// outside every support group (the stop-word fallback) is its own concept.
func conceptIndex(terms []string) conceptGroups {
	index := conceptGroups{groups: supportTermGroups(terms), of: make(map[string]int, len(terms))}
	for i, group := range index.groups {
		for _, term := range group {
			index.of[term] = i
		}
	}
	for _, term := range terms {
		if _, ok := index.of[term]; !ok {
			index.of[term] = len(index.groups)
			index.groups = append(index.groups, []string{term})
		}
	}
	return index
}

func scoreSupportingFields(groups [][]string, fields Fields) float64 {
	if len(groups) == 0 {
		return 0
	}
	identityFields := []struct {
		text   string
		weight float64
	}{{fields.Path, 1}, {fields.Title, .9}, {fields.Symbol, 1}, {fields.FQN, 1}, {fields.Breadcrumb, .8}, {fields.Heading, .8}}
	type weightedTerms struct {
		terms  map[string]struct{}
		weight float64
	}
	identity := make([]weightedTerms, 0, len(identityFields))
	for _, field := range identityFields {
		fieldTerms := map[string]struct{}{}
		for _, term := range tokenize(field.text) {
			fieldTerms[term] = struct{}{}
		}
		identity = append(identity, weightedTerms{terms: fieldTerms, weight: field.weight})
	}
	content := map[string]struct{}{}
	for _, term := range tokenize(fields.SourceSnippet) {
		content[term] = struct{}{}
	}
	total := 0.0
	matchedGroups := 0
	identityGroups := 0
	for _, group := range groups {
		bestIdentity := 0.0
		for _, field := range identity {
			if termGroupMatches(field.terms, group) && field.weight > bestIdentity {
				bestIdentity = field.weight
			}
		}
		best := bestIdentity
		if termGroupMatches(content, group) && .7 > best {
			best = .7
		}
		if best > 0 {
			total += best
			matchedGroups++
			if bestIdentity > 0 {
				identityGroups++
			}
		}
	}
	if total == 0 {
		return 0
	}
	if len(groups) == 1 && identityGroups == 0 {
		return .6
	}
	value := total / float64(len(groups))
	if matchedGroups >= 2 {
		value += .15
	}
	return math.Min(1, value)
}

func termGroupMatches(available map[string]struct{}, group []string) bool {
	for _, term := range group {
		if _, ok := available[term]; ok {
			return true
		}
	}
	return false
}

func ScoreCandidate(frame Frame, c search.Candidate) Score {
	return ScoreFields(frame, Fields{
		Path:          c.Path,
		Title:         strings.TrimSpace(c.Title + " " + linkTextAliases(c.Evidence)),
		Symbol:        c.Symbol,
		FQN:           c.FQN,
		Breadcrumb:    c.Breadcrumb,
		Heading:       c.Heading,
		Snippet:       evidenceSnippet(c.Evidence),
		SourceSnippet: trustworthyEvidenceSnippet(c.Evidence),
	})
}

// IdentifierTerms returns the normalized identifier components used by query
// specificity scoring.
func IdentifierTerms(text string) []string {
	return orderedSet(tokenize(text))
}

// PhraseTerms returns normalized identifier components in source order for
// exact phrase-boundary checks.
func PhraseTerms(text string) []string {
	return tokenize(text)
}

func EnrichCandidate(frame Frame, c search.Candidate) search.Candidate {
	filtered := make([]search.Evidence, 0, len(c.Evidence))
	for _, evidence := range c.Evidence {
		if strings.EqualFold(strings.TrimSpace(evidence.Type), "query_specificity") {
			continue
		}
		filtered = append(filtered, evidence)
	}
	c.Evidence = filtered
	score := ScoreCandidate(frame, c)
	if score.Value <= 0 {
		return c
	}
	c.Evidence = append(c.Evidence, search.MustNormalizeEvidence(search.Evidence{
		Type:     "query_specificity",
		RawScore: score.RankValue,
		Source:   "queryframe",
		Details: map[string]string{
			"matched":           strings.Join(score.Matched, ","),
			"identity_matched":  strings.Join(score.IdentityMatched, ","),
			"content_matched":   strings.Join(score.ContentMatched, ","),
			"content_available": fmt.Sprintf("%t", score.ContentAvailable),
			"support_score":     strconv.FormatFloat(score.SupportValue, 'g', -1, 64),
			"action":            frame.Action,
		},
	}))
	return c
}

func sortedTokenSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func exactMatchedTerms(terms []string, text string) map[string]struct{} {
	matched := map[string]struct{}{}
	available := map[string]struct{}{}
	for _, token := range tokenize(text) {
		available[token] = struct{}{}
	}
	for _, term := range terms {
		if _, ok := available[term]; ok {
			matched[term] = struct{}{}
		}
	}
	return matched
}

func SpecificityScore(evidence []search.Evidence) float64 {
	best := 0.0
	for _, ev := range evidence {
		if strings.EqualFold(strings.TrimSpace(ev.Type), "query_specificity") {
			if score := search.EvidenceScore(ev); score > best {
				best = score
			}
		}
	}
	return best
}

// SupportSpecificityScore returns specificity established by canonical source
// identity or source-owned content. Link labels and other routing snippets do
// not qualify as answer evidence.
func SupportSpecificityScore(evidence []search.Evidence) float64 {
	best := 0.0
	for _, ev := range evidence {
		if ev.Type != "query_specificity" || ev.Source != "queryframe" || ev.Details == nil {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(ev.Details["support_score"]), 64)
		if err == nil && value > best {
			best = value
		}
	}
	return best
}

func tokenize(text string) []string {
	parts := splitRe.FindAllString(text, -1)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		for _, tok := range splitIdentifier(part) {
			if len(tok) >= 2 {
				out = append(out, tok)
			}
		}
	}
	return out
}

func splitIdentifier(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	var b strings.Builder
	var prevLower bool
	for _, r := range text {
		isUpper := unicode.IsUpper(r)
		if isUpper && prevLower && b.Len() > 0 {
			out = append(out, strings.ToLower(b.String()))
			b.Reset()
		}
		b.WriteRune(r)
		prevLower = unicode.IsLower(r)
	}
	if b.Len() > 0 {
		out = append(out, strings.ToLower(b.String()))
	}
	return out
}

func appendTokenVariants(tokens []string, tok string) []string {
	switch {
	case strings.HasSuffix(tok, "ies") && len(tok) > 3:
		tokens = append(tokens, strings.TrimSuffix(tok, "ies")+"y")
	case strings.HasSuffix(tok, "s") && len(tok) > 3:
		tokens = append(tokens, strings.TrimSuffix(tok, "s"))
	}
	switch tok {
	case "embedded", "embedding", "embeddings", "embedder", "embedders":
		tokens = append(tokens, "embed", "embedded", "embedding", "embeddings")
	case "embed":
		tokens = append(tokens, "embedded", "embedding", "embeddings")
	case "notes", "note":
		tokens = append(tokens, "note", "notes")
	case "indexed", "indexing", "indexes":
		tokens = append(tokens, "index", "indexed", "indexing")
	}
	return tokens
}

func supportTermGroups(raw []string) [][]string {
	groups := make([][]string, 0, len(raw))
	for _, token := range raw {
		if isQuestionWord(token) || isStopWord(token) {
			continue
		}
		group := orderedSet(appendTokenVariants([]string{token}, token))
		sort.Strings(group)
		if len(group) == 0 {
			continue
		}
		mergeAt := -1
		for index := 0; index < len(groups); index++ {
			if !termGroupsOverlap(groups[index], group) {
				continue
			}
			if mergeAt < 0 {
				mergeAt = index
				groups[index] = orderedSet(append(groups[index], group...))
				sort.Strings(groups[index])
				continue
			}
			groups[mergeAt] = orderedSet(append(groups[mergeAt], groups[index]...))
			sort.Strings(groups[mergeAt])
			groups = append(groups[:index], groups[index+1:]...)
			index--
		}
		if mergeAt < 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

func termGroupsOverlap(left, right []string) bool {
	for _, l := range left {
		for _, r := range right {
			if l == r {
				return true
			}
		}
	}
	return false
}

func orderedSet(tokens []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		tok = strings.TrimSpace(strings.ToLower(tok))
		if tok == "" {
			continue
		}
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

func containsAny(tokens []string, values ...string) bool {
	set := map[string]struct{}{}
	for _, tok := range tokens {
		set[tok] = struct{}{}
	}
	for _, value := range values {
		if _, ok := set[value]; ok {
			return true
		}
	}
	return false
}

func mentionsTests(tokens []string) bool {
	return containsAny(tokens, "test", "tests", "coverage", "regression")
}

func isQuestionWord(tok string) bool {
	switch tok {
	case "how", "what", "where", "why", "when", "who", "which":
		return true
	default:
		return false
	}
}

func isStopWord(tok string) bool {
	switch tok {
	case "are", "is", "the", "this", "that", "does", "do", "for", "with", "and", "or", "to", "of", "in", "on", "a", "an":
		return true
	default:
		return false
	}
}

func hasCodeFormMatch(matched map[string]struct{}) bool {
	for term := range matched {
		if strings.Contains(term, "_") || strings.Contains(term, ".") || strings.Contains(term, "noteembed") || strings.Contains(term, "noteemb") {
			return true
		}
	}
	return false
}

func evidenceSnippet(evidence []search.Evidence) string {
	var parts []string
	for _, ev := range evidence {
		if ev.Details == nil {
			continue
		}
		for _, key := range []string{"snippet", "heading", "breadcrumb", "matched"} {
			if value := strings.TrimSpace(ev.Details[key]); value != "" {
				parts = append(parts, value)
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return filepath.ToSlash(strings.Join(parts, " "))
}

func trustworthyEvidenceSnippet(evidence []search.Evidence) string {
	var parts []string
	for _, ev := range evidence {
		if ev.Details == nil || strings.EqualFold(strings.TrimSpace(ev.Details["pathOnly"]), "true") {
			continue
		}
		if !sourceOwnedSnippetEvidence(ev) {
			continue
		}
		if value := strings.TrimSpace(ev.Details["snippet"]); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " ")
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

// ConceptCoverage returns the share of the query's concepts whose term or a
// variant appears as a word in text.
func (f Frame) ConceptCoverage(text string) float64 {
	if len(f.SupportTermGroups) == 0 {
		return 0
	}
	words := map[string]struct{}{}
	for _, tok := range tokenize(text) {
		for _, variant := range appendTokenVariants([]string{tok}, tok) {
			words[variant] = struct{}{}
		}
	}
	matched := 0
	for _, group := range f.SupportTermGroups {
		if termGroupMatches(words, group) {
			matched++
		}
	}
	return float64(matched) / float64(len(f.SupportTermGroups))
}

// linkTextAliases joins the labels that several linking notes agree on. They
// name the note the way its title does, so query specificity reads them with
// the title.
func linkTextAliases(evidence []search.Evidence) string {
	var aliases []string
	for _, ev := range evidence {
		if alias := ev.Details[search.LinkTextAliasDetail]; ev.Type == "link_text_match" && alias != "" {
			aliases = append(aliases, alias)
		}
	}
	return strings.Join(aliases, " ")
}
