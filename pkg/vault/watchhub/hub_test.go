package watchhub

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestEmitHintPathsFiltersIgnored(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, rhizomeDirName), 0o755); err != nil {
		t.Fatalf("mkdir .rhizome: %v", err)
	}
	ignorePath := filepath.Join(root, rhizomeDirName, "ignore")
	if err := os.WriteFile(ignorePath, []byte("ignored.md\n"), 0o644); err != nil {
		t.Fatalf("write ignore: %v", err)
	}

	hub, err := NewHub(root, Options{DisableFSNotify: true, Debounce: 5 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(func() { _ = hub.Close() })

	eventsCh := make(chan []WatchEvent, 1)
	unsub := hub.Subscribe("test", Filter{IncludeFiles: true}, func(ctx context.Context, events []WatchEvent) {
		eventsCh <- events
	}, nil)
	defer unsub()

	hub.EmitHintPaths([]string{"ignored.md", "ok.md"})

	select {
	case events := <-eventsCh:
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if events[0].RelPath != "ok.md" {
			t.Fatalf("expected ok.md, got %s", events[0].RelPath)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timed out waiting for events")
	}
}

func TestShouldFilterContextUsesHardIgnoreBoundaries(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, rhizomeDirName), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, rhizomeDirName, "ignore"),
		[]byte("hard-hidden/**\n"),
		0o644,
	))

	hub, err := NewHub(root, Options{
		DisableFSNotify: true,
		UserExcludes:    []string{"private/**"},
	})
	require.NoError(t, err)

	require.False(t, hub.shouldFilter(WatchEvent{RelPath: "private", IsDir: true}, true))
	require.False(t, hub.shouldFilter(WatchEvent{RelPath: "private/CONTEXT.md"}, false))
	require.True(t, hub.shouldFilter(WatchEvent{RelPath: "private/ordinary.md"}, false))
	require.True(t, hub.shouldFilter(WatchEvent{RelPath: "hard-hidden", IsDir: true}, true))
	require.True(t, hub.shouldFilter(WatchEvent{RelPath: "hard-hidden/CONTEXT.md"}, false))
	require.True(t, hub.shouldFilter(WatchEvent{RelPath: "node_modules/CONTEXT.md"}, false))
	require.True(t, hub.shouldFilter(WatchEvent{RelPath: "private/context.md"}, false))
}

func TestEmitHintPathHintsPreservesKinds(t *testing.T) {
	root := t.TempDir()
	hub, err := NewHub(root, Options{DisableFSNotify: true, Debounce: 5 * time.Millisecond})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(func() { _ = hub.Close() })

	eventsCh := make(chan []WatchEvent, 1)
	unsub := hub.Subscribe("test", Filter{IncludeFiles: true}, func(ctx context.Context, events []WatchEvent) {
		eventsCh <- events
	}, nil)
	defer unsub()

	hub.EmitHintPathHints([]HintPath{
		{Path: "created.md", Kind: "created"},
		{Path: "modified.md", Kind: "modified"},
		{Path: "removed.md", Kind: "removed"},
		{Path: "renamed.md", Kind: "renamed"},
		{Path: "recreated.md", Kind: "recreated"},
		{Path: ".rhizome/config.yml", Kind: "modified"},
	})

	var events []WatchEvent
	select {
	case events = <-eventsCh:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timed out waiting for events")
	}
	require.Len(t, events, 5)
	byPath := map[string]Op{}
	for _, event := range events {
		byPath[event.RelPath] = event.Op
	}
	require.True(t, byPath["created.md"].Has(OpCreate))
	require.True(t, byPath["modified.md"].Has(OpWrite))
	require.True(t, byPath["removed.md"].Has(OpRemove))
	require.True(t, byPath["renamed.md"].Has(OpRename))
	require.True(t, byPath["recreated.md"].Has(OpRemove))
	require.True(t, byPath["recreated.md"].Has(OpCreate))
	_, ok := byPath[".rhizome/config.yml"]
	require.False(t, ok)
}

