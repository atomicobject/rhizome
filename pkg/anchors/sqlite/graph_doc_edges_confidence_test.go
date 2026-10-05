package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGraphDocEdgesConfidence_RoundTrip verifies that edges written with all three confidence
// tiers are stored and returned with the correct fields.
func TestGraphDocEdgesConfidence_RoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	edges := []GraphDocEdge{
		{
			SrcPath:         "notes/a.md",
			DstPath:         "notes/b.md",
			Kind:            "wikilink",
			Confidence:      EdgeConfidenceExtracted,
			ConfidenceScore: 1.0,
			SourceLocation:  "L5",
		},
		{
			SrcPath:         "notes/a.md",
			DstPath:         "notes/c.md",
			Kind:            "wikilink",
			Confidence:      EdgeConfidenceInferred,
			ConfidenceScore: 0.7,
			SourceLocation:  "L12",
		},
		{
			SrcPath:         "notes/a.md",
			DstPath:         "notes/d.md",
			Kind:            "wikilink",
			Confidence:      EdgeConfidenceAmbiguous,
			ConfidenceScore: 0.2,
			SourceLocation:  "",
		},
	}

	require.NoError(t, store.ReplaceGraphDocEdgesWithConfidence(ctx, "notes/a.md", "wikilink", edges))

	got, err := store.GraphDocEdgesWithConfidenceForPaths(ctx, []string{"notes/a.md"})
	require.NoError(t, err)
	require.Len(t, got, 3)

	// Build a map for order-independent lookup.
	byDst := make(map[string]GraphDocEdge, len(got))
	for _, e := range got {
		byDst[e.DstPath] = e
	}

	e1 := byDst["notes/b.md"]
	require.Equal(t, "notes/a.md", e1.SrcPath)
	require.Equal(t, "wikilink", e1.Kind)
	require.Equal(t, EdgeConfidenceExtracted, e1.Confidence)
	require.InDelta(t, 1.0, e1.ConfidenceScore, 0.0001)
	require.Equal(t, "L5", e1.SourceLocation)

	e2 := byDst["notes/c.md"]
	require.Equal(t, EdgeConfidenceInferred, e2.Confidence)
	require.InDelta(t, 0.7, e2.ConfidenceScore, 0.0001)
	require.Equal(t, "L12", e2.SourceLocation)

	e3 := byDst["notes/d.md"]
	require.Equal(t, EdgeConfidenceAmbiguous, e3.Confidence)
	require.InDelta(t, 0.2, e3.ConfidenceScore, 0.0001)
	require.Equal(t, "", e3.SourceLocation)
}

// TestGraphDocEdgesConfidence_DefaultValuesByKind verifies that the legacy
// ReplaceGraphDocEdgesForPath path assigns kind-specific confidence defaults.
func TestGraphDocEdgesConfidence_DefaultValuesByKind(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	cases := []struct {
		kind  string
		dst   string
		score float64
	}{
		{kind: GraphDocEdgeKindWikilink, dst: "notes/y.md", score: 1.0},
		{kind: GraphDocEdgeKindMarkdownLink, dst: "notes/z.md", score: 0.9},
		{kind: NoteLinkKind("wikilink", "alias"), dst: "notes/w.md", score: 1.0},
		{kind: NoteLinkKind("mdlink", "heading"), dst: "notes/v.md", score: 0.9},
	}
	for _, tc := range cases {
		require.NoError(t, store.ReplaceGraphDocEdgesForPath(ctx, "notes/x.md", tc.kind, []string{tc.dst}))
	}

	got, err := store.AllGraphDocEdgesWithConfidence(ctx)
	require.NoError(t, err)
	require.Len(t, got, len(cases))

	byKind := make(map[string]GraphDocEdge, len(got))
	for _, e := range got {
		byKind[e.Kind] = e
	}
	for _, tc := range cases {
		e := byKind[tc.kind]
		require.Equal(t, EdgeConfidenceExtracted, e.Confidence)
		require.InDelta(t, tc.score, e.ConfidenceScore, 0.0001)
		require.Equal(t, "", e.SourceLocation)
	}
}

func TestGraphDocEdgesConfidence_Replace(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Write initial set.
	initial := []GraphDocEdge{
		{SrcPath: "a.md", DstPath: "b.md", Kind: "wikilink", Confidence: EdgeConfidenceExtracted, ConfidenceScore: 1.0},
		{SrcPath: "a.md", DstPath: "c.md", Kind: "wikilink", Confidence: EdgeConfidenceInferred, ConfidenceScore: 0.7},
	}
	require.NoError(t, store.ReplaceGraphDocEdgesWithConfidence(ctx, "a.md", "wikilink", initial))

	// Replace with a new set.
	updated := []GraphDocEdge{
		{SrcPath: "a.md", DstPath: "d.md", Kind: "wikilink", Confidence: EdgeConfidenceAmbiguous, ConfidenceScore: 0.2},
	}
	require.NoError(t, store.ReplaceGraphDocEdgesWithConfidence(ctx, "a.md", "wikilink", updated))

	got, err := store.GraphDocEdgesWithConfidenceForPaths(ctx, []string{"a.md"})
	require.NoError(t, err)
	require.Len(t, got, 1, "old edges should be replaced")
	require.Equal(t, "d.md", got[0].DstPath)
	require.Equal(t, EdgeConfidenceAmbiguous, got[0].Confidence)
}

// TestGraphDocEdgesConfidence_Normalize verifies that invalid confidence tiers and scores
// are normalized before persistence.
func TestGraphDocEdgesConfidence_Normalize(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	edges := []GraphDocEdge{
		{
			SrcPath:         "a.md",
			DstPath:         "b.md",
			Kind:            NoteLinkKind("mdlink", "heading"),
			Confidence:      "bogus",
			ConfidenceScore: 2.5,
		},
		{
			SrcPath:         "a.md",
			DstPath:         "c.md",
			Kind:            NoteLinkKind("wikilink", "alias"),
			Confidence:      EdgeConfidenceInferred,
			ConfidenceScore: -3,
		},
	}

	require.NoError(t, store.ReplaceGraphDocEdgesWithConfidence(ctx, "a.md", "wikilink", edges))

	got, err := store.AllGraphDocEdgesWithConfidence(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)

	byDst := make(map[string]GraphDocEdge, len(got))
	for _, e := range got {
		byDst[e.DstPath] = e
	}

	require.Equal(t, EdgeConfidenceExtracted, byDst["b.md"].Confidence)
	require.InDelta(t, 1.0, byDst["b.md"].ConfidenceScore, 0.0001)
	require.Equal(t, EdgeConfidenceInferred, byDst["c.md"].Confidence)
	require.InDelta(t, 1.0, byDst["c.md"].ConfidenceScore, 0.0001)
}
