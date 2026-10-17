package search

import (
	"slices"
	"strings"
	"unicode"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
)

// targetTokenNamesCode reports whether the query names tok the way a user names
// code: the request is nothing but the identifier, the token is qualified or
// carries an internal capital, the user quoted it, or a code cue word sits next
// to it. A bare lowercase prose word that merely collides with a member name
// fails this test and may only list candidates.
func targetTokenNamesCode(query, tok string, fqns []string, meaningfulWords int) bool {
	if meaningfulWords <= 1 {
		return true
	}
	if strings.ContainsAny(tok, "._:/\\") || symbolIsExplicitlyQualified(tok, fqns) {
		return true
	}
	if strings.ToLower(tok) != tok {
		return true
	}
	if explicitlyQuotedTarget(query, tok) {
		return true
	}
	return targetTokenHasCodeCue(query, tok)
}

func targetTokenHasCodeCue(query, tok string) bool {
	fields := strings.Fields(query)
	for index, field := range fields {
		if !strings.EqualFold(trimTargetFieldPunctuation(field), tok) {
			continue
		}
		for _, neighbor := range []int{index - 1, index + 1} {
			if neighbor < 0 || neighbor >= len(fields) {
				continue
			}
			if _, ok := targetResolutionCodeCues[strings.ToLower(trimTargetFieldPunctuation(fields[neighbor]))]; ok {
				return true
			}
		}
	}
	return false
}

func trimTargetFieldPunctuation(field string) string {
	return strings.TrimSuffix(strings.Trim(field, "`'\"()[]{}<>,:;!?"), ".")
}

func extractTargetSymbolTokens(query string) (fqns []string, symbols []string) {
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
		if _, ok := targetResolutionStopwords[lower]; ok && tok == lower {
			return
		}
		if len(tok) < 2 && tok == lower {
			return
		}
		if strings.ContainsAny(tok, "./:\\") {
			fqns = append(fqns, tok)
			if split := strings.LastIndexAny(tok, ".:/\\"); split >= 0 && split+1 < len(tok) {
				symbols = append(symbols, tok[split+1:])
			}
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
		if isTargetSymbolRune(r) {
			buf = append(buf, r)
			continue
		}
		flush()
	}
	flush()
	fqns, symbols = dedupeStrings(fqns), dedupeStrings(symbols)
	if len(fqns)+len(symbols) > 1 {
		filtered := symbols[:0]
		for _, symbol := range symbols {
			if _, qualifier := targetResolutionQualifiers[strings.ToLower(symbol)]; qualifier && !explicitlyQuotedTarget(query, symbol) {
				continue
			}
			filtered = append(filtered, symbol)
		}
		symbols = filtered
	}
	if len(symbols) > 1 {
		ordered := make([]string, 0, len(symbols))
		for _, symbol := range symbols {
			if !explicitlyQuotedTarget(query, symbol) {
				ordered = append(ordered, symbol)
			}
		}
		for _, symbol := range symbols {
			if explicitlyQuotedTarget(query, symbol) {
				ordered = append(ordered, symbol)
			}
		}
		symbols = ordered
	}
	return fqns, symbols
}

func explicitlyQuotedTarget(query, target string) bool {
	for _, quote := range []string{"`", `'`, `"`} {
		if strings.Contains(query, quote+target+quote) {
			return true
		}
	}
	return false
}

func targetLanguageQualifier(query string) codeanchor.Lang {
	qualifiers := ExplicitLanguageQualifiers(query)
	if len(qualifiers) == 0 {
		return ""
	}
	return qualifiers[len(qualifiers)-1]
}

