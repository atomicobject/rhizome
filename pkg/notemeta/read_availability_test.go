package notemeta

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/stretchr/testify/require"
)

func TestUsableNoteMetadataRowsRequireCurrentProviderAndOwnership(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "metadata.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	provider, ok := runtime.Provider(noteformat.FormatID("markdown"))
	require.True(t, ok)
	descriptor := provider.Descriptor()

	currentRow := func(path string) semdb.NoteMetadataRow {
		return semdb.NoteMetadataRow{
			Path: path, Title: path, ContentHash: path + "-hash", Mtime: 1, Size: 10, FormatID: string(descriptor.ID),
			Projection: semdb.NoteProjectionState{
				ProviderVersion: descriptor.ProviderVersion, ProjectionVersion: descriptor.ProjectionVersion,
				SourceContentHash: path + "-hash", Status: semdb.NoteProjectionStatusCurrent, UpdatedAt: 1,
			},
		}
	}
	staleProvider := currentRow("notes/old-provider.md")
	staleProvider.Projection.ProviderVersion = "superseded-provider"
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "initial", RawNotesHash: "raw-initial", LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{
			currentRow("notes/retained.md"),
			currentRow("notes/no-longer-owned.md"),
			staleProvider,
		},
		PropertyValues: []semdb.NotePropertyValueRow{
			{NotePath: "notes/retained.md", PropertyName: "summary", Source: semdb.NotePropertySourceFrontmatter, ValueText: "retained"},
			{NotePath: "notes/no-longer-owned.md", PropertyName: "summary", Source: semdb.NotePropertySourceFrontmatter, ValueText: "unowned"},
			{NotePath: "notes/old-provider.md", PropertyName: "summary", Source: semdb.NotePropertySourceFrontmatter, ValueText: "stale"},
		},
		Tags: []semdb.NoteTagRow{
			{NotePath: "notes/retained.md", TagNorm: "retained"},
			{NotePath: "notes/no-longer-owned.md", TagNorm: "unowned"},
			{NotePath: "notes/old-provider.md", TagNorm: "stale"},
		},
	}))

	currentOnly := struct{ Store }{Store: store}
	currentRows, err := indexer.UsableNoteMetadataRowsByPaths(ctx, currentOnly, []string{"notes/retained.md", "notes/old-provider.md"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"notes/retained.md"}, metadataRowPaths(currentRows))

	_, err = store.ApplyOwnershipTransitions(ctx, []semdb.OwnershipTransition{{Path: "notes/unrelated.md", Target: semdb.OwnershipTargetUnowned}})
	require.NoError(t, err)
	state, err := store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.False(t, state.Ready)

	paths := []string{"notes/retained.md", "notes/no-longer-owned.md", "notes/old-provider.md"}
	rows, err := indexer.UsableNoteMetadataRowsByPaths(ctx, store, paths)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"notes/retained.md", "notes/no-longer-owned.md"}, metadataRowPaths(rows))
	facts, err := indexer.UsableNoteFactsByPaths(ctx, store, paths)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"notes/retained.md", "notes/no-longer-owned.md"}, metadataRowPaths(facts.MetadataRows))
	require.ElementsMatch(t, []string{"notes/retained.md", "notes/no-longer-owned.md"}, propertyRowPaths(facts.PropertyValues))
	require.ElementsMatch(t, []string{"notes/retained.md", "notes/no-longer-owned.md"}, tagRowPaths(facts.Tags))
	count, err := indexer.UsableNoteMetadataCount(ctx, store, func(path string) bool {
		return path != "notes/no-longer-owned.md"
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	globallyCurrent, err := store.CurrentNoteMetadataRowsByPaths(ctx, paths)
	require.NoError(t, err)
	require.Empty(t, globallyCurrent)
}

func propertyRowPaths(rows []semdb.NotePropertyValueRow) []string {
	paths := make([]string, 0, len(rows))
	for _, row := range rows {
		paths = append(paths, row.NotePath)
	}
	return paths
}

func tagRowPaths(rows []semdb.NoteTagRow) []string {
	paths := make([]string, 0, len(rows))
	for _, row := range rows {
		paths = append(paths, row.NotePath)
	}
	return paths
}

func metadataRowPaths(rows map[string]MetadataRow) []string {
	paths := make([]string, 0, len(rows))
	for path := range rows {
		paths = append(paths, path)
	}
	return paths
}