func TestEmitHintResyncCallsStale(t *testing.T) {
	root := t.TempDir()
	hub, err := NewHub(root, Options{DisableFSNotify: true})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(func() { _ = hub.Close() })

	staleCh := make(chan StaleEvent, 1)
	unsub := hub.Subscribe("test", Filter{IncludeFiles: true}, nil, func(ctx context.Context, ev StaleEvent) {
		staleCh <- ev
	})
	defer unsub()

	hub.EmitHintResync()

	select {
	case ev := <-staleCh:
		if ev.Reason != StaleHintResync {
			t.Fatalf("expected %s, got %s", StaleHintResync, ev.Reason)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timed out waiting for stale event")
	}
}

func TestPrefixFilter(t *testing.T) {
	root := t.TempDir()
	hub, err := NewHub(root, Options{DisableFSNotify: true, Debounce: 5 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(func() { _ = hub.Close() })

	eventsCh := make(chan []WatchEvent, 1)
	unsub := hub.Subscribe("test", Filter{IncludeFiles: true, Prefixes: []string{"notes"}}, func(ctx context.Context, events []WatchEvent) {
		eventsCh <- events
	}, nil)
	defer unsub()

	hub.EmitHintPaths([]string{"notes/a.md", "docs/b.md"})

	select {
	case events := <-eventsCh:
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if events[0].RelPath != "notes/a.md" {
			t.Fatalf("expected notes/a.md, got %s", events[0].RelPath)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("timed out waiting for events")
	}
}

func TestEmitHintResyncCoalescesByDefault(t *testing.T) {
	root := t.TempDir()
	hub, err := NewHub(root, Options{DisableFSNotify: true, StaleMinInterval: time.Minute})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(func() { _ = hub.Close() })

	staleCh := make(chan StaleEvent, 4)
	unsub := hub.Subscribe("test", Filter{IncludeFiles: true}, nil, func(ctx context.Context, ev StaleEvent) {
		staleCh <- ev
	})
	defer unsub()

	hub.EmitHintResync()
	hub.EmitHintResync()

	select {
	case <-staleCh:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected first stale event")
	}
	select {
	case ev := <-staleCh:
		t.Fatalf("unexpected coalesced stale event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestMarkStaleDoesNotCoalesceIgnoreOrDirRename(t *testing.T) {
	root := t.TempDir()
	hub, err := NewHub(root, Options{DisableFSNotify: true, StaleMinInterval: time.Minute})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(func() { _ = hub.Close() })

	staleCh := make(chan StaleEvent, 8)
	unsub := hub.Subscribe("test", Filter{IncludeFiles: true}, nil, func(ctx context.Context, ev StaleEvent) {
		staleCh <- ev
	})
	defer unsub()

	hub.markStale(StaleEvent{Reason: StaleIgnoreChanged, Source: SourceFSNotify, Path: ".rhizome/ignore"})
	hub.markStale(StaleEvent{Reason: StaleIgnoreChanged, Source: SourceFSNotify, Path: ".rhizome/ignore"})
	hub.markStale(StaleEvent{Reason: StaleDirRenamed, Source: SourceFSNotify, Path: "notes"})
	hub.markStale(StaleEvent{Reason: StaleDirRenamed, Source: SourceFSNotify, Path: "notes"})

	var got []StaleReason
	timeout := time.After(200 * time.Millisecond)
	for len(got) < 4 {
		select {
		case ev := <-staleCh:
			got = append(got, ev.Reason)
		case <-timeout:
			t.Fatalf("expected 4 stale events, got %d (%v)", len(got), got)
		}
	}
}

func TestDispatchDoesNotHoldSubscriptionLockDuringHandlers(t *testing.T) {
	root := t.TempDir()
	hub, err := NewHub(root, Options{DisableFSNotify: true})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(func() { _ = hub.Close() })

	done := make(chan struct{})
	var unsub func()
	unsub = hub.Subscribe("self-unsub", Filter{IncludeFiles: true}, func(ctx context.Context, events []WatchEvent) {
		unsub()
		close(done)
	}, nil)

	hub.dispatch([]WatchEvent{{RelPath: "notes/a.md", AbsPath: filepath.Join(root, "notes", "a.md"), Op: OpWrite, IsDir: false}})

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("handler deadlocked while unsubscribing")
	}
}

func TestMarkStaleDoesNotHoldSubscriptionLockDuringHandlers(t *testing.T) {
	root := t.TempDir()
	hub, err := NewHub(root, Options{DisableFSNotify: true})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(func() { _ = hub.Close() })

	done := make(chan struct{})
	var unsub func()
	unsub = hub.Subscribe("self-unsub-stale", Filter{IncludeFiles: true}, nil, func(ctx context.Context, ev StaleEvent) {
		unsub()
		close(done)
	})

	hub.markStale(StaleEvent{Reason: StaleIgnoreChanged, Source: SourceFSNotify, Path: ".rhizome/ignore"})

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("stale handler deadlocked while unsubscribing")
	}
}

func TestDispatchSkipsUnsubscribedSnapshotEntries(t *testing.T) {
	root := t.TempDir()
	hub, err := NewHub(root, Options{DisableFSNotify: true})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })

	called := atomic.Bool{}
	unsub := hub.Subscribe("gone", Filter{IncludeFiles: true}, func(ctx context.Context, events []WatchEvent) {
		called.Store(true)
	}, func(ctx context.Context, ev StaleEvent) {
		called.Store(true)
	})
	unsub()

	hub.dispatch([]WatchEvent{{RelPath: "notes/a.md", AbsPath: filepath.Join(root, "notes", "a.md"), Op: OpWrite, IsDir: false}})
	hub.markStale(StaleEvent{Reason: StaleIgnoreChanged, Source: SourceFSNotify, Path: ".rhizome/ignore"})
	require.False(t, called.Load())
}

func TestAddWatchRootsSkipsInternalRoots(t *testing.T) {
	root := t.TempDir()
	hub, err := NewHub(root, Options{DisableFSNotify: true})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	backend := newFakeBackend(true)
	hub.setBackend(backend)

	internalRoot := filepath.Join(root, "internal-cache")
	if err := os.MkdirAll(internalRoot, 0o755); err != nil {
		t.Fatalf("mkdir internal root: %v", err)
	}
	notesRoot := filepath.Join(root, "notes")
	if err := os.MkdirAll(notesRoot, 0o755); err != nil {
		t.Fatalf("mkdir notes: %v", err)
	}

	if err := hub.AddRoot(internalRoot, RootOptions{Kind: RootInternal}); err != nil {
		t.Fatalf("add internal root: %v", err)
	}
	if err := hub.AddRoot(notesRoot, RootOptions{Kind: RootNotes}); err != nil {
		t.Fatalf("add notes root: %v", err)
	}
	hub.AddWatchRoots()

	watched := backend.WatchList()
	internalNorm := paths.NormalizeAbsPathForCompare(internalRoot)
	notesNorm := paths.NormalizeAbsPathForCompare(notesRoot)
	normalized := make([]string, 0, len(watched))
	for _, path := range watched {
		normalized = append(normalized, paths.NormalizeAbsPathForCompare(path))
	}
	if slices.Contains(normalized, internalNorm) {
		t.Fatalf("expected internal root to be skipped, watched=%v", watched)
	}
	if !slices.Contains(normalized, notesNorm) {
		t.Fatalf("expected notes root watch to be present, watched=%v", watched)
	}
}

func TestNewHubConfiguresBackendEventBuffer(t *testing.T) {
	root := t.TempDir()
	hub, err := NewHub(root, Options{ForceFSNotify: true, EventBuffer: 7})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })

	backend := hub.backendSnapshot()
	if backend == nil {
		t.Fatalf("expected backend")
	}
	if got := cap(backend.Events()); got != 7 {
		t.Fatalf("backend event buffer cap = %d, want 7", got)
	}
}

