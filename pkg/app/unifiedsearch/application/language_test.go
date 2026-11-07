package application

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestAnswerLanguageEligibilityAcrossSupportedPaths(t *testing.T) {
	tests := []struct {
		path, query string
		known       bool
	}{
		{"src/task.PY", "Python task handler", true},
		{"  src/task.GO \t", "Go code task handler", true},
		{"src/task.CS", "C# task handler", true},
		{"src/task.PHP", "PHP task handler", true},
		{"src/task.ts", "TypeScript task handler", true},
		{"src/task.tsx", "TypeScript task handler", true},
		{"src/task.mts", "TypeScript task handler", true},
		{"src/task.cts", "TypeScript task handler", true},
		{"src/task.js", "JavaScript task handler", true},
		{"src/task.jsx", "JavaScript task handler", true},
		{"src/task.mjs", "JavaScript task handler", true},
		{"src/task.CJS", "JavaScript task handler", true},
		{"src/task.rb", "Python task handler", false},
		{"src/Taskfile", "Python task handler", false},
	}
	result := func(path string, score float64) search.RankedResult {
		return search.RankedResult{Candidate: search.Candidate{
			SourceRef: search.SourceRef{Path: path}, Handle: knowledge.FileHandle(path), Type: "code", Evidence: strongQuerySpecificityEvidence(),
		}, FinalScore: score}
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			candidate := result(tt.path, 1)
			matching := AssessQuery(tt.query, search.IntentSearch, []search.RankedResult{candidate})
			require.Equal(t, RelevanceStrong, matching[0].Relevance)
			packet := BuildAssessedAnswerForFacets(search.IntentSearch, tt.query, nil, TargetResolution{}, nil, AvailabilityComplete, matching)
			require.Len(t, packet.MustRead, 1)
			require.Equal(t, tt.path, packet.MustRead[0].Path)

			query, control := "C# task handler", "reference/task.cs"
			if tt.query == query {
				query, control = "Python task handler", "reference/task.py"
			}
			assessed := AssessQuery(query, search.IntentSearch, []search.RankedResult{candidate, result(control, .9)})
			require.Equal(t, RelevanceStrong, assessed[1].Relevance)
			packet = BuildAssessedAnswerForFacets(search.IntentSearch, query, nil, TargetResolution{}, nil, AvailabilityComplete, assessed)
			if tt.known {
				require.Equal(t, RelevanceUseful, assessed[0].Relevance)
				require.Len(t, packet.MustRead, 1)
				require.Equal(t, control, packet.MustRead[0].Path)
			} else {
				require.Equal(t, RelevanceStrong, assessed[0].Relevance, "unknown paths retain their existing eligibility")
				require.Len(t, packet.MustRead, 1)
				require.Equal(t, tt.path, packet.MustRead[0].Path)
			}
		})
	}
}
