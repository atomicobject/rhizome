package mcp

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticToolRecordsStructuredFailureWithoutPayload(t *testing.T) {
	root := t.TempDir()
	var console bytes.Buffer
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &console})
	require.NoError(t, err)
	parent := diagnostics.NewOperation("command", "agent files")
	ctx := diagnostics.WithOperation(diagnostics.WithRecorder(context.Background(), recorder), parent)
	observed := ObserveTool("files", Config{}, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultError("PRIVATE_REQUEST secret-token"), nil
	})
	result, err := observed(ctx, mcpgo.CallToolRequest{Params: mcpgo.CallToolParams{Arguments: map[string]any{"query": "PRIVATE_QUERY"}}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.NoError(t, recorder.Close())
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, events.Events, 2)
	event := events.Events[1]
	require.Equal(t, "error", event.Attributes["status"])
	require.Equal(t, "tool_error", event.Attributes["reason_code"])
	require.Equal(t, parent.ID, event.ParentOperationID)
	require.Equal(t, parent.TraceID, event.TraceID)
	require.Equal(t, true, event.Attributes["structured_error"])
	reports, err := diagnostics.ReadReports(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Empty(t, reports.Reports)
	require.Empty(t, console.String(), "tool errors are owned by the structured response")
	require.Equal(t, "ERROR", event.Level)
	require.NotContains(t, console.String(), "PRIVATE")
	events, err = diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, events.Events, 2)
	for _, event := range events.Events {
		require.NotContains(t, event.Message, "PRIVATE")
	}
}

func TestDiagnosticToolPanicNeverRecordsSuccess(t *testing.T) {
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{})
	require.NoError(t, err)
	observed := ObserveTool("files", Config{}, func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) { panic("PRIVATE_PANIC") })
	require.Panics(t, func() {
		_, _ = observed(diagnostics.WithRecorder(context.Background(), recorder), mcpgo.CallToolRequest{})
	})
	require.NoError(t, recorder.Close())
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, events.Events, 2)
	require.Equal(t, "error", events.Events[1].Attributes["status"])
	require.Equal(t, "handler_panicked", events.Events[1].Attributes["reason_code"])
	require.NotContains(t, events.Events[1].Message, "PRIVATE")
}

func TestDiagnosticToolRecordsWrappedChildCancellation(t *testing.T) {
	root := t.TempDir()
	var console bytes.Buffer
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &console})
	require.NoError(t, err)
	observed := ObserveTool("files", Config{}, func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return nil, fmt.Errorf("child: %w", context.Canceled)
	})
	_, err = observed(diagnostics.WithRecorder(context.Background(), recorder), mcpgo.CallToolRequest{})
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, recorder.Close())
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Equal(t, "canceled", events.Events[1].Attributes["status"])
	require.Equal(t, "canceled", events.Events[1].Attributes["reason_code"])
	require.Empty(t, console.String())
}
