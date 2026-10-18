package notemeta

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestUnchangedSourceUpgradesMarkdownProjection(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	reader := fakeNoteReader{notes: map[string]string{"note.md": "~~~\n#incorrect\n~~~\n#visible"}}
	vault := obsidian.VaultDefinition{Path: t.TempDir()}
	legacy := legacyMarkdownProjector{}
	registry, err := noteformat.NewRegistry(legacy)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, legacy)
	require.NoError(t, err)
	oldIndexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	_, err = oldIndexer.EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)
	_, err = store.DB().ExecContext(ctx, `INSERT INTO note_tags(note_id,tag_norm) SELECT id,'incorrect' FROM notes WHERE path='note.md'`)
	require.NoError(t, err)
	result, err := testIndexer(t).EnsureIndexed(ctx, vault, reader, store)
	require.NoError(t, err)
	require.True(t, result.Dirty)
	tags, err := store.CurrentNoteTags(ctx, nil)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	require.Equal(t, "visible", tags[0].TagNorm)
}

func TestLoadAliasMapReturnsStoreReadFailure(t *testing.T) {
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "intel.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.Close())
	_, err = LoadAliasMap(context.Background(), store)
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = LoadAliasMap(ctx, store)
	require.Error(t, err)
}

type legacyMarkdownProjector struct{ markdown.Provider }

func (legacyMarkdownProjector) Descriptor() noteformat.Descriptor {
	d := markdown.New().Descriptor()
	d.ProjectionVersion = "markdown-projection-v3"
	return d
}
func (legacyMarkdownProjector) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	p, err := markdown.New().Project(source)
	p.ProjectionVersion = "markdown-projection-v3"
	return p, err
}

func TestProjectNoteSourceRejectsFatalProjection(t *testing.T) {
	projector := indexerTestProjector{descriptor: noteformat.Descriptor{ID: "markdown", Extensions: []string{".md"}, ProviderVersion: "provider-v1", ProjectionVersion: "projection-v1", OwnershipPolicy: noteformat.OwnershipDefault}, fatal: true}
	registry, err := noteformat.NewRegistry(projector)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, projector)
	require.NoError(t, err)
	indexer, err := NewIndexer(runtime)
	require.NoError(t, err)
	_, err = indexer.ProjectNoteSource("note.md", "body", 0)
	require.ErrorIs(t, err, ErrProjectionNotCurrent)
}
