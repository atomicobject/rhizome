package search

import "testing"

func TestDetectWarnings_MissingEvidence(t *testing.T) {
	results := []RankedResult{
		{Candidate: Candidate{Evidence: []Evidence{{Type: "intel_fts_match", RawScore: 0.8}}}},
	}

	warns := DetectWarnings(QuerySpec{Intent: IntentGoToDef}, results)
	if len(warns) == 0 || warns[0].Code != "missing_definition" {
		t.Fatalf("expected missing_definition warning, got %#v", warns)
	}

	warns = DetectWarnings(QuerySpec{Intent: IntentFindUsages}, results)
	if len(warns) == 0 || warns[0].Code != "missing_call_edges" {
		t.Fatalf("expected missing_call_edges warning, got %#v", warns)
	}

	warns = DetectWarnings(QuerySpec{Intent: IntentTestsForCode}, results)
	if len(warns) == 0 || warns[0].Code != "missing_tests" {
		t.Fatalf("expected missing_tests warning, got %#v", warns)
	}
}

func TestDetectWarnings_NoWarningWhenEvidencePresent(t *testing.T) {
	results := []RankedResult{
		{Candidate: Candidate{Evidence: []Evidence{{Type: "definition_anchor", RawScore: 1.0}}}},
	}
	if warns := DetectWarnings(QuerySpec{Intent: IntentGoToDef}, results); len(warns) != 0 {
		t.Fatalf("expected no warnings, got %#v", warns)
	}
}
