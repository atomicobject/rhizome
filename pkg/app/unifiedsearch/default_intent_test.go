package unifiedsearch

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestInferDefaultIntent(t *testing.T) {
	t.Run("seed only routes to related_to_seed", func(t *testing.T) {
		decision := InferDefaultIntent("", []string{"pkg/search/service.go"})
		require.Equal(t, search.IntentRelatedToSeed, decision.Intent)
		require.Equal(t, "heuristic", decision.Source)
	})

	t.Run("overview phrasing routes to overview", func(t *testing.T) {
		decision := InferDefaultIntent("how does indexing work", nil)
		require.Equal(t, search.IntentOverview, decision.Intent)
		require.Equal(t, "inferred", decision.Source)
		require.Greater(t, decision.Score, 0.0)
	})

	t.Run("docs for a code path routes to docs_for_code", func(t *testing.T) {
		decision := InferDefaultIntent("docs for pkg/search/service.go", nil)
		require.Equal(t, search.IntentDocsForCode, decision.Intent)
		require.Equal(t, "inferred", decision.Source)
	})

	t.Run("no signal falls back to search", func(t *testing.T) {
		decision := InferDefaultIntent("lane status", nil)
		require.Equal(t, search.IntentSearch, decision.Intent)
		require.Equal(t, "default", decision.Source)
	})
}

func TestExactCodeToolWarnings(t *testing.T) {
	warnings := ExactCodeToolWarnings("callers of Foo")
	require.Len(t, warnings, 1)
	require.Equal(t, "exact_code_tool_suggested", warnings[0].Code)
	require.Empty(t, ExactCodeToolWarnings("how does indexing work"))
}
