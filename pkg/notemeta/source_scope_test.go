package notemeta

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type sourceScopeReader struct {
	*obsidian.Note
	reads atomic.Int64
}

func (r *sourceScopeReader) GetContents(vault obsidian.VaultDefinition, path string) (string, error) {
	r.reads.Add(1)
	return r.Note.GetContents(vault, path)
}

func TestSourceScopeReusesCaptureAndDoesNotCrossOperations(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# First\n"), 0o644))
	store, err := sqlitefixture.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	provider := &projectionOnlyTestProjector{descriptor: noteformat.Descriptor{ID: "markdown", Extensions: []string{".md"}, ProviderVersion: "v1", ProjectionVersion: "v1", OwnershipPolicy: noteformat.OwnershipDefault}}
	formats, err := runtimeForProjector(provider)
	require.NoError(t, err)
	indexer, err := NewIndexer(formats)
	require.NoError(t, err)
	vault := obsidian.VaultDefinition{Path: root}
	reader := &sourceScopeReader{Note: &obsidian.Note{}}
	for _, text := range []string{"# First\n", "# Second\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte(text), 0o644))
		operation := indexer.WithSourceScope()
		provider.calls.Store(0)
		reader.reads.Store(0)
		_, err = operation.EnsureIndexed(t.Context(), vault, reader, store)
		require.NoError(t, err)
		sources, err := operation.LoadNoteSourceSnapshots(t.Context(), vault, reader, store, []string{"note.md"})
		require.NoError(t, err)
		require.Len(t, sources, 1)
		require.Equal(t, text, sources[0].Content)
		require.EqualValues(t, 1, reader.reads.Load())
		require.EqualValues(t, 1, provider.calls.Load())
		sources[0].Projection.Facts.Title.Value = "mutated result"
		again, err := operation.LoadNoteSourceSnapshots(t.Context(), vault, reader, store, []string{"note.md"})
		require.NoError(t, err)
		require.Equal(t, "provider title", again[0].Projection.Facts.Title.Value)
	}
}
