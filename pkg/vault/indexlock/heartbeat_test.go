package indexlock

import (
	"context"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestHeartbeatStopJoinsBeforeReturning(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runtime.lock")
	release, acquired, err := TryAcquire(path)
	require.NoError(t, err)
	require.True(t, acquired)
	defer release()
	stop := StartHeartbeat(context.Background(), path, time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	var stoppers sync.WaitGroup
	for i := 0; i < 8; i++ {
		stoppers.Add(1)
		go func() { defer stoppers.Done(); stop() }()
	}
	stoppers.Wait()
	stable := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(path, stable, stable))
	time.Sleep(20 * time.Millisecond)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.WithinDuration(t, stable, info.ModTime(), time.Millisecond, "joined heartbeat must not write after the owner finalizes")
}
