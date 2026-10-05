package watchhub

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPendingDirectoryInstallsEligibleDescendants(t *testing.T) {
	for _, includeHidden := range []bool{false, true} {
		name := "exclude-hidden"
		if includeHidden {
			name = "include-hidden"
		}
		t.Run(name, func(t *testing.T) {
			root := normalizePath(t.TempDir())
			notes := filepath.Join(root, "notes")
			require.NoError(t, os.MkdirAll(notes, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte("notes/incoming/hard/**\n"), 0o644))
			hub, err := NewHub(root, Options{DisableFSNotify: true, UserExcludes: []string{"notes/incoming/private/**"}})
			require.NoError(t, err)
			backend := newDirectoryInstallBackend()
			hub.setBackend(backend)
			t.Cleanup(func() { require.NoError(t, hub.Close()) })
			require.NoError(t, hub.AddRoot(root, RootOptions{Kind: RootNotes, IncludeHidden: !includeHidden}))
			require.NoError(t, hub.AddRoot(notes, RootOptions{Kind: RootNotes, IncludeHidden: includeHidden}))
			stale := make(chan StaleEvent, 8)
			hub.Subscribe("installed", Filter{}, nil, func(_ context.Context, event StaleEvent) { stale <- event })

			incoming := filepath.Join(notes, "incoming")
			for _, relative := range []string{"nested/deep", ".hidden/deep", "hard/deep", "private/deep", "node_modules/deep"} {
				require.NoError(t, os.MkdirAll(filepath.Join(incoming, relative), 0o755))
			}
			require.NoError(t, os.WriteFile(filepath.Join(incoming, "private", "deep", "CONTEXT.md"), []byte("context"), 0o644))
			hub.handleBackendEvent(BackendEvent{Path: incoming, Op: OpCreate, IsDir: true, IsDirKnown: true})
			// Drain this policy table's queued installation synchronously.
			require.Len(t, hub.pendingAdd, 1)
			hub.installPendingDirectory(<-hub.pendingAdd)
			hub.finishPendingAdd()
			require.Len(t, stale, 1)
			installed := <-stale
			require.Equal(t, incoming, installed.Path)
			require.Equal(t, StaleDirCreated, installed.Reason)
			require.Equal(t, SourceFSNotify, installed.Source)
			require.NoError(t, hub.WaitForReady(context.Background(), time.Second))

			want := []string{normalizePath(root), normalizePath(notes), incoming, filepath.Join(incoming, "nested"), filepath.Join(incoming, "nested", "deep"), filepath.Join(incoming, "private"), filepath.Join(incoming, "private", "deep")}
			if includeHidden {
				want = append(want, filepath.Join(incoming, ".hidden"), filepath.Join(incoming, ".hidden", "deep"))
			}
			for i := range want {
				want[i] = normalizePath(want[i])
			}
			require.ElementsMatch(t, want, backend.WatchList())
			hub.handleBackendEvent(BackendEvent{Path: incoming, Op: OpCreate, IsDir: true, IsDirKnown: true})
			require.Len(t, hub.pendingAdd, 1)
			hub.installPendingDirectory(<-hub.pendingAdd)
			hub.finishPendingAdd()
			require.Len(t, stale, 1)
			installed = <-stale
			require.Equal(t, incoming, installed.Path)
			require.Equal(t, StaleDirCreated, installed.Reason)
			require.Equal(t, SourceFSNotify, installed.Source)
			require.NoError(t, hub.WaitForReady(context.Background(), time.Second))
			require.ElementsMatch(t, want, backend.addedPaths())
		})
	}
}

