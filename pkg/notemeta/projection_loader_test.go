package notemeta

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestLoadProjectedEntriesForPathsUsesBoundedParallelismAndSortedOutput(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)

	release := make(chan struct{})
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})
	reader := &blockingNoteReader{
		notes: map[string]string{
			"d.md": "# D\n",
			"b.md": "# B\n",
			"a.md": "# A\n",
			"c.md": "# C\n",
		},
		release: release,
		started: make(chan struct{}, 4),
	}
	type loadResult struct {
		entries []projectedNoteEntry
		err     error
	}
	done := make(chan loadResult, 1)
	indexer := testIndexer(t)
	go func() {
		entries, err := indexer.loadProjectedEntriesForPaths(
			context.Background(),
			obsidian.VaultDefinition{Path: "/vault"},
			reader,
			[]string{"d.md", "b.md", "a.md", "c.md", "b.md"},
		)
		done <- loadResult{entries: entries, err: err}
	}()

	for range 2 {
		select {
		case <-reader.started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for bounded parallel projection reads")
		}
	}
	select {
	case <-reader.started:
		t.Fatal("projection loader exceeded the GOMAXPROCS worker bound")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	released = true

	result := <-done
	require.NoError(t, result.err)
	require.Equal(t, 2, reader.maxObservedActive())
	require.Equal(t, []string{"a.md", "b.md", "c.md", "d.md"}, projectedEntryPaths(result.entries))
}

func TestLoadProjectedEntriesForPathsHonorsPreCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &countingProjectionNoteReader{}

	_, err := testIndexer(t).loadProjectedEntriesForPaths(
		ctx,
		obsidian.VaultDefinition{Path: "/vault"},
		reader,
		[]string{"a.md", "b.md"},
	)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, reader.reads.Load())
}

func TestLoadProjectedEntriesForPathsNamesFailingAuthoredPath(t *testing.T) {
	reader := fakeNoteReader{
		notes: map[string]string{"a.md": "# A\n"},
		errs:  map[string]error{"broken.md": errors.New("injected read failure")},
	}

	_, err := testIndexer(t).loadProjectedEntriesForPaths(
		context.Background(),
		obsidian.VaultDefinition{Path: "/vault"},
		reader,
		[]string{"a.md", "broken.md"},
	)
	require.ErrorContains(t, err, "read note broken.md")
	require.ErrorContains(t, err, "injected read failure")
}

func projectedEntryPaths(entries []projectedNoteEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Entry.Path)
	}
	return paths
}

type countingProjectionNoteReader struct {
	reads atomic.Int32
}

func (r *countingProjectionNoteReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	r.reads.Add(1)
	return "# Note\n", nil
}

func (*countingProjectionNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return nil, nil
}

func (r *countingProjectionNoteReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	r.reads.Add(1)
	return time.Unix(1, 0), nil
}

func (*countingProjectionNoteReader) Title(path string) (string, bool) { return path, true }
