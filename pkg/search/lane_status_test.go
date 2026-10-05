package search

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestBuildLaneStatusesKeepsPartialTimeoutVisible(t *testing.T) {
	statuses := BuildLaneStatuses(
		[]Retriever{stubRetriever{name: "vector"}},
		[]laneRun{{name: "vector", status: LaneStateTimedOut, reason: context.DeadlineExceeded.Error(), resultCount: 0}},
		[]RankedResult{{
			Candidate: Candidate{
				Handle: knowledge.CodeChunkHandle("a1", "symbol", 0),
				Type:   "code",
				Evidence: []Evidence{{
					Type:     "code_vector_similarity",
					RawScore: 0.8,
				}},
			},
			FinalScore: 0.8,
		}},
	)

	status := statusForLane(statuses, "code_vector")
	require.Equal(t, LaneStateTimedOut, status.Status)
	require.Equal(t, 1, status.ResultCount)
}

func TestBuildLaneStatusesIncludesRationaleFTS(t *testing.T) {
	statuses := BuildLaneStatuses(
		[]Retriever{stubRetriever{name: "rationale_fts"}},
		[]laneRun{{name: "rationale_fts", status: LaneStateRan, resultCount: 1}},
		[]RankedResult{{
			Candidate: Candidate{
				Handle: knowledge.AnchorHandle("a1"),
				Owner:  knowledge.AnchorHandle("a1"),
				Type:   "anchor",
				Evidence: []Evidence{{
					Type:     "rationale_fts_match",
					RawScore: 0.7,
				}},
			},
			FinalScore: 0.7,
		}},
	)

	status := statusForLane(statuses, "rationale_fts")
	require.Equal(t, LaneStateRan, status.Status)
	require.Equal(t, []string{"rationale_fts"}, status.Retrievers)
	require.Equal(t, 1, status.ResultCount)
}

func TestLanesForEvidenceMapsBothIntelLexicalEvidenceTypes(t *testing.T) {
	// The intel_lexical retriever emits intel_doc_match for documentation rows
	// and intel_fts_match for code rows; both belong to the lane it ran.
	require.Equal(t, []string{"intel_fts"}, LanesForEvidence("intel_fts_match"))
	require.Equal(t, []string{"intel_fts"}, LanesForEvidence("intel_doc_match"))
}
