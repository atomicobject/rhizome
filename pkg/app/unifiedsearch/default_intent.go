package unifiedsearch

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/search"
)

// IntentDecision is the default-intent routing decision applied when a caller
// omits the search mode. Source is one of explicit, default, inferred, heuristic.
type IntentDecision struct {
	Intent   search.Intent
	Source   string
	Score    float64
	Warnings []search.Warning
}

// InferDefaultIntent picks the default intent for a query plus its raw seed
// tokens. Explicit caller intent always wins; this only runs when none was given.
func InferDefaultIntent(query string, seedTokens []string) IntentDecision {
	query = strings.TrimSpace(query)
	decision := IntentDecision{Intent: search.IntentSearch, Source: "default"}
	seeded := len(seedTokens) > 0
	if query == "" {
		if seeded {
			return IntentDecision{Intent: search.IntentRelatedToSeed, Source: "heuristic", Score: 0.8}
		}
		return decision
	}

	codeSeed := anyPath(seedTokens, isCodePath)
	docSeed := anyPath(seedTokens, isMarkdownDocQueryCompatibilityPath)
	codePathQuery := containsCodePath(query)
	docPathQuery := containsDocPath(query)

	switch {
	case wantsDocsForCode(query) && (codeSeed || codePathQuery || looksSymbolSpecific(query)):
		decision = IntentDecision{Intent: search.IntentDocsForCode, Source: "inferred", Score: 0.72}
	case wantsCodeForDocs(query) && (docSeed || docPathQuery):
		decision = IntentDecision{Intent: search.IntentCodeForDocs, Source: "inferred", Score: 0.72}
	case wantsOverview(query) && (seeded || containsPathLikeQuery(query)):
		decision = IntentDecision{Intent: search.IntentSubsystemOverview, Source: "inferred", Score: 0.74}
	case wantsOverview(query):
		decision = IntentDecision{Intent: search.IntentOverview, Source: "inferred", Score: 0.7}
	}
	if !search.IsPrecisionIntent(decision.Intent) {
		decision.Warnings = append(decision.Warnings, ExactCodeToolWarnings(query)...)
	}
	return decision
}

func anyPath(paths []string, match func(string) bool) bool {
	for _, path := range paths {
		if match(path) {
			return true
		}
	}
	return false
}

func wantsDocsForCode(query string) bool {
	q := normalizedModeQuery(query)
	return strings.Contains(q, "docs for ") ||
		strings.Contains(q, "documentation for ") ||
		strings.Contains(q, "rationale for ") ||
		strings.Contains(q, "design notes for ") ||
		strings.Contains(q, "explain docs for ")
}

func wantsCodeForDocs(query string) bool {
	q := normalizedModeQuery(query)
	return strings.Contains(q, "code for ") ||
		strings.Contains(q, "implementation for ") ||
		strings.Contains(q, "implemented by ") ||
		strings.Contains(q, "where is ") && strings.Contains(q, "implemented")
}

func wantsOverview(query string) bool {
	q := normalizedModeQuery(query)
	phrases := []string{
		"overview",
		"architecture",
		"how does",
		"how do",
		"how is",
		"walk me through",
		"onboard",
		"onboarding",
		"key modules",
		"entry points",
		"main pieces",
		"concept map",
	}
	for _, phrase := range phrases {
		if strings.Contains(q, phrase) {
			return true
		}
	}
	return false
}

// ExactCodeToolWarnings warns when a query looks like exact code navigation
// that a precision code tool answers better than semantic orientation.
func ExactCodeToolWarnings(query string) []search.Warning {
	q := normalizedModeQuery(query)
	exactPhrase := containsAny(q,
		"caller",
		"callee",
		"call graph",
		"go to def",
		"usage of",
		"usages of",
		"references to",
		"tests for",
	)
	symbolDefinition := looksSymbolSpecific(query) && containsAny(q, "definition", "reference", "usage", "test")
	if !(exactPhrase || symbolDefinition) {
		return nil
	}
	return []search.Warning{{
		Code:    "exact_code_tool_suggested",
		Kind:    "tool_routing",
		Source:  "semantic_query",
		Message: "This looks like exact code navigation; prefer code_symbol, code_references, or code_symbol_context when you need proof rather than orientation.",
	}}
}

func containsPathLikeQuery(query string) bool {
	for _, field := range strings.Fields(query) {
		field = strings.Trim(field, "`'\"()[]{}<>,:;!?")
		if strings.Contains(field, "/") || isCodePath(field) || isMarkdownDocQueryCompatibilityPath(field) {
			return true
		}
	}
	return false
}

func containsCodePath(query string) bool {
	for _, field := range strings.Fields(query) {
		if isCodePath(strings.Trim(field, "`'\"()[]{}<>,:;!?")) {
			return true
		}
	}
	return false
}

func containsDocPath(query string) bool {
	for _, field := range strings.Fields(query) {
		if isMarkdownDocQueryCompatibilityPath(strings.Trim(field, "`'\"()[]{}<>,:;!?")) {
			return true
		}
	}
	return false
}

func isCodePath(path string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(path)))
	if codefile.IsTypeScriptJavaScriptExtension(ext) {
		return true
	}
	switch ext {
	case ".go", ".py", ".cs", ".java", ".rb", ".rs", ".swift", ".kt":
		return true
	default:
		return false
	}
}

// isMarkdownDocQueryCompatibilityPath is a query-routing hint for legacy
// Markdown documentation. It never establishes note ownership.
func isMarkdownDocQueryCompatibilityPath(path string) bool {
	return strings.EqualFold(filepath.Ext(strings.TrimSpace(path)), ".md")
}

func looksSymbolSpecific(query string) bool {
	for _, field := range strings.Fields(query) {
		field = strings.Trim(field, "`'\"()[]{}<>,:;!?")
		if field == "" || strings.Contains(field, "/") {
			continue
		}
		if strings.Count(field, ".") >= 1 && hasUpperOrParen(field) {
			return true
		}
	}
	return false
}

func hasUpperOrParen(s string) bool {
	if strings.ContainsAny(s, "()") {
		return true
	}
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func normalizedModeQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

func containsAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
