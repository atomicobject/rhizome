package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func openTestRecorder(t *testing.T, root string, opts Options) *Recorder {
	t.Helper()
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	r, err := Open(root, opts)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	return r
}

func operationContext(kind string, at time.Time) context.Context {
	op := NewOperation(kind, "test")
	op.StartedAt = at
	return WithOperation(context.Background(), op)
}

func TestDisabledRecorderDoesNotCreateArtifacts(t *testing.T) {
	root := t.TempDir()
	var stderr bytes.Buffer
	r := openTestRecorder(t, root, Options{Disabled: true, Stderr: &stderr})
	r.Event(context.Background(), slog.LevelWarn, "indexing", "warning", "synthetic warning")
	require.NoError(t, r.PublishReport(context.Background(), Report{}))
	require.NoDirExists(t, filepath.Join(root, ".rhizome"))
	require.Contains(t, stderr.String(), "synthetic warning")
	r.Logger("test").Warn("visible logger warning")
	require.Contains(t, stderr.String(), "visible logger warning")
}

func TestPersistedLevelDoesNotHideConsoleWarnings(t *testing.T) {
	root := t.TempDir()
	var stderr bytes.Buffer
	r := openTestRecorder(t, root, Options{Level: slog.LevelError, Stderr: &stderr})
	r.Event(nil, slog.LevelWarn, "indexing", "warning", "visible warning")
	r.Event(nil, slog.LevelError, "indexing", "error", "visible error")
	require.Contains(t, stderr.String(), "visible warning")
	require.Contains(t, stderr.String(), "visible error")
	result, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	require.Equal(t, "ERROR", result.Events[0].Level)
}

func TestSkippedCapabilityReportRetainsReasonWithoutWarning(t *testing.T) {
	root := t.TempDir()
	var stderr bytes.Buffer
	r := openTestRecorder(t, root, Options{Stderr: &stderr})
	ctx := operationContext("runtime.capability", time.Now())
	require.NoError(t, r.PublishReport(ctx, Report{Status: "skipped", ReasonCode: "embeddings_disabled"}))
	result, err := ReadReports(root, Filter{Status: "skipped"})
	require.NoError(t, err)
	require.Len(t, result.Reports, 1)
	require.Equal(t, "embeddings_disabled", result.Reports[0].ReasonCode)
	require.Empty(t, stderr.String())
}

func TestQuietErrorRemainsInHistoryWithoutSuppressingOtherWarnings(t *testing.T) {
	root := t.TempDir()
	var stderr bytes.Buffer
	r := openTestRecorder(t, root, Options{Stderr: &stderr})
	r.EventQuiet(context.Background(), slog.LevelError, "command", "command.failed", "protocol owns this error")
	require.Empty(t, stderr.String())
	r.Event(context.Background(), slog.LevelWarn, "runtime", "warning", "separate warning")
	require.Contains(t, stderr.String(), "separate warning")
	result, err := ReadEvents(root, Filter{Level: slog.LevelError})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	require.Equal(t, "command.failed", result.Events[0].Name)
	require.Equal(t, "ERROR", result.Events[0].Level)
}

func TestEventsPreserveIdentityAndMirrorOnlyWarnings(t *testing.T) {
	root := t.TempDir()
	var stderr bytes.Buffer
	r := openTestRecorder(t, root, Options{Stderr: &stderr, Role: "test", Version: "synthetic"})
	ctx := WithRecorder(operationContext("index", time.Now()), r)
	op := OperationFromContext(ctx)
	r.Event(ctx, slog.LevelInfo, "indexing", "phase.finished", "ordinary status", slog.Int("notes", 12))
	r.Event(ctx, slog.LevelWarn, "provider", "retry", "synthetic retry", slog.String("api_key", "synthetic-secret"), slog.String("content", "synthetic body"), slog.Int("tokens", 42))
	r.Logger("writer").WarnContext(ctx, "busy", slog.String("event", "write.retry"), slog.Int("attempt", 2))
	log.New(r.StdlogWriter("legacy"), "", 0).Println("warning: synthetic failure")
	require.NoError(t, r.Close())
	result, err := ReadEvents(root, Filter{OperationID: op.ID})
	require.NoError(t, err)
	require.Len(t, result.Events, 3)
	require.Equal(t, "phase.finished", result.Events[0].Name)
	require.Equal(t, op.TraceID, result.Events[0].TraceID)
	require.Equal(t, "[redacted]", result.Events[1].Attributes["api_key"])
	require.Equal(t, "[redacted]", result.Events[1].Attributes["content"])
	require.Equal(t, float64(42), result.Events[1].Attributes["tokens"])
	require.NotContains(t, stderr.String(), "ordinary status")
	require.Contains(t, stderr.String(), "synthetic retry")
	all, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.Len(t, all.Events, 4)
	require.Equal(t, "WARN", all.Events[3].Level)
}

