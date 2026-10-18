package retrieval

import (
	"strings"
)

func filterDocTokens(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	docWords := map[string]struct{}{
		"doc":           {},
		"docs":          {},
		"document":      {},
		"documents":     {},
		"documentation": {},
	}
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if _, ok := docWords[tok]; ok {
			continue
		}
		out = append(out, tok)
	}
	return out
}

func tokenMatchScore(tokens []string, haystack string) float64 {
	if len(tokens) == 0 {
		return 0
	}
	haystack = strings.ToLower(haystack)
	if haystack == "" {
		return 0
	}
	hits := 0
	for _, tok := range tokens {
		if strings.Contains(haystack, tok) {
			hits++
		}
	}
	if hits == 0 {
		return 0
	}
	return float64(hits) / float64(len(tokens))
}
