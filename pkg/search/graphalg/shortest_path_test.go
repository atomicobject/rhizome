package graphalg

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpers to build test edges
func edge(src, dst, kind, confidence string, score float64) GraphDocEdgeMinimal {
	return GraphDocEdgeMinimal{
		SrcPath:         src,
		DstPath:         dst,
		Kind:            kind,
		Confidence:      confidence,
		ConfidenceScore: score,
	}
}

func TestShortestPath_DirectNeighbor(t *testing.T) {
	edges := []GraphDocEdgeMinimal{
		edge("a.md", "b.md", "wikilink", "extracted", 1.0),
	}
	result, err := ShortestPath(edges, "a.md", "b.md", 8)
	require.NoError(t, err)
	assert.Equal(t, "a.md", result.From)
	assert.Equal(t, "b.md", result.To)
	assert.Equal(t, 1, result.Hops)
	require.Len(t, result.Path, 1)
	assert.Equal(t, "a.md", result.Path[0].FromPath)
	assert.Equal(t, "b.md", result.Path[0].ToPath)
}

func TestShortestPath_PrefersHigherConfidenceRoute(t *testing.T) {
	edges := []GraphDocEdgeMinimal{
		edge("a.md", "b.md", "wikilink", "ambiguous", 0.2),
		edge("b.md", "d.md", "wikilink", "ambiguous", 0.2),
		edge("a.md", "c.md", "wikilink", "extracted", 1.0),
		edge("c.md", "d.md", "wikilink", "extracted", 1.0),
	}

	result, err := ShortestPath(edges, "a.md", "d.md", 4)
	require.NoError(t, err)
	require.Len(t, result.Path, 2)
	assert.Equal(t, "a.md", result.Path[0].FromPath)
	assert.Equal(t, "c.md", result.Path[0].ToPath)
	assert.Equal(t, "c.md", result.Path[1].FromPath)
	assert.Equal(t, "d.md", result.Path[1].ToPath)
}

func TestShortestPath_MultiHop(t *testing.T) {
	// A→B→C
	edges := []GraphDocEdgeMinimal{
		edge("a.md", "b.md", "wikilink", "extracted", 1.0),
		edge("b.md", "c.md", "wikilink", "extracted", 1.0),
	}
	result, err := ShortestPath(edges, "a.md", "c.md", 8)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Hops)
	require.Len(t, result.Path, 2)
	assert.Equal(t, "a.md", result.Path[0].FromPath)
	assert.Equal(t, "b.md", result.Path[0].ToPath)
	assert.Equal(t, "b.md", result.Path[1].FromPath)
	assert.Equal(t, "c.md", result.Path[1].ToPath)
}

func TestShortestPath_NoPath(t *testing.T) {
	// Two disconnected components: {a,b} and {c,d}
	edges := []GraphDocEdgeMinimal{
		edge("a.md", "b.md", "wikilink", "extracted", 1.0),
		edge("c.md", "d.md", "wikilink", "extracted", 1.0),
	}
	_, err := ShortestPath(edges, "a.md", "c.md", 8)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no path found")
}

