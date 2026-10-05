package graphalg

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to build a minimal score map.
func scoreMap(entries ...GraphDocScoreMinimal) map[string]GraphDocScoreMinimal {
	m := make(map[string]GraphDocScoreMinimal, len(entries))
	for _, e := range entries {
		m[e.DocPath] = e
	}
	return m
}

func TestFindSurprisingConnections_EmptyGraph(t *testing.T) {
	result := FindSurprisingConnections(nil, nil, 10, 0)
	assert.Empty(t, result)

	result = FindSurprisingConnections([]GraphDocEdgeForSurprise{}, scoreMap(), 10, 0)
	assert.Empty(t, result)
}

func TestFindSurprisingConnections_CrossCommunityScoresHigher(t *testing.T) {
	// Two communities A and B.
	// Edge within A: "calls" from note-a1 -> note-a2
	// Edge crossing A->B: "calls" from note-a1 -> note-b1
	// Cross-community edge should score higher.
	edges := []GraphDocEdgeForSurprise{
		{SrcPath: "note-a1", DstPath: "note-a2", Kind: "calls", Confidence: "extracted"},
		{SrcPath: "note-a1", DstPath: "note-b1", Kind: "calls", Confidence: "extracted"},
	}
	scores := scoreMap(
		GraphDocScoreMinimal{DocPath: "note-a1", DocType: "note", Community: "A", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "note-a2", DocType: "note", Community: "A", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "note-b1", DocType: "note", Community: "B", Inbound: 3, Outbound: 3, Authority: 0.5},
	)

	result := FindSurprisingConnections(edges, scores, 10, 0)
	require.Len(t, result, 2)
	// First result should be the cross-community edge.
	assert.Equal(t, "note-b1", result[0].DstPath)
	assert.Greater(t, result[0].Score, result[1].Score)

	// Cross-community reason should be present somewhere in the reasons.
	var hasBridgeCommunity bool
	for _, r := range result[0].Reasons {
		if strings.Contains(r, "bridges community") {
			hasBridgeCommunity = true
		}
	}
	assert.True(t, hasBridgeCommunity, "expected 'bridges community' in reasons: %v", result[0].Reasons)
}

func TestFindSurprisingConnections_ConfidenceWeighting(t *testing.T) {
	// Same topology and same label; one edge has a higher numeric confidence score.
	// Use different community pairs (X-Y vs X-Z) to avoid dedup eliminating one.
	edges := []GraphDocEdgeForSurprise{
		{SrcPath: "src1", DstPath: "dst1", Kind: "calls", Confidence: "inferred", ConfidenceScore: 0.9},
		{SrcPath: "src2", DstPath: "dst2", Kind: "calls", Confidence: "inferred", ConfidenceScore: 0.2},
	}
	scores := scoreMap(
		GraphDocScoreMinimal{DocPath: "src1", DocType: "code", Community: "X", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "dst1", DocType: "code", Community: "Y", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "src2", DocType: "code", Community: "X", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "dst2", DocType: "code", Community: "Z", Inbound: 3, Outbound: 3, Authority: 0.5},
	)

	result := FindSurprisingConnections(edges, scores, 10, 0)
	require.Len(t, result, 2)
	// Lower numeric confidence should rank as more surprising.
	assert.Equal(t, "src2", result[0].SrcPath)
	assert.Equal(t, "dst2", result[0].DstPath)
	assert.Greater(t, result[0].Score, result[1].Score)
}

func TestFindSurprisingConnections_DeduplicationByCommunityPair(t *testing.T) {
	// Two edges crossing the same community pair (A, B).
	// Only the highest-scored one should appear.
	edges := []GraphDocEdgeForSurprise{
		// Lower score: extracted
		{SrcPath: "a1", DstPath: "b1", Kind: "calls", Confidence: "extracted"},
		// Higher score: inferred (gets +2 instead of +1, plus cross-community +2)
		{SrcPath: "a2", DstPath: "b2", Kind: "calls", Confidence: "inferred"},
	}
	scores := scoreMap(
		GraphDocScoreMinimal{DocPath: "a1", DocType: "note", Community: "A", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "b1", DocType: "note", Community: "B", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "a2", DocType: "note", Community: "A", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "b2", DocType: "note", Community: "B", Inbound: 3, Outbound: 3, Authority: 0.5},
	)

	result := FindSurprisingConnections(edges, scores, 10, 0)
	// Both cross A↔B; deduplicated to 1 result (the higher-scored inferred edge).
	require.Len(t, result, 1)
	assert.Equal(t, "inferred", result[0].Confidence)
}

