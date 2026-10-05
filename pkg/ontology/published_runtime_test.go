package ontology

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestPublishedRuntimeWithStore_UsesCurrentPublishedState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/one.md", `---
type: Project
name: One
---
`)
	store, err := codeanchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Path: root}

	_, err = EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	runtime, err := PublishedRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, store)
	require.NoError(t, err)
	require.Same(t, store, runtime.Store)
	require.True(t, runtime.Ready)
	require.NotNil(t, runtime.Schema)
}

func TestPublishedRuntimeWithStore_LeavesDescriptorOnlyStaleRowsUntouched(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}

`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	store, err := codeanchorsqlite.Open(filepath.Join(root, "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, codeanchorsqlite.NoteMetadataSnapshot{
		State: codeanchorsqlite.NoteMetadataState{NotesHash: "published-notes", LoadedAt: 17, Ready: true},
		Notes: []codeanchorsqlite.NoteMetadataRow{{
			Path:        "notes/stale.html",
			ContentHash: "stale-html",
			FormatID:    "html",
			Projection:  codeanchorsqlite.NoteProjectionState{Status: codeanchorsqlite.NoteProjectionStatusStale},
		}},
	}))
	wantState := codeanchorsqlite.OntologySchemaState{
		SchemaHash:             schema.Hash,
		NotesHash:              "published-notes",
		MaterializationVersion: OntologyMaterializationVersion,
		LoadedAt:               17,
		Ready:                  true,
	}
	require.NoError(t, store.UpsertOntologySchemaState(ctx, wantState))

	runtime, err := PublishedRuntimeWithStore(ctx, descriptorOnlyHTMLNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, store)
	require.NoError(t, err)
	require.True(t, runtime.Ready)
	require.NotNil(t, runtime.Schema)
	require.Equal(t, schema.Hash, runtime.Schema.Hash)

	paths, err := store.AllNoteMetadataPaths(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/stale.html"}, paths)
	state, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Equal(t, wantState, state)
}

