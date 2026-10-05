package retrieval

import (
	"path/filepath"
	"strings"
)

func tokenizeQuery(q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	fields := strings.FieldsFunc(q, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', '-', '_', '/', '.', ':', '(', ')', '[', ']', '{', '}', ',', ';':
			return true
		default:
			return false
		}
	})
	seen := map[string]struct{}{}
	var tokens []string
	for _, f := range fields {
		if len(f) < 2 {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		tokens = append(tokens, f)
	}
	return tokens
}

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

func titleFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
