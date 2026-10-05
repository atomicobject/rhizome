package notemeta

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestEnsureIndexedPublishesOneSourceCapture(t *testing.T) {
	for _, cached := range []bool{false, true} {
		name := "direct"
		if cached {
			name = "cached"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			reader := &changingSourceReader{fakeNoteReader: fakeNoteReader{notes: map[string]string{"note.md": "# Stable A\n"}}}
			var notes obsidian.NoteReader = reader
			if cached {
				notes = &changingSnapshotReader{changingSourceReader: reader}
			}
			indexer := testIndexer(t)
			vault := obsidian.VaultDefinition{Path: t.TempDir()}
			for step, title := range []string{"Stable A", "Transient B", "Stable A", "Stable A"} {
				result, err := indexer.EnsureIndexed(ctx, vault, notes, store)
				require.NoError(t, err)
				require.Equal(t, step < 3, result.Dirty)
				rows, err := store.CurrentNoteMetadataRows(ctx)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, title, rows[0].Title)
				state, err := store.GetNoteMetadataState(ctx)
				require.NoError(t, err)
				require.Equal(t, notesHashFromRows(rows), state.RawNotesHash)
				require.Equal(t, int32(step+1), reader.captures.Load())
			}
			current, err := indexer.MetadataStateCurrent(ctx, vault, notes, store)
			require.NoError(t, err)
			require.True(t, current)
		})
	}
}

type changingSourceReader struct {
	fakeNoteReader
	captures atomic.Int32
}

func (r *changingSourceReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	if r.captures.Add(1) == 2 {
		return "# Transient B\n", nil
	}
	return "# Stable A\n", nil
}

type changingSnapshotReader struct {
	*changingSourceReader
}

func (r *changingSnapshotReader) EntriesSnapshot(context.Context) ([]cache.Entry, error) {
	content, err := r.GetContents(obsidian.VaultDefinition{}, "note.md")
	if err != nil {
		return nil, err
	}
	return []cache.Entry{{Path: "note.md", Content: content, ModTime: time.Unix(1, 0)}}, nil
}

func TestEnsureIndexedRepairsLegacyIncoherentGeneration(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	indexer := testIndexer(t)
	vault := obsidian.VaultDefinition{Path: "/vault"}
	reader := fakeNoteReader{notes: map[string]string{"note.md": "# Transient B\n"}}
	_, err = indexer.EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	// Captured from indexer v11 after its first read saw A and projection saw B.
	state.NotesHash = "2dffbb175d81bc1223de45b757d74c6c5cb75c04fd50469144b851285723e9c2"
	state.RawNotesHash = "110d83f7542c0fad0ec61f8745f45704669e8c253dbba8fe774690ba74d5ffef"
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, MetadataDelta{State: state}))
	reader.notes["note.md"] = "# Stable A\n"

	current, err := indexer.MetadataStateCurrent(ctx, vault, reader, store)
	require.NoError(t, err)
	require.False(t, current)
	result, err := indexer.EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	rows, err := store.CurrentNoteMetadataRows(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "Stable A", rows[0].Title)
	state, err = store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.Equal(t, notesHashFromRows(rows), state.RawNotesHash)
	current, err = indexer.MetadataStateCurrent(ctx, vault, reader, store)
	require.NoError(t, err)
	require.True(t, current)
}

func TestEnsureIndexedNoopDoesNotStatOrProject(t *testing.T) {
	for _, snapshotUnavailable := range []bool{false, true} {
		name := "direct"
		if snapshotUnavailable {
			name = "snapshot_unavailable"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			projector := &projectionOnlyTestProjector{descriptor: noteformat.Descriptor{
				ID: "markdown", Extensions: []string{".md"}, ProviderVersion: "provider-v1", ProjectionVersion: "projection-v1", OwnershipPolicy: noteformat.OwnershipDefault,
			}}
			formats, err := runtimeForProjector(projector)
			require.NoError(t, err)
			indexer, err := NewIndexer(formats)
			require.NoError(t, err)
			reader := &countingNoteReader{fakeNoteReader: fakeNoteReader{notes: map[string]string{
				"a.md": "# A\n", "b.md": "# B\n", "c.md": "# C\n",
			}}}
			var notes obsidian.NoteReader = reader
			if snapshotUnavailable {
				notes = &unavailableSnapshotReader{countingNoteReader: reader}
			}
			vault := obsidian.VaultDefinition{Path: t.TempDir()}
			_, err = indexer.EnsureIndexed(ctx, vault, notes, store)
			require.NoError(t, err)
			before, err := store.GetNoteMetadataState(ctx)
			require.NoError(t, err)
			reader.reset()
			projector.calls.Store(0)

			result, err := indexer.EnsureIndexed(ctx, vault, notes, store)
			require.NoError(t, err)
			require.False(t, result.Dirty)
			require.Equal(t, int64(1), reader.lists.Load())
			require.Equal(t, int64(3), reader.reads.Load())
			require.Zero(t, reader.stats.Load())
			require.Zero(t, projector.calls.Load())
			after, err := store.GetNoteMetadataState(ctx)
			require.NoError(t, err)
			require.Equal(t, before, after)

			reader.reset()
			reader.notes["b.md"] = "# Changed B\n"
			result, err = indexer.EnsureIndexed(ctx, vault, notes, store)
			require.NoError(t, err)
			require.True(t, result.Dirty)
			require.Equal(t, int64(1), reader.lists.Load())
			require.Equal(t, int64(3), reader.reads.Load())
			require.Equal(t, int64(3), reader.stats.Load())
			require.Equal(t, int32(3), projector.calls.Load())
		})
	}
}

type unavailableSnapshotReader struct {
	*countingNoteReader
}

func (*unavailableSnapshotReader) EntriesSnapshot(context.Context) ([]cache.Entry, error) {
	return nil, errors.New("snapshot unavailable")
}

func TestEnsureIndexedCaptureFailuresPreservePublishedState(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	indexer := testIndexer(t)
	vault := obsidian.VaultDefinition{Path: t.TempDir()}
	reader := fakeNoteReader{notes: map[string]string{"note.md": "# Original\n"}}
	_, err = indexer.EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)
	before, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	beforeRows, err := store.CurrentNoteMetadataRows(ctx)
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for _, test := range []struct {
		name   string
		ctx    context.Context
		reader obsidian.NoteReader
		error  string
	}{
		{name: "read", ctx: ctx, reader: fakeNoteReader{notes: reader.notes, errs: map[string]error{"note.md": errors.New("read unavailable")}}, error: "read note note.md: read unavailable"},
		{name: "stat", ctx: ctx, reader: &unavailableStatReader{fakeNoteReader{notes: map[string]string{"note.md": "# Changed\n"}}}, error: "stat note note.md: stat unavailable"},
		{name: "canceled", ctx: canceled, reader: reader, error: "context canceled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := indexer.EnsureIndexed(test.ctx, vault, test.reader, store)
			require.ErrorContains(t, err, test.error)
			require.Nil(t, result)
			after, err := store.GetNoteMetadataState(ctx)
			require.NoError(t, err)
			require.Equal(t, before, after)
			rows, err := store.CurrentNoteMetadataRows(ctx)
			require.NoError(t, err)
			require.Equal(t, beforeRows, rows)
		})
	}
}

type unavailableStatReader struct {
	fakeNoteReader
}

func (*unavailableStatReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, errors.New("stat unavailable")
}
