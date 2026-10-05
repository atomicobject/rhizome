package cmd

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticAgentRequestsRetainErrorsWithoutProtocolNoise(t *testing.T) {
	for _, test := range []struct {
		name, status, reason string
		err                  error
		cancel               bool
	}{
		{name: "forwarding failure", status: "error", reason: "not_found", err: os.ErrNotExist},
		{name: "structured error", status: "error", reason: "structured_error"},
		{name: "canceled", status: "canceled", reason: "canceled", cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			var stderr bytes.Buffer
			recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &stderr})
			require.NoError(t, err)
			parent := diagnostics.NewOperation("command", "mcp")
			ctx := diagnostics.WithOperation(diagnostics.WithRecorder(context.Background(), recorder), parent)
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			_, finish := observeAgentRequest(ctx, "files", "mcp_stdio")
			if test.cancel {
				cancel()
			}
			finish(false, test.err)
			require.NoError(t, recorder.Close())
			require.Empty(t, stderr.String())
			events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
			require.NoError(t, err)
			require.Len(t, events.Events, 2)
			terminal := events.Events[1]
			require.Equal(t, test.status, terminal.Attributes["status"])
			require.Equal(t, test.reason, terminal.Attributes["reason_code"])
			require.Equal(t, parent.ID, terminal.ParentOperationID)
			require.Equal(t, parent.TraceID, terminal.TraceID)
		})
	}
}
