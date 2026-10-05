package notemeta

import (
	"context"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestBuildPathDeltaTouchOnlyDirtyPathsProduceSourceTouches(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writePublishedTestFile(t, root, "notes/source.md", "See [[target]].\n")
	writePublishedTestFile(t, root, "notes/target.md", "# Target\n")
	store := openPublishedMetadataStore(t, root)
	indexer, _ := publishedMetadataIndexer(t)
	vault := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}, Links: obsidian.LinkTypeBoth}
	reader := &obsidian.Note{}

	_, err := indexer.EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	edges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, edges)

	sourceMtime := touchPublishedNote(t, root, "notes/source.md", 2*time.Second)
	targetMtime := touchPublishedNote(t, root, "notes/target.md", 2*time.Second)
	dirty, err := indexer.DiscoverDirtyPaths(ctx, vault, reader, store)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/source.md", "notes/target.md"}, dirty.Changed,
		"mtime differences must still be read and re-hashed")

	touchDelta, err := indexer.BuildPathDelta(ctx, vault, reader, store, dirty.Changed, dirty.Deleted)
	require.NoError(t, err)
	require.NotNil(t, touchDelta)
	require.Empty(t, touchDelta.Notes)
	require.Empty(t, touchDelta.WikilinkEdges)
	require.Equal(t, state, touchDelta.State)
	require.ElementsMatch(t, []string{"notes/source.md", "notes/target.md"}, touchedPaths(touchDelta.SourceTouches))
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *touchDelta))

	settled, err := indexer.DiscoverDirtyPaths(ctx, vault, reader, store)
	require.NoError(t, err)
	require.Empty(t, settled.Changed)
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/source.md", "notes/target.md"})
	require.NoError(t, err)
	require.Equal(t, sourceMtime, rows["notes/source.md"].Mtime)
	require.Equal(t, targetMtime, rows["notes/target.md"].Mtime)
	updatedEdges, err := store.GraphDocNoteEdges(ctx)
	require.NoError(t, err)
	require.Equal(t, edges, updatedEdges)

	writePublishedTestFile(t, root, "notes/source.md", "See [[target]] again.\n")
	nextTargetMtime := touchPublishedNote(t, root, "notes/target.md", 2*time.Second)
	dirty, err = indexer.DiscoverDirtyPaths(ctx, vault, reader, store)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/source.md", "notes/target.md"}, dirty.Changed)

	mixed, err := indexer.BuildPathDelta(ctx, vault, reader, store, dirty.Changed, dirty.Deleted)
	require.NoError(t, err)
	require.NotNil(t, mixed)
	require.Len(t, mixed.Notes, 1)
	require.Equal(t, "notes/source.md", mixed.Notes[0].Path)
	require.Equal(t, []string{"notes/target.md"}, touchedPaths(mixed.SourceTouches))
	require.NotEqual(t, state.LoadedAt, mixed.State.LoadedAt)
	require.NotEqual(t, state.RawNotesHash, mixed.State.RawNotesHash)

	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *mixed))
	rows, err = store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/target.md"})
	require.NoError(t, err)
	require.Equal(t, nextTargetMtime, rows["notes/target.md"].Mtime)
	clean, err := indexer.DiscoverDirtyPaths(ctx, vault, reader, store)
	require.NoError(t, err)
	require.Empty(t, clean.Changed)
}
