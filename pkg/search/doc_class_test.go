package search

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInferDocClass_RequiresTypedNoteOwnership(t *testing.T) {
	require.Equal(t, DocClassNone, InferDocClass("docs/hubs/Decision.mD", "code"))
	require.Equal(t, DocClassHub, InferDocClass("docs/hubs/Decision.mD", "note"))
}

func TestInferDocClass_SeparatesSpecEffortAndAnalysisFromReference(t *testing.T) {
	require.Equal(t, DocClassSpec, InferDocClass("docs/specs/product/search.md", "note"))
	require.Equal(t, DocClassEffort, InferDocClass("docs/efforts/2026-09-12-search.md", "note"))
	require.Equal(t, DocClassAnalysis, InferDocClass("docs/reference/analysis/Search gaps.md", "note"))
	require.Equal(t, DocClassReference, InferDocClass("docs/reference/subsystems/search.md", "note"))
}
