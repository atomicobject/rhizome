package retrieval

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/stretchr/testify/require"
)

func TestNoteLexicalRetriever_IgnoresDocTokens(t *testing.T) {
	ctx := context.Background()
	r := &NoteLexicalRetriever{
		PathSource: NotePathSourceFunc(func(context.Context) ([]string, error) {
			return []string{
				"docs/hubs/Search (Hub).MD",
				"docs/hubs/Vision + Operating Model (Hub).md",
			}, nil
		}),
	}

	results, err := r.Retrieve(ctx, search.QuerySpec{
		Text:   "search documentation",
		Limits: search.Limits{Total: 5},
	})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Equal(t, "docs/hubs/Search (Hub).MD", results[0].Path)
}

func TestNoteLexicalRetrieverMarksExactTitleWithoutClippingOtherEvidence(t *testing.T) {
	r := &NoteLexicalRetriever{PathSource: NotePathSourceFunc(func(context.Context) ([]string, error) {
		return []string{"docs/hubs/Search (Hub).md", "docs/hubs/Search Operations.md"}, nil
	})}
	results, err := r.Retrieve(context.Background(), search.QuerySpec{Text: "search (hub)", Limits: search.Limits{Total: 5}})
	require.NoError(t, err)
	require.Equal(t, "docs/hubs/Search (Hub).md", results[0].Path)
	types := map[string]bool{}
	for _, evidence := range results[0].Evidence {
		types[evidence.Type] = true
	}
	require.True(t, types["note_title_match"])
	require.True(t, types["note_title_exact"])
}

func TestNoteLexicalRetrieverUsesAuthoritativeCatalogTitle(t *testing.T) {
	r := &NoteLexicalRetriever{
		PathSource: NotePathSourceFunc(func(context.Context) ([]string, error) {
			return []string{"docs/specs/effort-note-frozen-section-inline-display.md", "docs/efforts/effort-note-frozen-section-inline-display.md"}, nil
		}),
		CatalogSource: NoteCatalogSourceFunc(func(context.Context) ([]NoteCatalogEntry, error) {
			return []NoteCatalogEntry{
				{Path: "docs/specs/effort-note-frozen-section-inline-display.md", Title: "Effort Note Frozen Section Rendering"},
				{Path: "docs/efforts/effort-note-frozen-section-inline-display.md", Title: "Implementation effort"},
			}, nil
		}),
	}
	results, err := r.Retrieve(context.Background(), search.QuerySpec{Text: "Effort Note Frozen Section Rendering", Limits: search.Limits{Total: 5}})
	require.NoError(t, err)
	require.Equal(t, "docs/specs/effort-note-frozen-section-inline-display.md", results[0].Path)
	require.True(t, hasEvidenceTypeForTest(results[0].Evidence, "note_title_exact"))
}

func hasEvidenceTypeForTest(evidence []search.Evidence, typ string) bool {
	for _, item := range evidence {
		if item.Type == typ {
			return true
		}
	}
	return false
}

func TestNoteLexicalPathPrefixIsLiteral(t *testing.T) {
	require.True(t, pathMatchesPrefix("docs/100%/a_b.md", []string{"docs/100%"}))
	require.False(t, pathMatchesPrefix("docs/100x/a-b.md", []string{"docs/100%"}))
	require.False(t, pathMatchesPrefix("Docs/100%/a_b.md", []string{"docs/100%"}))
	require.False(t, pathMatchesPrefix("docs/100-other/a_b.md", []string{"docs/100"}))
}

func TestNoteLexicalRetrieverUsesBoundedTypoFallbackOnlyForBroadIntent(t *testing.T) {
	r := &NoteLexicalRetriever{PathSource: NotePathSourceFunc(func(context.Context) ([]string, error) {
		return []string{"docs/hubs/Search.md", "docs/hubs/Embeddings.md"}, nil
	})}
	results, err := r.Retrieve(context.Background(), search.QuerySpec{Text: "serach", Intent: search.IntentSearch, Limits: search.Limits{Total: 5}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "docs/hubs/Search.md", results[0].Path)
	require.Equal(t, "typo_title_match", results[0].Evidence[0].Type)

	results, err = r.Retrieve(context.Background(), search.QuerySpec{Text: "serach", Intent: search.IntentGoToDef, Limits: search.Limits{Total: 5}})
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestNoteLexicalRetrieverRejectsUnrelatedTypoMatches(t *testing.T) {
	for _, query := range []string{"semantic query", "query"} {
		t.Run(query, func(t *testing.T) {
			r := &NoteLexicalRetriever{PathSource: NotePathSourceFunc(func(context.Context) ([]string, error) {
				return []string{"notes/Every.md"}, nil
			})}
			results, err := r.Retrieve(context.Background(), search.QuerySpec{Text: query, Intent: search.IntentSearch})
			require.NoError(t, err)
			require.Empty(t, results)
		})
	}
}

func TestNoteLexicalRetrieverDoesNotCorrectOneWordOfMultiwordQuery(t *testing.T) {
	r := &NoteLexicalRetriever{PathSource: NotePathSourceFunc(func(context.Context) ([]string, error) {
		return []string{"notes/Search.md"}, nil
	})}
	results, err := r.Retrieve(context.Background(), search.QuerySpec{Text: "semantic serach", Intent: search.IntentSearch})
	require.NoError(t, err)
	require.Empty(t, results)
}
