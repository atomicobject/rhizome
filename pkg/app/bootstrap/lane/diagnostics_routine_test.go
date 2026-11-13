package lane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestRoutineJobMatrix(t *testing.T) {
	fast, slow := 20*time.Millisecond, time.Second
	for _, kind := range []Kind{KindWatcherBatch, KindEmbedCycle, KindGraphCycle, KindValidationRefresh} {
		require.True(t, routineJob(kind, "success", fast, false, false), kind)
		require.False(t, routineJob(kind, "success", slow, false, false), "%s: slow", kind)
		require.False(t, routineJob(kind, "success", fast, true, false), "%s: fallback", kind)
		require.False(t, routineJob(kind, "success", fast, false, true), "%s: truncated", kind)
		for _, status := range []string{"error", "canceled", "preempted", "skipped"} {
			require.False(t, routineJob(kind, status, fast, false, false), "%s: %s", kind, status)
		}
	}
	for _, kind := range []Kind{KindExplicitIndex, KindBootCatchUp} {
		require.False(t, routineJob(kind, "success", fast, false, false), kind)
	}
}

type jobBody = func(context.Context, *indexlock.Lease, Reporter) error

func failedJob(context.Context, *indexlock.Lease, Reporter) error { return errors.New("job failed") }

func diagnosticContext(t *testing.T) (context.Context, string) {
	t.Helper()
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: io.Discard})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recorder.Close()) })
	return diagnostics.WithRecorder(context.Background(), recorder), root
}

func finishJob(t *testing.T, ctx context.Context, id string, kind Kind, run jobBody, elapsed time.Duration) *jobHandle {
	t.Helper()
	h := newJobHandle(id, kind, run, DefaultHistory)
	h.admitDiagnostics(ctx, "test-trigger")
	var err error
	if run != nil {
		err = run(h.diagnosticCtx, nil, h)
	}
	h.finishWithClock(err, func() time.Time { return h.started.Add(elapsed) })
	return h
}

func TestRoutineReportDurationIncludesSummary(t *testing.T) {
	ctx, root := diagnosticContext(t)
	h := newJobHandle("job-summary", KindWatcherBatch, nil, DefaultHistory)
	h.admitDiagnostics(ctx, "test-trigger")
	now := h.started
	h.summaryFn = func(context.Context) json.RawMessage {
		now = now.Add(2 * routineReportLimit)
		return nil
	}
	h.finishWithClock(nil, func() time.Time { return now })
	finished := finishedEvent(t, root, h)
	require.EqualValues(t, (2 * routineReportLimit).Milliseconds(), finished.Attributes["duration_ms"])
	require.Equal(t, true, finished.Attributes["report"])
	require.Len(t, diagnosticReports(t, root), 1)
}

func TestRoutineReportSuppression(t *testing.T) {
	under, at, slow := routineReportLimit-time.Millisecond, routineReportLimit, 3*routineReportLimit
	type testCase struct {
		name    string
		kind    Kind
		elapsed time.Duration
		run     jobBody
		status  string
		publish bool
	}
	var cases []testCase
	for _, kind := range []Kind{KindWatcherBatch, KindEmbedCycle, KindGraphCycle, KindValidationRefresh} {
		cases = append(cases,
			testCase{name: string(kind) + " under limit", kind: kind, elapsed: under},
			testCase{name: string(kind) + " at limit", kind: kind, elapsed: at, publish: true},
			testCase{name: string(kind) + " slow", kind: kind, elapsed: slow, publish: true},
		)
	}
	cases = append(cases,
		testCase{name: "explicit index", kind: KindExplicitIndex, elapsed: under, publish: true},
		testCase{name: "boot catch-up", kind: KindBootCatchUp, elapsed: under, publish: true},
		testCase{name: "failed", kind: KindWatcherBatch, elapsed: under, publish: true, status: "error", run: failedJob},
		testCase{name: "skipped", kind: KindEmbedCycle, elapsed: under, publish: true, status: "skipped", run: func(context.Context, *indexlock.Lease, Reporter) error {
			return ErrSkipped
		}},
		testCase{name: "degraded fallback", kind: KindWatcherBatch, elapsed: under, publish: true, run: func(ctx context.Context, _ *indexlock.Lease, _ Reporter) error {
			indexingperf.AddCount(ctx, indexingperf.CalledgeSourceFallbackCount, 1)
			return nil
		}},
		testCase{name: "untyped note projection", kind: KindWatcherBatch, elapsed: under, run: func(ctx context.Context, _ *indexlock.Lease, _ Reporter) error {
			indexingperf.AddCount(ctx, indexingperf.OntologyNodesFallbackProjectionsPlanned, 1)
			return nil
		}},
		testCase{name: "truncated metrics", kind: KindGraphCycle, elapsed: under, publish: true, run: func(ctx context.Context, _ *indexlock.Lease, _ Reporter) error {
			overLongPhase := indexingperf.WithPhase(ctx, strings.Repeat("p", 121))
			indexingperf.AddCount(overLongPhase, indexingperf.CalledgeCacheBuildFqnCandidateCount, 1)
			return nil
		}},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, root := diagnosticContext(t)
			handle := finishJob(t, ctx, "job-1", tc.kind, tc.run, tc.elapsed)
			status := tc.status
			if status == "" {
				status = "success"
			}
			finished := finishedEvent(t, root, handle)
			require.Equal(t, string(tc.kind), finished.Attributes["job_kind"])
			require.Equal(t, "test-trigger", finished.Attributes["trigger"])
			require.Equal(t, status, finished.Attributes["status"])
			require.EqualValues(t, tc.elapsed.Milliseconds(), finished.Attributes["duration_ms"])
			for _, key := range []string{"queue_wait_ms", "lock_wait_ms", "execution_ms"} {
				require.Contains(t, finished.Attributes, key)
			}
			require.Equal(t, tc.publish, finished.Attributes["report"], "attributes: %v", finished.Attributes)
			reports := diagnosticReports(t, root)
			if !tc.publish {
				require.Empty(t, reports)
				var snapshot indexingperf.Snapshot
				require.NoError(t, json.Unmarshal(finished.Metrics, &snapshot), "the event carries the job's metrics snapshot")
				require.NotEmpty(t, snapshot.Spans)
				return
			}
			require.Len(t, reports, 1)
			require.Equal(t, handle.ID(), reports[0].Attributes["job_id"])
			require.Equal(t, status, reports[0].Status)
			require.True(t, reports[0].FinishedAt.Equal(handle.started.Add(tc.elapsed)), "the report finishes when the job did")
			require.Nil(t, finished.Metrics, "a published report owns the metrics")
		})
	}
}