// ExplicitLanguageQualifiers returns programming languages that constrain a
// query. A language-named symbol remains a target when it is the only target or
// is explicitly quoted.
func ExplicitLanguageQualifiers(query string) []codeanchor.Lang {
	var qualifiers []codeanchor.Lang
	hasOtherTarget := false
	fields := strings.Fields(query)
	for index, field := range fields {
		token := strings.Trim(field, "`'\"()[]{}<>,:;!?")
		token = strings.TrimSuffix(token, ".")
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		if _, stopword := targetResolutionStopwords[lower]; stopword {
			continue
		}
		if lower == "go" && index+1 < len(fields) && strings.EqualFold(strings.Trim(fields[index+1], "`'\"()[]{}<>,:;!?"), "to") {
			continue
		}
		if lang, ok := targetResolutionLanguageQualifiers[lower]; ok && !explicitlyQuotedTarget(query, token) {
			if lower == "go" && !goTokenIsLanguageQualifier(fields, index, token) {
				hasOtherTarget = true
				continue
			}
			if !slices.Contains(qualifiers, lang) {
				qualifiers = append(qualifiers, lang)
			}
			continue
		}
		hasOtherTarget = true
	}
	if !hasOtherTarget {
		return nil
	}
	return qualifiers
}

func goTokenIsLanguageQualifier(fields []string, index int, token string) bool {
	if index >= 0 && index < len(fields) && strings.HasSuffix(strings.TrimSpace(fields[index]), ".") {
		return false
	}
	neighbor := func(index int) string {
		if index < 0 || index >= len(fields) {
			return ""
		}
		return strings.ToLower(strings.Trim(fields[index], "`'\"()[]{}<>,:;!?"))
	}
	if previous := neighbor(index - 1); previous == "in" || previous == "of" || previous == "using" || previous == "written" {
		return true
	}
	switch neighbor(index + 1) {
	case "code", "implementation", "module", "package", "function", "method", "service", "type", "interface":
		return true
	default:
		if token != "Go" || index+1 >= len(fields) {
			return false
		}
		next := strings.Trim(fields[index+1], "`'\"()[]{}<>,:;!?")
		return codeShapedLanguageContext(next)
	}
}

func codeShapedLanguageContext(value string) bool {
	if strings.ContainsAny(value, ".:_") {
		return true
	}
	for index, r := range []rune(value) {
		if index > 0 && (unicode.IsUpper(r) || unicode.IsDigit(r)) {
			return true
		}
	}
	return false
}

func extractTargetWordTokens(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	if len(fields) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, field := range fields {
		field = strings.Trim(field, "`'\"()[]{}<>,:;!?")
		field = strings.TrimSuffix(field, ".")
		if field == "" {
			continue
		}
		if _, ok := targetResolutionStopwords[field]; ok {
			continue
		}
		if _, ok := targetResolutionQualifiers[field]; ok {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	return out
}

func isTargetSymbolRune(r rune) bool {
	switch {
	case unicode.IsLetter(r), unicode.IsDigit(r):
		return true
	case r == '_', r == '.', r == '/', r == ':', r == '\\':
		return true
	default:
		return false
	}
}

var targetResolutionStopwords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "and": {}, "or": {}, "to": {}, "for": {}, "of": {}, "this": {},
	"that": {}, "these": {}, "those": {}, "find": {}, "show": {}, "list": {}, "locate": {}, "where": {},
	"who": {}, "what": {}, "how": {}, "explain": {}, "definition": {}, "definitions": {}, "usage": {},
	"usages": {}, "callers": {}, "callee": {}, "callees": {}, "calls": {}, "tests": {}, "testing": {},
	"docs": {}, "doc": {}, "overview": {}, "subsystem": {}, "in": {},
}

// targetResolutionCodeCues mark an adjacent token as code even when the token
// itself is spelled like an ordinary word ("the func retry").
var targetResolutionCodeCues = map[string]struct{}{
	"function": {}, "func": {}, "method": {}, "type": {}, "struct": {}, "class": {},
	"interface": {}, "symbol": {}, "def": {}, "fn": {},
}

var targetResolutionQualifiers = map[string]struct{}{
	"python": {}, "go": {}, "golang": {}, "typescript": {}, "javascript": {}, "csharp": {}, "php": {},
}

var targetResolutionLanguageQualifiers = map[string]codeanchor.Lang{
	"python": codeanchor.LangPy, "go": codeanchor.LangGo, "golang": codeanchor.LangGo,
	"typescript": codeanchor.LangTS, "javascript": codeanchor.LangTS,
	"csharp": codeanchor.LangCs, "c#": codeanchor.LangCs, "php": codeanchor.LangPhp,
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func mapHasKey(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}
