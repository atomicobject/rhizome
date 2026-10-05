package search

import "strings"

// Warning captures intent-specific fallback diagnostics.
type Warning struct {
	Code    string `json:"code"`
	Kind    string `json:"kind,omitempty"`
	Source  string `json:"source,omitempty"`
	Message string `json:"message"`
}

// DetectWarnings returns intent-specific warnings based on evidence observed in results.
func DetectWarnings(spec QuerySpec, results []RankedResult) []Warning {
	var warnings []Warning
	switch spec.Intent {
	case IntentGoToDef:
		if !hasEvidence(results, "definition_anchor") {
			warnings = append(warnings, Warning{
				Code:    "missing_definition",
				Message: "No definition anchors found; falling back to lexical/refs.",
			})
		}
	case IntentFindUsages, IntentCallers, IntentCallees:
		if !hasEvidence(results, "call_edge") {
			warnings = append(warnings, Warning{
				Code:    "missing_call_edges",
				Message: "No call edges found; falling back to lexical/refs.",
			})
		}
	case IntentTestsForCode:
		if !hasEvidence(results, "tests_path", "tests_package") {
			warnings = append(warnings, Warning{
				Code:    "missing_tests",
				Message: "No test files detected; falling back to lexical/refs.",
			})
		}
	case IntentSubsystemOverview:
		if shouldWarnSubsystemOverviewFallback(spec, results) {
			warnings = append(warnings, Warning{
				Code:    "subsystem_overview_fallback",
				Message: "Explicit subsystem seeds did not localize strongly; falling back toward broader repo-level results.",
			})
		}
	}
	return warnings
}

func shouldWarnSubsystemOverviewFallback(spec QuerySpec, results []RankedResult) bool {
	if spec.Intent != IntentSubsystemOverview || !spec.HasExplicitSeeds || len(spec.ExplicitSeedPaths) == 0 || len(results) == 0 {
		return false
	}
	window := results[:min(len(results), 8)]
	localDocs := 0
	localCode := 0
	genericDocs := 0
	anyCode := false
	for _, res := range results {
		if res.Type == "code" && !IsTestPath(res.Path) {
			anyCode = true
			break
		}
	}
	for _, res := range window {
		if res.DocClass == DocClassRepoGlobal || res.DocClass == DocClassGenerated {
			genericDocs++
		}
		if SeedPathProximity(res.Path, spec.ExplicitSeedPaths) <= 0 {
			continue
		}
		if isDocCandidate(res.Path, res.Type) {
			localDocs++
		}
		if res.Type == "code" && !IsTestPath(res.Path) {
			localCode++
		}
	}
	if localDocs+localCode == 0 {
		return true
	}
	if anyCode && localCode == 0 {
		return true
	}
	return genericDocs >= 3 && localDocs == 0 && localCode == 0
}

func hasEvidence(results []RankedResult, types ...string) bool {
	if len(results) == 0 || len(types) == 0 {
		return false
	}
	want := make(map[string]struct{}, len(types))
	for _, t := range types {
		if t = strings.TrimSpace(strings.ToLower(t)); t != "" {
			want[t] = struct{}{}
		}
	}
	if len(want) == 0 {
		return false
	}
	for _, res := range results {
		for _, ev := range res.Evidence {
			if _, ok := want[strings.ToLower(ev.Type)]; ok {
				return true
			}
		}
	}
	return false
}
