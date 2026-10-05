package lane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticLaneSkippedWorkPreservesErrorWithoutFailureNoise(t *testing.T) {
	executor, ctx, root := diagnosticLane(t, nil)
	var stderr bytes.Buffer
	restore := diagnostics.FromContext(ctx).SetStderr(&stderr)
	defer restore()
	original := errors.New("PRIVATE_OBSOLETE_INPUT")
	jobErr := errors.Join(original, ErrSkipped)
	handle, _, err := executor.Submit(ctx, Request{Kind: KindEmbedCycle, Run: func(context.Context, Reporter) error { return jobErr }})
	require.NoError(t, err)
	<-handle.Done()
	require.ErrorIs(t, handle.Err(), original)
	require.ErrorIs(t, handle.Err(), ErrSkipped)
	require.Equal(t, jobErr, handle.Err())
	report := diagnosticReportFor(t, diagnosticReports(t, root), handle)
	require.Equal(t, "skipped", report.Status)
	require.Equal(t, "job_skipped", report.ReasonCode)
	require.Empty(t, report.Error)
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{Subsystem: "indexing-lane"})
	require.NoError(t, err)
	var terminal diagnostics.Event
	for _, event := range events.Events {
		if event.Name == "job.finished" {
			terminal = event
		}
	}
	require.Equal(t, "INFO", terminal.Level)
	require.Equal(t, "skipped", terminal.Attributes["status"])
	require.Equal(t, "job_skipped", terminal.Attributes["reason_code"])
	stream, unsubscribe := handle.Subscribe()
	defer unsubscribe()
	for event := range stream {
		if event.Type == EventDone {
			require.Equal(t, OutcomeFailed, event.Outcome)
			require.False(t, event.OK)
		}
	}
	require.Empty(t, stderr.String())
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "PRIVATE_OBSOLETE_INPUT")
}

func diagnosticLane(t *testing.T, mutate func(*Options)) (*Executor, context.Context, string) {
	t.Helper()
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: io.Discard})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recorder.Close()) })
	opts := Options{LockPath: filepath.Join(root, ".rhizome", "index.lock"), PriorityPoll: time.Millisecond}
	if mutate != nil {
		mutate(&opts)
	}
	executor := New(opts)
	t.Cleanup(executor.Close)
	return executor, diagnostics.WithRecorder(context.Background(), recorder), root
}

func TestDiagnosticLaneConcurrentLateCancellationPublishesOnce(t *testing.T) {
	executor, ctx, root := diagnosticLane(t, nil)
	var cancelers sync.WaitGroup
	for i := 0; i < 16; i++ {
		handle, _, err := executor.Submit(ctx, Request{Kind: KindExplicitIndex})
		require.NoError(t, err)
		cancelers.Add(1)
		go func() {
			defer cancelers.Done()
			for j := 0; j < 128; j++ {
				handle.Cancel()
			}
			<-handle.Done()
			handle.Cancel()
		}()
		<-handle.Done()
	}
	cancelers.Wait()
	reports := diagnosticReports(t, root)
	require.Len(t, reports, 16)
	ids := make(map[string]struct{}, len(reports))
	for _, report := range reports {
		require.Contains(t, []string{"success", "canceled"}, report.Status)
		ids[report.OperationID] = struct{}{}
	}
	require.Len(t, ids, 16)
}

func diagnosticReports(t *testing.T, root string) []diagnostics.Report {
	t.Helper()
	result, err := diagnostics.ReadReports(root, diagnostics.Filter{Limit: 100})
	require.NoError(t, err)
	return result.Reports
}

func TestDiagnosticLaneCloseWaitsForRemovedQueuedReport(t *testing.T) {
	executor, ctx, root := diagnosticLane(t, nil)
	started := make(chan struct{})
	running, _, err := executor.Submit(ctx, Request{Kind: KindExplicitIndex, Run: func(ctx context.Context, _ Reporter) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}})
	require.NoError(t, err)
	<-started
	summarizing, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	queued, _, err := executor.Submit(ctx, Request{Kind: KindWatcherBatch, Summary: func(context.Context) json.RawMessage {
		close(summarizing)
		<-release
		return nil
	}})
	require.NoError(t, err)
	go queued.Cancel()
	<-summarizing
	closed := make(chan struct{}, 2)
	go func() { executor.Close(); closed <- struct{}{} }()
	<-running.Done()
	go func() { executor.Close(); closed <- struct{}{} }()
	select {
	case <-closed:
		t.Fatal("Close returned before the removed queued job published its report")
	case <-time.After(20 * time.Millisecond):
	}
	unblock()
	for i := 0; i < 2; i++ {
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Fatal("Close did not finish after report publication")
		}
	}
	require.NoError(t, diagnostics.FromContext(ctx).Close())
	reports := diagnosticReports(t, root)
	require.Len(t, reports, 2)
	require.Equal(t, "canceled", diagnosticReportFor(t, reports, queued).Status)
}

