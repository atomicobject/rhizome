//go:build !windows

package runtime

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStopCancelledDuringIntentReadLeavesStartupIntact(t *testing.T) {
	registry, vault := intentRegistry(t)
	path := registry.intentPath(vault)
	require.NoError(t, syscall.Mkfifo(path, 0o600))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	finished := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		// Also release a reader waiting to open the FIFO if an assertion
		// fails before the test connects its writer.
		require.Eventually(t, func() bool {
			select {
			case <-finished:
				return true
			default:
			}
			if fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0o600); err == nil {
				_ = syscall.Close(fd)
			}
			return false
		}, 5*time.Second, time.Millisecond, "intent reader must finish")
	})
	go func() {
		defer close(finished)
		_, _, err := registry.CancelIntent(ctx, vault, "starting")
		result <- err
	}()

	// Opening the write side succeeds only once CancelIntent has passed its
	// initial context check and reached the file read. Withhold the record
	// until cancellation, simulating a slow filesystem without a timing race.
	var writer *os.File
	require.Eventually(t, func() bool {
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0o600)
		if err != nil {
			return false
		}
		writer = os.NewFile(uintptr(fd), path)
		return true
	}, 5*time.Second, time.Millisecond)
	defer writer.Close()
	cancel()
	// Exceed the pipe buffer so the write waits for the reader to consume
	// data before we close it; macOS can otherwise miss a brief connection.
	_, err := writer.WriteString(strings.Repeat(" ", 1<<20))
	require.NoError(t, err)
	require.NoError(t, json.NewEncoder(writer).Encode(StartIntent{VaultPath: vault, Token: "starting"}))
	require.NoError(t, writer.Close())
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("canceled stop did not finish after the intent read completed")
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeNamedPipe, "the canceled stop must not replace the intent with a cancelled record")
}
