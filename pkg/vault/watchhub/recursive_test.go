package watchhub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
)

func TestAddWatchRoots_RecursiveDirectories(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	hub, err := NewHub(root, Options{})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(func() { _ = hub.Close() })

	if err := hub.AddRoot(root, RootOptions{Kind: RootNotes}); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	hub.AddWatchRoots()

	backend := waitForBackendReady(t, hub, 2*time.Second)
	if backend == nil {
		t.Skip("backend disabled; cannot inspect watch list")
	}

	watchList := backend.WatchList()
	assertWatchlistContains(t, watchList, root)
	// For recursive backends (FSEvents on macOS), we only watch the root.
	// For non-recursive backends (fsnotify), we watch each subdirectory.
	if !backend.IsRecursive() {
		assertWatchlistContains(t, watchList, filepath.Join(root, "a"))
		assertWatchlistContains(t, watchList, nested)
	}
}

func TestWatchHub_EmitsEventForNestedFile(t *testing.T) {
	if runtime.GOOS == "darwin" && raceEnabled() {
		t.Skip("fsnotify-backed nested file events are not reliable on darwin under -race")
	}

	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	hub, err := NewHub(root, Options{
		Debounce:      10 * time.Millisecond,
		ForceFSNotify: true,
	})
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(func() { _ = hub.Close() })

	if err := hub.AddRoot(root, RootOptions{Kind: RootNotes}); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	hub.AddWatchRoots()
	readyTimeout := 2 * time.Second
	if runtime.GOOS == "darwin" {
		readyTimeout = 5 * time.Second
	}
	backend := waitForBackendReady(t, hub, readyTimeout)
	if backend != nil && !backend.IsRecursive() {
		assertWatchlistContains(t, backend.WatchList(), nested)
	}

	eventsCh := make(chan WatchEvent, 32)
	staleCh := make(chan StaleEvent, 1)
	unsub := hub.Subscribe("test", Filter{IncludeFiles: true}, func(ctx context.Context, events []WatchEvent) {
		for _, ev := range events {
			eventsCh <- ev
		}
	}, func(ctx context.Context, ev StaleEvent) {
		select {
		case staleCh <- ev:
		default:
		}
	})
	defer unsub()

	if runtime.GOOS == "darwin" && backend != nil && !backend.IsRecursive() {
		probe := filepath.Join(nested, ".watchhub-probe.md")
		if err := os.WriteFile(probe, []byte("#probe"), 0o644); err != nil {
			t.Fatalf("write probe: %v", err)
		}
		probeRel := filepath.ToSlash(filepath.Join("a", "b", ".watchhub-probe.md"))
		if !waitForRelPathOK(eventsCh, staleCh, probeRel, 1500*time.Millisecond) {
			t.Skip("fsnotify backend did not emit probe event on darwin; watcher unavailable in this environment")
		}
	}

	target := filepath.Join(nested, "note.md")
	if err := os.WriteFile(target, []byte("#nested"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	timeout := 4 * time.Second
	if runtime.GOOS == "windows" {
		timeout = 12 * time.Second
	} else if runtime.GOOS == "darwin" {
		timeout = 8 * time.Second
	}
	expectedRel := filepath.ToSlash(filepath.Join("a", "b", "note.md"))
	waitForRelPath(t, eventsCh, staleCh, expectedRel, timeout)
}

func assertWatchlistContains(t *testing.T, watchList []string, path string) {
	want := normalizePath(path)
	for _, entry := range watchList {
		if normalizePath(entry) == want {
			return
		}
	}
	t.Fatalf("watch list missing %s; got %v", want, watchList)
}

func waitForRelPath(t *testing.T, eventsCh <-chan WatchEvent, staleCh <-chan StaleEvent, expected string, timeout time.Duration) {
	t.Helper()
	ok, msg := waitForRelPathResult(eventsCh, staleCh, expected, timeout)
	if ok {
		return
	}
	t.Fatal(msg)
}

func waitForRelPathOK(eventsCh <-chan WatchEvent, staleCh <-chan StaleEvent, expected string, timeout time.Duration) bool {
	ok, _ := waitForRelPathResult(eventsCh, staleCh, expected, timeout)
	return ok
}

func waitForRelPathResult(eventsCh <-chan WatchEvent, staleCh <-chan StaleEvent, expected string, timeout time.Duration) (bool, string) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	seen := make([]WatchEvent, 0, 8)
	var stale *StaleEvent
	for {
		select {
		case ev := <-eventsCh:
			seen = append(seen, ev)
			if ev.RelPath == expected {
				return true, ""
			}
		case ev := <-staleCh:
			stale = &ev
		case <-deadline.C:
			msg := fmt.Sprintf("timed out waiting for %s; events=%v", expected, summarizeEvents(seen))
			if stale != nil {
				msg = fmt.Sprintf("%s stale=%v", msg, *stale)
			}
			return false, msg
		}
	}
}

func waitForBackendReady(t *testing.T, hub *Hub, timeout time.Duration) Backend {
	t.Helper()
	if err := hub.WaitForReady(context.Background(), timeout); err != nil {
		t.Fatalf("timed out waiting for watcher backend to be ready: %v", err)
	}
	return hub.backendSnapshot()
}

func summarizeEvents(events []WatchEvent) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, fmt.Sprintf("%s op=%d src=%s", ev.RelPath, ev.Op, ev.Source))
		if len(out) >= 10 {
			break
		}
	}
	return out
}

func normalizePath(path string) string {
	resolved := paths.ResolveSymlinks(path)
	if resolved != "" {
		return filepath.Clean(resolved.String())
	}
	return filepath.Clean(path)
}
