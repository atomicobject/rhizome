package queryframe

import (
	"sort"
	"strings"
	"unicode"
)

func tokenize(text string) []string {
	out := []string{}
	visitTokens(text, func(token string) bool { out = append(out, token); return true })
	return out
}

// visitTokens is the canonical Unicode/camel boundary scanner. Consumers that
// only test membership can stop without allocating a token list for every title.
func visitTokens(text string, visit func(string) bool) {
	start := -1
	prevLower := false
	emit := func(end int) bool {
		if start < 0 || end-start < 2 {
			return true
		}
		return visit(strings.ToLower(text[start:end]))
	}
	for index, r := range text {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			if !emit(index) {
				return
			}
			start = -1
			prevLower = false
			continue
		}
		if start < 0 {
			start = index
		} else if unicode.IsUpper(r) && prevLower {
			if !emit(index) {
				return
			}
			start = index
		}
		prevLower = unicode.IsLower(r)
	}
	emit(len(text))
}

func appendTokenVariants(tokens []string, tok string) []string {
	visitTokenVariants(tok, func(variant string) { tokens = append(tokens, variant) })
	return tokens
}

func visitTokenVariants(tok string, visit func(string)) {
	switch {
	case strings.HasSuffix(tok, "ies") && len(tok) > 3:
		visit(strings.TrimSuffix(tok, "ies") + "y")
	case strings.HasSuffix(tok, "s") && len(tok) > 3:
		visit(strings.TrimSuffix(tok, "s"))
	}
	switch tok {
	case "embedded", "embedding", "embeddings", "embedder", "embedders":
		visit("embed")
		visit("embedded")
		visit("embedding")
		visit("embeddings")
	case "embed":
		visit("embedded")
		visit("embedding")
		visit("embeddings")
	case "notes", "note":
		visit("note")
		visit("notes")
	case "indexed", "indexing", "indexes":
		visit("index")
		visit("indexed")
		visit("indexing")
	}
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

// ConceptMatcher compiles the query's concept groups for repeated title/link
// matching. Coverage owns its scratch state, so one matcher is safe for facets.
type ConceptMatcher struct {
	byTerm map[string][]int
	count  int
}

func (f Frame) ConceptMatcher() ConceptMatcher {
	m := ConceptMatcher{byTerm: make(map[string][]int), count: len(f.SupportTermGroups)}
	for index, group := range f.SupportTermGroups {
		for _, term := range group {
			m.byTerm[term] = append(m.byTerm[term], index)
		}
	}
	return m
}

func (m ConceptMatcher) Coverage(text string) float64 {
	if m.count == 0 {
		return 0
	}
	var local [64]bool
	seen := local[:]
	if m.count > len(local) {
		seen = make([]bool, m.count)
	}
	matched := 0
	match := func(term string) {
		for _, index := range m.byTerm[term] {
			if !seen[index] {
				seen[index] = true
				matched++
			}
		}
	}
	visitTokens(text, func(token string) bool { match(token); visitTokenVariants(token, match); return matched < m.count })
	return float64(matched) / float64(m.count)
}
