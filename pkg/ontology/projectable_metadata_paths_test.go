package ontology

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/stretchr/testify/require"
)

func TestProjectableMetadataPathsExcludesNonCurrentExecutableRows(t *testing.T) {
	ctx := context.Background()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	markdown, ok := runtime.Provider("markdown")
	require.True(t, ok)
	descriptor := markdown.Descriptor()
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{
			{
				Path: "notes/current.md", ContentHash: "current", FormatID: string(descriptor.ID),
				Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent, SourceContentHash: "current", ProviderVersion: descriptor.ProviderVersion, ProjectionVersion: descriptor.ProjectionVersion},
			},
			{
				Path: "notes/fatal.md", FormatID: string(descriptor.ID),
				Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusFatal, ProviderVersion: descriptor.ProviderVersion, ProjectionVersion: descriptor.ProjectionVersion, DiagnosticCode: "projector_fatal", DiagnosticDetail: "source rejected"},
			},
			{
				Path: "notes/stale.md", ContentHash: "stale", FormatID: string(descriptor.ID),
				Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusStale, SourceContentHash: "stale", ProviderVersion: descriptor.ProviderVersion, ProjectionVersion: descriptor.ProjectionVersion},
			},
		},
	}))

	projectable, err := ProjectableMetadataPaths(ctx, indexer, store)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/current.md"}, projectable)
}
