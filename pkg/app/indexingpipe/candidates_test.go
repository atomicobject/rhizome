package indexingpipe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProcessCandidatesReadsSkipsAndReportsProgress(t *testing.T) {
	root := t.TempDir()
	keep := writeCandidateFile(t, root, "keep.md", "keep")
	skip := writeCandidateFile(t, root, "skip.md", "skip")
	missing := FileCandidate{
		AbsPath: filepath.Join(root, "missing.md"),
		RelPath: "missing.md",
		Kind:    FileKindNote,
	}

	var mu sync.Mutex
	var discovered []string
	var completed []string
	var skipped []string
	var processed []string
	err := ProcessCandidates(
		context.Background(),
		[]FileCandidate{keep, skip, missing},
		CandidateProcessOptions{
			WorkerCount: 1,
			Progress: &ProgressCallbacks{
				OnDiscovered: func(candidate FileCandidate) {
					mu.Lock()
					discovered = append(discovered, candidate.RelPath)
					mu.Unlock()
				},
				OnCompleted: func(candidate FileCandidate) {
					mu.Lock()
					completed = append(completed, candidate.RelPath)
					mu.Unlock()
				},
			},
		},
		func(candidate FileCandidate) bool { return candidate.RelPath != "skip.md" },
		func(candidate FileCandidate) {
			mu.Lock()
			skipped = append(skipped, candidate.RelPath)
			mu.Unlock()
		},
		func(payload FilePayload) error {
			mu.Lock()
			processed = append(processed, payload.Candidate.RelPath+":"+string(payload.Content))
			mu.Unlock()
			return nil
		},
	)
	if err != nil {
		t.Fatalf("ProcessCandidates error: %v", err)
	}

	if !slices.Equal(discovered, []string{"keep.md", "skip.md", "missing.md"}) {
		t.Fatalf("discovered = %v", discovered)
	}
	slices.Sort(completed)
	if !slices.Equal(completed, []string{"keep.md", "missing.md", "skip.md"}) {
		t.Fatalf("completed = %v", completed)
	}
	if !slices.Equal(skipped, []string{"skip.md"}) {
		t.Fatalf("skipped = %v", skipped)
	}
	if !slices.Equal(processed, []string{"keep.md:keep"}) {
		t.Fatalf("processed = %v", processed)
	}
}

func TestProcessCandidatesBoundsReadAndProcessWorkers(t *testing.T) {
	root := t.TempDir()
	candidates := make([]FileCandidate, 6)
	for i := range candidates {
		candidates[i] = writeCandidateFile(t, root, "file-"+string(rune('a'+i))+".md", "body")
	}

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32
	errCh := make(chan error, 1)
	go func() {
		errCh <- ProcessCandidates(
			context.Background(), candidates,
			CandidateProcessOptions{WorkerCount: 2, QueueCapacity: 1},
			nil, nil,
			func(FilePayload) error {
				current := active.Add(1)
				for {
					observed := maxActive.Load()
					if current <= observed || maxActive.CompareAndSwap(observed, current) {
						break
					}
				}
				select {
				case started <- struct{}{}:
				default:
				}
				<-release
				active.Add(-1)
				return nil
			},
		)
	}()

	for range 2 {
		select {
		case <-started:
		case err := <-errCh:
			t.Fatalf("ProcessCandidates returned before workers started: %v", err)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for workers")
		}
	}
	if got := maxActive.Load(); got != 2 {
		t.Fatalf("max active workers = %d, want 2", got)
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatalf("ProcessCandidates error: %v", err)
	}
}

func TestProcessCandidatesReturnsContextCancellation(t *testing.T) {
	root := t.TempDir()
	candidate := writeCandidateFile(t, root, "cancel.md", "body")
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})

	errCh := make(chan error, 1)
	go func() {
		errCh <- ProcessCandidates(
			ctx, []FileCandidate{candidate}, CandidateProcessOptions{WorkerCount: 1}, nil, nil,
			func(FilePayload) error {
				close(started)
				<-ctx.Done()
				return ctx.Err()
			},
		)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for processing to start")
	}
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("ProcessCandidates error = %v, want context cancellation", err)
	}
}

