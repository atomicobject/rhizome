package notemeta

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestBuildPathDeltaDoesNotWriteBeforePublication(t *testing.T) {
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	vault := obsidian.VaultDefinition{Path: root}
	initial := fakeNoteReader{notes: map[string]string{"note.md": "---\nstatus: old\n---\n"}}
	_, err = testIndexer(t).EnsureIndexed(context.Background(), vault, initial, store)
	require.NoError(t, err)

	updated := fakeNoteReader{notes: map[string]string{"note.md": "---\nstatus: new\n---\n"}}
	delta, err := testIndexer(t).BuildPathDelta(context.Background(), vault, updated, store, []string{"note.md"}, nil)
	require.NoError(t, err)
	require.NotNil(t, delta)

	oldRows, err := store.CurrentNotePathsByPropertyValue(context.Background(), "status", "old", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"note.md"}, oldRows)
	newRows, err := store.CurrentNotePathsByPropertyValue(context.Background(), "status", "new", 0)
	require.NoError(t, err)
	require.Empty(t, newRows)

	require.NoError(t, store.ApplyNoteMetadataDelta(context.Background(), *delta))
	newRows, err = store.CurrentNotePathsByPropertyValue(context.Background(), "status", "new", 0)
	require.NoError(t, err)
	require.Equal(t, []string{"note.md"}, newRows)
}

func TestBuildPathDeltaBootstrapsUnreadyStore(t *testing.T) {
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	vault := obsidian.VaultDefinition{Path: root}
	reader := fakeNoteReader{notes: map[string]string{
		"a.md": "---\nstatus: one\n---\n",
		"b.md": "---\nstatus: two\n---\n",
	}}

	delta, err := testIndexer(t).BuildPathDelta(context.Background(), vault, reader, store, []string{"a.md"}, nil)
	require.NoError(t, err)
	require.Len(t, delta.Notes, 2, "an unready store requires a complete bootstrap delta")
	require.NoError(t, store.ApplyNoteMetadataDelta(context.Background(), *delta))
	paths, err := store.CurrentNoteMetadataPaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"a.md", "b.md"}, paths)
}

func TestBuildPathDeltaBootstrapsStaleDerivationState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	vault := obsidian.VaultDefinition{Path: root}
	reader := fakeNoteReader{notes: map[string]string{
		"changed.md":   "# Changed\n",
		"untouched.md": "# Untouched\n",
	}}
	_, err = testIndexer(t).EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, semdb.NoteMetadataDelta{State: semdb.NoteMetadataState{
		NotesHash:    "legacy-derivation",
		RawNotesHash: state.RawNotesHash,
		LoadedAt:     nextLoadedAt(state.LoadedAt),
		Ready:        true,
	}}))

	delta, err := testIndexer(t).BuildPathDelta(ctx, vault, reader, store, []string{"changed.md"}, nil)
	require.NoError(t, err)
	require.Len(t, delta.Notes, 2, "stale derivation requires all notes to receive current rows")
}

func TestDiscoverDirtyPathsFindsChangesAndDeletes(t *testing.T) {
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	vault := obsidian.VaultDefinition{Path: root}
	initial := fakeNoteReader{notes: map[string]string{
		"change.md": "old",
		"delete.md": "delete",
		"keep.md":   "keep",
	}}
	_, err = testIndexer(t).EnsureIndexed(context.Background(), vault, initial, store)
	require.NoError(t, err)

	clean, err := testIndexer(t).DiscoverDirtyPaths(context.Background(), vault, initial, store)
	require.NoError(t, err)
	require.Empty(t, clean.Changed)
	require.Empty(t, clean.Deleted)
	require.Equal(t, 3, clean.Scanned)

	current := fakeNoteReader{notes: map[string]string{
		"change.md": "new",
		"keep.md":   "keep",
		"new.md":    "new note",
	}}
	dirty, err := testIndexer(t).DiscoverDirtyPaths(context.Background(), vault, current, store)
	require.NoError(t, err)
	require.Equal(t, []string{"change.md", "new.md"}, dirty.Changed)
	require.Equal(t, []string{"delete.md"}, dirty.Deleted)
	require.Equal(t, 3, dirty.Scanned)
}

