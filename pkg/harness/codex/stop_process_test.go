package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

func TestStopDeadlineKillsChildBlockedOnApprovalWrites(t *testing.T) {
	binary, err := os.Executable()
	require.NoError(t, err)
	connection, err := startStdio(context.Background(), binary, []string{"-test.run=TestCodexNeverReadsHelper", "--", "codex-never-read"}, t.TempDir())
	require.NoError(t, err)
	transport := connection.(*stdioTransport)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
		defer cancel()
		_ = transport.Close(ctx)
	})
	readyCtx, cancelReady := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelReady()
	ready, err := transport.Recv(readyCtx)
	require.NoError(t, err)
	require.Equal(t, "fixture/ready", ready.Method)
	const deadline = defaultShutdownTimeout
	session := newSession(transport, harness.SessionOptions{}, t.TempDir(), time.Second, deadline, true)
	session.stateMu.Lock()
	for i := 0; i < 4; i++ {
		id := json.RawMessage(`"` + strings.Repeat(string(rune('a'+i)), 1<<20) + `"`)
		session.pending[string(id)] = pendingApproval{id: id, method: "item/commandExecution/requestApproval"}
	}
	session.stateMu.Unlock()

	started := time.Now()
	require.NoError(t, session.Stop())
	require.Less(t, time.Since(started), deadline+150*time.Millisecond)
	select {
	case <-transport.Exited():
	default:
		t.Fatal("child process still running after Stop returned")
	}
}

func TestCodexNeverReadsHelper(t *testing.T) {
	if !slices.Contains(os.Args, "codex-never-read") {
		return
	}
	fmt.Fprintln(os.Stdout, `{"method":"fixture/ready"}`)
	for {
		time.Sleep(time.Hour)
	}
}
