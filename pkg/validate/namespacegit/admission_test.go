package namespacegit_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/validate/namespacegit"
	"github.com/stretchr/testify/require"
)

func TestGitSelectorsRefusedBeforeScratch(t *testing.T) {
	f := newFixture(t)
	moves := []namespacegit.Move{{Source: "notes/Old.md", Destination: "notes/New.md"}}
	for _, selector := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE"} {
		t.Run(selector, func(t *testing.T) {
			scratch := canonicalTemp(t)
			before := inventory(t, f.root)
			t.Setenv(selector, "synthetic-unsupported-selection")
			prepared, err := namespacegit.Prepare(context.Background(), f.root, scratch, moves, originalFiles(t, f.root, moves))
			require.Error(t, err)
			require.NotErrorIs(t, err, namespacegit.ErrFallback)
			require.Contains(t, err.Error(), selector)
			require.NotContains(t, err.Error(), "synthetic-unsupported-selection")
			require.Nil(t, prepared)
			entries, err := os.ReadDir(scratch)
			require.NoError(t, err)
			require.Empty(t, entries)
			assertLiveUnchanged(t, before, inventory(t, f.root))
		})
	}
}

func TestMalformedAdmissionAndScratchAreHardFailures(t *testing.T) {
	for _, variant := range []string{"escape", "metadata", "overlap", "missing-source", "nonregular", "occupied", "scratch-collision", "live-git-scratch", "index-symlink"} {
		t.Run(variant, func(t *testing.T) {
			f := newFixture(t)
			moves := []namespacegit.Move{{Source: "notes/Old.md", Destination: "notes/New.md"}}
			files := originalFiles(t, f.root, moves)
			scratch := canonicalTemp(t)
			switch variant {
			case "escape":
				moves[0].Destination = "../escaped.md"
			case "metadata":
				moves[0].Destination = ".git/index"
			case "overlap":
				moves = append(moves, namespacegit.Move{Source: "notes/New.md", Destination: "Again.md"})
			case "missing-source":
				files = nil
			case "nonregular":
				files[0].Mode = os.ModeSymlink
			case "occupied":
				moves[0].Destination = "notes/Existing.md"
				files = originalFiles(t, f.root, moves)
			case "scratch-collision":
				writeFile(t, scratch, "git-preparation/foreign", "preserve\n", 0o644)
			case "live-git-scratch":
				scratch = filepath.Join(f.root, ".git")
			case "index-symlink":
				index := filepath.Join(f.root, ".git", "index")
				backing := filepath.Join(canonicalTemp(t), "original.index")
				require.NoError(t, os.Rename(index, backing))
				if err := os.Symlink(backing, index); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			before, scratchBefore := inventory(t, f.root), inventory(t, scratch)
			prepared, err := namespacegit.Prepare(context.Background(), f.root, scratch, moves, files)
			require.Error(t, err)
			require.NotErrorIs(t, err, namespacegit.ErrFallback)
			require.Nil(t, prepared)
			assertLiveUnchanged(t, before, inventory(t, f.root))
			require.Equal(t, scratchBefore, inventory(t, scratch))
		})
	}
}

func TestSafeGitPreparationFallback(t *testing.T) {
	for _, variant := range []string{"no-repository", "linked-worktree", "corrupt-index", "missing-git"} {
		t.Run(variant, func(t *testing.T) {
			f := newFixture(t)
			switch variant {
			case "no-repository":
				f.root = canonicalTemp(t)
				writeFile(t, f.root, "notes/Old.md", "source\n", 0o644)
			case "linked-worktree":
				f.root = canonicalTemp(t)
				writeFile(t, f.root, "notes/Old.md", "source\n", 0o644)
				writeFile(t, f.root, ".git", "gitdir: synthetic-unsupported\n", 0o644)
			case "corrupt-index":
				writeFile(t, f.root, ".git/index", "invalid synthetic index\n", 0o644)
			case "missing-git":
				t.Setenv("PATH", canonicalTemp(t))
			}
			moves := []namespacegit.Move{{Source: "notes/Old.md", Destination: "notes/New.md"}}
			before := inventory(t, f.root)
			prepared, err := namespacegit.Prepare(context.Background(), f.root, canonicalTemp(t), moves, originalFiles(t, f.root, moves))
			require.ErrorIs(t, err, namespacegit.ErrFallback)
			require.Nil(t, prepared)
			assertLiveUnchanged(t, before, inventory(t, f.root))
		})
	}
}

func TestRedirectedGitDirectoryIsAHardFailure(t *testing.T) {
	backing := newFixture(t)
	root := canonicalTemp(t)
	if err := os.Symlink(filepath.Join(backing.root, ".git"), filepath.Join(root, ".git")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeFile(t, root, "Old.md", "source\n", 0o644)
	moves := []namespacegit.Move{{Source: "Old.md", Destination: "New.md"}}
	before, backingBefore := inventory(t, root), inventory(t, backing.root)
	scratch := canonicalTemp(t)
	prepared, err := namespacegit.Prepare(context.Background(), root, scratch, moves, originalFiles(t, root, moves))
	require.Error(t, err)
	require.NotErrorIs(t, err, namespacegit.ErrFallback)
	require.Nil(t, prepared)
	entries, err := os.ReadDir(scratch)
	require.NoError(t, err)
	require.Empty(t, entries)
	assertLiveUnchanged(t, before, inventory(t, root))
	assertLiveUnchanged(t, backingBefore, inventory(t, backing.root))
}

func TestCapturedOriginalsAndFsmonitorNonexecution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable fsmonitor")
	}
	f := newFixture(t)
	moves := []namespacegit.Move{{Source: "notes/Old.md", Destination: "notes/New.md"}}
	files := originalFiles(t, f.root, moves)
	marker := filepath.Join(canonicalTemp(t), "fsmonitor-ran")
	hook := filepath.Join(canonicalTemp(t), "fsmonitor")
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\nprintf called > '"+marker+"'\nexit 1\n"), 0o755))
	f.git(t, "config", "core.fsmonitor", hook)
	writeFile(t, f.root, moves[0].Source, "later authored edit\n", 0o644)
	before := inventory(t, f.root)
	scratch := canonicalTemp(t)
	prepared, err := namespacegit.Prepare(context.Background(), f.root, scratch, moves, files)
	require.NoError(t, err)
	require.NotNil(t, prepared)
	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err), "configured executable fsmonitor ran")
	body, err := os.ReadFile(filepath.Join(scratch, "git-preparation", "tree", "notes", "New.md"))
	require.NoError(t, err)
	require.Equal(t, "base source\n", string(body), "helper must use captured original, not later authored content")
	assertLiveUnchanged(t, before, inventory(t, f.root))
}