func TestDiscoverDirtyPathsDoesNotRequireProjector(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitefixture.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	vault := obsidian.VaultDefinition{Path: root}
	reader := fakeNoteReader{notes: map[string]string{"note.md": "# Note\n"}}
	_, err = testIndexer(t).EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)

	runtimeV1 := providerFreshnessTestRuntime(t, "provider-v1", "projection-v1")
	indexerV1, err := NewIndexer(runtimeV1)
	require.NoError(t, err)
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	v1StateHash, err := metadataStateHashWithSelectedProviders(vault, state.RawNotesHash, runtimeV1, []string{"note.md"})
	require.NoError(t, err)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, semdb.NoteMetadataDelta{State: semdb.NoteMetadataState{
		NotesHash:    v1StateHash,
		RawNotesHash: state.RawNotesHash,
		LoadedAt:     nextLoadedAt(state.LoadedAt),
		Ready:        true,
	}}))

	clean, err := indexerV1.DiscoverDirtyPaths(ctx, vault, reader, store)
	require.NoError(t, err)
	require.Empty(t, clean.Changed)
	require.Empty(t, clean.Deleted)
	require.Equal(t, state.RawNotesHash, mustCurrentRawNotesHash(t, ctx, store))
	current, _, err := metadataStateCurrentWithSelectedProviders(ctx, vault, reader, store, runtimeV1)
	require.NoError(t, err)
	require.True(t, current)

	runtimeV2 := providerFreshnessTestRuntime(t, "provider-v2", "projection-v1")
	indexerV2, err := NewIndexer(runtimeV2)
	require.NoError(t, err)
	dirty, err := indexerV2.DiscoverDirtyPaths(ctx, vault, reader, store)
	require.NoError(t, err)
	require.Equal(t, []string{"note.md"}, dirty.Changed)
	require.Empty(t, dirty.Deleted)
	current, _, err = metadataStateCurrentWithSelectedProviders(ctx, vault, reader, store, runtimeV2)
	require.NoError(t, err)
	require.False(t, current)

	state, err = store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	v2StateHash, err := metadataStateHashWithSelectedProviders(vault, state.RawNotesHash, runtimeV2, []string{"note.md"})
	require.NoError(t, err)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, semdb.NoteMetadataDelta{State: semdb.NoteMetadataState{
		NotesHash:    v2StateHash,
		RawNotesHash: state.RawNotesHash,
		LoadedAt:     nextLoadedAt(state.LoadedAt),
		Ready:        true,
	}}))

	clean, err = indexerV2.DiscoverDirtyPaths(ctx, vault, reader, store)
	require.NoError(t, err)
	require.Empty(t, clean.Changed)
	require.Empty(t, clean.Deleted)
	require.Equal(t, state.RawNotesHash, mustCurrentRawNotesHash(t, ctx, store))
	current, _, err = metadataStateCurrentWithSelectedProviders(ctx, vault, reader, store, runtimeV2)
	require.NoError(t, err)
	require.True(t, current)
}

type providerFreshnessTestProvider struct {
	descriptor noteformat.Descriptor
}

func (p providerFreshnessTestProvider) Descriptor() noteformat.Descriptor {
	return p.descriptor
}

func providerFreshnessTestRuntime(t *testing.T, providerVersion, projectionVersion string) noteformat.Runtime {
	t.Helper()
	registry, err := noteformat.NewRegistry(providerFreshnessTestProvider{descriptor: noteformat.Descriptor{
		ID:                "markdown",
		Extensions:        []string{".md"},
		ProviderVersion:   providerVersion,
		ProjectionVersion: projectionVersion,
		OwnershipPolicy:   noteformat.OwnershipDefault,
	}})
	require.NoError(t, err)
	formats, err := noteformat.NewRuntime(registry)
	require.NoError(t, err)
	return formats
}

func mustCurrentRawNotesHash(t *testing.T, ctx context.Context, store Store) string {
	t.Helper()
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	return state.RawNotesHash
}
