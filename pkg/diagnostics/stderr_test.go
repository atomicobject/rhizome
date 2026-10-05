package diagnostics

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetStderrRestoresConsoleDestination(t *testing.T) {
	var original, capture bytes.Buffer
	recorder, err := Open(t.TempDir(), Options{Disabled: true, Stderr: &original})
	require.NoError(t, err)
	defer recorder.Close()
	restore := recorder.SetStderr(&capture)
	recorder.Event(context.Background(), slog.LevelWarn, "runtime", "captured", "")
	restore()
	restore()
	recorder.Event(context.Background(), slog.LevelWarn, "runtime", "restored", "")
	require.Contains(t, capture.String(), "captured")
	require.NotContains(t, capture.String(), "restored")
	require.Contains(t, original.String(), "restored")
	require.NotContains(t, original.String(), "captured")
}
