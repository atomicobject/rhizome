package notemeta

import (
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestNestedEmptyMetadataKeysHaveSnapshotDeltaParity(t *testing.T) {
	for _, tc := range []struct{ path, initial, updated string }{
		{"note.md", "---\npayload: {before: old}\nstate: old\n---\n# Note\n", "---\npayload: {\"\": kept}\nstate: ready\n---\n# Note\n"},
		{"note.html", `<head><script id="rhizome-metadata" type="application/json">{"payload":{"before":"old"},"state":"old"}</script></head><body><h1>Note</h1></body>`, `<head><script id="rhizome-metadata" type="application/json">{"payload":{"":"kept"},"state":"ready"}</script></head><body><h1>Note</h1></body>`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			ctx := t.Context()
			root := t.TempDir()
			vault := obsidian.VaultDefinition{Path: root, Includes: []string{"*.md", "*.html"}}
			indexer := testIndexer(t)
			incremental, err := sqlitefixture.Open(filepath.Join(root, "incremental.db"))
			require.NoError(t, err)
			defer incremental.Close()
			reader := fakeNoteReader{notes: map[string]string{tc.path: tc.initial}}
			_, err = indexer.EnsureIndexed(ctx, vault, reader, incremental)
			require.NoError(t, err)
			reader.notes[tc.path] = tc.updated
			require.NoError(t, indexer.SyncPaths(ctx, vault, reader, incremental, []string{tc.path}, nil))
			fresh, err := sqlitefixture.Open(filepath.Join(root, "fresh.db"))
			require.NoError(t, err)
			defer fresh.Close()
			_, err = indexer.EnsureIndexed(ctx, vault, reader, fresh)
			require.NoError(t, err)
			got, err := incremental.CurrentNotePropertyValues(ctx, []string{tc.path}, nil, 0)
			require.NoError(t, err)
			want, err := fresh.CurrentNotePropertyValues(ctx, []string{tc.path}, nil, 0)
			require.NoError(t, err)
			require.NotEmpty(t, got)
			require.Equal(t, want, got)
			gotState, err := incremental.GetNoteMetadataState(ctx)
			require.NoError(t, err)
			wantState, err := fresh.GetNoteMetadataState(ctx)
			require.NoError(t, err)
			require.Equal(t, wantState.NotesHash, gotState.NotesHash)
			require.Equal(t, wantState.RawNotesHash, gotState.RawNotesHash)
			snapshots, err := indexer.BuildNoteSourceSnapshots(ctx, vault, reader)
			require.NoError(t, err)
			require.Len(t, snapshots, 1)
			require.Equal(t, map[string]any{"": "kept"}, snapshots[0].Frontmatter["payload"])
			require.Equal(t, "ready", snapshots[0].Frontmatter["state"])
		})
	}
}