func TestFindSurprisingConnections_HubFiltering(t *testing.T) {
	// Edge from a pure-hub node (outbound > 50, inbound < 3) should be excluded.
	edges := []GraphDocEdgeForSurprise{
		{SrcPath: "hub", DstPath: "leaf", Kind: "calls", Confidence: "extracted"},
		{SrcPath: "src", DstPath: "dst", Kind: "calls", Confidence: "extracted"},
	}
	scores := scoreMap(
		GraphDocScoreMinimal{DocPath: "hub", DocType: "note", Community: "A", Inbound: 2, Outbound: 60, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "leaf", DocType: "note", Community: "B", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "src", DocType: "note", Community: "A", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "dst", DocType: "note", Community: "B", Inbound: 3, Outbound: 3, Authority: 0.5},
	)

	result := FindSurprisingConnections(edges, scores, 10, 0)
	// Hub edge should be excluded; only src->dst remains.
	require.Len(t, result, 1)
	assert.Equal(t, "src", result[0].SrcPath)
	assert.Equal(t, "dst", result[0].DstPath)
}

func TestFindSurprisingConnections_CrossTypeBoundary(t *testing.T) {
	// code↔note edge scores higher than note↔note edge (same confidence, same community).
	// Use different community pairs to avoid dedup eliminating one.
	edges := []GraphDocEdgeForSurprise{
		{SrcPath: "note1", DstPath: "note2", Kind: "coderefs", Confidence: "extracted"},
		{SrcPath: "code1", DstPath: "note3", Kind: "coderefs", Confidence: "extracted"},
	}
	scores := scoreMap(
		GraphDocScoreMinimal{DocPath: "note1", DocType: "note", Community: "A", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "note2", DocType: "note", Community: "B", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "code1", DocType: "code", Community: "C", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "note3", DocType: "note", Community: "D", Inbound: 3, Outbound: 3, Authority: 0.5},
	)

	result := FindSurprisingConnections(edges, scores, 10, 0)
	require.Len(t, result, 2)
	// Cross-type (code↔note) should rank first: gets +1 conf, +2 cross-community, +2 cross-type = 5.0
	// note↔note edge gets +1 conf, +2 cross-community = 3.0
	assert.Equal(t, "code1", result[0].SrcPath)
	assert.Greater(t, result[0].Score, result[1].Score)
	// Reason should mention boundary.
	var hasBoundary bool
	for _, r := range result[0].Reasons {
		if strings.Contains(r, "boundary") {
			hasBoundary = true
		}
	}
	assert.True(t, hasBoundary, "expected boundary reason, got: %v", result[0].Reasons)
}

func TestFindSurprisingConnections_ExplicitLinksExcluded(t *testing.T) {
	// wikilink, mdlink, and note_link:* edges should be excluded.
	edges := []GraphDocEdgeForSurprise{
		{SrcPath: "a", DstPath: "b", Kind: "wikilink", Confidence: "extracted"},
		{SrcPath: "a", DstPath: "c", Kind: "mdlink", Confidence: "extracted"},
		{SrcPath: "a", DstPath: "d", Kind: "note_link:type:sub", Confidence: "extracted"},
		{SrcPath: "a", DstPath: "e", Kind: "calls", Confidence: "extracted"},
	}
	scores := scoreMap(
		GraphDocScoreMinimal{DocPath: "a", DocType: "code", Community: "X", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "b", DocType: "note", Community: "Y", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "c", DocType: "note", Community: "Y", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "d", DocType: "note", Community: "Y", Inbound: 3, Outbound: 3, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "e", DocType: "note", Community: "Y", Inbound: 3, Outbound: 3, Authority: 0.5},
	)

	result := FindSurprisingConnections(edges, scores, 10, 0)
	// Only the "calls" edge survives.
	require.Len(t, result, 1)
	assert.Equal(t, "calls", result[0].EdgeKind)
}

func TestFindSurprisingConnections_MinScore(t *testing.T) {
	// Edges below minScore threshold are excluded.
	// extracted + same community + same type = 1.0 score
	edges := []GraphDocEdgeForSurprise{
		{SrcPath: "a", DstPath: "b", Kind: "calls", Confidence: "extracted"},
	}
	scores := scoreMap(
		GraphDocScoreMinimal{DocPath: "a", DocType: "note", Community: "X", Inbound: 5, Outbound: 5, Authority: 0.5},
		GraphDocScoreMinimal{DocPath: "b", DocType: "note", Community: "X", Inbound: 5, Outbound: 5, Authority: 0.5},
	)

	// Score for this edge = 1.0 (extracted only). With minScore=2.0, it should be excluded.
	result := FindSurprisingConnections(edges, scores, 10, 2.0)
	assert.Empty(t, result)

	// With minScore=0, it should be included.
	result = FindSurprisingConnections(edges, scores, 10, 0)
	require.Len(t, result, 1)
}

