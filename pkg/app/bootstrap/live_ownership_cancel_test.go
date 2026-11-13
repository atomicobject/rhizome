package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestWatcherCancellationBeforeMarkPreservesScope(t *testing.T) {
	for _, leading := range []bool{true, false} {
		name := "after-refresh"
		if leading {
			name = "during-leading-refresh"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for _, path := range []string{"first.md", "second.md", "untouched.md"} {
				require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("# Original\n"), 0o600))
			}
			w, originalCache, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
			t.Cleanup(func() { _ = originalCache.Close(); _ = store.Close() })
			w.retainWatchEvents(nil, true)
			runOwnershipBatch(t, w)
			var blocked atomic.Bool
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			c, err := cache.NewService(root, cache.Options{AdmitNote: func(path paths.NotePath) bool {
				if path.String() == "first.md" && blocked.CompareAndSwap(true, false) {
					close(entered)
					<-release
				}
				return true
			}})
			require.NoError(t, err)
			t.Cleanup(func() { _ = c.Close() })
			require.NoError(t, c.EnsureReady(t.Context()))
			w.cacheService = c
			require.NoError(t, os.WriteFile(filepath.Join(root, "first.md"), []byte("# Changed first\n"), 0o600))
			c.MarkDirty("first.md", cache.DirtyModified)
			want := []string{"first.md"}
			if leading {
				require.NoError(t, os.WriteFile(filepath.Join(root, "second.md"), []byte("# Changed second\n"), 0o600))
				c.MarkDirty("second.md", cache.DirtyModified)
				want = append(want, "second.md")
			}
			blocked.Store(true)
			interrupted := w.processOwnershipBatch()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("cache admission did not block")
			}
			explicit, _, err := w.lane.Submit(t.Context(), lane.Request{Kind: lane.KindExplicitIndex})
			require.NoError(t, err)
			close(release)
			watcherJobTerminal(t, interrupted)
			require.ErrorIs(t, interrupted.Err(), lane.ErrPreempted)
			watcherJobTerminal(t, explicit)
			unready, err := store.HasUnreadyDerivedWork(t.Context())
			require.NoError(t, err)
			require.False(t, unready, "interruption must precede mark()")
			require.Zero(t, c.Metrics().StaleCount, "the cache or watcher restored the inputs")
			var processed []string
			w.onProcessedPath = func(path string) { processed = append(processed, path) }
			retry := w.processOwnershipBatch()
			watcherJobTerminal(t, retry)
			require.NoError(t, retry.Err())
			require.ElementsMatch(t, want, processed, "retry must keep the interrupted scope")
			require.Zero(t, c.Metrics().ResyncCount)
		})
	}
}

func TestWatcherCancellationAfterMarkKeepsCoarseRecovery(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"changed.md", "untouched.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte("# Original\n"), 0o600))
	}
	w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	t.Cleanup(func() { _ = c.Close(); _ = store.Close() })
	w.retainWatchEvents(nil, true)
	runOwnershipBatch(t, w)
	w.afterPreparedOwnershipCommit = func(ctx context.Context) error {
		priority, err := indexlock.RequestPriority(filepath.Join(root, ".rhizome", "index.lock"))
		if err != nil {
			return err
		}
		defer priority.Close()
		<-ctx.Done()
		return ctx.Err()
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "changed.md"), []byte("# Changed\n"), 0o600))
	c.MarkDirty("changed.md", cache.DirtyModified)
	interrupted := w.processOwnershipBatch()
	watcherJobTerminal(t, interrupted)
	require.ErrorIs(t, interrupted.Err(), context.Canceled)
	unready, err := store.HasUnreadyDerivedWork(t.Context())
	require.NoError(t, err)
	require.True(t, unready)
	w.afterPreparedOwnershipCommit = nil
	var processed []string
	w.onProcessedPath = func(path string) { processed = append(processed, path) }
	retry := w.processOwnershipBatch()
	watcherJobTerminal(t, retry)
	require.NoError(t, retry.Err())
	require.ElementsMatch(t, []string{"changed.md", "untouched.md"}, processed)
}

func TestWatcherCancellationDuringTrailingRefreshMarksCacheStale(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Original\n"), 0o600))
	w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
	t.Cleanup(func() { _ = c.Close(); _ = store.Close() })
	var explicit lane.Handle
	w.onProcessedPath = func(string) {
		var err error
		explicit, _, err = w.lane.Submit(t.Context(), lane.Request{Kind: lane.KindExplicitIndex})
		require.NoError(t, err)
	}
	c.MarkDirty("note.md", cache.DirtyModified)
	interrupted := w.processOwnershipBatch()
	watcherJobTerminal(t, interrupted)
	require.ErrorIs(t, interrupted.Err(), lane.ErrPreempted)
	watcherJobTerminal(t, explicit)
	unready, err := store.HasUnreadyDerivedWork(t.Context())
	require.NoError(t, err)
	require.False(t, unready, "structural publication completed before cancellation")
	require.Positive(t, c.Metrics().StaleCount, "a completed batch interrupted during cache refresh still needs recovery")
}

func TestWatcherStructuralErrorsStillMarkCacheStale(t *testing.T) {
	for _, failure := range []error{errors.New("synthetic structural failure"), context.Canceled, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Original\n"), 0o600))
			w, c, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
			t.Cleanup(func() { _ = c.Close(); _ = store.Close() })
			w.afterPreparedOwnershipCommit = func(ctx context.Context) error {
				require.NoError(t, ctx.Err(), "the batch context is still live")
				return failure
			}
			c.MarkDirty("note.md", cache.DirtyModified)
			failed := w.processOwnershipBatch()
			watcherJobTerminal(t, failed)
			require.ErrorIs(t, failed.Err(), failure)
			require.Positive(t, c.Metrics().StaleCount, "every non-batch-cancellation error keeps the stale mark")
		})
	}
}