func diagnosticReportFor(t *testing.T, reports []diagnostics.Report, handle Handle) diagnostics.Report {
	t.Helper()
	for _, report := range reports {
		if report.Attributes["job_id"] == handle.ID() {
			return report
		}
	}
	t.Fatalf("no diagnostic report for %s", handle.ID())
	return diagnostics.Report{}
}

func TestDiagnosticLaneCoalescesOneCollectorAndReport(t *testing.T) {
	executor, ctx, root := diagnosticLane(t, nil)
	parent := diagnostics.NewOperation("request", "test")
	ctx = diagnostics.WithOperation(ctx, parent)
	ctx, cancelClient := context.WithCancel(ctx)
	started, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var jobOperation diagnostics.Operation
	handle, _, err := executor.Submit(ctx, Request{Kind: KindExplicitIndex, Summary: func(jobCtx context.Context) json.RawMessage {
		collector := indexingperf.FromContext(jobCtx)
		var totals int
		for _, span := range collector.Snapshot().Spans {
			if span.Name == "total" {
				totals++
				assert.Equal(t, int64(1), span.Count)
			}
		}
		assert.Equal(t, 1, totals, "summary sees the completed operation span")
		encoded, err := json.Marshal(collector.RenderSummary())
		assert.NoError(t, err)
		return encoded
	}, Run: func(jobCtx context.Context, _ Reporter) error {
		jobOperation = diagnostics.OperationFromContext(jobCtx)
		assert.NotNil(t, indexingperf.FromContext(jobCtx))
		assert.NotNil(t, diagnostics.FromContext(jobCtx))
		indexingperf.AddCount(jobCtx, "test.accepted", 4)
		close(started)
		<-release
		assert.NoError(t, jobCtx.Err(), "client cancellation must not own the lane job")
		return nil
	}})
	require.NoError(t, err)
	<-started
	cancelClient()
	joining := diagnostics.NewOperation("request", "join")
	joinCtx := diagnostics.WithOperation(context.WithoutCancel(ctx), joining)
	joinedHandle, joined, err := executor.Submit(joinCtx, Request{Kind: KindExplicitIndex})
	require.NoError(t, err)
	require.True(t, joined)
	require.Equal(t, handle.ID(), joinedHandle.ID())
	close(release)
	<-handle.Done()
	reports := diagnosticReports(t, root)
	require.Len(t, reports, 1)
	require.Equal(t, "success", reports[0].Status)
	require.Equal(t, "index", reports[0].Kind)
	require.Equal(t, parent.ID, reports[0].ParentOperationID)
	require.Equal(t, jobOperation.ID, reports[0].OperationID)
	require.Contains(t, string(reports[0].Metrics), "test.accepted")
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{Subsystem: "indexing-lane", Limit: 100})
	require.NoError(t, err)
	var joinEvents int
	for _, event := range events.Events {
		if event.Name == "job.joined" {
			joinEvents++
			require.Equal(t, joining.ID, event.Attributes["joining_operation_id"])
			require.Equal(t, jobOperation.ID, event.OperationID)
		}
	}
	require.Equal(t, 1, joinEvents)
}

func TestDiagnosticLaneQueuedCancelAndShutdown(t *testing.T) {
	executor, ctx, root := diagnosticLane(t, func(opts *Options) { opts.checkPriority = func() bool { return true } })
	first, _, err := executor.Submit(ctx, Request{Kind: KindWatcherBatch, Run: func(context.Context, Reporter) error { t.Error("held job ran"); return nil }})
	require.NoError(t, err)
	first.Cancel()
	select {
	case <-first.Done():
	case <-time.After(time.Second):
		t.Fatal("queued cancel did not complete")
	}
	second, _, err := executor.Submit(ctx, Request{Kind: KindEmbedCycle})
	require.NoError(t, err)
	executor.Close()
	<-second.Done()
	reports := diagnosticReports(t, root)
	require.Len(t, reports, 2)
	firstReport := diagnosticReportFor(t, reports, first)
	require.Equal(t, "canceled", firstReport.Status)
	require.Equal(t, "explicit_cancel", firstReport.ReasonCode)
	require.Zero(t, firstReport.ExecutionMS)
	secondReport := diagnosticReportFor(t, reports, second)
	require.Equal(t, "canceled", secondReport.Status)
	require.Equal(t, "runtime_shutdown", secondReport.ReasonCode)
}