func TestPendingDirectoryResyncAfterInstallation(t *testing.T) {
	root := normalizePath(t.TempDir())
	incoming := filepath.Join(root, "incoming")
	nested := filepath.Join(incoming, "nested")
	hub, err := NewHub(root, Options{DisableFSNotify: true, Debounce: time.Millisecond})
	require.NoError(t, err)
	backend := newPendingBlockingBackend(nested)
	hub.setBackend(backend)
	hub.Start(context.Background())
	t.Cleanup(func() { require.NoError(t, hub.Close()) })
	t.Cleanup(backend.unblock)
	require.NoError(t, hub.AddRoot(root, RootOptions{Kind: RootNotes}))
	require.NoError(t, os.MkdirAll(nested, 0o755))
	events := make(chan WatchEvent, 8)
	stale := make(chan StaleEvent, 8)
	visible := make(chan bool, 8)
	target := filepath.Join(nested, "note.md")
	hub.Subscribe("discovery-gap", Filter{IncludeDirs: true}, func(_ context.Context, batch []WatchEvent) {
		for _, event := range batch {
			events <- event
		}
	}, func(_ context.Context, event StaleEvent) {
		_, statErr := os.Stat(target)
		visible <- statErr == nil
		stale <- event
	})
	hub.handleBackendEvent(BackendEvent{Path: incoming, Op: OpCreate, IsDir: true, IsDirKnown: true})
	waitForPendingInstall(t, backend)
	waitForRelPath(t, events, stale, "incoming", time.Second)
	require.Error(t, hub.WaitForReady(context.Background(), 25*time.Millisecond))
	require.Empty(t, stale)
	require.NoError(t, os.WriteFile(target, []byte("written before nested watch"), 0o644))
	backend.unblock()
	first := receiveDirectoryStale(t, stale)
	require.Equal(t, incoming, first.Path)
	require.True(t, <-visible)
	assertWatchlistContains(t, backend.WatchList(), nested)
	require.NoError(t, hub.WaitForReady(context.Background(), time.Second))

	second := filepath.Join(root, "second")
	require.NoError(t, os.MkdirAll(filepath.Join(second, "nested"), 0o755))
	hub.handleBackendEvent(BackendEvent{Path: second, Op: OpCreate, IsDir: true, IsDirKnown: true})
	require.Equal(t, second, receiveDirectoryStale(t, stale).Path)
	assertWatchlistContains(t, backend.WatchList(), filepath.Join(second, "nested"))
}

func TestPendingDirectoryCancellationDrainsInstallation(t *testing.T) {
	for _, closeHub := range []bool{false, true} {
		name := "cancel-context"
		if closeHub {
			name = "close-hub"
		}
		t.Run(name, func(t *testing.T) {
			root := normalizePath(t.TempDir())
			incoming := filepath.Join(root, "incoming")
			hub, err := NewHub(root, Options{DisableFSNotify: true})
			require.NoError(t, err)
			backend := newPendingBlockingBackend(incoming)
			hub.setBackend(backend)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hub.Start(ctx)
			t.Cleanup(func() { require.NoError(t, hub.Close()) })
			t.Cleanup(backend.unblock)
			require.NoError(t, hub.AddRoot(root, RootOptions{Kind: RootNotes}))
			require.NoError(t, os.MkdirAll(filepath.Join(incoming, "nested", "deep"), 0o755))
			stale := make(chan StaleEvent, 8)
			hub.Subscribe("cancelled-install", Filter{}, nil, func(_ context.Context, event StaleEvent) { stale <- event })
			hub.handleBackendEvent(BackendEvent{Path: incoming, Op: OpCreate, IsDir: true, IsDirKnown: true})
			waitForPendingInstall(t, backend)
			closed := make(chan error, 1)
			if closeHub {
				go func() { closed <- hub.Close() }()
			} else {
				cancel()
			}
			select {
			case <-hub.context().Done():
			case <-time.After(time.Second):
				t.Fatal("hub context was not cancelled")
			}
			select {
			case <-closed:
				t.Fatal("Close returned with watch installation blocked")
			default:
			}
			backend.unblock()
			if closeHub {
				select {
				case err := <-closed:
					require.NoError(t, err)
				case <-time.After(time.Second):
					t.Fatal("Close did not drain watch installation")
				}
			} else {
				require.NoError(t, hub.WaitForReady(context.Background(), time.Second))
				require.NoError(t, hub.Close())
			}
			require.ElementsMatch(t, []string{root, incoming}, backend.addedPaths())
			require.Empty(t, stale)
		})
	}
}

