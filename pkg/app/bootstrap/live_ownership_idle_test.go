package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	anchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// countingLane counts admitted watcher batches.
type countingLane struct {
	lane.Lane
	batches atomic.Int32
}

func (l *countingLane) Submit(ctx context.Context, req lane.Request) (lane.Handle, bool, error) {
	if req.Kind == lane.KindWatcherBatch {
		l.batches.Add(1)
	}
	return l.Lane.Submit(ctx, req)
}

// tickingWatcher runs the watcher loop on a test-owned ticker. Each send on
// tick returns once the loop took it; a second send proves the first was
// handled, including waiting for any batch it admitted.
func tickingWatcher(t *testing.T, w *unifiedSemanticWatcher) (*countingLane, func()) {
	t.Helper()
	counted := &countingLane{Lane: w.lane}
	w.lane = counted
	tick := make(chan time.Time)
	w.opts.Tick = tick
	ctx, cancel := context.WithCancel(context.Background())
	w.runCtx = ctx
	w.ownershipWake = make(chan struct{}, 1)
	w.watcherDone = make(chan struct{})
	go w.run()
	t.Cleanup(func() {
		cancel()
		<-w.watcherDone
	})
	return counted, func() {
		for range 2 {
			select {
			case tick <- time.Now():
			case <-time.After(30 * time.Second):
				t.Fatal("watcher loop did not take the tick")
			}
		}
	}
}

func TestWatcherIdleTicksAdmitNoBatch(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\n"), 0o644))
	w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	t.Cleanup(func() { _ = store.Close(); _ = cacheService.Close() })
	counted, tick := tickingWatcher(t, w)

	for range 5 {
		tick()
	}
	require.Zero(t, counted.batches.Load(), "idle ticks must not admit lane jobs")
}

func TestWatcherTickSubmitsForEachPendingTrigger(t *testing.T) {
	ctx := context.Background()
	triggers := map[string]func(t *testing.T, w *unifiedSemanticWatcher, store *anchorsqlite.Store){
		"cache stale": func(_ *testing.T, w *unifiedSemanticWatcher, _ *anchorsqlite.Store) {
			w.cacheService.MarkStale()
		},
		"cache dirty": func(_ *testing.T, w *unifiedSemanticWatcher, _ *anchorsqlite.Store) {
			w.cacheService.MarkDirty("note.md", cache.DirtyModified)
		},
		"restored inputs": func(_ *testing.T, w *unifiedSemanticWatcher, _ *anchorsqlite.Store) {
			w.restoreWatchInputs(map[string]cache.DirtyKind{"note.md": cache.DirtyModified}, false, nil)
		},
		"restored resync": func(_ *testing.T, w *unifiedSemanticWatcher, _ *anchorsqlite.Store) {
			w.restoreWatchInputs(nil, true, nil)
		},
		"pending reconciliation": func(t *testing.T, _ *unifiedSemanticWatcher, store *anchorsqlite.Store) {
			_, err := store.ApplyOwnershipTransitions(ctx, []anchorsqlite.OwnershipTransition{{
				Path: "gone.md", Target: anchorsqlite.OwnershipTargetNote,
				Note: &anchorsqlite.NoteSourceState{Title: "Gone", FormatID: "markdown", ContentHash: "before", Mtime: 1, Size: 1,
					ProviderVersion: "builtin-markdown-v1", ProjectionVersion: "builtin-markdown-v1", Status: anchorsqlite.NoteProjectionStatusCurrent, ObservedAt: 1},
			}})
			require.NoError(t, err)
			_, err = store.ApplyOwnershipTransitions(ctx, []anchorsqlite.OwnershipTransition{{Path: "gone.md", Target: anchorsqlite.OwnershipTargetUnowned}})
			require.NoError(t, err)
			_, pending, err := store.PendingOwnershipReconciliation(ctx)
			require.NoError(t, err)
			require.True(t, pending)
		},
		"unready derived work": func(t *testing.T, _ *unifiedSemanticWatcher, store *anchorsqlite.Store) {
			_, err := store.MarkDerivedDirty(ctx, []codeanchor.DerivedScope{{Kind: codeanchor.DerivedNotes}})
			require.NoError(t, err)
		},
	}
	for name, trigger := range triggers {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\n"), 0o644))
			w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
			t.Cleanup(func() { _ = store.Close(); _ = cacheService.Close() })
			counted, tick := tickingWatcher(t, w)
			tick()
			require.Zero(t, counted.batches.Load())

			trigger(t, w, store)
			tick()
			require.Positive(t, counted.batches.Load(), "a tick with pending work must admit a batch")
		})
	}

	t.Run("cache not ready", func(t *testing.T) {
		root := t.TempDir()
		w, cacheService, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
		t.Cleanup(func() { _ = store.Close(); _ = cacheService.Close() })
		cold, err := cache.NewService(root, cache.Options{})
		require.NoError(t, err)
		t.Cleanup(func() { _ = cold.Close() })
		w.cacheService = cold
		counted, tick := tickingWatcher(t, w)
		tick()
		require.Positive(t, counted.batches.Load())
	})
}

