package jsonstdio

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloseBoundsProcessDiscovery(t *testing.T) {
	connection, _ := startChild(t, "blocked-write")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := connection.Recv(ctx)
	require.NoError(t, err)
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "ps"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelClose()
	started := time.Now()
	err = connection.Close(closeCtx)
	require.Less(t, time.Since(started), 450*time.Millisecond, "process discovery must reserve time to kill and reap the child")
	require.NoError(t, err)
	select {
	case <-connection.Exited():
	default:
		t.Fatal("Close returned before reaping the child")
	}
}
