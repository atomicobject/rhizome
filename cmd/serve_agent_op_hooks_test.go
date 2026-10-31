package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/stretchr/testify/require"
)

// Another cache reader refreshing concurrently must not hide the recrawl
// resyncCache waits for.
func TestResyncCacheReturnsDespiteConcurrentReader(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\n"), 0o644))
	svc, err := cache.NewService(root, cache.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })
	require.NoError(t, svc.EnsureReady(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	readerCtx, stopReader := context.WithCancel(ctx)
	defer stopReader()
	go func() {
		for readerCtx.Err() == nil {
			_ = svc.Refresh(readerCtx)
		}
	}()
	require.NoError(t, resyncCache(ctx, svc))
}
