package intent

import (
	"strings"

	"github.com/atomicobject/rhizome/pkg/search"
)

// KeywordMatch returns true when the query contains intent-specific keyword hints.
// Used as a lightweight guard before accepting embedding-based intent detection.
func KeywordMatch(intent search.Intent, query string) bool {
	switch intent {
	case search.IntentSearch,
		search.IntentDocsForCode,
		search.IntentRelatedToSeed,
		search.IntentOverview,
		search.IntentCodeForDocs:
		return true
	}
	q := strings.ToLower(query)
	switch intent {
	case search.IntentConsolidation:
		return containsAny(q, "consolidate", "merge", "dedupe", "deduplicate")
	case search.IntentGoToDef:
		return containsAny(q, "definition", "define", "defined", "go to", "where is")
	case search.IntentFindUsages, search.IntentCallers, search.IntentCallees:
		return containsAny(q, "usage", "usages", "call", "callers", "callees", "references", "refers")
	case search.IntentExplainSymbol:
		return containsAny(q, "explain", "what does", "what is", "how does")
	case search.IntentTestsForCode:
		return containsAny(q, "test", "tests", "spec", "specs")
	case search.IntentRefactorImpact:
		return containsAny(q, "refactor", "impact", "rename", "change")
	case search.IntentImplementers:
		return containsAny(q, "implement", "implements", "implementers")
	case search.IntentOverrides:
		return containsAny(q, "override", "overrides")
	case search.IntentImports:
		return containsAny(q, "import", "imports")
	case search.IntentDataFlow:
		return containsAny(q, "data flow", "flow", "trace")
	case search.IntentSecurityAudit:
		return containsAny(q, "security", "audit", "vulnerability")
	default:
		return false
	}
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(s, n) {
			return true
		}
	}
	return false
}
