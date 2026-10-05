package relevance

import (
	"context"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

type stubGraphDocScoreStore struct {
	scores map[string]semdb.GraphDocScore
	edges  []semdb.GraphDocEdge
	err    error
}

func (s *stubGraphDocScoreStore) GraphDocScoresByPaths(ctx context.Context, paths []string) (map[string]semdb.GraphDocScore, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[string]semdb.GraphDocScore)
	for _, p := range paths {
		if sc, ok := s.scores[p]; ok {
			out[p] = sc
		}
	}
	return out, nil
}

func (s *stubGraphDocScoreStore) GraphDocEdgesWithConfidenceForPaths(ctx context.Context, paths []string) ([]semdb.GraphDocEdge, error) {
	if s.err != nil {
		return nil, s.err
	}
	pathSet := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		pathSet[p] = struct{}{}
	}
	var out []semdb.GraphDocEdge
	for _, e := range s.edges {
		if _, ok := pathSet[e.SrcPath]; ok {
			out = append(out, e)
			continue
		}
		if _, ok := pathSet[e.DstPath]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}

func hasEvidence(c search.Candidate, typ string) bool {
	for _, ev := range c.Evidence {
		if ev.Type == typ {
			return true
		}
	}
	return false
}

func TestGraphDocScoreRanker_InsertsEvidenceAndRanks(t *testing.T) {
	base := &WeightedRanker{}
	store := &stubGraphDocScoreStore{scores: map[string]semdb.GraphDocScore{
		"a.md":    {DocPath: "a.md", DocType: "note", Authority: 0.8, Hub: 0.3, Community: "c1"},
		"b.md":    {DocPath: "b.md", DocType: "note", Authority: 0.0, Hub: 0.0, Community: "c2"},
		"seed.md": {DocPath: "seed.md", DocType: "note", Authority: 0, Hub: 0, Community: "c1"},
	}}

	r := GraphDocScoreRanker{Base: base, Store: store}
	spec := search.QuerySpec{Seeds: []knowledge.Handle{knowledge.NoteHandle("seed.md")}}
	candidates := []search.Candidate{
		{Handle: knowledge.NoteHandle("a.md"), Owner: knowledge.NoteHandle("a.md"), Path: "a.md"},
		{Handle: knowledge.NoteHandle("b.md"), Owner: knowledge.NoteHandle("b.md"), Path: "b.md"},
	}

	results, err := r.Rank(context.Background(), spec, candidates)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "a.md", results[0].Path, "candidate with graph evidence should rank first")
	require.True(t, hasEvidence(results[0].Candidate, "graph_hits_authority"))
	require.True(t, hasEvidence(results[0].Candidate, "graph_hits_hub"))
	require.True(t, hasEvidence(results[0].Candidate, "same_community"))
	require.False(t, hasEvidence(results[1].Candidate, "graph_hits_authority"))
}

func TestGraphDocScoreRanker_RequiresBase(t *testing.T) {
	r := GraphDocScoreRanker{}
	_, err := r.Rank(context.Background(), search.QuerySpec{}, nil)
	require.Error(t, err)
}

func TestGraphDocScoreRanker_EdgeConfidenceEvidence(t *testing.T) {
	base := &WeightedRanker{}
	store := &stubGraphDocScoreStore{
		scores: map[string]semdb.GraphDocScore{
			"a.md":    {DocPath: "a.md", DocType: "note"},
			"b.md":    {DocPath: "b.md", DocType: "note"},
			"seed.md": {DocPath: "seed.md", DocType: "note"},
		},
		edges: []semdb.GraphDocEdge{
			// a.md connected to seed via extracted edge (score 1.0)
			{SrcPath: "seed.md", DstPath: "a.md", Kind: "wikilink", Confidence: "extracted", ConfidenceScore: 1.0},
			// b.md connected to seed via inferred edge (score 0.7)
			{SrcPath: "seed.md", DstPath: "b.md", Kind: "wikilink", Confidence: "inferred", ConfidenceScore: 0.7},
		},
	}

	r := GraphDocScoreRanker{Base: base, Store: store}
	spec := search.QuerySpec{Seeds: []knowledge.Handle{knowledge.NoteHandle("seed.md")}}
	candidates := []search.Candidate{
		{Handle: knowledge.NoteHandle("a.md"), Owner: knowledge.NoteHandle("a.md"), Path: "a.md"},
		{Handle: knowledge.NoteHandle("b.md"), Owner: knowledge.NoteHandle("b.md"), Path: "b.md"},
	}

	results, err := r.Rank(context.Background(), spec, candidates)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// Both should have graph_edge_confidence evidence.
	require.True(t, hasEvidence(results[0].Candidate, "graph_edge_confidence"), "first candidate should have edge confidence evidence")
	require.True(t, hasEvidence(results[1].Candidate, "graph_edge_confidence"), "second candidate should have edge confidence evidence")

	// Candidate with extracted edge (score 1.0) should rank above inferred (score 0.7).
	require.Equal(t, "a.md", results[0].Path, "extracted edge candidate should rank first")

	// Verify the raw scores are correct.
	findEvidence := func(c search.Candidate, typ string) (search.Evidence, bool) {
		for _, ev := range c.Evidence {
			if ev.Type == typ {
				return ev, true
			}
		}
		return search.Evidence{}, false
	}
	evA, _ := findEvidence(results[0].Candidate, "graph_edge_confidence")
	evB, _ := findEvidence(results[1].Candidate, "graph_edge_confidence")
	require.InDelta(t, 1.0, evA.RawScore, 0.001)
	require.InDelta(t, 0.7, evB.RawScore, 0.001)
}

