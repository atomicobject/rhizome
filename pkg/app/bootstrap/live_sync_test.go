package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/testutil/indexingworkload"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestSyncWatcherPublishesWhileEmbeddingsAreBlocked(t *testing.T) {
	rt, w, provider := liveWorkloadWatcher(t)
	rt.liveWatcher.Store(w)
	provider.block.Store(true)
	t.Cleanup(func() {
		provider.block.Store(false)
		close(provider.release)
	})
	path := "notes/project-0000.md"
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, path), []byte(indexingworkload.Note(0, 16, "First")), 0o600))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, rt.SyncWatcher(ctx))
	select {
	case <-provider.entered:
	case <-ctx.Done():
		t.Fatal("embedding call never started")
	}
	before, err := rt.IntelStore().CurrentNoteMetadataRowsByPaths(ctx, []string{path})
	require.NoError(t, err)
	// A second disk edit has no watcher event and arrives while derived work
	// remains blocked. The public synchronization path must still publish it.
	require.NoError(t, os.WriteFile(filepath.Join(rt.VaultPath, path), []byte(indexingworkload.Note(0, 16, "Second")), 0o600))
	require.NoError(t, rt.SyncWatcher(ctx))
	after, err := rt.IntelStore().CurrentNoteMetadataRowsByPaths(ctx, []string{path})
	require.NoError(t, err)
	require.NotEqual(t, before[path].ContentHash, after[path].ContentHash)
}

// The watcher in this test never receives filesystem events, as when a write's
// event is still in flight. SyncWatcher must apply the change anyway.
func TestSyncWatcherAppliesChangesWhoseEventsHaveNotArrived(t *testing.T) {
	root := t.TempDir()
	writeWatcherSchema(t, root, "type Task @node(paths: [\"notes/*.md\"]) {\n  status: String\n}\n")
	note := func(name, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(root, "notes", name), []byte(body), 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	note("a.md", "---\nstatus: open\n---\n# A\n")
	note("b.md", "---\nstatus: open\n---\n# B\n")
	definition := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md"}}
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, definition)
	defer store.Close()
	defer cacheService.Close()
	rt := &LiveRuntime{VaultDef: definition, VaultPath: root, noteFormats: testNoteRuntime(t)}
	codeCfg := testCodeConfig(root)
	rt.codeCfg.Store(&codeCfg)
	rt.intelStore.Store(store)
	rt.liveWatcher.Store(w)
	ctx := context.Background()
	hashes := func() map[string]string {
		rows, err := store.CurrentNoteMetadataRows(ctx)
		require.NoError(t, err)
		out := map[string]string{}
		for _, row := range rows {
			out[row.Path] = row.ContentHash
		}
		return out
	}

	require.NoError(t, rt.SyncWatcher(ctx))
	initial := hashes()
	require.Len(t, initial, 2)
	changes, waiting, err := rt.sourceChanges(ctx, w)
	require.NoError(t, err)
	require.Empty(t, changes, "a current projection has nothing to apply")
	require.False(t, waiting)

	// Same size, written within the same second as the indexed version.
	note("a.md", "---\nstatus: done\n---\n# A\n")
	require.NoError(t, rt.SyncWatcher(ctx))
	require.NotEqual(t, initial["notes/a.md"], hashes()["notes/a.md"])

	// A rewrite that keeps the size and the stored modification second is
	// caught however old that second is.
	hour := time.Now().Add(-time.Hour).Truncate(time.Second)
	aPath := filepath.Join(root, "notes", "a.md")
	require.NoError(t, os.Chtimes(aPath, hour.Add(100*time.Millisecond), hour.Add(100*time.Millisecond)))
	require.NoError(t, rt.SyncWatcher(ctx))
	note("a.md", "---\nstatus: open\n---\n# A\n")
	require.NoError(t, os.Chtimes(aPath, hour.Add(900*time.Millisecond), hour.Add(900*time.Millisecond)))
	require.NoError(t, rt.SyncWatcher(ctx))
	require.Equal(t, initial["notes/a.md"], hashes()["notes/a.md"])

	// A rewrite that restores the exact timestamp is caught once its event
	// reaches the watcher.
	restored := hour.Add(900 * time.Millisecond)
	note("a.md", "---\nstatus: wait\n---\n# A\n")
	require.NoError(t, os.Chtimes(aPath, restored, restored))
	w.retainWatchEvents(map[string]cache.DirtyKind{"notes/a.md": cache.DirtyModified}, false)
	require.NoError(t, rt.SyncWatcher(ctx))
	require.NotEqual(t, initial["notes/a.md"], hashes()["notes/a.md"])

	note("c.md", "# C\n")
	require.NoError(t, os.Remove(filepath.Join(root, "notes", "b.md")))
	require.NoError(t, rt.SyncWatcher(ctx))
	current := hashes()
	require.Contains(t, current, "notes/c.md")
	require.NotContains(t, current, "notes/b.md")
	changes, waiting, err = rt.sourceChanges(ctx, w)
	require.NoError(t, err)
	require.Empty(t, changes)
	require.False(t, waiting)

	// A late event for a change already applied needs no batch; a
	// configuration-class event and a stored note that selection no longer
	// returns do.
	w.retainWatchEvents(map[string]cache.DirtyKind{"notes/a.md": cache.DirtyModified}, false)
	changes, waiting, err = rt.sourceChanges(ctx, w)
	require.NoError(t, err)
	require.Empty(t, changes)
	require.False(t, waiting)
	// That event costs one rehash; while it waits, later reads trust the stat.
	info, err := os.Stat(aPath)
	require.NoError(t, err)
	require.True(t, rt.sourceVerified("notes/a.md", info, current["notes/a.md"], w.events("notes/a.md")))
	// Bookkeeping drops paths that are not selected notes.
	w.retainWatchEvents(map[string]cache.DirtyKind{"scratch.tmp": cache.DirtyCreated}, false)
	_, _, err = rt.sourceChanges(ctx, w)
	require.NoError(t, err)
	require.Zero(t, w.events("scratch.tmp"))
	require.NotContains(t, rt.verifiedSources, "notes/b.md")
	w.retainWatchEvents(map[string]cache.DirtyKind{".rhizome/ignore": cache.DirtyModified}, false)
	_, waiting, err = rt.sourceChanges(ctx, w)
	require.NoError(t, err)
	require.True(t, waiting)
	w.takePendingWatchEvents()
	rt.VaultDef = obsidian.VaultDefinition{Root: root, Includes: []string{"notes/c.md"}}
	_, waiting, err = rt.sourceChanges(ctx, w)
	require.NoError(t, err)
	require.True(t, waiting, "notes/a.md is stored but no longer discovered")
}
