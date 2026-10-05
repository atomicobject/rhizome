package serve

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

func TestRecoverInterruptedWritesEstablishesStartupBarrierForCleanVault(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, RecoverInterruptedWrites(context.Background(), obsidian.VaultDefinition{Path: root}))

	info, err := os.Lstat(filepath.Join(root, ".rhizome"))
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Zero(t, info.Mode()&os.ModeSymlink)
}

func TestRecoverInterruptedWritesWaitsForPreviousWriter(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, ".rhizome", "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RecoverInterruptedWrites(ctx, obsidian.VaultDefinition{Path: root}) }()
	select {
	case err := <-done:
		t.Fatalf("recovery returned while another writer held the index lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, release())
	require.NoError(t, <-done)
}

func TestRecoverInterruptedWritesCanCancelWhileWaiting(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, ".rhizome", "index.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RecoverInterruptedWrites(ctx, obsidian.VaultDefinition{Path: root}) }()
	require.Eventually(t, func() bool { return indexlock.CheckPriority(lockPath) }, time.Second, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.False(t, indexlock.CheckPriority(lockPath))
}
