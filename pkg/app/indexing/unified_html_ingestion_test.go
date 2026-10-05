package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/html"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunUnifiedCore_ProjectableHTMLPublishesWithoutMarkdownStructure(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "reports"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "Guide.md"), []byte("# Guide\n\nmarkdown lifecycle marker\n"), 0o600))
	htmlPath := filepath.Join(root, "reports", "Prototype.html")
	require.NoError(t, os.WriteFile(htmlPath, []byte(`<!doctype html><html><head><title>Prototype</title></head><body><main id="results">htmlvisiblemarker</main><script>const supplementalLifecycleMarker = true</script></body></html>`), 0o600))

	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md", "reports/*.html"}}
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{
		Notes:          obsidian.LocalVaultConfig{Includes: definition.Includes},
		NoteEmbeddings: &embeddings.Config{Enabled: false}, CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
	indexer := projectableHTMLIndexer(t)
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))

	store, cleanup, err := obsidian.OpenIntelStore(root, false)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	assertCurrentHTMLProjection(t, ctx, store, "reports/Prototype.html")
	sections, err := store.IntelDocSectionsByPath(ctx, "reports/Prototype.html")
	require.NoError(t, err)
	require.Empty(t, sections, "root-only HTML must not enter Markdown section ingestion")
	visible, err := store.SearchIntelFTS(ctx, "htmlvisiblemarker", 10)
	require.NoError(t, err)
	require.Len(t, visible, 1)
	require.Equal(t, "note_region_visible", visible[0].Type)
	supplemental, err := store.SearchIntelFTS(ctx, "supplementalLifecycleMarker", 10)
	require.NoError(t, err)
	require.Len(t, supplemental, 1)
	require.Equal(t, "note_region_supplemental", supplemental[0].Type)

	require.NoError(t, os.WriteFile(htmlPath, []byte(`<!doctype html><title>Prototype v2</title><p>htmlreplacementmarker</p>`), 0o600))
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	rows, err := store.SearchIntelFTS(ctx, "htmlvisiblemarker", 10)
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = store.SearchIntelFTS(ctx, "htmlreplacementmarker", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	require.NoError(t, os.WriteFile(htmlPath, []byte{0xff, 0xfe, 0xfd}, 0o600))
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	metadata, err := store.DurableNoteMetadataRowsByPaths(ctx, []string{"reports/Prototype.html"})
	require.NoError(t, err)
	require.Equal(t, semdb.NoteProjectionStatusFatal, metadata["reports/Prototype.html"].Projection.Status)
	rows, err = store.SearchIntelFTS(ctx, "htmlreplacementmarker", 10)
	require.NoError(t, err)
	require.Empty(t, rows)

	require.NoError(t, os.WriteFile(htmlPath, []byte(`<!doctype html><title>Recovered</title><p>htmlrecoveredmarker</p>`), 0o600))
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	assertCurrentHTMLProjection(t, ctx, store, "reports/Prototype.html")
	rows, err = store.SearchIntelFTS(ctx, "htmlrecoveredmarker", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	require.NoError(t, os.Remove(htmlPath))
	require.NoError(t, RunUnifiedCore(ctx, UnifiedOptions{VaultPath: root, VaultDef: definition, NoteMetadata: indexer}))
	rows, err = store.SearchIntelFTS(ctx, "htmlrecoveredmarker", 10)
	require.NoError(t, err)
	require.Empty(t, rows)
	metadata, err = store.DurableNoteMetadataRowsByPaths(ctx, []string{"reports/Prototype.html"})
	require.NoError(t, err)
	require.Empty(t, metadata)
}

func projectableHTMLIndexer(t *testing.T) notemeta.Indexer {
	t.Helper()
	registry, err := noteformat.NewRegistry(markdown.New(), html.New())
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, markdown.New(), html.New())
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}

func assertCurrentHTMLProjection(t *testing.T, ctx context.Context, store *semdb.Store, path string) {
	t.Helper()
	rows, err := store.DurableNoteMetadataRowsByPaths(ctx, []string{path})
	require.NoError(t, err)
	row, ok := rows[path]
	require.True(t, ok)
	require.Equal(t, "html", row.FormatID)
	require.Equal(t, semdb.NoteProjectionStatusCurrent, row.Projection.Status)
}
