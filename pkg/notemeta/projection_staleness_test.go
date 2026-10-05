package notemeta

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/stretchr/testify/require"
)

func TestIndexerProjectionRowsStaleRetriesOnlyProjectableOrInvalidSources(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		formatID string
		provider string
		version  string
		want     bool
	}{
		{name: "registered HTML without executable composition", path: "notes/published.html", formatID: "html", provider: "html-provider-v1", version: "html-projection-v2", want: false},
		{name: "projectable Markdown", path: "notes/published.md", formatID: "markdown", want: true},
		{name: "unknown format", path: "notes/published.html", formatID: "unknown", want: true},
		{name: "path mismatched format", path: "notes/published.html", formatID: "markdown", want: true},
		{name: "descriptor version mismatch", path: "notes/published.html", formatID: "html", provider: "html-provider-v0", version: "html-projection-v1", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			indexer, err := NewIndexer(descriptorOnlyHTMLRuntime(t))
			require.NoError(t, err)
			if tc.name == "registered HTML without executable composition" {
				runtime, err := indexer.FormatRuntime()
				require.NoError(t, err)
				provider, ok := runtime.Provider("html")
				require.True(t, ok)
				descriptor := provider.Descriptor()
				tc.provider = descriptor.ProviderVersion
				tc.version = descriptor.ProjectionVersion
			}
			store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "metadata.db"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, semdb.NoteMetadataSnapshot{
				State: semdb.NoteMetadataState{NotesHash: "state", RawNotesHash: "raw", LoadedAt: 1, Ready: true},
				Notes: []semdb.NoteMetadataRow{{
					Path: tc.path, ContentHash: "source-hash", Mtime: 1, Size: 1, FormatID: tc.formatID,
					Projection: semdb.NoteProjectionState{ProviderVersion: tc.provider, ProjectionVersion: tc.version, SourceContentHash: "source-hash", Status: semdb.NoteProjectionStatusStale, UpdatedAt: 1},
				}},
			}))

			stale, err := indexer.projectionRowsStale(ctx, store, []string{tc.path})
			require.NoError(t, err)
			require.Equal(t, tc.want, stale)
		})
	}
}
