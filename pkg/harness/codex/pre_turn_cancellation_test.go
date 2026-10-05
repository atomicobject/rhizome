package codex

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

func TestCodexBlockedPreTurnWritesObserveCallerCancellation(t *testing.T) {
	for _, test := range []struct {
		name    string
		options harness.SessionOptions
	}{
		{name: "turn start"},
		{name: "MCP reload", options: harness.SessionOptions{MCPServers: []harness.MCPServer{{Name: "local", Command: "rzm"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			inputReader, inputWriter := io.Pipe()
			outputReader, outputWriter := io.Pipe()
			t.Cleanup(func() {
				_ = inputReader.Close()
				_ = outputWriter.Close()
			})
			transport := newJSONTransport(outputReader, inputWriter, inputWriter)
			session := newSession(transport, test.options, "/tmp/project", 10*time.Millisecond, 10*time.Millisecond, true)
			session.stateMu.Lock()
			session.threadID = "thread-1"
			session.stateMu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			started := time.Now()
			err := session.SendTurn(ctx, "blocked")
			require.Less(t, time.Since(started), 500*time.Millisecond)
			require.True(t, errors.Is(err, harness.ErrTimeout) || errors.Is(err, harness.ErrTransportClosed), "%v", err)
			require.ErrorIs(t, session.SendTurn(context.Background(), "later"), harness.ErrTransportClosed)
			require.NoError(t, session.Stop())
		})
	}
}
