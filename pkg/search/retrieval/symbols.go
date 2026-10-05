package retrieval

import (
	"strings"
	"unicode"
)

var symbolStopwords = map[string]struct{}{
	"the":         {},
	"a":           {},
	"an":          {},
	"and":         {},
	"or":          {},
	"to":          {},
	"for":         {},
	"of":          {},
	"this":        {},
	"that":        {},
	"these":       {},
	"those":       {},
	"find":        {},
	"show":        {},
	"list":        {},
	"locate":      {},
	"where":       {},
	"who":         {},
	"what":        {},
	"how":         {},
	"explain":     {},
	"definition":  {},
	"definitions": {},
	"usage":       {},
	"usages":      {},
	"callers":     {},
	"callee":      {},
	"callees":     {},
	"calls":       {},
	"tests":       {},
	"testing":     {},
	"docs":        {},
	"doc":         {},
}

func extractSymbolTokens(query string) (fqns []string, symbols []string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	addToken := func(tok string) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return
		}
		lower := strings.ToLower(tok)
		if _, ok := symbolStopwords[lower]; ok && tok == lower {
			return
		}
		if len(tok) < 2 && tok == lower {
			return
		}
		if strings.ContainsAny(tok, "./:") {
			fqns = append(fqns, tok)
			return
		}
		symbols = append(symbols, tok)
	}

	var buf []rune
	flush := func() {
		if len(buf) == 0 {
			return
		}
		addToken(string(buf))
		buf = buf[:0]
	}

	for _, r := range query {
		if isSymbolRune(r) {
			buf = append(buf, r)
			continue
		}
		flush()
	}
	flush()

	fqns = dedupeStrings(fqns)
	symbols = dedupeStrings(symbols)
	return fqns, symbols
}

func isSymbolRune(r rune) bool {
	switch {
	case unicode.IsLetter(r), unicode.IsDigit(r):
		return true
	case r == '_', r == '.', r == '/', r == ':':
		return true
	default:
		return false
	}
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
