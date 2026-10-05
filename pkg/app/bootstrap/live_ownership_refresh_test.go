package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	anchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestLiveOwnershipDeletionClearsIncidentGraphEdges(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	for path, content := range map[string]string{
		"deleted.md": "# Deleted\n[[target]]",
		"source.md":  "# Source\n[[deleted]] and [kept](kept.md)",
		"target.md":  "# Target",
		"kept.md":    "# Kept",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "notes", path), []byte(content), 0o644))
	}
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth})
	now := time.Unix(0, 0)
	w.opts.Now = func() time.Time { return now }
	t.Cleanup(func() { require.NoError(t, store.Close()); require.NoError(t, cacheService.Close()) })
	for _, path := range []string{"notes/deleted.md", "notes/source.md", "notes/target.md", "notes/kept.md"} {
		cacheService.MarkDirty(path, cache.DirtyModified)
	}
	runOwnershipBatch(t, w)
	requireLiveOwnershipCompleted(t, w)
	wikiEdges, err := store.GraphDocEdgesByKind(ctx, anchorsqlite.GraphDocEdgeKindWikilink)
	require.NoError(t, err)
	require.Len(t, wikiEdges, 2)

	now = now.Add(time.Nanosecond)
	require.NoError(t, os.Remove(filepath.Join(root, "notes", "deleted.md")))
	cacheService.MarkDirty("notes/deleted.md", cache.DirtyRemoved)
	runOwnershipBatch(t, w)
	requireLiveOwnershipCompleted(t, w)
	wikiEdges, err = store.GraphDocEdgesByKind(ctx, anchorsqlite.GraphDocEdgeKindWikilink)
	require.NoError(t, err)
	require.Empty(t, wikiEdges)
	mdEdges, err := store.GraphDocEdgesByKind(ctx, anchorsqlite.GraphDocEdgeKindMarkdownLink)
	require.NoError(t, err)
	require.Len(t, mdEdges, 1)
	require.Equal(t, "notes/source.md", mdEdges[0].SrcPath)
	require.Equal(t, "notes/kept.md", mdEdges[0].DstPath)
	_, pending, err := store.PendingOwnershipReconciliation(ctx)
	require.NoError(t, err)
	require.False(t, pending)
}

func TestLiveOwnershipConfigReloadRefreshesMetadata(t *testing.T) {
	root := t.TempDir()
	writeWatcherConfig(t, root, "both")
	writeWatcherSchema(t, root, `type Project @node(paths: ["notes/*.md"]) { name: String! }`)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "target.md"), []byte(`# Target`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "source.md"), []byte("---\ntype: Project\nname: Source\n---\nSee [[target]] and [target](target.md).\n"), 0o644))
	definition, err := obsidian.LoadDefinitionFromPath(root)
	require.NoError(t, err)
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, definition)
	t.Cleanup(func() { require.NoError(t, store.Close()); require.NoError(t, cacheService.Close()) })
	cacheService.MarkDirty("notes/source.md", cache.DirtyModified)
	cacheService.MarkDirty("notes/target.md", cache.DirtyModified)
	runOwnershipBatch(t, w)
	requireLiveOwnershipCompleted(t, w)
	wikiEdges, err := store.GraphDocEdgesByKind(context.Background(), anchorsqlite.GraphDocEdgeKindWikilink)
	require.NoError(t, err)
	require.Len(t, wikiEdges, 1)

	writeWatcherConfig(t, root, "markdown")
	cacheService.MarkDirty(".rhizome/config.yml", cache.DirtyModified)
	runOwnershipBatch(t, w)
	requireLiveOwnershipCompleted(t, w)
	wikiEdges, err = store.GraphDocEdgesByKind(context.Background(), anchorsqlite.GraphDocEdgeKindWikilink)
	require.NoError(t, err)
	require.Empty(t, wikiEdges)
	mdEdges, err := store.GraphDocEdgesByKind(context.Background(), anchorsqlite.GraphDocEdgeKindMarkdownLink)
	require.NoError(t, err)
	require.Len(t, mdEdges, 1)
	typ, ok, err := store.GetOntologyTypeByPath(context.Background(), "notes/source.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "Project", typ.TypeName)
}

func TestLiveOwnershipPublicationRequiresExplicitIndexerBeforeMetadataMutation(t *testing.T) {
	store, err := anchorsqlite.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	w := &unifiedSemanticWatcher{runCtx: context.Background(), intelStore: store}
	err = w.reconcileOwnershipBatch(context.Background(), nil, true, false, 0, time.Now(), 0)
	require.ErrorContains(t, err, "note format runtime is required")
	state, err := store.GetNoteMetadataState(context.Background())
	require.NoError(t, err)
	require.False(t, state.Ready)
	paths, err := store.CurrentNoteMetadataPaths(context.Background())
	require.NoError(t, err)
	require.Empty(t, paths)
}

func TestLiveOwnershipSchemaChangeRefreshesOntology(t *testing.T) {
	root := t.TempDir()
	writeWatcherConfig(t, root, "both")
	writeWatcherSchema(t, root, `type Project @node(paths: ["notes/*.md"]) { name: String! }`)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "project.md"), []byte("---\ntype: Project\nname: Roadmap\n---\n"), 0o644))
	definition, err := obsidian.LoadDefinitionFromPath(root)
	require.NoError(t, err)
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, definition)
	t.Cleanup(func() { require.NoError(t, store.Close()); require.NoError(t, cacheService.Close()) })
	cacheService.MarkDirty("notes/project.md", cache.DirtyModified)
	runOwnershipBatch(t, w)
	requireLiveOwnershipCompleted(t, w)
	typ, ok, err := store.GetOntologyTypeByPath(context.Background(), "notes/project.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "Project", typ.TypeName)

	writeWatcherSchema(t, root, `type Project @node(paths: ["notes/*.md"]) { title: String! }`)
	cacheService.MarkDirty(".rhizome/ontology/schema.graphql", cache.DirtyModified)
	runOwnershipBatch(t, w)
	requireLiveOwnershipCompleted(t, w)
	typ, ok, err = store.GetOntologyTypeByPath(context.Background(), "notes/project.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "Project", typ.TypeName)
	assessment, ok, err := store.GetOntologyAssessmentByPath(context.Background(), "notes/project.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, assessment.AssessmentJSON, "missing_required_field")
}

func requireLiveOwnershipCompleted(t *testing.T, w *unifiedSemanticWatcher) {
	t.Helper()
	current, last := w.health.snapshot()
	require.Nil(t, current)
	require.NotNil(t, last)
	require.Equal(t, "completed", last.Status, "epoch failed: %s", last.Error)
}