// The real lane reaches publication through its queue, file lock, and
// heartbeat, whose cost depends on the host, so the report decision is checked
// against the duration the event itself records.
func TestDiagnosticLaneRoutineReportFollowsRecordedDuration(t *testing.T) {
	cases := []struct {
		name          string
		kind          Kind
		run           jobBody
		alwaysPublish bool
	}{
		{name: "watcher batch", kind: KindWatcherBatch},
		{name: "embed cycle", kind: KindEmbedCycle},
		{name: "graph cycle", kind: KindGraphCycle},
		{name: "validation refresh", kind: KindValidationRefresh},
		{name: "explicit index", kind: KindExplicitIndex, alwaysPublish: true},
		{name: "boot catch-up", kind: KindBootCatchUp, alwaysPublish: true},
		{name: "failed", kind: KindWatcherBatch, run: failedJob, alwaysPublish: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor, ctx, root := diagnosticLane(t, nil)
			handle, _, err := executor.Submit(ctx, Request{Kind: tc.kind, Trigger: "test-trigger", Run: tc.run})
			require.NoError(t, err)
			<-handle.Done()
			finished := finishedEvent(t, root, handle)
			require.Equal(t, string(tc.kind), finished.Attributes["job_kind"])
			require.Equal(t, "test-trigger", finished.Attributes["trigger"])
			durationMS, ok := finished.Attributes["duration_ms"].(float64)
			require.True(t, ok, "attributes: %v", finished.Attributes)
			publish := tc.alwaysPublish || durationMS >= float64(routineReportLimit.Milliseconds())
			require.Equal(t, publish, finished.Attributes["report"], "attributes: %v", finished.Attributes)
			reports := diagnosticReports(t, root)
			if !publish {
				require.Empty(t, reports)
				require.NotEmpty(t, finished.Metrics)
				return
			}
			require.Len(t, reports, 1)
			require.Equal(t, handle.ID(), reports[0].Attributes["job_id"])
			require.Nil(t, finished.Metrics)
		})
	}
}

func TestRoutineJobsLeaveOnlySlowAndFailedReports(t *testing.T) {
	ctx, root := diagnosticContext(t)
	for i := 0; i < 40; i++ {
		finishJob(t, ctx, fmt.Sprintf("job-%d", i), KindWatcherBatch, nil, 20*time.Millisecond)
	}
	slow := finishJob(t, ctx, "job-slow", KindWatcherBatch, nil, routineReportLimit+50*time.Millisecond)
	failed := finishJob(t, ctx, "job-failed", KindGraphCycle, failedJob, 20*time.Millisecond)
	reports := diagnosticReports(t, root)
	require.Len(t, reports, 2)
	require.Equal(t, "success", diagnosticReportFor(t, reports, slow).Status)
	require.Equal(t, "error", diagnosticReportFor(t, reports, failed).Status)
	require.Len(t, finishedEvents(t, root), 42, "every job keeps its terminal event")
}

func finishedEvents(t *testing.T, root string) []diagnostics.Event {
	t.Helper()
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{Subsystem: "indexing-lane", Limit: 1000})
	require.NoError(t, err)
	var finished []diagnostics.Event
	for _, event := range events.Events {
		if event.Name == "job.finished" {
			finished = append(finished, event)
		}
	}
	return finished
}

func finishedEvent(t *testing.T, root string, handle Handle) diagnostics.Event {
	t.Helper()
	for _, event := range finishedEvents(t, root) {
		if event.Attributes["job_id"] == handle.ID() {
			return event
		}
	}
	t.Fatalf("no job.finished event for %s", handle.ID())
	return diagnostics.Event{}
}

func TestDiagnosticLaneKeepsReportsWhenInfoEventsAreNotRetained(t *testing.T) {
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: io.Discard, Level: slog.LevelWarn})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recorder.Close()) })
	executor := New(Options{LockPath: filepath.Join(root, ".rhizome", "index.lock"), PriorityPoll: time.Millisecond})
	t.Cleanup(executor.Close)
	handle, _, err := executor.Submit(diagnostics.WithRecorder(context.Background(), recorder), Request{Kind: KindWatcherBatch, Trigger: "test-trigger"})
	require.NoError(t, err)
	<-handle.Done()
	require.Len(t, diagnosticReports(t, root), 1, "with job.finished dropped, the report is the job's only record")
}
