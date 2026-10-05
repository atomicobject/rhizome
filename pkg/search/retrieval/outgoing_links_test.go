package retrieval

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"github.com/stretchr/testify/require"
)

func TestOutgoingLinkRetrieverUsesPersistedCurrentLinkFacts(t *testing.T) {
	store := outgoingLinkTestStore(t, semdb.NoteProjectionStatusCurrent, semdb.NoteProjectionStatusCurrent)
	retriever := OutgoingLinkRetriever{Store: store}

	results, err := retriever.Retrieve(context.Background(), search.QuerySpec{Seeds: []knowledge.Handle{
		knowledge.NoteHandle("notes/Source.MD"),
	}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "notes/Decision.MD", results[0].Path)
	require.Equal(t, "persisted_note_links", results[0].Evidence[0].Source)
}

func TestOutgoingLinkRetrieverOmitsStalePersistedFacts(t *testing.T) {
	store := outgoingLinkTestStore(t, semdb.NoteProjectionStatusCurrent, semdb.NoteProjectionStatusStale)
	retriever := OutgoingLinkRetriever{Store: store}

	results, err := retriever.Retrieve(context.Background(), search.QuerySpec{Seeds: []knowledge.Handle{
		knowledge.NoteHandle("notes/Source.MD"),
	}})
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestOutgoingLinkRetrieverWithoutStoreOmitsLinkEvidence(t *testing.T) {
	retriever := OutgoingLinkRetriever{}
	results, err := retriever.Retrieve(context.Background(), search.QuerySpec{Seeds: []knowledge.Handle{
		knowledge.NoteHandle("notes/source.md"),
	}})
	require.NoError(t, err)
	require.Empty(t, results)
}

func outgoingLinkTestStore(t *testing.T, sourceStatus, targetStatus semdb.NoteProjectionStatus) *semdb.Store {
	t.Helper()
	store, err := semdb.Open(filepath.Join(t.TempDir(), "intel.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	const sourcePath = "notes/Source.MD"
	const targetPath = "notes/Decision.MD"
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(context.Background(), semdb.NoteMetadataSnapshot{
		State: semdb.NoteMetadataState{NotesHash: "notes", RawNotesHash: "raw-notes", LoadedAt: 1, Ready: true},
		Notes: []semdb.NoteMetadataRow{
			outgoingLinkTestRow(sourcePath, "source-hash", sourceStatus),
			outgoingLinkTestRow(targetPath, "target-hash", targetStatus),
		},
		WikilinkEdges: []semdb.GraphDocEdgeRow{{
			SrcPath: sourcePath,
			DstPath: targetPath,
			Kind:    semdb.GraphDocEdgeKindWikilink,
		}},
	}))
	return store
}

func outgoingLinkTestRow(path, hash string, status semdb.NoteProjectionStatus) semdb.NoteMetadataRow {
	projection := semdb.NoteProjectionState{Status: status, UpdatedAt: 1}
	if status == semdb.NoteProjectionStatusCurrent {
		projection.ProviderVersion = "markdown-provider-v1"
		projection.ProjectionVersion = "markdown-projection-v3"
		projection.SourceContentHash = hash
	}
	return semdb.NoteMetadataRow{
		Path:        path,
		ContentHash: hash,
		Mtime:       1,
		Size:        1,
		FormatID:    "markdown",
		Projection:  projection,
	}
}
