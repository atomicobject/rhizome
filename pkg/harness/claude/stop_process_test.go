package claude

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/eventstream"
	"github.com/stretchr/testify/require"
)

func TestStopDeadlineKillsChildBlockedOnApprovalWrites(t *testing.T) {
	binary, err := os.Executable()
	require.NoError(t, err)
	connection, err := startStdio(context.Background(), binary, []string{"-test.run=TestClaudeNeverReadsHelper", "--", "claude-never-read"}, t.TempDir())
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
	require.Equal(t, "fixture_ready", ready.Type)
	lifetime, cancel := context.WithCancel(context.Background())
	const deadline = defaultShutdownTimeout
	session := &session{
		transport: transport, stream: eventstream.New(), lifetime: lifetime, cancel: cancel,
		shutdownTimeout: deadline, responses: make(map[string]chan message), responseBacklog: make(map[string]message),
		pending: make(map[string]pendingApproval), items: make(map[string]harness.EventKind), interactive: true,
	}
	for i := 0; i < 4; i++ {
		id := strings.Repeat(string(rune('a'+i)), 1<<20)
		session.pending[id] = pendingApproval{request: controlRequest{Subtype: "can_use_tool"}}
	}
	session.reader.Add(1)
	go session.readLoop()

	started := time.Now()
	require.NoError(t, session.Stop())
	require.Less(t, time.Since(started), deadline+150*time.Millisecond)
	select {
	case <-transport.Exited():
	default:
		t.Fatal("child process still running after Stop returned")
	}
}

func TestClaudeNeverReadsHelper(t *testing.T) {
	if !slices.Contains(os.Args, "claude-never-read") {
		return
	}
	fmt.Fprintln(os.Stdout, `{"type":"fixture_ready"}`)
	for {
		time.Sleep(time.Hour)
	}
}
