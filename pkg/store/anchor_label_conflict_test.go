package store

import (
	"context"
	"fmt"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

// Two notes whose default labels collide (both "Run") must not share one
// anchor row: the note with the smaller path keeps the label in either
// indexing order, and the other note is reported instead of silently
// rebound to a symbol it never named.
func TestUpsertNoteWithCleanupRefusesCollidingDefaultLabels(t *testing.T) {
	parse := func(path, pkg string) codeanchor.Note {
		note, err := codeanchor.ParseNote(path, "---\ncode-anchors:\n  go:\n    - symbol: "+pkg+".Run\n---\n")
		require.NoError(t, err)
		require.Equal(t, "Run", note.DefinedAnchors[0].Label)
		return note
	}
	a, b := parse("notes/a.md", "example.com/a"), parse("notes/b.md", "example.com/b")

	for name, order := range map[string][]codeanchor.Note{"a first": {a, b}, "b first": {b, a}} {
		for _, batch := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s batch=%v", name, batch), func(t *testing.T) {
				ctx := context.Background()
				store, err := Open(currentSchemaTestDBPath(t, "test.db"))
				require.NoError(t, err)
				defer func() { _ = store.Close() }()

				var conflicts []codeanchor.AnchorLabelConflict
				if batch {
					items := []codeanchor.NoteWithKeepLabels{{Note: order[0], KeepLabels: []string{"Run"}}, {Note: order[1], KeepLabels: []string{"Run"}}}
					result, err := store.UpsertNotesWithCleanupBatch(ctx, items)
					require.NoError(t, err)
					conflicts = result.LabelConflicts
				} else {
					for _, note := range order {
						result, err := store.UpsertNoteWithCleanup(ctx, note, []string{"Run"})
						require.NoError(t, err)
						conflicts = append(conflicts, result.LabelConflicts...)
					}
				}
				require.Equal(t, []codeanchor.AnchorLabelConflict{{Label: "Run", Kept: "notes/a.md", Dropped: []string{"notes/b.md"}}}, conflicts)

				anchors, err := store.Anchors(ctx)
				require.NoError(t, err)
				require.Len(t, anchors, 1)
				require.Equal(t, "example.com/a", anchors[0].BaseSym.Pkg)
				owners, err := store.NotePathsByAnchorIDs(ctx, []int64{anchors[0].ID})
				require.NoError(t, err)
				require.Equal(t, []string{"notes/a.md"}, owners[anchors[0].ID])
			})
		}
	}
}
