package actions

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestPropertyMutationReadsLatestContentAfterWriterReleases(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, []byte("# Note\noriginal\n"), 0o644))
	lockPath := filepath.Join(root, ".rhizome", "index.lock")
	releaseWriter, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = releaseWriter() })

	vault, note := stubVault{path: root}, &obsidian.Note{}
	done := make(chan error, 1)
	go func() {
		_, err := SetPropertyOnFiles(t.Context(), vault, note, "status", "open", []string{"note.md"}, false, false)
		done <- err
	}()
	require.Eventually(t, func() bool { return indexlock.CheckPriority(lockPath) }, time.Second, 10*time.Millisecond)
	require.NoError(t, os.WriteFile(path, []byte("# Note\nupdated while waiting\n"), 0o644))
	require.NoError(t, releaseWriter())
	require.NoError(t, <-done)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(content), "updated while waiting")
	require.Contains(t, string(content), "status: open")
}

func TestCanceledDeleteDoesNotWriteOrLeavePriority(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, []byte("# Note\n"), 0o644))
	lockPath := filepath.Join(root, ".rhizome", "index.lock")
	releaseWriter, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	defer func() { _ = releaseWriter() }()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- DeleteNote(stubVault{path: root}, DeleteParams{Context: ctx, NotePath: "note"}) }()
	require.Eventually(t, func() bool { return indexlock.CheckPriority(lockPath) }, time.Second, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.FileExists(t, path)
	require.False(t, indexlock.CheckPriority(lockPath), "priority request was left behind")
}
