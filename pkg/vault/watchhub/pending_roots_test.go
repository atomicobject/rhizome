package watchhub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPendingDirectoryUsesEachNestedRootPolicy(t *testing.T) {
	for _, nestedHidden := range []bool{true, false} {
		name := "permissive-nested"
		if !nestedHidden {
			name = "strict-nested"
		}
		t.Run(name, func(t *testing.T) {
			root := normalizePath(t.TempDir())
			incoming := filepath.Join(root, "incoming")
			nested := filepath.Join(incoming, "notes")
			require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("incoming/notes/hard/**\n"), 0o644))
			hub, err := NewHub(root, Options{DisableFSNotify: true, UserExcludes: []string{"incoming/notes/private/**"}})
			require.NoError(t, err)
			backend := newDirectoryInstallBackend()
			hub.setBackend(backend)
			hub.Start(context.Background())
			t.Cleanup(func() { require.NoError(t, hub.Close()) })
			require.NoError(t, hub.AddRoot(root, RootOptions{Kind: RootNotes, IncludeHidden: !nestedHidden}))
			require.NoError(t, hub.AddRoot(nested, RootOptions{Kind: RootNotes, IncludeHidden: nestedHidden}))
			require.NoError(t, hub.AddRoot(filepath.Join(nested, "hard", "registered"), RootOptions{Kind: RootNotes, IncludeHidden: true}))
			for _, relative := range []string{".outer/deep", "notes/.hidden/deep", "notes/visible", "notes/private/deep", "notes/node_modules/deep", "notes/hard/registered/.hidden"} {
				require.NoError(t, os.MkdirAll(filepath.Join(incoming, relative), 0o755))
			}
			require.NoError(t, os.WriteFile(filepath.Join(nested, "private", "deep", "CONTEXT.md"), []byte("context"), 0o644))
			stale := make(chan StaleEvent, 8)
			hub.Subscribe("nested-roots", Filter{}, nil, func(_ context.Context, event StaleEvent) { stale <- event })
			hub.handleBackendEvent(BackendEvent{Path: incoming, Op: OpCreate, IsDir: true, IsDirKnown: true})
			require.Equal(t, incoming, receiveDirectoryStale(t, stale).Path)
			require.NoError(t, hub.WaitForReady(context.Background(), time.Second))

			want := []string{root, incoming, nested, filepath.Join(nested, "visible"), filepath.Join(nested, "private"), filepath.Join(nested, "private", "deep")}
			if nestedHidden {
				want = append(want, filepath.Join(nested, ".hidden"), filepath.Join(nested, ".hidden", "deep"))
			} else {
				want = append(want, filepath.Join(incoming, ".outer"), filepath.Join(incoming, ".outer", "deep"))
			}
			require.ElementsMatch(t, want, backend.WatchList())
			require.ElementsMatch(t, want, backend.addedPaths())
		})
	}
}

func TestPendingDirectoryReinstallsRegisteredNestedRoot(t *testing.T) {
	fixture := normalizePath(t.TempDir())
	root := filepath.Join(fixture, "vault")
	incoming := filepath.Join(root, "incoming")
	nested := filepath.Join(incoming, "notes")
	hidden := filepath.Join(nested, ".hidden", "deep")
	sibling := filepath.Join(root, "incoming-other")
	require.NoError(t, os.MkdirAll(hidden, 0o755))
	require.NoError(t, os.MkdirAll(sibling, 0o755))
	hub, err := NewHub(root, Options{DisableFSNotify: true})
	require.NoError(t, err)
	backend := newDirectoryInstallBackend()
	hub.setBackend(backend)
	hub.Start(context.Background())
	t.Cleanup(func() { require.NoError(t, hub.Close()) })
	require.NoError(t, hub.AddRoot(root, RootOptions{Kind: RootNotes}))
	require.NoError(t, hub.AddRoot(nested, RootOptions{Kind: RootNotes, IncludeHidden: true}))
	assertWatchlistContains(t, backend.WatchList(), hidden)
	oldWatches := backend.WatchList()
	require.NoError(t, os.Rename(incoming, filepath.Join(fixture, "removed")))
	hub.handleBackendEvent(BackendEvent{Path: incoming, Op: OpRename, IsDir: true, IsDirKnown: true})
	require.ElementsMatch(t, []string{root, sibling}, backend.WatchList())
	require.False(t, hub.roots.HasWalked(nested))
	require.NoError(t, os.MkdirAll(hidden, 0o755))
	stale := make(chan StaleEvent, 8)
	hub.Subscribe("recreated-root", Filter{}, nil, func(_ context.Context, event StaleEvent) { stale <- event })
	hub.handleBackendEvent(BackendEvent{Path: incoming, Op: OpCreate, IsDir: true, IsDirKnown: true})
	require.Equal(t, incoming, receiveDirectoryStale(t, stale).Path)
	require.NoError(t, hub.WaitForReady(context.Background(), time.Second))
	require.ElementsMatch(t, oldWatches, backend.WatchList())
}

func TestPendingDirectoryInstallsRootUnderHiddenAncestor(t *testing.T) {
	root := normalizePath(t.TempDir())
	incoming := filepath.Join(root, "incoming")
	nested := filepath.Join(incoming, ".hidden", "notes")
	hub, err := NewHub(root, Options{DisableFSNotify: true})
	require.NoError(t, err)
	backend := newDirectoryInstallBackend()
	hub.setBackend(backend)
	hub.Start(context.Background())
	t.Cleanup(func() { require.NoError(t, hub.Close()) })
	require.NoError(t, hub.AddRoot(root, RootOptions{Kind: RootNotes}))
	require.NoError(t, hub.AddRoot(nested, RootOptions{Kind: RootNotes}))
	require.NoError(t, os.MkdirAll(filepath.Join(nested, "visible"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(nested, ".private"), 0o755))
	hub.installPendingDirectory(incoming)
	require.ElementsMatch(t, []string{root, incoming, nested, filepath.Join(nested, "visible")}, backend.WatchList())
}
