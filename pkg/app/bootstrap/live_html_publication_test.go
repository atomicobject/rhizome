package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	anchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/html"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestLiveOwnership_ProjectableHTMLPublicationConvergesLexicalEvidence(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "reports"), 0o755))
	path := filepath.Join(root, "reports", "status.html")
	require.NoError(t, os.WriteFile(path, []byte("<!doctype html><title>Status</title><p>livehtmlmarker</p>"), 0o644))
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"reports/*.html"}}
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, definition)
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(func() { _ = cacheService.Close() })
	runtime := liveProjectableHTMLRuntime(t)
	w.noteRuntime = runtime
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	w.noteMetadataIndexer = indexer
	now := time.Now()
	w.opts.Now = func() time.Time { return now }

	cacheService.MarkDirty("reports/status.html", cache.DirtyModified)
	runOwnershipBatch(t, w)
	assertLiveHTMLCurrent(t, store, "reports/status.html")
	rows, err := store.SearchIntelFTS(context.Background(), "livehtmlmarker", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "note_region_visible", rows[0].Type)

	require.NoError(t, os.WriteFile(path, []byte("<!doctype html><title>Status</title><p>livehtmlreplacement</p>"), 0o644))
	now = now.Add(time.Minute)
	cacheService.MarkDirty("reports/status.html", cache.DirtyModified)
	runOwnershipBatch(t, w)
	rows, err = store.SearchIntelFTS(context.Background(), "livehtmlmarker", 10)
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = store.SearchIntelFTS(context.Background(), "livehtmlreplacement", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	require.NoError(t, os.Remove(path))
	now = now.Add(time.Minute)
	cacheService.MarkDirty("reports/status.html", cache.DirtyRemoved)
	runOwnershipBatch(t, w)
	rows, err = store.SearchIntelFTS(context.Background(), "livehtmlreplacement", 10)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestLiveCacheSelectionIncludesProjectableHTML(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "reports"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "report.md"), []byte("# Report\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "reports", "status.html"), []byte("<!doctype html><title>Status</title><p>Current</p>"), 0o644))
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"**/*.md", "**/*.html"}}
	runtime := liveProjectableHTMLRuntime(t)
	provider, ok := runtime.ProviderForPath("reports/status.html")
	require.True(t, ok)
	source, err := noteformat.NewAuthoredSource("reports/status.html", provider.Descriptor(), []byte("<!doctype html><title>Status</title><p>Current</p>"), 1)
	require.NoError(t, err)
	projection, err := runtime.Project(source)
	require.NoError(t, err)
	require.Equal(t, noteformat.ProjectionStatusCurrent, projection.Status)
	policy, err := liveCacheSelectionPolicy(definition, runtime, testCodeConfig(root))
	require.NoError(t, err)
	require.True(t, policy.Admit("reports/status.html"))
	discovered, err := policy.DiscoverFiles()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"report.md", "reports/status.html"}, discovered)
	cacheService, err := cache.NewService(root, cache.Options{
		DiscoverFiles: policy.DiscoverFiles,
		AdmitNote:     policy.Admit,
		UserExcludes:  policy.UserExcludes,
		NoteRuntime:   &runtime,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cacheService.Close()) })
	require.NoError(t, cacheService.EnsureReady(context.Background()))
	require.ElementsMatch(t, []string{"report.md", "reports/status.html"}, cacheService.Paths())
}

func liveProjectableHTMLRuntime(t *testing.T) noteformat.Runtime {
	t.Helper()
	registry, err := noteformat.NewRegistry(markdown.New(), html.New())
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, markdown.New(), html.New())
	require.NoError(t, err)
	return runtime
}

func assertLiveHTMLCurrent(t *testing.T, store *anchorsqlite.Store, path string) {
	t.Helper()
	rows, err := store.DurableNoteMetadataRowsByPaths(context.Background(), []string{path})
	require.NoError(t, err)
	row, ok := rows[path]
	require.True(t, ok)
	require.Equal(t, "html", row.FormatID)
	require.Equal(t, anchorsqlite.NoteProjectionStatusCurrent, row.Projection.Status)
}
