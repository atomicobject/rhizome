package codeanchor

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func TestWatcher_WaitForDrainSignals(t *testing.T) {
	w := &Watcher{}

	// No pending work should return immediately.
	w.WaitForDrain()

	w.markPending()
	done := make(chan struct{})
	go func() {
		w.WaitForDrain()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("WaitForDrain returned before drain")
	case <-time.After(20 * time.Millisecond):
	}

	w.markDrained()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WaitForDrain did not return after drain")
	}
}

func TestWatcher_isExcludedPath(t *testing.T) {
	w := &Watcher{
		excludeDirs: map[string]struct{}{
			"node_modules": {},
			"vendor":       {},
		},
	}

	tests := []struct {
		path     string
		excluded bool
	}{
		{"src/main.py", false},
		{"node_modules/pkg/index.js", true},
		{"foo/vendor/lib.go", true},
		{".git/config", true},
		{"src/.hidden/file.py", true},
		{"src/app/file.py", false},
	}
	for _, tc := range tests {
		got := w.isExcludedPath(tc.path)
		require.Equal(t, tc.excluded, got, "path=%s", tc.path)
	}
}

func TestWatcher_shouldSkipDir(t *testing.T) {
	w := &Watcher{
		excludeDirs: map[string]struct{}{
			"node_modules": {},
			"__pycache__":  {},
		},
	}

	tests := []struct {
		path string
		name string
		skip bool
	}{
		{"/repo/src", "src", false},
		{"/repo/node_modules", "node_modules", true},
		{"/repo/.git", ".git", true},
		{"/repo/__pycache__", "__pycache__", true},
		{"/repo/lib", "lib", false},
	}
	for _, tc := range tests {
		got := w.shouldSkipDir(tc.path, tc.name)
		require.Equal(t, tc.skip, got, "path=%s name=%s", tc.path, tc.name)
	}
}

func TestDefaultWatcherOptions(t *testing.T) {
	opts := defaultWatcherOptions()
	require.Contains(t, opts.excludeDirs, "node_modules")
	require.Contains(t, opts.excludeDirs, "vendor")
	require.Contains(t, opts.excludeDirs, "__pycache__")
}

func TestWatcherNoteSourceUsesCanonicalRelativePathAndFileMtime(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes", "source.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("# Source\n"), 0o644))
	mtime := time.Unix(1_700_000_000, 0)
	require.NoError(t, os.Chtimes(path, mtime, mtime))

	svc := NewServiceWithOptions(nil, nil, WithBasePath(root), WithoutWarmCache())
	w := &Watcher{
		service: svc,
		noteSourceFactory: func(path string, content []byte, mtime int64) NoteSource {
			return newTestNoteSource(path, string(content), mtime)
		},
	}

	source, err := w.noteSource(path, []byte("# Source\n"))
	require.NoError(t, err)
	require.Equal(t, "notes/source.md", source.NotePathString())
	require.Equal(t, mtime.Unix(), source.NoteMtimeUnix())
	require.NotEmpty(t, source.NoteContentHashString())
}

func TestNewWatcherRequiresCanonicalNoteSourceFactory(t *testing.T) {
	root := t.TempDir()
	svc := NewServiceWithOptions(nil, nil, WithBasePath(root), WithoutWarmCache())

	_, err := NewWatcher(svc, []string{root}, nil, nil)

	require.ErrorContains(t, err, "note source factory is required")
}

func TestWatcher_isExcludedPath_GlobPatterns(t *testing.T) {
	root := t.TempDir()
	genPath := filepath.Join("rhizome-generated", "out.go")
	buildPath := filepath.Join("rhizome-bundle-output", "out.js")
	otherPath := filepath.Join("src", "main.go")

	w := &Watcher{
		noteRoots:    []string{root},
		codeRoots:    []string{root},
		excludeDirs:  map[string]struct{}{},
		excludeGlobs: []string{"rhizome-generated/**", "**/rhizome-bundle-output/**"},
	}

	require.True(t, w.isExcludedPath(genPath))
	require.True(t, w.isExcludedPath(buildPath))
	require.False(t, w.isExcludedPath(otherPath))
}

type blockingRescanIndexer struct {
	started chan struct{}
	release chan struct{}
	second  chan struct{}
	calls   atomic.Int32
}

func (idx *blockingRescanIndexer) Lang() Lang { return LangPy }

func (idx *blockingRescanIndexer) IndexFile(_ []byte, ref paths.CodePathRef) (FileSummary, error) {
	switch idx.calls.Add(1) {
	case 1:
		close(idx.started)
		<-idx.release
	case 2:
		close(idx.second)
	}
	return FileSummary{FilePath: ref.Rel.String(), Lang: LangPy, ParseStatus: ParseOK}, nil
}

func TestWatcherScheduleStaleRescanQueuesSinglePendingRun(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "code.py"), []byte("def run(): pass\n"), 0o644))
	idx := &blockingRescanIndexer{started: make(chan struct{}), release: make(chan struct{}), second: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-idx.release:
		default:
			close(idx.release)
		}
	})
	svc := NewServiceWithOptions(&stubStore{}, []LanguageIndexer{idx}, WithBasePath(root), WithoutWarmCache(), WithWriteAccess())
	// Scheduler coverage must not depend on exclusions of temp ancestors such as /tmp.
	w, err := NewWatcherWithOptions(svc, nil, []string{root}, nil, func(opts *watcherOptions) {
		opts.excludeDirs = nil
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w.scheduleStaleRescan(ctx)
	select {
	case <-idx.started:
	case <-ctx.Done():
		t.Fatal("first rescan did not reach indexer")
	}
	for i := 0; i < 3; i++ {
		w.scheduleStaleRescan(ctx)
	}
	close(idx.release)
	select {
	case <-idx.second:
	case <-ctx.Done():
		t.Fatal("pending rescan did not run")
	}
	// Wait for the scheduler, which outlives the per-pass drain signal.
	require.Eventually(t, func() bool {
		w.staleRescanMu.Lock()
		defer w.staleRescanMu.Unlock()
		return !w.staleRescanRunning
	}, 5*time.Second, time.Millisecond)
	require.EqualValues(t, 2, idx.calls.Load(), "pending requests should coalesce into one follow-up")
}
