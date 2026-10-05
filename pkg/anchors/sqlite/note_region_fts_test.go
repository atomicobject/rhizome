package sqlite

import (
	"context"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestNoteMetadataSearchRegionsReplaceLexicalFTS(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	snapshot := NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "v1", RawNotesHash: "v1", LoadedAt: 1, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "reports/visible.html", Title: "Visible", FormatID: "html", ContentHash: "v1"},
			{Path: "reports/script.html", Title: "Script", FormatID: "html", ContentHash: "s1"},
			{Path: "notes/markdown.md", Title: "Markdown", FormatID: "markdown", ContentHash: "m1"},
		},
		SearchRegions: []NoteSearchRegionRow{
			{NotePath: "reports/visible.html", Ordinal: 0, Origin: "derived", Kind: "visible", Text: "sharedtoken", MediaType: "text/plain"},
			{NotePath: "reports/script.html", Ordinal: 0, Origin: "derived", Kind: "supplemental", Text: "sharedtoken", MediaType: "application/javascript"},
			{NotePath: "notes/markdown.md", Ordinal: 0, Origin: "authored", Kind: "visible", Text: "markdownonlytoken", MediaType: "text/markdown"},
		},
	}
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, snapshot))

	rows, err := store.SearchIntelFTS(ctx, "sharedtoken", 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, noteRegionFTSVisible, rows[0].Type)
	require.Equal(t, "reports/visible.html", rows[0].Path)

	rows, err = store.SearchIntelFTS(ctx, "markdownonlytoken", 10)
	require.NoError(t, err)
	require.Empty(t, rows, "Markdown search regions must not duplicate structural section FTS")

	delta := NoteMetadataDelta{
		State:         NoteMetadataState{NotesHash: "v2", RawNotesHash: "v2", LoadedAt: 2, Ready: true},
		Notes:         []NoteMetadataRow{{Path: "reports/visible.html", Title: "Visible", FormatID: "html", ContentHash: "v2"}},
		SearchRegions: []NoteSearchRegionRow{{NotePath: "reports/visible.html", Ordinal: 0, Origin: "derived", Kind: "visible", Text: "replacementtoken", MediaType: "text/plain"}},
		DeletedPaths:  []string{"reports/script.html"},
	}
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, delta))

	rows, err = store.SearchIntelFTS(ctx, "sharedtoken", 10)
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = store.SearchIntelFTS(ctx, "replacementtoken", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "reports/visible.html", rows[0].Path)
}

func TestSearchIntelFTSNoteScopesIncludeProviderRegions(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "note-scope.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "v1", RawNotesHash: "v1", LoadedAt: 1, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "reports/visible.html", Title: "Visible", FormatID: "html", ContentHash: "v1"},
			{Path: "reports/script.html", Title: "Script", FormatID: "html", ContentHash: "s1"},
		},
		SearchRegions: []NoteSearchRegionRow{
			{NotePath: "reports/visible.html", Ordinal: 0, Origin: "derived", Kind: "visible", Text: "needle", MediaType: "text/plain"},
			{NotePath: "reports/script.html", Ordinal: 0, Origin: "derived", Kind: "supplemental", Text: "needle", MediaType: "application/javascript"},
		},
	}))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/code.go", []codeanchor.IntelAnchor{{
		AnchorID: "code", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/code.go", Symbol: "Code", Fingerprint: "code",
	}}, nil, []codeanchor.IntelFTSRow{{ItemType: "anchor", ItemID: "code", Path: "pkg/code.go", Title: "Code", Body: "needle needle needle"}}))
	_, err = store.db.ExecContext(ctx, `INSERT INTO ontology_note_types(note_path, type_name, schema_hash, updated_at) VALUES
		('reports/visible.html', 'Report', 'schema', 1), ('reports/script.html', 'Report', 'schema', 1)`)
	require.NoError(t, err)
	for name, filters := range map[string]IntelSearchFilters{
		"note scope": {Types: []string{"note"}},
		"note type":  {NoteTypes: []string{"Report"}},
	} {
		t.Run(name, func(t *testing.T) {
			rows, err := store.SearchIntelFTSFiltered(ctx, "needle", 10, filters)
			require.NoError(t, err)
			require.Len(t, rows, 2)
			require.ElementsMatch(t, []string{"reports/visible.html", "reports/script.html"}, []string{rows[0].Path, rows[1].Path})
			rows, err = store.SearchIntelFTSFiltered(ctx, "needle", 1, filters)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, "reports/visible.html", rows[0].Path)
		})
	}
}