func TestProcessCandidatesReturnsProcessError(t *testing.T) {
	root := t.TempDir()
	candidate := writeCandidateFile(t, root, "fail.md", "body")
	wantErr := errors.New("process failed")

	err := ProcessCandidates(
		context.Background(), []FileCandidate{candidate}, CandidateProcessOptions{WorkerCount: 1}, nil, nil,
		func(FilePayload) error { return wantErr },
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("ProcessCandidates error = %v, want %v", err, wantErr)
	}
}

func TestProcessCandidatesHandlesReadError(t *testing.T) {
	root := t.TempDir()
	missing := FileCandidate{AbsPath: filepath.Join(root, "missing.md"), RelPath: "missing.md", Kind: FileKindNote}
	var handled atomic.Int32
	var completed atomic.Int32
	var handledCandidate string
	var handledErr error

	err := ProcessCandidates(
		context.Background(), []FileCandidate{missing},
		CandidateProcessOptions{
			WorkerCount: 1,
			OnReadError: func(_ context.Context, candidate FileCandidate, err error) error {
				handledCandidate = candidate.RelPath
				handledErr = err
				handled.Add(1)
				return nil
			},
			Progress: &ProgressCallbacks{OnCompleted: func(FileCandidate) { completed.Add(1) }},
		},
		nil, nil,
		func(FilePayload) error {
			return errors.New("process must not run after a read failure")
		},
	)
	if err != nil {
		t.Fatalf("ProcessCandidates error: %v", err)
	}
	if got := handled.Load(); got != 1 {
		t.Fatalf("handled = %d, want 1", got)
	}
	if handledCandidate != "missing.md" {
		t.Fatalf("handled candidate = %q", handledCandidate)
	}
	if !errors.Is(handledErr, os.ErrNotExist) {
		t.Fatalf("read error = %v, want not exist", handledErr)
	}
	if got := completed.Load(); got != 1 {
		t.Fatalf("completed = %d, want 1", got)
	}
}

func TestProcessCandidatesReturnsReadErrorHandlerError(t *testing.T) {
	root := t.TempDir()
	missing := FileCandidate{AbsPath: filepath.Join(root, "missing.md"), RelPath: "missing.md", Kind: FileKindNote}
	wantErr := errors.New("retain fatal source identity")

	err := ProcessCandidates(
		context.Background(), []FileCandidate{missing},
		CandidateProcessOptions{
			WorkerCount: 1,
			OnReadError: func(context.Context, FileCandidate, error) error {
				return wantErr
			},
		},
		nil, nil,
		func(FilePayload) error {
			return errors.New("process must not run after a read failure")
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("ProcessCandidates error = %v, want %v", err, wantErr)
	}
}

func TestProcessCandidatesDoesNotMutateInput(t *testing.T) {
	root := t.TempDir()
	candidates := []FileCandidate{
		writeCandidateFile(t, root, "a.md", "a"),
		writeCandidateFile(t, root, "b.md", "b"),
	}
	want := slices.Clone(candidates)

	err := ProcessCandidates(
		context.Background(), candidates, CandidateProcessOptions{WorkerCount: 1}, nil, nil,
		func(FilePayload) error { return nil },
	)
	if err != nil {
		t.Fatalf("ProcessCandidates error: %v", err)
	}
	if !slices.Equal(candidates, want) {
		t.Fatalf("input candidates mutated: got %#v want %#v", candidates, want)
	}
}

func writeCandidateFile(t *testing.T, root, relPath, content string) FileCandidate {
	t.Helper()
	absPath := filepath.Join(root, relPath)
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", absPath, err)
	}
	return FileCandidate{AbsPath: absPath, RelPath: relPath, Kind: FileKindNote}
}
