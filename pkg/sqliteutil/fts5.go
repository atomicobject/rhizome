package sqliteutil

import (
	"strings"
	"unicode"
)

// FTS5QueryFromText converts free-form user text into a safe FTS5 MATCH expression.
//
// It intentionally does not accept FTS5 query syntax; it extracts "word-ish" tokens
// and joins them with OR to produce a broad-but-rankable lexical query.
func FTS5QueryFromText(text string) string {
	tokens := fts5Tokens(text)
	if len(tokens) == 0 {
		return ""
	}

	// Remove stopwords only when the query is "sentence-y" enough.
	if len(tokens) >= 4 {
		filtered := tokens[:0]
		for _, tok := range tokens {
			if isStopword(tok) {
				continue
			}
			filtered = append(filtered, tok)
		}
		tokens = filtered
	}

	// Drop ultra-short tokens (often noise in FTS).
	filtered := tokens[:0]
	for _, tok := range tokens {
		if len(tok) < 2 {
			continue
		}
		filtered = append(filtered, tok)
	}
	tokens = filtered

	// If we stripped everything, fall back to the raw tokens.
	if len(tokens) == 0 {
		tokens = fts5Tokens(text)
		filtered = tokens[:0]
		for _, tok := range tokens {
			if len(tok) < 2 {
				continue
			}
			filtered = append(filtered, tok)
		}
		tokens = filtered
	}

	// Deduplicate (preserving order) and cap for sanity.
	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	const maxTokens = 12
	for _, tok := range tokens {
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, `"`+tok+`"`)
		if len(out) >= maxTokens {
			break
		}
	}

	return strings.Join(out, " OR ")
}

func fts5Tokens(text string) []string {
	// We only emit tokens that are safe as FTS5 phrase terms, so we can quote
	// each term and avoid operator parsing ("OR", "NOT", etc).
	var out []string
	var buf []rune

	flush := func() {
		if len(buf) == 0 {
			return
		}
		out = append(out, strings.ToLower(string(buf)))
		buf = buf[:0]
	}

	for _, r := range text {
		// Keep letters/digits/underscore, treat everything else as a separator.
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			buf = append(buf, r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func isStopword(tok string) bool {
	switch tok {
	case "a", "an", "and", "are", "as", "at",
		"be", "been", "being", "by",
		"did", "do", "does",
		"for", "from",
		"have", "has",
		"how",
		"i", "in", "is", "it",
		"not",
		"of", "on", "or",
		"that", "the", "their", "these", "this", "those", "to",
		"was", "we", "were", "what", "when", "where", "who", "why", "with",
		"you":
		return true
	default:
		return false
	}
}
