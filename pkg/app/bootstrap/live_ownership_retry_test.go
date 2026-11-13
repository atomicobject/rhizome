package bootstrap

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestWatcherStructuralFailuresBackOffAndNewInputResetsDelay(t *testing.T) {
	for _, resync := range []bool{false, true} {
		name := "file-event"
		if resync {
			name = "stale-event"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\n"), 0o600))
			w, warm, store := newLiveOwnershipTestWatcher(t, root, obsidian.VaultDefinition{Path: root})
			t.Cleanup(func() { _ = warm.Close(); _ = store.Close() })
			var fail atomic.Bool
			fail.Store(true)
			cold, err := cache.NewService(root, cache.Options{DiscoverFiles: func() ([]string, error) {
				if fail.Load() {
					return nil, errors.New("synthetic structural failure")
				}
				return []string{"note.md"}, nil
			}})
			require.NoError(t, err)
			t.Cleanup(func() { _ = cold.Close() })
			w.cacheService = cold
			var now atomic.Int64
			now.Store(time.Now().UnixNano())
			w.opts.Now = func() time.Time { return time.Unix(0, now.Load()) }
			counted, tick := tickingWatcher(t, w)
			tick()
			require.Equal(t, int32(1), counted.batches.Load())
			for i, delay := range []time.Duration{3 * time.Second, 6 * time.Second, 12 * time.Second, 24 * time.Second, 48 * time.Second, time.Minute, time.Minute} {
				now.Add(int64(delay - time.Nanosecond))
				tick()
				require.Equal(t, int32(i+1), counted.batches.Load(), "retry before %s", delay)
				now.Add(int64(time.Nanosecond))
				tick()
				require.Equal(t, int32(i+2), counted.batches.Load(), "retry at %s", delay)
			}
			// A fresh event resets the accumulated delay without waiting for it.
			if resync {
				w.retainInputs(nil, true, map[string]int64{"stale_overflow": 1})
			} else {
				w.retainInputs(map[string]cache.DirtyKind{"note.md": cache.DirtyModified}, false, nil)
			}
			tick()
			require.Equal(t, int32(9), counted.batches.Load())
			if !resync {
				// A tick can consume the input before its wake is queued.
				w.ownershipWake <- struct{}{}
				tick()
				require.Equal(t, int32(9), counted.batches.Load(), "a late wake must not reset the same input twice")
			}
			now.Add(int64(3*time.Second - time.Nanosecond))
			tick()
			require.Equal(t, int32(9), counted.batches.Load())
			fail.Store(false)
			now.Add(int64(time.Nanosecond))
			tick()
			require.GreaterOrEqual(t, counted.batches.Load(), int32(10))
			paths, err := store.NotePaths(t.Context())
			require.NoError(t, err)
			require.Equal(t, []string{"note.md"}, paths)
		})
	}
}
