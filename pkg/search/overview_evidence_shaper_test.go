package search

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestOverviewEvidenceShaper_PromotesCredibleCodeIntoWindow(t *testing.T) {
	spec := QuerySpec{Intent: IntentOverview, Limits: Limits{Total: 10}}
	results := []RankedResult{
		noteResult("docs/hubs/Search (Hub).md", 0.99),
		noteResult("docs/reference/search.md", 0.94),
		noteResult("docs/specs/product/search.md", 0.93),
		noteResult("README.md", 0.92),
		codeResult("pkg/search/service.go", 0.88, Evidence{Type: "symbol_match", RawScore: 0.86}),
		codeResult("pkg/search/planner/planner.go", 0.82, Evidence{Type: "code_vector_similarity", RawScore: 0.81}),
	}

	shaped, err := (&OverviewEvidenceShaper{Window: 4}).Shape(context.Background(), spec, results)
	require.NoError(t, err)

	require.Equal(t, "docs/hubs/Search (Hub).md", shaped[0].Path)
	require.Equal(t, "pkg/search/service.go", shaped[2].Path)
	require.Equal(t, "pkg/search/planner/planner.go", shaped[3].Path)
}

func TestOverviewEvidenceShaper_DoesNotPromoteWeakPathOnlyCode(t *testing.T) {
	spec := QuerySpec{Intent: IntentOverview, Limits: Limits{Total: 10}}
	results := []RankedResult{
		noteResult("docs/hubs/Search (Hub).md", 0.99),
		noteResult("docs/reference/search.md", 0.94),
		noteResult("docs/specs/product/search.md", 0.93),
		noteResult("README.md", 0.92),
		codeResult("pkg/search/service.go", 0.9, Evidence{
			Type:     "intel_fts_match",
			RawScore: 0.9,
			Details:  map[string]string{"pathOnly": "true"},
		}),
	}

	shaped, err := (&OverviewEvidenceShaper{Window: 4}).Shape(context.Background(), spec, results)
	require.NoError(t, err)

	require.Equal(t, results, shaped)
}

func TestOverviewEvidenceShaper_IgnoresPrecisionIntents(t *testing.T) {
	spec := QuerySpec{Intent: IntentGoToDef, Limits: Limits{Total: 10}}
	results := []RankedResult{
		noteResult("docs/hubs/Search (Hub).md", 0.99),
		noteResult("docs/reference/search.md", 0.94),
		codeResult("pkg/search/service.go", 0.9, Evidence{Type: "symbol_exact", RawScore: 1}),
	}

	shaped, err := (&OverviewEvidenceShaper{Window: 2}).Shape(context.Background(), spec, results)
	require.NoError(t, err)

	require.Equal(t, results, shaped)
}

func noteResult(path string, score float64) RankedResult {
	return RankedResult{
		Candidate: Candidate{
			Handle:   knowledge.NoteHandle(path),
			Owner:    knowledge.NoteHandle(path),
			Type:     "note",
			Path:     path,
			DocClass: InferDocClass(path, "note"),
		},
		FinalScore: score,
	}
}

func codeResult(path string, score float64, evidence ...Evidence) RankedResult {
	return RankedResult{
		Candidate: Candidate{
			Handle:   knowledge.FileHandle(path),
			Owner:    knowledge.FileHandle(path),
			Type:     "code",
			Path:     path,
			Evidence: evidence,
		},
		FinalScore: score,
	}
}