func TestCancellationIsNotFallback(t *testing.T) {
	f := newFixture(t)
	moves := []namespacegit.Move{{Source: "notes/Old.md", Destination: "notes/New.md"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scratch := canonicalTemp(t)
	prepared, err := namespacegit.Prepare(ctx, f.root, scratch, moves, originalFiles(t, f.root, moves))
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, prepared)
	entries, err := os.ReadDir(scratch)
	require.NoError(t, err)
	require.Empty(t, entries)

	// A real child blocks after starting; cancellation must terminate the Git
	// command and preserve the live index, rather than selecting fallback.
	program, err := os.Executable()
	require.NoError(t, err)
	bin := canonicalTemp(t)
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	body, err := os.ReadFile(program)
	require.NoError(t, err)
	writeFile(t, bin, name, string(body), 0o755)
	marker := filepath.Join(canonicalTemp(t), "child-started")
	t.Setenv("NAMESPACE_GIT_CHILD_MARKER", marker)
	t.Setenv("PATH", bin)
	before := inventory(t, f.root)
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	files := originalFiles(t, f.root, moves)
	go func() {
		_, err := namespacegit.Prepare(ctx, f.root, scratch, moves, files)
		done <- err
	}()
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("preparation ended before child start: %v", err)
		case <-deadline:
			t.Fatal("Git child did not start")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-done:
		require.True(t, errors.Is(err, context.Canceled))
		require.NotErrorIs(t, err, namespacegit.ErrFallback)
	case <-time.After(5 * time.Second):
		t.Fatal("Git cancellation did not finish")
	}
	assertLiveUnchanged(t, before, inventory(t, f.root))
}

func TestMain(m *testing.M) {
	if marker := os.Getenv("NAMESPACE_GIT_CHILD_MARKER"); marker != "" {
		if err := os.WriteFile(marker, []byte("started"), 0o600); err != nil {
			os.Exit(2)
		}
		for {
			time.Sleep(time.Minute)
		}
	}
	os.Exit(m.Run())
}
