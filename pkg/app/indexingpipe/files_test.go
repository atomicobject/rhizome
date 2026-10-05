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

func TestProcessFilesReadsCandidatesAndSkipsHidden(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "notes", "keep.md"), "keep")
	mustWriteFile(t, filepath.Join(root, "notes", "skip.md"), "skip")
	mustWriteFile(t, filepath.Join(root, ".hidden", "hidden.md"), "hidden")
	mustWriteFile(t, filepath.Join(root, "code", "main.go"), "package main")

	var got []string
	var skipped []string
	var mu sync.Mutex
	err := ProcessFiles(context.Background(), ProcessOptions{
		Root:        root,
		WorkerCount: 2,
	}, func(path string, _ os.DirEntry, modTime int64) (FileCandidate, bool, error) {
		if filepath.Ext(path) != ".md" {
			return FileCandidate{}, false, nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return FileCandidate{}, false, relErr
		}
		return FileCandidate{
			AbsPath: path,
			RelPath: filepath.ToSlash(rel),
			ModTime: modTime,
			Kind:    FileKindNote,
		}, true, nil
	}, func(candidate FileCandidate) bool {
		return candidate.RelPath != "notes/skip.md"
	}, func(candidate FileCandidate) {
		mu.Lock()
		skipped = append(skipped, candidate.RelPath)
		mu.Unlock()
	}, func(payload FilePayload) error {
		mu.Lock()
		got = append(got, payload.Candidate.RelPath)
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessFiles error: %v", err)
	}
	slices.Sort(got)
	slices.Sort(skipped)
	if !slices.Equal(got, []string{"notes/keep.md"}) {
		t.Fatalf("got processed %v", got)
	}
	if !slices.Equal(skipped, []string{"notes/skip.md"}) {
		t.Fatalf("got skipped %v", skipped)
	}
}

func TestProcessFilesReturnsProcessError(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "notes", "fail.md"), "fail")
	wantErr := errors.New("boom")

	err := ProcessFiles(context.Background(), ProcessOptions{
		Root:        root,
		WorkerCount: 1,
	}, func(path string, _ os.DirEntry, modTime int64) (FileCandidate, bool, error) {
		return FileCandidate{
			AbsPath: path,
			RelPath: "notes/fail.md",
			ModTime: modTime,
			Kind:    FileKindNote,
		}, true, nil
	}, nil, nil, func(FilePayload) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("got err %v want %v", err, wantErr)
	}
}

func TestCountFilesMatchesCandidateClassification(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "notes", "a.md"), "a")
	mustWriteFile(t, filepath.Join(root, "notes", "b.txt"), "b")
	mustWriteFile(t, filepath.Join(root, ".hidden", "c.md"), "c")

	count, err := CountFiles(context.Background(), ProcessOptions{
		Root: root,
	}, func(path string, _ os.DirEntry, modTime int64) (FileCandidate, bool, error) {
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return FileCandidate{}, false, relErr
		}
		return FileCandidate{
			AbsPath: path,
			RelPath: filepath.ToSlash(rel),
			ModTime: modTime,
			Kind:    FileKindNote,
		}, filepath.Ext(path) == ".md", nil
	})
	if err != nil {
		t.Fatalf("CountFiles error: %v", err)
	}
	if count != 1 {
		t.Fatalf("got count %d want 1", count)
	}
}

func TestFileDiscovery_MissingAndNonDirectoryRootsRemainEmpty(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file.md")
	mustWriteFile(t, file, "content")
	for _, path := range []string{filepath.Join(root, "missing"), file} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			opts := ProcessOptions{Root: path}
			classify := func(string, os.DirEntry, int64) (FileCandidate, bool, error) {
				return FileCandidate{}, false, errors.New("unavailable root must not be classified")
			}
			if err := ProcessFiles(context.Background(), opts, classify, nil, nil, func(FilePayload) error { return nil }); err != nil {
				t.Fatalf("ProcessFiles: %v", err)
			}
			count, err := CountFiles(context.Background(), opts, classify)
			if err != nil || count != 0 {
				t.Fatalf("CountFiles = %d, %v; want empty root", count, err)
			}
		})
	}
}

func TestProcessFilesProgressDiscoveryBackpressuresOnWorkerQueue(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "notes", "a.md"), "a")
	mustWriteFile(t, filepath.Join(root, "notes", "b.md"), "b")
	mustWriteFile(t, filepath.Join(root, "notes", "c.md"), "c")
	mustWriteFile(t, filepath.Join(root, "notes", "d.md"), "d")
	mustWriteFile(t, filepath.Join(root, "notes", "e.md"), "e")

	var discovered atomic.Int32
	var blocked atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})

	errCh := make(chan error, 1)
	go func() {
		errCh <- ProcessFiles(context.Background(), ProcessOptions{
			Root:          root,
			WorkerCount:   1,
			QueueCapacity: 1,
			Progress: &ProgressCallbacks{
				OnDiscovered: func(FileCandidate) {
					discovered.Add(1)
				},
			},
		}, func(path string, _ os.DirEntry, modTime int64) (FileCandidate, bool, error) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return FileCandidate{}, false, relErr
			}
			return FileCandidate{
				AbsPath: path,
				RelPath: filepath.ToSlash(rel),
				ModTime: modTime,
				Kind:    FileKindNote,
			}, filepath.Ext(path) == ".md", nil
		}, nil, nil, func(payload FilePayload) error {
			if blocked.CompareAndSwap(0, 1) {
				select {
				case started <- struct{}{}:
				default:
				}
				<-release
			}
			return nil
		})
	}()

	select {
	case <-started:
	case err := <-errCh:
		t.Fatalf("ProcessFiles returned early: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for processing to start")
	}

	time.Sleep(200 * time.Millisecond)
	got := discovered.Load()
	if got >= 5 {
		t.Fatalf("expected bounded discovery backlog, got discovered=%d", got)
	}
	if got < 2 {
		t.Fatalf("expected discovery to get at least one item ahead, got discovered=%d", got)
	}
	close(release)

	if err := <-errCh; err != nil {
		t.Fatalf("ProcessFiles error: %v", err)
	}
	if discovered.Load() != 5 {
		t.Fatalf("expected full discovery after release, got %d", discovered.Load())
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
