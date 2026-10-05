package search

// HasPrimaryEvidence reports whether a candidate carries the relationship proof
// requested by a precision intent. Broad topical evidence remains supporting.
func HasPrimaryEvidence(intent Intent, evidence []Evidence) bool {
	if !IsPrecisionIntent(intent) {
		return false
	}
	for _, item := range evidence {
		switch intent {
		case IntentGoToDef:
			if item.Type == "definition_anchor" || item.Type == "symbol_exact" {
				return true
			}
		case IntentTestsForCode:
			if item.Type == "tests_path" || item.Type == "tests_package" {
				return true
			}
		case IntentCallers, IntentCallees:
			if item.Type == "call_edge" {
				return true
			}
		case IntentFindUsages, IntentRefactorImpact:
			if item.Type == "code_ref" || item.Type == "call_edge" {
				return true
			}
		case IntentImplementers, IntentOverrides, IntentImports:
			if item.Type == "code_ref" || item.Type == "anchor_graph_edge" || (intent == IntentImplementers && item.Type == "implements_edge") {
				return true
			}
		}
	}
	return false
}
