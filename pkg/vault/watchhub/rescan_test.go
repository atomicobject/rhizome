package watchhub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// FSEvents MustScanSubDirs rescans inside trees the hub never delivers must
// not force a full vault resync; rescans that can hide watched paths must.
func TestMustRescanMarksStaleOnlyForWatchedTrees(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, rhizomeDirName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, rhizomeDirName, "ignore"), []byte("/_Archive/\n/vendored/\n"), 0o644))

	hub, err := NewHub(root, Options{DisableFSNotify: true, StaleMinInterval: -1})
	require.NoError(t, err)
	require.NoError(t, hub.roots.Add(filepath.Join(root, "vendored", "lib"), RootOptions{Kind: RootCode}))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(func() { _ = hub.Close() })

	staleCh := make(chan StaleEvent, 16)
	unsub := hub.Subscribe("test", Filter{IncludeFiles: true}, nil, func(_ context.Context, ev StaleEvent) {
		staleCh <- ev
	})
	defer unsub()

	rescan := func(rel string) bool {
		hub.handleBackendEvent(BackendEvent{Path: filepath.Join(root, filepath.FromSlash(rel)), MustRescan: true, IsDirKnown: true, IsDir: true})
		select {
		case ev := <-staleCh:
			require.Equal(t, StaleOverflow, ev.Reason)
			return true
		case <-time.After(50 * time.Millisecond):
			return false
		}
	}

	for _, rel := range []string{".git", ".git/objects/ab", "node_modules/pkg", "notes/node_modules", "_Archive", "_Archive/old", ".rhizome/cache", ".claude/projects", "vendored/other"} {
		require.False(t, rescan(rel), "rescan of %s must not mark stale", rel)
	}
	for _, rel := range []string{".", "notes", "notes/sub", ".rhizome", ".rhizome/ontology", "vendored", "vendored/lib"} {
		require.True(t, rescan(rel), "rescan of %s must mark stale", rel)
	}
	// A rescan of an ancestor of the vault root may hide every vault path.
	require.True(t, rescan(".."), "rescan of the vault parent must mark stale")
}
