package diagnostics

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOfflineReadersReturnBoundedFilteredEvidenceAndCorruptionWarnings(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	r := openTestRecorder(t, root, Options{Now: func() time.Time { return now }})
	ctx := operationContext("index", now.Add(-time.Second))
	op := OperationFromContext(ctx)
	op.TraceID = "shared-trace"
	ctx = WithOperation(ctx, op)
	for i := 0; i < 10; i++ {
		now = now.Add(time.Second)
		r.Event(ctx, slog.LevelInfo, "indexing", "step", "", slog.Int("count", i))
	}
	now = now.Add(time.Second)
	r.Event(ctx, slog.LevelWarn, "writer", "retry", "")
	require.NoError(t, r.PublishReport(ctx, Report{Status: "error", ReasonCode: "synthetic_error"}))
	require.NoError(t, r.Close())
	files, err := os.ReadDir(filepath.Join(r.dir, "events"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	path := filepath.Join(r.dir, "events", files[0].Name())
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = file.WriteString("{broken}\n{\"partial\":")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	result, err := ReadEvents(root, Filter{TraceID: op.TraceID, Limit: 3, Since: now.Add(-time.Hour)})
	require.NoError(t, err)
	require.Len(t, result.Events, 3)
	require.True(t, result.Coverage.Truncated)
	require.Equal(t, "retry", result.Events[2].Name)
	require.Equal(t, float64(8), result.Events[0].Attributes["count"])
	require.Contains(t, strings.Join(result.Coverage.Warnings, " "), "malformed event")
	require.Contains(t, strings.Join(result.Coverage.Warnings, " "), "earlier coverage is unknown")
	filtered, err := ReadEvents(root, Filter{Level: slog.LevelWarn, Subsystem: "writer", OperationID: op.ID})
	require.NoError(t, err)
	require.Len(t, filtered.Events, 1)
	reports, err := ReadReports(root, Filter{Kind: "index", Status: "error", TraceID: op.TraceID})
	require.NoError(t, err)
	require.Len(t, reports.Reports, 1)
	require.Equal(t, op.ID, reports.Reports[0].OperationID)
	limited, err := ReadEvents(root, Filter{MaxBytes: 200})
	require.NoError(t, err)
	require.True(t, limited.Coverage.Truncated)
	require.LessOrEqual(t, limited.Coverage.Bytes, int64(200))
}

func TestOversizedMalformedEventDoesNotHideFollowingValidRecord(t *testing.T) {
	root := t.TempDir()
	r := openTestRecorder(t, root, Options{})
	r.Event(context.Background(), slog.LevelInfo, "test", "kept", "")
	require.NoError(t, r.Close())
	entries, err := os.ReadDir(filepath.Join(r.dir, "events"))
	require.NoError(t, err)
	path := filepath.Join(r.dir, "events", entries[0].Name())
	valid, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append([]byte(strings.Repeat("x", DefaultMaxEventBytes*2)+"\n"), valid...), 0o600))
	result, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	require.Contains(t, strings.Join(result.Coverage.Warnings, " "), "oversized event record omitted")
}

func TestMalformedReportsAndSymlinksRemainInert(t *testing.T) {
	root := t.TempDir()
	r := openTestRecorder(t, root, Options{})
	require.NoError(t, os.WriteFile(filepath.Join(r.dir, "reports", "20261004T120000000000000Z-malformed.json"), []byte("{bad"), 0o600))
	outside := filepath.Join(t.TempDir(), "user.json")
	require.NoError(t, os.WriteFile(outside, []byte("user-owned"), 0o600))
	link := filepath.Join(r.dir, "reports", "20261004T120000000000000Z-link.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlink creation unavailable")
	}
	result, err := ReadReports(root, Filter{})
	require.NoError(t, err)
	require.Empty(t, result.Reports)
	require.Contains(t, strings.Join(result.Coverage.Warnings, " "), "malformed diagnostics report")
	require.NoError(t, os.Symlink(outside, filepath.Join(r.dir, "latest-index.json")))
	err = r.PublishReport(operationContext("index", time.Now()), Report{Status: "success"})
	require.Error(t, err)
	data, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "user-owned", string(data))
	r.Event(context.Background(), slog.LevelInfo, "test", "prune", "")
	_, err = os.Lstat(link)
	require.NoError(t, err)
}

func TestMissingArtifactsAndUnsupportedSchemasAreExplicit(t *testing.T) {
	root := t.TempDir()
	_, err := ReadLatest(root, "index")
	require.ErrorIs(t, err, ErrNotFound)
	result, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.NotEmpty(t, result.Coverage.Warnings)
	require.NoDirExists(t, filepath.Join(root, ".rhizome"))
	r := openTestRecorder(t, root, Options{})
	event := Event{SchemaVersion: 99, Time: time.Now(), ProcessID: "synthetic", Level: "INFO"}
	data, err := json.Marshal(event)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(r.dir, "events", "20261004-synthetic-000001-4194304.jsonl"), append(data, '\n'), 0o600))
	result, err = ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.Empty(t, result.Events)
	require.Contains(t, strings.Join(result.Coverage.Warnings, " "), "unsupported event schema")
}

func TestLoadOptionsOnlyUsesRepositoryDiagnosticsSection(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o700))
	config := []byte("unrelated:\n  apiKey: synthetic-not-a-live-key\ndiagnostics:\n  enabled: false\n  retentionDays: 3\n  maxBytes: 1048576\n  level: warn\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), config, 0o600))
	opts, err := LoadOptions(root)
	require.NoError(t, err)
	require.True(t, opts.Disabled)
	require.Equal(t, 3, opts.RetentionDays)
	require.Equal(t, int64(1048576), opts.MaxBytes)
	require.Equal(t, slog.LevelWarn, opts.Level)
	opts.Stderr = io.Discard
	r, err := Open(root, opts)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	require.NoDirExists(t, filepath.Join(root, ".rhizome", "diagnostics"))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("diagnostics:\n  level: synthetic-not-a-live-key\n"), 0o600))
	_, err = LoadOptions(root)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "synthetic-not-a-live-key")
	defaults, err := LoadOptions(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, 7, defaults.RetentionDays)
	require.Equal(t, DefaultMaxBytes, defaults.MaxBytes)
}