func TestShortestPath_MaxHopsExceeded(t *testing.T) {
	// A→B→C→D, path exists (3 hops) but maxHops=2
	edges := []GraphDocEdgeMinimal{
		edge("a.md", "b.md", "wikilink", "extracted", 1.0),
		edge("b.md", "c.md", "wikilink", "extracted", 1.0),
		edge("c.md", "d.md", "wikilink", "extracted", 1.0),
	}
	_, err := ShortestPath(edges, "a.md", "d.md", 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no path found within 2 hops")
}

func TestShortestPath_Bidirectional(t *testing.T) {
	// Edge goes A→B in the edge list, but BFS is undirected.
	edges := []GraphDocEdgeMinimal{
		edge("a.md", "b.md", "wikilink", "extracted", 1.0),
		edge("b.md", "c.md", "wikilink", "extracted", 1.0),
	}
	// Forward A→C
	r1, err := ShortestPath(edges, "a.md", "c.md", 8)
	require.NoError(t, err)

	// Reverse C→A should find same length
	r2, err := ShortestPath(edges, "c.md", "a.md", 8)
	require.NoError(t, err)

	assert.Equal(t, r1.Hops, r2.Hops)
}

func TestShortestPath_EdgeMetadata(t *testing.T) {
	edges := []GraphDocEdgeMinimal{
		edge("a.go", "b.go", "calls", "inferred", 0.7),
	}
	result, err := ShortestPath(edges, "a.go", "b.go", 8)
	require.NoError(t, err)
	require.Len(t, result.Path, 1)
	hop := result.Path[0]
	assert.Equal(t, "calls", hop.EdgeKind)
	assert.Equal(t, "inferred", hop.Confidence)
	assert.InDelta(t, 0.7, hop.ConfidenceScore, 0.001)
}

func TestShortestPath_EmptyGraph(t *testing.T) {
	_, err := ShortestPath(nil, "a.md", "b.md", 8)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestShortestPath_NegativeMaxHops(t *testing.T) {
	edges := []GraphDocEdgeMinimal{
		edge("a.md", "b.md", "wikilink", "extracted", 1.0),
	}
	_, err := ShortestPath(edges, "a.md", "b.md", -2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maxHops must be >= 0")
}

// --- ResolvePathFuzzy tests ---

func TestResolvePathFuzzy_ExactMatch(t *testing.T) {
	paths := []string{"pkg/cache/service.go", "pkg/auth/service.go", "docs/design.md"}
	got, err := ResolvePathFuzzy(paths, "pkg/cache/service.go")
	require.NoError(t, err)
	assert.Equal(t, "pkg/cache/service.go", got)
}

func TestResolvePathFuzzy_ExactMatchCaseInsensitive(t *testing.T) {
	paths := []string{"Docs/Design.md", "pkg/cache/service.go"}
	got, err := ResolvePathFuzzy(paths, "docs/design.md")
	require.NoError(t, err)
	assert.Equal(t, "Docs/Design.md", got)
}

func TestResolvePathFuzzy_BasenameMatch(t *testing.T) {
	paths := []string{"pkg/cache/service.go", "pkg/auth/handler.go"}
	// "service" matches "service.go" basename without extension
	got, err := ResolvePathFuzzy(paths, "service")
	require.NoError(t, err)
	assert.Equal(t, "pkg/cache/service.go", got)
}

func TestResolvePathFuzzy_BasenameAmbiguous(t *testing.T) {
	paths := []string{"pkg/cache/service.go", "pkg/auth/service.go"}
	_, err := ResolvePathFuzzy(paths, "service")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
	assert.Contains(t, err.Error(), "pkg/cache/service.go")
	assert.Contains(t, err.Error(), "pkg/auth/service.go")
}

func TestResolvePathFuzzy_SubstringMatch(t *testing.T) {
	paths := []string{
		"pkg/cache/service.go",
		"pkg/auth/handler.go",
		"docs/cache-design.md",
	}
	// "cache service" scores highest for "pkg/cache/service.go" (2 terms match)
	got, err := ResolvePathFuzzy(paths, "cache service")
	require.NoError(t, err)
	assert.Equal(t, "pkg/cache/service.go", got)
}

func TestResolvePathFuzzy_Ambiguous(t *testing.T) {
	// Both paths contain "service" equally
	paths := []string{"pkg/cache/service.go", "pkg/auth/service.go"}
	_, err := ResolvePathFuzzy(paths, "service.go")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "ambiguous"), "expected ambiguous error, got: %s", err.Error())
}

func TestResolvePathFuzzy_NoMatch(t *testing.T) {
	paths := []string{"pkg/cache/service.go", "docs/design.md"}
	_, err := ResolvePathFuzzy(paths, "zzznotfound")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no node matching")
}