func TestNewHubDefaultsBackendEventBuffer(t *testing.T) {
	root := t.TempDir()
	hub, err := NewHub(root, Options{ForceFSNotify: true})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close() })

	backend := hub.backendSnapshot()
	if backend == nil {
		t.Fatalf("expected backend")
	}
	if got := cap(backend.Events()); got != defaultBackendEventBuffer {
		t.Fatalf("backend event buffer cap = %d, want %d", got, defaultBackendEventBuffer)
	}
}

func TestAddRootWatchForIgnoreWatchesRhizomeRoot(t *testing.T) {
	root := t.TempDir()
	rhizomeRoot := filepath.Join(root, ".rhizome")
	if err := os.MkdirAll(filepath.Join(rhizomeRoot, "ontology"), 0o755); err != nil {
		t.Fatalf("mkdir .rhizome tree: %v", err)
	}

	hub, err := NewHub(root, Options{DisableFSNotify: true})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	backend := newFakeBackend(false)
	hub.setBackend(backend)

	if err := hub.AddRoot(root, RootOptions{Kind: RootNotes, WatchForIgnore: true}); err != nil {
		t.Fatalf("add root: %v", err)
	}

	normalized := make([]string, 0, len(backend.WatchList()))
	for _, path := range backend.WatchList() {
		normalized = append(normalized, paths.NormalizeAbsPathForCompare(path))
	}
	if !slices.Contains(normalized, paths.NormalizeAbsPathForCompare(rhizomeRoot)) {
		t.Fatalf("expected .rhizome root watch to be present, watched=%v", backend.WatchList())
	}
	if !slices.Contains(normalized, paths.NormalizeAbsPathForCompare(filepath.Join(rhizomeRoot, "ontology"))) {
		t.Fatalf("expected .rhizome/ontology watch to be present, watched=%v", backend.WatchList())
	}
}