func TestDiagnosticTerminalEventFollowsReportAndLateSubscriberReceivesIt(t *testing.T) {
	executor, ctx, root := diagnosticLane(t, nil)
	finalizing, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var operationID string
	handle, _, err := executor.Submit(ctx, Request{Kind: KindExplicitIndex, Summary: func(ctx context.Context) json.RawMessage {
		operationID = diagnostics.OperationFromContext(ctx).ID
		close(finalizing)
		<-release
		return nil
	}})
	require.NoError(t, err)
	<-finalizing
	events, unsubscribe := handle.Subscribe()
	defer unsubscribe()
	handle.Cancel() // A drained result cannot change while publication finishes.
	close(release)
	var terminal Event
	for event := range events {
		if event.Type == EventDone {
			terminal = event
			break
		}
	}
	require.Equal(t, EventDone, terminal.Type)
	require.Equal(t, OutcomeOK, terminal.Outcome)
	latest, err := diagnostics.ReadLatest(root, "index")
	require.NoError(t, err, "the stream's terminal event implies its report is available")
	require.Equal(t, operationID, latest.OperationID)
	<-handle.Done()
}

func TestDiagnosticPublishFailureDoesNotSuppressTerminalEvent(t *testing.T) {
	executor, ctx, _ := diagnosticLane(t, nil)
	require.NoError(t, diagnostics.FromContext(ctx).Close())
	handle, _, err := executor.Submit(ctx, Request{Kind: KindExplicitIndex})
	require.NoError(t, err)
	events, unsubscribe := handle.Subscribe()
	defer unsubscribe()
	var terminal Event
	for event := range events {
		if event.Type == EventDone {
			terminal = event
		}
	}
	require.Equal(t, EventDone, terminal.Type)
	require.Equal(t, OutcomeOK, terminal.Outcome)
	<-handle.Done()
	require.NoError(t, handle.Err())
}

func TestDiagnosticLanePreemptReportWaitsForDrain(t *testing.T) {
	executor, ctx, root := diagnosticLane(t, nil)
	started, canceled, drained := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-drained:
		default:
			close(drained)
		}
	}()
	background, _, err := executor.Submit(ctx, Request{Kind: KindWatcherBatch, Run: func(ctx context.Context, _ Reporter) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-drained
		return ctx.Err()
	}})
	require.NoError(t, err)
	<-started
	explicit, _, err := executor.Submit(ctx, Request{Kind: KindExplicitIndex})
	require.NoError(t, err)
	<-canceled
	require.Empty(t, diagnosticReports(t, root), "cancellation is not drained completion")
	close(drained)
	<-background.Done()
	<-explicit.Done()
	reports := diagnosticReports(t, root)
	require.Len(t, reports, 2)
	report := diagnosticReportFor(t, reports, background)
	require.Equal(t, "preempted", report.Status)
	require.Equal(t, "explicit_index", report.ReasonCode)
}

func TestDiagnosticLanePreflightFailureThenSuccess(t *testing.T) {
	executor, ctx, root := diagnosticLane(t, nil)
	executor.SetPreflight(func(Kind) error { return errors.New("provider response body must never persist") })
	failed, _, err := executor.Submit(ctx, Request{Kind: KindExplicitIndex, Run: func(context.Context, Reporter) error { t.Error("failed preflight ran"); return nil }})
	require.NoError(t, err)
	<-failed.Done()
	executor.SetPreflight(nil)
	success, _, err := executor.Submit(ctx, Request{Kind: KindExplicitIndex})
	require.NoError(t, err)
	<-success.Done()
	reports := diagnosticReports(t, root)
	require.Len(t, reports, 2)
	report := diagnosticReportFor(t, reports, failed)
	require.Equal(t, "error", report.Status)
	require.Equal(t, "preflight_failed", report.ReasonCode)
	require.Equal(t, "operation_failed", report.Error)
	require.NotContains(t, report.Error, "provider response")
	require.Equal(t, "success", diagnosticReportFor(t, reports, success).Status)
	latest, err := diagnostics.ReadLatest(root, "index")
	require.NoError(t, err)
	require.Equal(t, diagnosticReportFor(t, reports, success).OperationID, latest.OperationID)
}
