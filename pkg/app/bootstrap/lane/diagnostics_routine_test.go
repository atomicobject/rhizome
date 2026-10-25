package lane

import (
	"context"
	"encoding/json"
	"errors"
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

func TestDiagnosticLaneRoutineReportSuppression(t *testing.T) {
	cases := []struct {
		name    string
		kind    Kind
		run     func(context.Context, *indexlock.Lease, Reporter) error
		publish bool
	}{
		{name: "watcher batch", kind: KindWatcherBatch},
		{name: "embed cycle", kind: KindEmbedCycle},
		{name: "graph cycle", kind: KindGraphCycle},
		{name: "validation refresh", kind: KindValidationRefresh},
		{name: "explicit index", kind: KindExplicitIndex, publish: true},
		{name: "boot catch-up", kind: KindBootCatchUp, publish: true},
		{name: "failed", kind: KindWatcherBatch, publish: true, run: func(context.Context, *indexlock.Lease, Reporter) error {
			return errors.New("job failed")
		}},
		{name: "skipped", kind: KindEmbedCycle, publish: true, run: func(context.Context, *indexlock.Lease, Reporter) error {
			return ErrSkipped
		}},
		{name: "degraded fallback", kind: KindWatcherBatch, publish: true, run: func(ctx context.Context, _ *indexlock.Lease, _ Reporter) error {
			indexingperf.AddCount(ctx, indexingperf.CalledgeSourceFallbackCount, 1)
			return nil
		}},
		{name: "untyped note projection", kind: KindWatcherBatch, run: func(ctx context.Context, _ *indexlock.Lease, _ Reporter) error {
			indexingperf.AddCount(ctx, indexingperf.OntologyNodesFallbackProjectionsPlanned, 1)
			return nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor, ctx, root := diagnosticLane(t, nil)
			handle, _, err := executor.Submit(ctx, Request{Kind: tc.kind, Trigger: "test-trigger", Run: tc.run})
			require.NoError(t, err)
			<-handle.Done()
			reports := diagnosticReports(t, root)
			finished := finishedEvent(t, root, handle)
			require.Equal(t, string(tc.kind), finished.Attributes["job_kind"])
			require.Equal(t, "test-trigger", finished.Attributes["trigger"])
			for _, key := range []string{"status", "duration_ms", "queue_wait_ms", "lock_wait_ms", "execution_ms"} {
				require.Contains(t, finished.Attributes, key)
			}
			require.Equal(t, tc.publish, finished.Attributes["report"])
			if tc.publish {
				require.Len(t, reports, 1)
				require.Equal(t, handle.ID(), reports[0].Attributes["job_id"])
				require.Nil(t, finished.Metrics, "a published report owns the metrics")
				return
			}
			require.Empty(t, reports)
			var snapshot indexingperf.Snapshot
			require.NoError(t, json.Unmarshal(finished.Metrics, &snapshot), "the event carries the job's metrics snapshot")
			require.NotEmpty(t, snapshot.Spans)
		})
	}
}

func TestDiagnosticLaneRoutineJobsLeaveOnlySlowAndFailedReports(t *testing.T) {
	executor, ctx, root := diagnosticLane(t, nil)
	for i := 0; i < 40; i++ {
		handle, _, err := executor.Submit(ctx, Request{Kind: KindWatcherBatch})
		require.NoError(t, err)
		<-handle.Done()
	}
	slow, _, err := executor.Submit(ctx, Request{Kind: KindWatcherBatch, Run: func(ctx context.Context, _ *indexlock.Lease, _ Reporter) error {
		select {
		case <-time.After(routineReportLimit + 50*time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	require.NoError(t, err)
	<-slow.Done()
	failed, _, err := executor.Submit(ctx, Request{Kind: KindGraphCycle, Run: func(context.Context, *indexlock.Lease, Reporter) error {
		return errors.New("graph failed")
	}})
	require.NoError(t, err)
	<-failed.Done()
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
