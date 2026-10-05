package claude

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/eventstream"
	"github.com/stretchr/testify/require"
)

func TestClaudeBlockedUserMessageWriteObservesCallerCancellation(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	t.Cleanup(func() {
		_ = inputReader.Close()
		_ = outputWriter.Close()
	})
	transport := newJSONTransport(outputReader, inputWriter, inputWriter)
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	session := &session{
		transport: transport, stream: eventstream.New(), lifetime: lifetime, cancel: cancelLifetime,
		interruptTimeout: 10 * time.Millisecond, cancelTimeout: 10 * time.Millisecond, shutdownTimeout: 10 * time.Millisecond,
		responses: make(map[string]chan message), responseBacklog: make(map[string]message),
		pending: make(map[string]pendingApproval), items: make(map[string]harness.EventKind), interactive: true,
	}
	session.reader.Add(1)
	go session.readLoop()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := session.SendTurn(ctx, "blocked")
	require.Less(t, time.Since(started), 500*time.Millisecond)
	require.True(t, errorsIsAny(err, harness.ErrTimeout, harness.ErrTransportClosed), "%v", err)
	require.ErrorIs(t, session.SendTurn(context.Background(), "later"), harness.ErrTransportClosed)
	require.NoError(t, session.Stop())
}

func errorsIsAny(err error, targets ...error) bool {
	for _, target := range targets {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
