//go:build !windows

package agentcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExecuteCancellationReapsProcessDescendants(t *testing.T) {
	requireNode(t)
	pidFile := filepath.Join(t.TempDir(), "descendant.pid")
	fake := writeExecuteServer(t, `
const child = spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], {stdio:"ignore"});
writeFileSync(frame.params.input.pidFile, String(child.pid));`)
	options := executeOptions(t, fake, `await rzm.files({pidFile:`+string(mustJSON(t, pidFile))+`});`)
	options.Timeout = 30 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result ExecuteResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := Execute(ctx, options)
		done <- outcome{result, err}
	}()
	// Interrupt only after the descendant exists, independent of Node startup time.
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(pidFile)
		return err == nil && len(data) > 0
	}, 10*time.Second, 20*time.Millisecond)
	cancel()
	select {
	case out := <-done:
		require.NoError(t, out.err)
		require.NotNil(t, out.result.Error)
		require.Equal(t, "cancelled", out.result.Error.Code)
	case <-time.After(5 * time.Second):
		t.Fatal("execution did not stop after cancellation")
	}
	data, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	pid, err := strconv.Atoi(string(data))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		err := syscall.Kill(pid, 0)
		return errors.Is(err, syscall.ESRCH)
	}, 2*time.Second, 20*time.Millisecond)
}
