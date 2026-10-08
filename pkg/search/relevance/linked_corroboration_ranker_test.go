package relevance

import (
	"context"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

type fakeEdgeStore []semdb.GraphDocEdge

func (f fakeEdgeStore) GraphDocEdgesWithConfidenceForPaths(context.Context, []string) ([]semdb.GraphDocEdge, error) {
	return f, nil
}

type capturingRanker struct{ got []search.Candidate }

func (r *capturingRanker) Rank(_ context.Context, _ search.QuerySpec, candidates []search.Candidate) ([]search.RankedResult, error) {
	r.got = candidates
	return nil, nil
}

func noteCandidate(path string, evidence ...search.Evidence) search.Candidate {
	h := knowledge.NoteHandle(path)
	return search.Candidate{Handle: h, Owner: h, Type: "note", Path: path, Evidence: evidence}
}

func TestLinkedCorroborationCountsOnlyLinksBetweenRelevantCandidates(t *testing.T) {
	title := search.Evidence{Type: "note_title_match", RawScore: 0.8}
	store := fakeEdgeStore{
		{SrcPath: "a.md", DstPath: "b.md", Kind: semdb.GraphDocEdgeKindWikilink},
		{SrcPath: "a.md", DstPath: "b.md", Kind: semdb.NoteLinkKind("wikilink", "alias")},
		{SrcPath: "c.md", DstPath: "a.md", Kind: semdb.GraphDocEdgeKindWikilink},
		{SrcPath: "hub.md", DstPath: "a.md", Kind: semdb.GraphDocEdgeKindWikilink},
		{SrcPath: "linked-only.md", DstPath: "a.md", Kind: semdb.GraphDocEdgeKindWikilink},
		{SrcPath: "a.md", DstPath: "outside.md", Kind: semdb.GraphDocEdgeKindWikilink},
	}
	base := &capturingRanker{}
	_, err := (&LinkedCorroborationRanker{Base: base, Store: store}).Rank(context.Background(), search.QuerySpec{Text: "catalog migration", Intent: search.IntentSearch}, []search.Candidate{
		noteCandidate("a.md", title),
		noteCandidate("b.md", search.Evidence{Type: "note_vector_similarity", RawScore: 0.6}),
		noteCandidate("c.md", title),
		noteCandidate("hub.md", search.Evidence{Type: "graph_hits_authority", RawScore: 1}),
		noteCandidate("linked-only.md", search.Evidence{Type: "link_text_match", RawScore: 0.9}),
		noteCandidate("isolated.md", title),
	})
	require.NoError(t, err)

	corroboration := map[string]search.Evidence{}
	for _, c := range base.got {
		for _, ev := range c.Evidence {
			if ev.Type == "linked_corroboration" {
				corroboration[c.Path] = ev
			}
		}
	}
	require.Len(t, corroboration, 3, "hub, link-text-only, and isolated candidates gain nothing")
	require.Equal(t, 0.75, corroboration["a.md"].RawScore)
	require.Equal(t, "b.md, c.md", corroboration["a.md"].Details["neighbors"])
	require.Equal(t, 0.5, corroboration["b.md"].RawScore)
	require.Equal(t, 0.5, corroboration["c.md"].RawScore)
}
