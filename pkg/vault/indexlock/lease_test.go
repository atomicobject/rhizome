package indexlock

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLeaseRequiresAcquiredOwnershipForTheExactVault(t *testing.T) {
	root := t.TempDir()
	var absent *Lease
	require.ErrorIs(t, absent.Validate(root), ErrLeaseNotHeld)
	require.ErrorIs(t, (&Lease{}).Validate(root), ErrLeaseNotHeld)
	_, _, err := TryAcquireLease("", AcquireOptions{})
	require.Error(t, err)

	lease, acquired, err := TryAcquireLease(root, AcquireOptions{Role: "test/writer"})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { require.NoError(t, lease.Release()) })
	require.NoError(t, lease.Validate(root))
	require.ErrorIs(t, lease.Validate(t.TempDir()), ErrLeaseNotHeld)
	copied := *lease
	lease.StartHeartbeat(context.Background(), time.Millisecond)
	require.NoError(t, copied.Release())
	require.ErrorIs(t, lease.Validate(root), ErrLeaseNotHeld)
	require.NoFileExists(t, filepath.Join(root, ".rhizome", "index.lock"))
	require.NoError(t, lease.Release())

	successor, acquired, err := TryAcquireLease(root, AcquireOptions{})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { require.NoError(t, successor.Release()) })
	require.ErrorIs(t, copied.Validate(root), ErrLeaseNotHeld)
	require.NoError(t, successor.Validate(root))
}

func TestLeaseCannotBorrowAnExistingSameProcessLock(t *testing.T) {
	root := t.TempDir()
	release, acquired, err := TryAcquire(filepath.Join(root, ".rhizome", "index.lock"))
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { require.NoError(t, release()) })
	lease, acquired, err := TryAcquireLease(root, AcquireOptions{})
	require.NoError(t, err)
	require.False(t, acquired)
	require.Nil(t, lease)
}

func TestLeaseRejectsAReplacedOwnerToken(t *testing.T) {
	root := t.TempDir()
	lease, acquired, err := TryAcquireLease(root, AcquireOptions{})
	require.NoError(t, err)
	require.True(t, acquired)
	path := filepath.Join(root, ".rhizome", "index.lock")
	owner, ok := ReadLockData(path)
	require.True(t, ok)
	owner.Token = "synthetic-successor"
	replacement, err := json.Marshal(owner)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, replacement, 0o644))
	require.ErrorIs(t, lease.Validate(root), ErrLeaseNotHeld)
	require.Error(t, lease.Release())
	current, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, replacement, current)
}
