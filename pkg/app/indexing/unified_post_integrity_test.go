package indexing

import (
	"bytes"
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/stretchr/testify/require"
)

func TestIntegrityFailureRecordsWarnEventWithTable(t *testing.T) {
	for _, result := range []string{
		"malformed inverted index for FTS5 table main.intel_fts",
		`fts5: corruption found reading blob 412316860436 from table "intel_fts"`,
	} {
		t.Run(result, func(t *testing.T) { requireIntegrityWarn(t, result) })
	}
}

func requireIntegrityWarn(t *testing.T, result string) {
	root := t.TempDir()
	var stderr bytes.Buffer
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: &stderr})
	require.NoError(t, err)
	ctx := diagnostics.WithRecorder(context.Background(), recorder)

	reportIntegrityFailure(ctx, sqliteutil.IntegrityCheckFailed{Result: result})
	require.NoError(t, recorder.Close())

	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{Subsystem: "indexing"})
	require.NoError(t, err)
	require.Len(t, events.Events, 1)
	event := events.Events[0]
	require.Equal(t, "WARN", event.Level)
	require.Equal(t, "integrity.failed", event.Name)
	require.Equal(t, "intel_fts", event.Attributes["table"])
	require.Equal(t, 1, bytes.Count(stderr.Bytes(), []byte("integrity.failed")), "the recorder mirrors the warning once")
}