type fakeBackend struct {
	mu        sync.Mutex
	recursive bool
	events    chan BackendEvent
	errors    chan error
	watches   map[string]struct{}
}

func newFakeBackend(recursive bool) *fakeBackend {
	return &fakeBackend{
		recursive: recursive,
		events:    make(chan BackendEvent, 8),
		errors:    make(chan error, 8),
		watches:   make(map[string]struct{}),
	}
}

func (f *fakeBackend) Start(context.Context) error { return nil }

func (f *fakeBackend) Close() error { return nil }

func (f *fakeBackend) AddPath(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watches[path] = struct{}{}
	return nil
}

func (f *fakeBackend) RemovePath(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.watches, path)
	return nil
}

func (f *fakeBackend) Events() <-chan BackendEvent { return f.events }

func (f *fakeBackend) Errors() <-chan error { return f.errors }

func (f *fakeBackend) IsRecursive() bool { return f.recursive }

func (f *fakeBackend) WatchList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.watches))
	for path := range f.watches {
		out = append(out, path)
	}
	return out
}

type blockingBackend struct {
	*fakeBackend
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingBackend(recursive bool) *blockingBackend {
	return &blockingBackend{
		fakeBackend: newFakeBackend(recursive),
		started:     make(chan struct{}),
		release:     make(chan struct{}),
	}
}

func (b *blockingBackend) AddPath(path string) error {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return b.fakeBackend.AddPath(path)
}

func TestWaitForReady_WaitsForWatchInstallation(t *testing.T) {
	root := t.TempDir()
	requireDir := filepath.Join(root, "nested")
	if err := os.MkdirAll(requireDir, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	hub, err := NewHub(root, Options{DisableFSNotify: true})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	backend := newBlockingBackend(false)
	hub.setBackend(backend)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(func() { _ = hub.Close() })

	done := make(chan error, 1)
	go func() {
		done <- hub.AddRoot(root, RootOptions{Kind: RootNotes})
	}()

	select {
	case <-backend.started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for AddPath to start")
	}

	if err := hub.WaitForReady(context.Background(), 50*time.Millisecond); err == nil {
		t.Fatal("expected WaitForReady to time out while watch installation is blocked")
	}

	close(backend.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AddRoot: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for AddRoot to finish")
	}

	if err := hub.WaitForReady(context.Background(), 500*time.Millisecond); err != nil {
		t.Fatalf("WaitForReady: %v", err)
	}
}

func TestBackendReadySettleOnlyDelaysRealFSNotify(t *testing.T) {
	if got := backendReadySettle(newFakeBackend(false)); got != 0 {
		t.Fatalf("fake backend settle = %s, want 0", got)
	}
	if got := backendReadySettle(&fsnotifyBackend{}); got < 50*time.Millisecond {
		t.Fatalf("fsnotify backend settle = %s, want at least 50ms", got)
	}
}
