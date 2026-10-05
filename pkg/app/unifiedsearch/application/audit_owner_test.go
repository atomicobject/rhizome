package application

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestCanonicalSourceResultsGroupsNoteChunksAndRetainsExactEvidence(t *testing.T) {
	h := knowledge.NoteHandle("docs/hubs/Search (Hub).md")
	results := []search.RankedResult{
		{Candidate: search.Candidate{Handle: h, Owner: h, Type: "note", Path: "docs/hubs/Search (Hub).md", Evidence: []search.Evidence{{Type: "note_title_exact", RawScore: 1}}}, FinalScore: 1},
		{Candidate: search.Candidate{Handle: knowledge.Handle{Kind: "note_chunk", ID: "chunk"}, Owner: h, Type: "note", Path: "docs/hubs/Search (Hub).md", Evidence: []search.Evidence{{Type: "intel_doc_match", RawScore: .8}}}, FinalScore: .8},
	}
	got := CanonicalizeSources(results)
	require.Len(t, got, 1)
	require.True(t, hasEvidence(got[0].Evidence, "note_title_exact"))
	require.True(t, hasEvidence(got[0].Evidence, "intel_doc_match"))
}

func hasEvidence(evidence []search.Evidence, typ string) bool {
	for _, item := range evidence {
		if item.Type == typ {
			return true
		}
	}
	return false
}

func TestAssessSourcesSeparatesPrecisionPrimaryFromSupporting(t *testing.T) {
	results := []search.RankedResult{
		{Candidate: search.Candidate{Handle: knowledge.AnchorHandle("definition"), Evidence: []search.Evidence{{Type: "definition_anchor", RawScore: 1}}}},
		{Candidate: search.Candidate{Handle: knowledge.NoteHandle("context.md"), Evidence: []search.Evidence{{Type: "note_vector_similarity", RawScore: 0.9}}}},
	}
	assessed := AssessQuery("", search.IntentGoToDef, results)
	require.Equal(t, EvidencePrimary, assessed[0].Eligibility)
	require.Equal(t, "definition", assessed[0].Relationship)
	require.Equal(t, RelevanceStrong, assessed[0].Relevance)
	require.Equal(t, EvidenceSupporting, assessed[1].Eligibility)
	require.Equal(t, RelevanceWeak, assessed[1].Relevance)
}

func TestResolveExactNavigationTargetPreservesAmbiguity(t *testing.T) {
	sources := []SourceAssessment{
		{Result: search.RankedResult{Candidate: search.Candidate{Path: "docs/spec.md", Type: "note", Evidence: []search.Evidence{{Type: "note_title_exact"}}}}},
		{Result: search.RankedResult{Candidate: search.Candidate{Path: "docs/effort.md", Type: "note", Evidence: []search.Evidence{{Type: "note_title_exact"}}}}},
	}
	target := ResolveNavigationTarget(TargetResolution{}, sources)
	require.Equal(t, search.TargetStatusAmbiguous, target.Status)
	require.Len(t, target.Candidates, 2)
}

func TestAvailabilityFromLanesDistinguishesPartialAndUnavailable(t *testing.T) {
	require.Equal(t, AvailabilityComplete, AvailabilityFromLanes([]search.LaneStatus{{Status: search.LaneStateRan}}))
	require.Equal(t, AvailabilityPartial, AvailabilityFromLanes([]search.LaneStatus{{Status: search.LaneStateRan}, {Status: search.LaneStateTimedOut}}))
	require.Equal(t, AvailabilityUnavailable, AvailabilityFromLanes([]search.LaneStatus{{Status: search.LaneStateTimedOut}}))
}

func TestBuildAssessedAnswerRequiresResolvedPrecisionTargetForHighConfidence(t *testing.T) {
	sources := []SourceAssessment{{
		Result:      search.RankedResult{Candidate: search.Candidate{Path: "pkg/search/service.go", Type: "code", Evidence: []search.Evidence{{Type: "definition_anchor", RawScore: 1}}}},
		Eligibility: EvidencePrimary, Relationship: "definition", Relevance: RelevanceStrong, SupportedRoles: []string{"implementation"},
	}}
	unresolved := BuildAssessedAnswer(search.IntentGoToDef, "definition for Search", TargetResolution{}, nil, AvailabilityComplete, sources)
	require.Equal(t, "low", unresolved.Confidence.Level)

	statusOnly := BuildAssessedAnswer(search.IntentGoToDef, "definition for Search", TargetResolution{Status: search.TargetStatusInferredSymbol, Confidence: 0.93}, nil, AvailabilityComplete, sources)
	require.Equal(t, "low", statusOnly.Confidence.Level)

	resolved := BuildAssessedAnswer(search.IntentGoToDef, "definition for Search", TargetResolution{Status: search.TargetStatusInferredSymbol, Confidence: 0.93, Selected: &search.TargetCandidate{FQN: "pkg.Search", Path: "pkg/search/service.go"}}, nil, AvailabilityComplete, sources)
	require.Equal(t, "high", resolved.Confidence.Level)
}