func TestOfflineReaderCanReadAStillActiveWriterSegment(t *testing.T) {
	root := t.TempDir()
	r := openTestRecorder(t, root, Options{})
	r.Event(context.Background(), slog.LevelInfo, "runtime", "started", "")
	first, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.Len(t, first.Events, 1)
	require.Empty(t, first.Coverage.Warnings, "an ownership lock must not lock readable event bytes")
	r.Event(context.Background(), slog.LevelInfo, "runtime", "ready", "")
	second, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.Len(t, second.Events, 2)
	require.Equal(t, "ready", second.Events[1].Name)
}

func TestReportOutcomeAndLatestSurviveRetention(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	r := openTestRecorder(t, root, Options{Now: func() time.Time { return now }, RetentionDays: 7})
	ctx := operationContext("index", now.Add(-time.Second))
	r.Event(ctx, slog.LevelInfo, "indexing", "operation.started", "")
	require.NoError(t, r.PublishReport(ctx, Report{Status: "error", ReasonCode: "source_read_failed", Error: "synthetic error", QueueWaitMS: 3, LockWaitMS: 4, ExecutionMS: 993, Metrics: json.RawMessage(`{"phases":[{"name":"discovery","count":4}]}`)}))
	require.NoError(t, r.Close())
	latest, err := ReadLatest(root, "index")
	require.NoError(t, err)
	require.Equal(t, "error", latest.Status)
	require.Equal(t, int64(1000), latest.DurationMS)
	old := now.Add(-8 * 24 * time.Hour)
	require.NoError(t, filepath.WalkDir(r.dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		return os.Chtimes(path, old, old)
	}))
	next := openTestRecorder(t, root, Options{Now: func() time.Time { return now }})
	next.Event(context.Background(), slog.LevelInfo, "watcher", "batch.started", "")
	require.NoError(t, next.PublishReport(operationContext("watcher", now), Report{Status: "success"}))
	latestAfter, err := ReadLatest(root, "index")
	require.NoError(t, err)
	require.Equal(t, latest.OperationID, latestAfter.OperationID)
	byID, err := ReadReport(root, latest.OperationID)
	require.NoError(t, err)
	require.Equal(t, "error", byID.Status)
	history, err := ReadReports(root, Filter{Kind: "index"})
	require.NoError(t, err)
	require.Empty(t, history.Reports)
	entries, err := os.ReadDir(filepath.Join(r.dir, "events"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestOlderCompletionCannotReplaceLatest(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	r := openTestRecorder(t, root, Options{})
	newer := operationContext("index", now)
	require.NoError(t, r.PublishReport(newer, Report{Status: "canceled", FinishedAt: now.Add(time.Minute)}))
	require.NoError(t, r.PublishReport(operationContext("index", now.Add(-time.Hour)), Report{Status: "success", FinishedAt: now}))
	latest, err := ReadLatest(root, "index")
	require.NoError(t, err)
	require.Equal(t, OperationFromContext(newer).ID, latest.OperationID)
}

func TestQuotaReservationsPreserveLatestAndUnknownFiles(t *testing.T) {
	root := t.TempDir()
	opts := Options{MaxBytes: 8192, MaxSegmentBytes: 1024, MaxEventBytes: 1024, MaxReportBytes: 1024}
	r := openTestRecorder(t, root, opts)
	ctx := operationContext("index", time.Now())
	require.NoError(t, r.PublishReport(ctx, Report{Status: "success"}))
	unknown := filepath.Join(r.dir, "events", "user.log")
	require.NoError(t, os.WriteFile(unknown, []byte("user content"), 0o600))
	for i := 0; i < 12; i++ {
		peer := openTestRecorder(t, root, opts)
		peer.Event(context.Background(), slog.LevelInfo, "test", "reserved", strings.Repeat("x", 100))
	}
	files, err := r.scanLocked()
	require.NoError(t, err)
	var reserved int64
	for _, file := range files {
		reserved += file.size
	}
	require.LessOrEqual(t, reserved, opts.MaxBytes)
	err = r.PublishReport(operationContext("index", time.Now()), Report{Status: "error", Error: strings.Repeat("e", 300)})
	require.NoError(t, err, "event reservations must leave report publication headroom")
	latest, err := ReadLatest(root, "index")
	require.NoError(t, err)
	require.Equal(t, "error", latest.Status)
	data, err := os.ReadFile(unknown)
	require.NoError(t, err)
	require.Equal(t, "user content", string(data))
}

func TestBoundedEventsReportsAndSensitiveMetrics(t *testing.T) {
	root := t.TempDir()
	r := openTestRecorder(t, root, Options{MaxEventBytes: 1024, MaxReportBytes: 4096})
	r.Event(context.Background(), slog.LevelInfo, "test", "large", strings.Repeat("x", 1<<20), slog.String("payload", strings.Repeat("y", 1<<20)))
	ctx := operationContext("index", time.Now())
	require.NoError(t, r.PublishReport(ctx, Report{Status: "success", Metrics: json.RawMessage(`{"count":9007199254740993,"api_key":"synthetic-secret","body":"synthetic body","phase":"discovery"}`)}))
	latest, err := ReadLatest(root, "index")
	require.NoError(t, err)
	require.Contains(t, string(latest.Metrics), "9007199254740993")
	require.NotContains(t, string(latest.Metrics), "synthetic-secret")
	require.NotContains(t, string(latest.Metrics), "synthetic body")
	events, err := ReadEvents(root, Filter{})
	require.NoError(t, err)
	require.Len(t, events.Events, 1)
	require.True(t, events.Events[0].Truncated)
	require.Empty(t, events.Events[0].Message)
	before := latest.OperationID
	err = r.PublishReport(operationContext("index", time.Now()), Report{Status: "error", Metrics: json.RawMessage(`{"bad":`)})
	require.Error(t, err)
	latest, err = ReadLatest(root, "index")
	require.NoError(t, err)
	require.Equal(t, before, latest.OperationID)
}

func TestMetricsRetainLargeTypedShape(t *testing.T) {
	root := t.TempDir()
	r := openTestRecorder(t, root, Options{})
	series := make([]map[string]any, 960)
	for i := range series {
		series[i] = map[string]any{"name": "provider.latency", "phase": "embed_notes", "count": i, "total": i * 2, "max": i * 3, "p50": 10, "p95": 20}
	}
	metrics, err := json.Marshal(map[string]any{"metrics": series})
	require.NoError(t, err)
	require.NoError(t, r.PublishReport(operationContext("index", time.Now()), Report{Status: "success", Metrics: metrics}))
	report, err := ReadLatest(root, "index")
	require.NoError(t, err)
	require.False(t, report.Truncated)
	var output struct {
		Metrics []map[string]any `json:"metrics"`
	}
	require.NoError(t, json.Unmarshal(report.Metrics, &output))
	require.Len(t, output.Metrics, 960)
	require.Equal(t, float64(959), output.Metrics[959]["count"])
}

func TestPropagateKeepsLaneCancellationAndOnlyDiagnosticValues(t *testing.T) {
	type privateKey struct{}
	r := openTestRecorder(t, t.TempDir(), Options{Disabled: true})
	from, cancelFrom := context.WithCancel(context.WithValue(WithRecorder(operationContext("watcher", time.Now()), r), privateKey{}, true))
	cancelFrom()
	onto, cancelOnto := context.WithCancel(context.Background())
	result := Propagate(from, onto)
	require.NoError(t, result.Err())
	require.Nil(t, result.Value(privateKey{}))
	require.Same(t, r, FromContext(result))
	require.Equal(t, OperationFromContext(from), OperationFromContext(result))
	cancelOnto()
	require.ErrorIs(t, result.Err(), context.Canceled)
}

func TestConcurrentRecorderUseAndClose(t *testing.T) {
	r := openTestRecorder(t, t.TempDir(), Options{})
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				r.Event(context.Background(), slog.LevelInfo, "test", "concurrent", "")
			}
		}()
	}
	wg.Add(1)
	go func() { defer wg.Done(); require.NoError(t, r.Close()) }()
	wg.Wait()
	require.NoError(t, r.Close())
	require.True(t, errors.Is(r.PublishReport(context.Background(), Report{}), ErrClosed))
}