func TestPendingDirectorySkipsIneligibleRoots(t *testing.T) {
	for _, name := range []string{"hidden", "hidden-ancestor", "removed-root"} {
		t.Run(name, func(t *testing.T) {
			root := normalizePath(t.TempDir())
			hub, err := NewHub(root, Options{DisableFSNotify: true})
			require.NoError(t, err)
			backend := newDirectoryInstallBackend()
			hub.setBackend(backend)
			hub.Start(context.Background())
			t.Cleanup(func() { require.NoError(t, hub.Close()) })
			require.NoError(t, hub.AddRoot(root, RootOptions{Kind: RootNotes, IncludeHidden: name == "hidden-ancestor"}))
			want := []string{root}
			incoming := filepath.Join(root, ".hidden")
			if name == "hidden-ancestor" {
				notes := filepath.Join(root, "notes")
				require.NoError(t, os.MkdirAll(notes, 0o755))
				require.NoError(t, hub.AddRoot(notes, RootOptions{Kind: RootNotes}))
				want = append(want, notes)
				incoming = filepath.Join(notes, ".hidden", "incoming")
			}
			if name == "removed-root" {
				incoming = filepath.Join(root, "incoming")
			}
			require.NoError(t, os.MkdirAll(filepath.Join(incoming, "nested"), 0o755))
			if name == "removed-root" {
				hub.RemoveRoot(root)
			}
			stale := make(chan StaleEvent, 8)
			hub.Subscribe("skipped", Filter{}, nil, func(_ context.Context, event StaleEvent) { stale <- event })
			hub.installPendingDirectory(incoming)
			require.ElementsMatch(t, want, backend.addedPaths())
			require.Empty(t, stale)
		})
	}
}

func receiveDirectoryStale(t *testing.T, stale <-chan StaleEvent) StaleEvent {
	t.Helper()
	select {
	case event := <-stale:
		require.Equal(t, StaleDirCreated, event.Reason)
		require.Equal(t, SourceFSNotify, event.Source)
		return event
	case <-time.After(time.Second):
		t.Fatal("directory installation did not request resync")
		return StaleEvent{}
	}
}

type directoryInstallBackend struct {
	*fakeBackend
	addedMu sync.Mutex
	added   []string
}

func newDirectoryInstallBackend() *directoryInstallBackend {
	return &directoryInstallBackend{fakeBackend: newFakeBackend(false)}
}

func (b *directoryInstallBackend) AddPath(path string) error {
	b.addedMu.Lock()
	b.added = append(b.added, path)
	b.addedMu.Unlock()
	return b.fakeBackend.AddPath(path)
}

func (b *directoryInstallBackend) addedPaths() []string {
	b.addedMu.Lock()
	defer b.addedMu.Unlock()
	return append([]string(nil), b.added...)
}

type pendingBlockingBackend struct {
	*directoryInstallBackend
	blockPath string
	entered   chan struct{}
	release   chan struct{}
	enterOnce sync.Once
	freeOnce  sync.Once
}

func newPendingBlockingBackend(path string) *pendingBlockingBackend {
	return &pendingBlockingBackend{directoryInstallBackend: newDirectoryInstallBackend(), blockPath: path, entered: make(chan struct{}), release: make(chan struct{})}
}

func (b *pendingBlockingBackend) AddPath(path string) error {
	if path == b.blockPath {
		b.enterOnce.Do(func() { close(b.entered) })
		<-b.release
	}
	return b.directoryInstallBackend.AddPath(path)
}

func (b *pendingBlockingBackend) unblock() {
	b.freeOnce.Do(func() { close(b.release) })
}

func waitForPendingInstall(t *testing.T, backend *pendingBlockingBackend) {
	t.Helper()
	select {
	case <-backend.entered:
	case <-time.After(time.Second):
		t.Fatal("pending directory installation did not start")
	}
}
