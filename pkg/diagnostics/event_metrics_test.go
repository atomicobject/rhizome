package diagnostics

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEventMetricsRoundTripWithRedaction(t *testing.T) {
	root := t.TempDir()
	r := openTestRecorder(t, root, Options{})
	metrics := json.RawMessage(`{"regions":{"sidebar":{"commits":3,"max_ms":4.5}},"api_key":"secret-value"}`)
	r.EventMetrics(t.Context(), slog.LevelInfo, "web.client", "web.client", "", metrics, slog.String("surface", "desktop"))

	result, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	event := result.Events[0]
	require.Equal(t, "desktop", event.Attributes["surface"])
	require.True(t, event.Truncated, "redaction marks the event truncated")
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(event.Metrics, &decoded))
	require.Equal(t, "[redacted]", decoded["api_key"])
	require.Equal(t, 3.0, decoded["regions"].(map[string]any)["sidebar"].(map[string]any)["commits"])
}

func TestEventMetricsDroppedWhenInvalidOrOversized(t *testing.T) {
	root := t.TempDir()
	r := openTestRecorder(t, root, Options{MaxEventBytes: 2048})
	deep := json.RawMessage(strings.Repeat("[", 40) + strings.Repeat("]", 40))
	large := json.RawMessage(`{"samples":"` + strings.Repeat("x", 4096) + `"}`)
	nearCap := json.RawMessage(`{"samples":"` + strings.Repeat("x", 1900) + `"}`)
	for _, metrics := range []json.RawMessage{json.RawMessage(`{"broken"`), deep, large, nearCap} {
		r.EventMetrics(t.Context(), slog.LevelInfo, "web.client", "web.client", "", metrics, slog.String("surface", "browser"))
	}

	result, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 4, "bad metrics never cost the event")
	for _, event := range result.Events {
		require.Nil(t, event.Metrics)
		require.True(t, event.Truncated)
		require.Equal(t, "browser", event.Attributes["surface"], "identity attributes survive dropped metrics")
	}
}
