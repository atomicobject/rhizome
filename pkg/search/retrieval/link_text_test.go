package retrieval

import (
	"context"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

type fakeLinkTextSource []semdb.LinkTextRow

func (f fakeLinkTextSource) NoteLinkTextMatches(context.Context, []string, int) ([]semdb.LinkTextRow, error) {
	return f, nil
}

func linkText(label, line string) string { return semdb.AppendLinkText("", label, line) }

func TestLinkTextRetrieverScoresTargetsByWhatLinkingNotesCallThem(t *testing.T) {
	label := linkText("catalog migration", "- Kickoff for the catalog migration")
	store := fakeLinkTextSource{
		{SrcPath: "a.md", DstPath: "Projects/Larkspur.md", LinkText: label, SrcTargets: 3},
		{SrcPath: "b.md", DstPath: "Projects/Larkspur.md", LinkText: label, SrcTargets: 3},
		{SrcPath: "c.md", DstPath: "Projects/Larkspur.md", LinkText: label, SrcTargets: 3},
		{SrcPath: "d.md", DstPath: "Birds/Club list.md", LinkText: label, SrcTargets: 3},
		{SrcPath: "e.md", DstPath: "Notes/Line only.md", LinkText: linkText("", "notes on the catalog migration plan"), SrcTargets: 3},
		{SrcPath: "Index.md", DstPath: "Notes/Hub listed.md", LinkText: label, SrcTargets: 40},
		{SrcPath: "Projects/Larkspur.md", DstPath: "Projects/Larkspur.md", LinkText: label, SrcTargets: 3},
		{SrcPath: "f.md", DstPath: "Notes/Unrelated.md", LinkText: linkText("budget", "budget review"), SrcTargets: 3},
	}
	got, err := (&LinkTextRetriever{Store: store}).Retrieve(context.Background(), search.QuerySpec{Text: "catalog migration"})
	require.NoError(t, err)

	scores := map[string]float64{}
	for _, c := range got {
		require.Len(t, c.Evidence, 1)
		require.Equal(t, "link_text_match", c.Evidence[0].Type)
		scores[c.Path] = c.Evidence[0].RawScore
	}
	require.Equal(t, map[string]float64{
		"Projects/Larkspur.md": 0.875, // three agreeing notes: 1 - 0.5^3
		"Birds/Club list.md":   0.5,   // one stray label
		"Notes/Line only.md":   0.25,  // the line around a link counts half
		"Notes/Hub listed.md":  0.25,  // a source linking 40 targets counts 20/40
	}, scores)
	require.Equal(t, "Projects/Larkspur.md", got[0].Path)
	require.Equal(t, "3", got[0].Evidence[0].Details["linking_notes"])
	require.Equal(t, "catalog migration", got[0].Evidence[0].Details["label"])
	require.Equal(t, "catalog migration", got[0].Evidence[0].Details[search.LinkTextAliasDetail], "three notes agree on the label")
	require.Equal(t, "Birds/Club list.md", got[1].Path)
	require.NotContains(t, got[1].Evidence[0].Details, search.LinkTextAliasDetail, "one note's label is not an alias")
}

func TestLinkTextRetrieverSkipsTypeFilteredQueries(t *testing.T) {
	store := fakeLinkTextSource{{SrcPath: "a.md", DstPath: "b.md", LinkText: linkText("catalog", ""), SrcTargets: 1}}
	got, err := (&LinkTextRetriever{Store: store}).Retrieve(context.Background(), search.QuerySpec{Text: "catalog", Filters: search.Filters{NoteTypes: []string{"Project"}}})
	require.NoError(t, err)
	require.Empty(t, got)
}
