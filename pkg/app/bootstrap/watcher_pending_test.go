package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
	"github.com/stretchr/testify/require"
)

func TestLiveOwnershipSavedPathsSurviveCacheReads(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Original\n"), 0o644))
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	t.Cleanup(func() { require.NoError(t, store.Close()); require.NoError(t, cacheService.Close()) })
	runOwnershipBatch(t, w)
	hub, err := watchhub.NewHub(root, watchhub.Options{DisableFSNotify: true, Debounce: time.Millisecond})
	require.NoError(t, err)
	hub.Start(ctx)
	t.Cleanup(func() { require.NoError(t, hub.Close()) })
	t.Cleanup(cache.SubscribeWatchHub(hub, cacheService))
	w.watchHub = hub
	w.subscribeWatchHub()
	t.Cleanup(w.stopWatchHub)
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Saved\n"), 0o644))
	// The synchronous save has already published metadata before its completion hint.
	prep, err := w.noteMetadataIndexer.PreparePublishedMetadata(ctx, []paths.NotePath{"note.md"}, nil)
	require.NoError(t, err)
	delta, err := w.noteMetadataIndexer.BuildPublishedMetadataDeltaFromPreparation(ctx, w.vaultDef, &obsidian.Note{}, store, prep)
	require.NoError(t, err)
	require.NotNil(t, delta)
	require.NoError(t, store.ApplyNoteMetadataDelta(ctx, *delta))
	hub.EmitHintPaths([]string{"note.md"})
	require.Eventually(t, func() bool {
		w.pendingMu.Lock()
		defer w.pendingMu.Unlock()
		return len(w.pendingByRel) > 0 && len(cacheService.DirtySnapshot()) > 0
	}, time.Second, time.Millisecond)
	require.NoError(t, cacheService.Refresh(ctx))
	require.NoError(t, cacheService.Refresh(ctx))
	require.Empty(t, cacheService.DirtySnapshot())
	runOwnershipBatch(t, w)
	debt, err := store.PendingDerivedWork(ctx, time.Now(), 100)
	require.NoError(t, err)
	require.NotEmpty(t, debt, "saved path must schedule derived work after cache reads")
	requireLiveOwnershipCompleted(t, w)
}

func TestWatcherTriggerReasonsSurviveRetryWithoutFlatteningNewEvents(t *testing.T) {
	w := &unifiedSemanticWatcher{}
	w.retainWatchInputs(map[string]cache.DirtyKind{"note.md": cache.DirtyModified}, true, map[string]int64{"stale_overflow": 2, "directory_create": 1})
	dirty, resync, reasons := w.takePendingWatchInputs()
	w.retainWatchInputs(map[string]cache.DirtyKind{"note.md": cache.DirtyRemoved}, true, map[string]int64{"stale_ignore_changed": 1})
	w.restoreWatchInputs(dirty, resync, reasons)
	retried, resync, retained := w.takePendingWatchInputs()
	require.Equal(t, cache.DirtyRemoved, retried["note.md"])
	require.True(t, resync)
	require.Equal(t, map[string]int64{"stale_overflow": 2, "directory_create": 1, "stale_ignore_changed": 1}, retained)
	_, resync, retained = w.takePendingWatchInputs()
	require.False(t, resync)
	require.Empty(t, retained)
}

func TestWatcherPendingRetryPreservesNewerChanges(t *testing.T) {
	w := &unifiedSemanticWatcher{}
	w.retainWatchEvents(map[string]cache.DirtyKind{"note.md": cache.DirtyModified}, false)
	dirty, _ := w.takePendingWatchEvents()
	w.retainWatchEvents(map[string]cache.DirtyKind{"note.md": cache.DirtyRemoved}, true)
	w.restoreWatchEvents(dirty, false)
	retried, resync := w.takePendingWatchEvents()
	require.Equal(t, cache.DirtyRemoved, retried["note.md"])
	require.True(t, resync)
}