func TestSyncPublishedPaths_MixedMarkdownAndDescriptorOnlyHTMLStaysReadyAfterSecondRun(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	markdownContent := `---
name: Project
---
`
	htmlContent := "<h1>Trusted guide</h1>\n"
	writeOntologyNote(t, root, "notes/project.md", markdownContent)
	writeOntologyNote(t, root, "docs/guide.html", htmlContent)

	store, err := codeanchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md", "docs/*.html"}}
	indexer := descriptorOnlyHTMLNoteMetadataIndexer(t)
	formats, err := indexer.FormatRuntime()
	require.NoError(t, err)
	markdownSource, markdownDescriptor := publishedSourceForTest(t, root, "notes/project.md", markdownContent, formats)
	htmlSource, htmlDescriptor := publishedSourceForTest(t, root, "docs/guide.html", htmlContent, formats)
	require.NoError(t, store.ReplaceNoteMetadataSnapshot(ctx, codeanchorsqlite.NoteMetadataSnapshot{
		State: codeanchorsqlite.NoteMetadataState{NotesHash: "published-notes", LoadedAt: 17, Ready: true},
		Notes: []codeanchorsqlite.NoteMetadataRow{
			{
				Path:        "notes/project.md",
				ContentHash: markdownSource.ContentHash(),
				Mtime:       markdownSource.Mtime(),
				Size:        int64(len(markdownContent)),
				FormatID:    string(markdownSource.Format()),
				Projection: codeanchorsqlite.NoteProjectionState{
					ProviderVersion:   markdownDescriptor.ProviderVersion,
					ProjectionVersion: markdownDescriptor.ProjectionVersion,
					SourceContentHash: markdownSource.ContentHash(),
					Status:            codeanchorsqlite.NoteProjectionStatusCurrent,
				},
			},
			{
				Path:        "docs/guide.html",
				ContentHash: htmlSource.ContentHash(),
				Mtime:       htmlSource.Mtime(),
				Size:        int64(len(htmlContent)),
				FormatID:    string(htmlSource.Format()),
				Projection: codeanchorsqlite.NoteProjectionState{
					ProviderVersion:   htmlDescriptor.ProviderVersion,
					ProjectionVersion: htmlDescriptor.ProjectionVersion,
					SourceContentHash: htmlSource.ContentHash(),
					Status:            codeanchorsqlite.NoteProjectionStatusStale,
				},
			},
		},
	}))

	first, err := SyncPublishedPaths(ctx, indexer, vaultDef, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, first.Rebuilt)
	firstRuntime, err := PublishedRuntimeWithStore(ctx, indexer, vaultDef, store)
	require.NoError(t, err)
	require.True(t, firstRuntime.Ready)
	firstState, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)

	second, err := SyncPublishedPaths(ctx, indexer, vaultDef, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	require.False(t, second.Rebuilt, "descriptor-only HTML must not make ontology readiness incomplete")
	require.False(t, second.Dirty)
	secondRuntime, err := PublishedRuntimeWithStore(ctx, indexer, vaultDef, store)
	require.NoError(t, err)
	require.True(t, secondRuntime.Ready)
	secondState, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Equal(t, firstState, secondState)

	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, []string{"docs/guide.html"})
	require.NoError(t, err)
	require.Equal(t, codeanchorsqlite.NoteProjectionStatusStale, rows["docs/guide.html"].Projection.Status)
	states, err := store.OntologyNoteStatesByPaths(ctx, []string{"docs/guide.html"})
	require.NoError(t, err)
	require.Empty(t, states, "descriptor-only HTML remains durable source identity without ontology state")
}

func publishedSourceForTest(t *testing.T, root, notePath, content string, formats noteformat.Runtime) (noteformat.AuthoredSource, noteformat.Descriptor) {
	t.Helper()
	provider, ok := formats.ProviderForPath(paths.RelPath(notePath))
	require.True(t, ok)
	info, err := os.Stat(filepath.Join(root, notePath))
	require.NoError(t, err)
	source, err := noteformat.NewAuthoredSource(paths.NotePath(notePath), provider.Descriptor(), []byte(content), info.ModTime().Unix())
	require.NoError(t, err)
	return source, provider.Descriptor()
}

func TestPublishedRuntimeWithStore_RejectsMismatchedPublishedState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Project @node(paths: ["notes/*.md"]) {
  name: String!
}
`)
	writeOntologyNote(t, root, "notes/one.md", `---
type: Project
name: One
---
`)
	store, err := codeanchorsqlite.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Path: root}
	_, err = EnsureIndexed(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)

	state, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	state.NotesHash = "stale-notes"
	require.NoError(t, store.UpsertOntologySchemaState(ctx, state))

	runtime, err := PublishedRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, store)
	require.NoError(t, err)
	require.False(t, runtime.Ready)
	require.Nil(t, runtime.Schema)
}

func TestPublishedRuntimeWithStore_PreservesSchemaStatus(t *testing.T) {
	t.Run("missing schema disables the runtime", func(t *testing.T) {
		store, err := codeanchorsqlite.Open(filepath.Join(t.TempDir(), "db.sqlite"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })

		runtime, err := PublishedRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: t.TempDir()}, store)
		require.NoError(t, err)
		require.Same(t, store, runtime.Store)
		require.False(t, runtime.Ready)
		require.Nil(t, runtime.Schema)
	})

	t.Run("invalid schema remains an error", func(t *testing.T) {
		root := t.TempDir()
		writeOntologySchema(t, root, "type Project {")
		store, err := codeanchorsqlite.Open(filepath.Join(root, "db.sqlite"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })

		runtime, err := PublishedRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root}, store)
		require.Error(t, err)
		require.Same(t, store, runtime.Store)
		require.False(t, runtime.Ready)
		require.Nil(t, runtime.Schema)
	})
}