func TestFindSurprisingConnections_NoScores_BetweennessFallback(t *testing.T) {
	// Build a barbell graph: two cliques connected by a single bridge edge.
	// clique1: {a,b,c} fully connected
	// clique2: {d,e,f} fully connected
	// bridge: c -> d
	//
	// The bridge edge should rank #1 by betweenness centrality since all
	// shortest paths between clique1 and clique2 pass through it.
	edges := []GraphDocEdgeForSurprise{
		// clique1
		{SrcPath: "a", DstPath: "b", Kind: "calls", Confidence: "extracted"},
		{SrcPath: "a", DstPath: "c", Kind: "calls", Confidence: "extracted"},
		{SrcPath: "b", DstPath: "c", Kind: "calls", Confidence: "extracted"},
		// bridge
		{SrcPath: "c", DstPath: "d", Kind: "calls", Confidence: "extracted"},
		// clique2
		{SrcPath: "d", DstPath: "e", Kind: "calls", Confidence: "extracted"},
		{SrcPath: "d", DstPath: "f", Kind: "calls", Confidence: "extracted"},
		{SrcPath: "e", DstPath: "f", Kind: "calls", Confidence: "extracted"},
	}

	result := FindSurprisingConnections(edges, nil, 3, 0)
	require.NotEmpty(t, result)

	// Bridge edge (c<->d) should be #1.
	top := result[0]
	isBridge := (top.SrcPath == "c" && top.DstPath == "d") ||
		(top.SrcPath == "d" && top.DstPath == "c")
	assert.True(t, isBridge, "expected bridge edge c<->d to rank #1, got %s<->%s", top.SrcPath, top.DstPath)
	assert.Equal(t, []string{"bridges graph structure (betweenness centrality)"}, top.Reasons)

	undirectedScores := func(result []SurprisingConnection) map[string]float64 {
		scores := make(map[string]float64, len(result))
		for _, edge := range result {
			key := edge.SrcPath + "|" + edge.DstPath
			if edge.SrcPath > edge.DstPath {
				key = edge.DstPath + "|" + edge.SrcPath
			}
			scores[key] = edge.Score
		}
		return scores
	}
	t.Run("ignores stored edge orientation", func(t *testing.T) {
		original := FindSurprisingConnections([]GraphDocEdgeForSurprise{
			{SrcPath: "a", DstPath: "b", Kind: "calls", Confidence: "extracted"},
			{SrcPath: "b", DstPath: "c", Kind: "calls", Confidence: "extracted"},
		}, nil, 10, 0)
		reversed := FindSurprisingConnections([]GraphDocEdgeForSurprise{
			{SrcPath: "b", DstPath: "a", Kind: "calls", Confidence: "extracted"},
			{SrcPath: "c", DstPath: "b", Kind: "calls", Confidence: "extracted"},
		}, nil, 10, 0)
		// A directed traversal scores a consistently oriented chain like the
		// undirected graph, so the converging orientation is the discriminator.
		converging := FindSurprisingConnections([]GraphDocEdgeForSurprise{
			{SrcPath: "a", DstPath: "b", Kind: "calls", Confidence: "extracted"},
			{SrcPath: "c", DstPath: "b", Kind: "calls", Confidence: "extracted"},
		}, nil, 10, 0)
		require.Len(t, original, 2)
		require.Len(t, reversed, 2)
		require.Len(t, converging, 2)
		originalScores := undirectedScores(original)
		assert.InDelta(t, originalScores["a|b"], originalScores["b|c"], 1e-9, fmt.Sprintf("scores = %#v", originalScores))
		assert.Equal(t, originalScores, undirectedScores(reversed))
		assert.Equal(t, originalScores, undirectedScores(converging))
	})
	t.Run("ties break by source and destination", func(t *testing.T) {
		result := FindSurprisingConnections([]GraphDocEdgeForSurprise{
			{SrcPath: "b", DstPath: "c", Kind: "calls", Confidence: "extracted"},
			{SrcPath: "a", DstPath: "b", Kind: "calls", Confidence: "extracted"},
		}, nil, 10, 0)
		require.Len(t, result, 2)
		assert.Equal(t, [][2]string{{"a", "b"}, {"b", "c"}}, [][2]string{{result[0].SrcPath, result[0].DstPath}, {result[1].SrcPath, result[1].DstPath}})
	})
}

func TestFindSurprisingConnections_NoScores_UsesConfidenceMagnitude(t *testing.T) {
	edges := []GraphDocEdgeForSurprise{
		{SrcPath: "a", DstPath: "b", Kind: "calls", Confidence: "inferred", ConfidenceScore: 0.9},
		{SrcPath: "b", DstPath: "c", Kind: "calls", Confidence: "inferred", ConfidenceScore: 0.9},
		{SrcPath: "c", DstPath: "d", Kind: "calls", Confidence: "inferred", ConfidenceScore: 0.2},
		{SrcPath: "d", DstPath: "a", Kind: "calls", Confidence: "inferred", ConfidenceScore: 0.9},
	}

	result := FindSurprisingConnections(edges, nil, 10, 0)
	require.Len(t, result, 4)
	assert.Equal(t, "c", result[0].SrcPath)
	assert.Equal(t, "d", result[0].DstPath)
	assert.Greater(t, result[0].Score, result[1].Score)
}

func TestFindSurprisingConnections_NoScores_RespectsMinScore(t *testing.T) {
	edges := []GraphDocEdgeForSurprise{
		{SrcPath: "a", DstPath: "b", Kind: "calls", Confidence: "extracted"},
	}

	result := FindSurprisingConnections(edges, nil, 10, 10)
	assert.Empty(t, result)
}
