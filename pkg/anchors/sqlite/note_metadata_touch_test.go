package sqlite

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func touchTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(currentSchemaTestDBPath(t, "touch.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedTouchMetadata(t *testing.T, ctx context.Context, store *Store) NoteMetadataState {
	t.Helper()
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, NoteMetadataSnapshot{
		State: NoteMetadataState{NotesHash: "notes-hash", RawNotesHash: "raw-hash", LoadedAt: 42, Ready: true},
		Notes: []NoteMetadataRow{
			{Path: "notes/source.md", Title: "Source", ContentHash: "a", Mtime: 10, Size: 100},
			{Path: "notes/target.md", Title: "Target", ContentHash: "b", Mtime: 11, Size: 120},
		},
		PropertyValues: []NotePropertyValueRow{
			{NotePath: "notes/source.md", PropertyName: "type", Source: NotePropertySourceFrontmatter, ValueText: "Note", ValueNorm: "note", ValueKind: NotePropertyValueString},
		},
		Tags:            []NoteTagRow{{NotePath: "notes/source.md", TagNorm: "topic/source"}},
		FragmentTargets: []NoteFragmentTargetRow{{NotePath: "notes/target.md", Kind: NoteFragmentTargetHeading, Target: "Target", TargetNorm: "target", Ordinal: 1}},
		WikilinkEdges:   []GraphDocEdgeRow{{SrcPath: "notes/source.md", DstPath: "notes/target.md", Kind: "wikilink"}},
	}))
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	return state
}

func derivedRowDump(t *testing.T, ctx context.Context, store *Store) []string {
	t.Helper()
	out := []string{}
	for _, query := range []string{
		`SELECT n.path, p.property_id, p.value_text, p.value_norm FROM note_property_values p JOIN notes n ON n.id = p.note_id ORDER BY 1,2,3`,
		`SELECT n.path, t.tag_norm FROM note_tags t JOIN notes n ON n.id = t.note_id ORDER BY 1,2`,
		`SELECT n.path, m.target_kind, m.target_text, m.ordinal FROM note_fragment_targets m JOIN notes n ON n.id = m.note_id ORDER BY 1,2,3,4`,
		`SELECT src_path, dst_path, kind FROM graph_doc_edges ORDER BY 1,2,3`,
	} {
		rows, err := store.db.QueryContext(ctx, query)
		require.NoError(t, err)
		for rows.Next() {
			cols, err := rows.Columns()
			require.NoError(t, err)
			values := make([]any, len(cols))
			pointers := make([]any, len(cols))
			for i := range values {
				pointers[i] = &values[i]
			}
			require.NoError(t, rows.Scan(pointers...))
			out = append(out, fmt.Sprintf("%v", values))
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
	}
	return out
}

func TestApplyNoteMetadataDeltaSourceTouchesUpdateOnlyFreshnessColumns(t *testing.T) {
	ctx := context.Background()
	store := touchTestStore(t)
	state := seedTouchMetadata(t, ctx, store)
	before := derivedRowDump(t, ctx, store)
	fingerprintBefore, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)

	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
		State: state,
		SourceTouches: []NoteSourceTouch{
			{Path: "notes/source.md", Mtime: 900, Size: 100},
			{Path: "notes/target.md", Mtime: 901, Size: 120},
		},
	}))

	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/source.md", "notes/target.md"})
	require.NoError(t, err)
	require.Equal(t, int64(900), rows["notes/source.md"].Mtime)
	require.Equal(t, int64(901), rows["notes/target.md"].Mtime)
	require.Equal(t, "a", rows["notes/source.md"].ContentHash)

	require.Equal(t, before, derivedRowDump(t, ctx, store))
	updated, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.Equal(t, state, updated)
	fingerprintAfter, err := store.GraphWebFingerprint(ctx)
	require.NoError(t, err)
	require.Equal(t, fingerprintBefore, fingerprintAfter, "an mtime-only note write must not bump the graph revision")
}

func TestApplyNoteMetadataDeltaSourceTouchesRejectRewrittenPaths(t *testing.T) {
	ctx := context.Background()
	store := touchTestStore(t)
	state := seedTouchMetadata(t, ctx, store)

	err := store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
		State:         state,
		Notes:         []NoteMetadataRow{{Path: "notes/source.md", Title: "Source", ContentHash: "c", Mtime: 20, Size: 130}},
		SourceTouches: []NoteSourceTouch{{Path: "notes/source.md", Mtime: 900, Size: 100}},
	})
	require.ErrorContains(t, err, "notes/source.md")

	err = store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
		State:         state,
		DeletedPaths:  []string{"notes/target.md"},
		SourceTouches: []NoteSourceTouch{{Path: "notes/target.md", Mtime: 900, Size: 120}},
	})
	require.ErrorContains(t, err, "notes/target.md")
}

func TestApplyNoteMetadataDeltaSourceTouchesIgnoreMissingPaths(t *testing.T) {
	ctx := context.Background()
	store := touchTestStore(t)
	state := seedTouchMetadata(t, ctx, store)

	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, NoteMetadataDelta{
		State:         state,
		SourceTouches: []NoteSourceTouch{{Path: "notes/gone.md", Mtime: 900, Size: 10}},
	}))
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/gone.md"})
	require.NoError(t, err)
	require.Empty(t, rows)
}
