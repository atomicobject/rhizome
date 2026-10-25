package notemeta

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestLoadNoteSourceSnapshotsClassifiesMissingPersistedSources(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	source := filepath.Join(root, "source.md")
	require.NoError(t, os.WriteFile(source, []byte("# Source\n"), 0o644))
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	vault := obsidian.VaultDefinition{Path: root}
	indexer := testIndexer(t)
	_, err = indexer.EnsureIndexed(ctx, vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	require.NoError(t, os.Rename(source, filepath.Join(t.TempDir(), "source.md")))

	for _, tc := range []struct {
		name    string
		reader  obsidian.NoteReader
		missing bool
	}{
		{"filesystem", &obsidian.Note{}, true},
		{"fs_not_exist", fakeNoteReader{errs: map[string]error{"source.md": os.ErrNotExist}}, true},
		{"permission", fakeNoteReader{errs: map[string]error{"source.md": os.ErrPermission}}, false},
		{"cancellation", fakeNoteReader{errs: map[string]error{"source.md": context.Canceled}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := indexer.LoadNoteSourceSnapshots(ctx, vault, tc.reader, store, []string{"source.md"})
			require.Error(t, err)
			require.Equal(t, tc.missing, errors.Is(err, ErrProjectionNotCurrent))
			if !tc.missing {
				require.ErrorIs(t, err, tc.reader.(fakeNoteReader).errs["source.md"])
			}
			rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"source.md"})
			require.NoError(t, err)
			require.Contains(t, rows, "source.md", "a read must not retire persisted identity")
		})
	}
}
