package diagnostics

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	evidence "github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/stretchr/testify/require"
)

// perfFixture writes events through a real recorder with a controlled clock.
type perfFixture struct {
	t        *testing.T
	recorder *evidence.Recorder
	now      time.Time
}

func newPerfFixture(t *testing.T, root string, start time.Time) *perfFixture {
	f := &perfFixture{t: t, now: start}
	recorder, err := evidence.Open(root, evidence.Options{Stderr: io.Discard, Now: func() time.Time { return f.now }})
	require.NoError(t, err)
	f.recorder = recorder
	return f
}

func (f *perfFixture) at(offset time.Duration) *perfFixture {
	f.now = f.now.Add(offset)
	return f
}

func (f *perfFixture) finished(kind, trigger, status string, durationMS int64, extra ...slog.Attr) string {
	op := evidence.NewOperation(kind, trigger)
	attrs := append([]slog.Attr{slog.String("kind", kind), slog.String("trigger", trigger), slog.String("status", status), slog.Int64("duration_ms", durationMS)}, extra...)
	f.recorder.Event(evidence.WithOperation(context.Background(), op), slog.LevelInfo, kind, kind+".finished", "", attrs...)
	return op.ID
}

func (f *perfFixture) event(level slog.Level, subsystem, name string, op evidence.Operation, attrs ...slog.Attr) {
	f.recorder.Event(evidence.WithOperation(context.Background(), op), level, subsystem, name, "", attrs...)
}

func readPerf(t *testing.T, root string, now time.Time, since string) Result {
	result, err := Read(Options{Vault: root, Mode: "perf", Now: now, Since: since})
	require.NoError(t, err)
	require.NotNil(t, result.Perf)
	return result
}

func flagsOfKind(summary PerfSummary, kind string) []PerfFlag {
	var flags []PerfFlag
	for _, flag := range summary.Flags {
		if flag.Kind == kind {
			flags = append(flags, flag)
		}
	}
	return flags
}

func TestPerfGroupsReportPercentilesOnlyWithEnoughSamples(t *testing.T) {
	root := t.TempDir()
	start := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	f := newPerfFixture(t, root, start)
	for i := int64(1); i <= 25; i++ {
		status := "success"
		if i == 25 {
			status = "error"
		}
		f.at(time.Second).finished("http.request", "/api/v1/notes", status, i)
	}
	for i := int64(1); i <= 5; i++ {
		f.at(time.Second).finished("tool", "search", "success", 100*i)
	}
	f.at(time.Second).finished("ignored", "no-duration", "success", 0)
	op := evidence.NewOperation("indexing-job", "watcher-batch")
	f.event(slog.LevelInfo, "indexing-lane", "job.finished", op, slog.String("job_kind", "watcher-batch"), slog.String("status", "success"),
		slog.Int64("duration_ms", 7), slog.Int64("queue_wait_ms", 2), slog.Int64("lock_wait_ms", 1))
	require.NoError(t, f.recorder.Close())

	summary := *readPerf(t, root, f.now, "1h").Perf
	groups := map[string]PerfGroup{}
	for _, group := range summary.Groups {
		groups[group.Kind+" "+group.Name] = group
	}
	notes := groups["http.request /api/v1/notes"]
	require.Equal(t, 25, notes.Count)
	require.Equal(t, 1, notes.Errors)
	require.Equal(t, int64(13), notes.MedianMS)
	require.NotNil(t, notes.P95MS)
	require.Equal(t, int64(24), *notes.P95MS)
	require.Equal(t, int64(25), notes.MaxMS)

	search := groups["tool search"]
	require.Equal(t, 5, search.Count)
	require.Nil(t, search.P95MS, "five samples do not support a p95")
	require.Equal(t, summary.Groups[0], search, "groups sort by total time")

	job := groups["indexing-job watcher-batch"]
	require.Equal(t, 1, job.Count)
	require.Equal(t, &WaitSummary{MedianMS: 2, MaxMS: 2}, job.QueueWait)
	require.Equal(t, &WaitSummary{MedianMS: 1, MaxMS: 1}, job.LockWait)
}

func TestPerfFlagsRuntimeBurstsUnfinishedWorkAndIntegrity(t *testing.T) {
	root := t.TempDir()
	start := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	f := newPerfFixture(t, root, start)
	for range 5 {
		f.at(5*time.Second).finished("runtime.serve", "start", "success", 200)
	}
	f.at(10*time.Minute).finished("runtime.serve", "start", "success", 200)
	f.at(5*time.Minute).finished("runtime.serve", "start", "success", 200)

	hung := evidence.NewOperation("command", "rzm search")
	f.at(time.Second).event(slog.LevelInfo, "command", "command.started", hung)
	done := evidence.NewOperation("command", "rzm index")
	f.event(slog.LevelInfo, "command", "command.started", done)
	f.at(time.Second).event(slog.LevelInfo, "command", "operation.finished", done, slog.Int64("duration_ms", 1000), slog.String("kind", "command"), slog.String("trigger", "rzm index"))
	f.at(2*time.Minute).event(slog.LevelWarn, "indexing", "index.integrity_check_failed", evidence.NewOperation("index", "post"), slog.String("table", "intel_fts"))
	recent := evidence.NewOperation("command", "rzm serve")
	f.at(10*time.Second).event(slog.LevelInfo, "command", "command.started", recent)
	f.at(10*time.Second).finished("http.request", "/api/v1/runtime", "success", 1)
	require.NoError(t, f.recorder.Close())

	summary := *readPerf(t, root, f.now, "2h").Perf
	bursts := flagsOfKind(summary, "runtime_start_burst")
	require.Len(t, bursts, 1, "isolated starts are not a burst")
	require.Equal(t, 5, bursts[0].Count)
	require.Len(t, bursts[0].OperationIDs, 5)

	unfinished := flagsOfKind(summary, "unfinished_operations")
	require.Len(t, unfinished, 1)
	require.Equal(t, []string{hung.ID}, unfinished[0].OperationIDs, "finished and recent operations are not flagged")

	integrity := flagsOfKind(summary, "index_integrity")
	require.Len(t, integrity, 1)
	require.Contains(t, integrity[0].Observation, "intel_fts")

	var text bytes.Buffer
	require.NoError(t, WriteText(&text, readPerf(t, root, f.now, "2h")))
	require.Contains(t, text.String(), "[runtime_start_burst]")
	require.Contains(t, text.String(), "not diagnoses")
}

func TestPerfDisclosesPartialWindowCoverage(t *testing.T) {
	root := t.TempDir()
	start := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	f := newPerfFixture(t, root, start)
	f.finished("tool", "search", "success", 5)
	require.NoError(t, f.recorder.Close())

	result := readPerf(t, root, start.Add(time.Hour), "")
	require.Equal(t, 1, len(result.Perf.Groups))
	require.True(t, strings.Contains(strings.Join(result.Coverage.Warnings, "\n"), "requested start precedes examined retained evidence"),
		"the default 24h window starts before the only evidence")
}
