package logging

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/stretchr/testify/require"
)

func TestQuietCompletionRetainsFailureAndIndependentWarnings(t *testing.T) {
	root := t.TempDir()
	var stderr bytes.Buffer
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &stderr})
	require.NoError(t, err)
	ctx := diagnostics.WithRecorder(context.Background(), recorder)
	op := diagnostics.NewOperation("tool", "files")
	CompleteEventQuiet(ctx, op, "error", "not_found", map[string]any{"content_blocks": 1})
	require.Empty(t, stderr.String())
	recorder.Event(ctx, slog.LevelWarn, "storage", "storage.warning", "actionable warning")
	require.Contains(t, stderr.String(), "actionable warning")
	require.NoError(t, recorder.Close())
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Len(t, events.Events, 2)
	require.Equal(t, "ERROR", events.Events[0].Level)
	require.Equal(t, "error", events.Events[0].Attributes["status"])
	require.Equal(t, "not_found", events.Events[0].Attributes["reason_code"])
	require.Contains(t, events.Events[0].Attributes, "duration_ms")
	reports, err := diagnostics.ReadReports(root, diagnostics.Filter{})
	require.NoError(t, err)
	require.Empty(t, reports.Reports)
}