func TestGraphDocScoreRanker_EdgeConfidence_NoSeedEdges(t *testing.T) {
	// Candidates not connected to seeds should not receive edge confidence evidence.
	base := &WeightedRanker{}
	store := &stubGraphDocScoreStore{
		scores: map[string]semdb.GraphDocScore{
			"a.md":    {DocPath: "a.md", DocType: "note"},
			"seed.md": {DocPath: "seed.md", DocType: "note"},
		},
		edges: []semdb.GraphDocEdge{
			// Edge between two non-seed nodes — no seed connection.
			{SrcPath: "a.md", DstPath: "other.md", Kind: "wikilink", Confidence: "extracted", ConfidenceScore: 1.0},
		},
	}

	r := GraphDocScoreRanker{Base: base, Store: store}
	spec := search.QuerySpec{Seeds: []knowledge.Handle{knowledge.NoteHandle("seed.md")}}
	candidates := []search.Candidate{
		{Handle: knowledge.NoteHandle("a.md"), Owner: knowledge.NoteHandle("a.md"), Path: "a.md"},
	}

	results, err := r.Rank(context.Background(), spec, candidates)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.False(t, hasEvidence(results[0].Candidate, "graph_edge_confidence"), "no seed edge means no confidence evidence")
}

func TestGraphDocScoreRanker_NormalizesPathsForGraphEvidence(t *testing.T) {
	base := &WeightedRanker{}
	store := &stubGraphDocScoreStore{
		scores: map[string]semdb.GraphDocScore{
			"notes/a.md": {DocPath: "notes/a.md", DocType: "note", Authority: 0.8},
			"seed.md":    {DocPath: "seed.md", DocType: "note"},
		},
		edges: []semdb.GraphDocEdge{
			{SrcPath: "seed.md", DstPath: "notes/a.md", Kind: "wikilink", Confidence: "extracted", ConfidenceScore: 1.0},
		},
	}

	r := GraphDocScoreRanker{Base: base, Store: store}
	results, err := r.Rank(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.NoteHandle(`seed.md`)},
	}, []search.Candidate{
		{Handle: knowledge.NoteHandle("notes/a.md"), Owner: knowledge.NoteHandle("notes/a.md"), Path: `notes\a.md`},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.True(t, hasEvidence(results[0].Candidate, "graph_hits_authority"))
	require.True(t, hasEvidence(results[0].Candidate, "graph_edge_confidence"))
}

func TestGraphDocScoreRanker_UsesNodeChunkOwnerAsSeedPath(t *testing.T) {
	base := &WeightedRanker{}
	store := &stubGraphDocScoreStore{
		scores: map[string]semdb.GraphDocScore{
			"notes/a.md":    {DocPath: "notes/a.md", DocType: "note", Community: "c1"},
			"notes/seed.md": {DocPath: "notes/seed.md", DocType: "note", Community: "c1"},
		},
		edges: []semdb.GraphDocEdge{
			{SrcPath: "notes/seed.md", DstPath: "notes/a.md", Kind: "wikilink", Confidence: "extracted", ConfidenceScore: 1.0},
		},
	}

	r := GraphDocScoreRanker{Base: base, Store: store}
	results, err := r.Rank(context.Background(), search.QuerySpec{
		Seeds: []knowledge.Handle{knowledge.NodeChunkHandle("node-1", "notes/seed.md", "body", 0)},
	}, []search.Candidate{
		{Handle: knowledge.NoteHandle("notes/a.md"), Owner: knowledge.NoteHandle("notes/a.md"), Path: "notes/a.md"},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.True(t, hasEvidence(results[0].Candidate, "same_community"))
	require.True(t, hasEvidence(results[0].Candidate, "graph_edge_confidence"))
}