// gatedRecrawlWatcher swaps in a cache whose first stale-triggered recrawl
// waits for release, and records each resync invalidation the watcher
// publishes (one per full discovery forced by a resync).
func gatedRecrawlWatcher(t *testing.T, root string) (w *unifiedSemanticWatcher, store *anchorsqlite.Store, release func(), resyncs *atomic.Int32) {
	t.Helper()
	w, helperCache, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	require.NoError(t, helperCache.Close())
	policy, err := liveCacheSelectionPolicy(w.vaultDef, w.noteRuntime, w.codeCfg)
	require.NoError(t, err)
	var crawls atomic.Int32
	gate := make(chan struct{})
	svc, err := cache.NewService(root, cache.Options{DiscoverFiles: func() ([]string, error) {
		if crawls.Add(1) == 2 {
			<-gate
		}
		if policy.DiscoverFiles != nil {
			return policy.DiscoverFiles()
		}
		return obsidian.DiscoverFiles(w.vaultDef)
	}, AdmitNote: policy.Admit, UserExcludes: policy.UserExcludes})
	require.NoError(t, err)
	require.NoError(t, svc.EnsureReady(context.Background()))
	w.cacheService = svc
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(func() { release(); _ = svc.Close(); _ = store.Close() })
	resyncs = &atomic.Int32{}
	w.publishEvent = func(kind string, data any) {
		if fields, ok := data.(map[string]any); ok && kind == globalEventIndexInvalidated && fields["reason"] == "resync" {
			resyncs.Add(1)
		}
	}
	return w, store, release, resyncs
}

// staleEvent delivers a watch hub stale event to both subscribers.
func staleEvent(w *unifiedSemanticWatcher) {
	w.cacheService.MarkStale()
	w.retainInputs(nil, true, map[string]int64{"stale_overflow": 1})
}

// settle runs batches until a tick would admit none.
func settle(t *testing.T, w *unifiedSemanticWatcher) {
	t.Helper()
	require.Eventually(t, func() bool {
		if !w.hasPendingWork() {
			return true
		}
		runOwnershipBatch(t, w)
		return false
	}, 30*time.Second, 10*time.Millisecond)
}

func TestWatcherStaleWithRecrawlRunsOneFullDiscovery(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "first.md"), []byte("# First\n"), 0o644))
	w, store, release, resyncs := gatedRecrawlWatcher(t, root)

	staleEvent(w)
	runOwnershipBatch(t, w)
	require.Zero(t, resyncs.Load(), "the resync waits for the recrawl the stale event started")

	// An edit during the recrawl still lands through a scoped batch.
	require.NoError(t, os.WriteFile(filepath.Join(root, "during.md"), []byte("# During\n"), 0o644))
	w.cacheService.MarkDirty("during.md", cache.DirtyCreated)
	w.retainWatchInputs(map[string]cache.DirtyKind{"during.md": cache.DirtyCreated}, false, nil)
	runOwnershipBatch(t, w)
	paths, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.Contains(t, paths, "during.md")
	require.Zero(t, resyncs.Load())

	release()
	settle(t, w)
	require.Equal(t, int32(1), resyncs.Load(), "one stale event and its recrawl run exactly one full discovery")
	paths, err = store.NotePaths(context.Background())
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"first.md", "during.md"}, paths)
}

func TestWatcherSecondStaleDuringRecrawlConverges(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "first.md"), []byte("# First\n"), 0o644))
	w, store, release, resyncs := gatedRecrawlWatcher(t, root)

	staleEvent(w)
	runOwnershipBatch(t, w)
	// A change whose event was lost in the second overflow.
	require.NoError(t, os.WriteFile(filepath.Join(root, "lost.md"), []byte("# Lost\n"), 0o644))
	staleEvent(w)
	runOwnershipBatch(t, w)
	require.Zero(t, resyncs.Load())

	release()
	settle(t, w)
	require.Positive(t, resyncs.Load())
	paths, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"first.md", "lost.md"}, paths)
}

func TestWatcherCanceledBatchIsRediscovered(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\n"), 0o644))
	w, store, release, _ := gatedRecrawlWatcher(t, root)
	release()

	// A batch canceled after its refresh drained the path (SPEC-0104 US3).
	w.cacheService.MarkDirty("note.md", cache.DirtyModified)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, w.reconcileBatchUnderLease(ctx, nil))
	w.cacheService.MarkStale()
	require.True(t, w.hasPendingWork(), "a canceled batch leaves work for the next tick")

	settle(t, w)
	paths, err := store.NotePaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"note.md"}, paths)
}
