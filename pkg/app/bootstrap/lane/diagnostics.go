package lane

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/logging"
)

func (h *jobHandle) admitDiagnostics(ctx context.Context, trigger string) {
	kind := "indexing-job"
	if h.kind == KindExplicitIndex || h.kind == KindBootCatchUp {
		kind = "index"
	}
	if trigger == "" {
		trigger = string(h.kind)
	}
	parent := diagnostics.OperationFromContext(ctx)
	op := diagnostics.NewOperation(kind, trigger)
	op.ParentID = parent.ID
	if parent.ID != "" {
		op.TraceID = parent.TraceID
		if op.TraceID == "" {
			op.TraceID = parent.ID
		}
	}
	h.diagnosticCtx = diagnostics.WithOperation(diagnostics.Propagate(ctx, context.Background()), op)
	h.collector = indexingperf.NewBounded()
	h.diagnosticCtx = indexingperf.WithCollector(h.diagnosticCtx, h.collector)
	h.finishTotal = indexingperf.StartSpan(indexingperf.WithPhase(h.diagnosticCtx, "total"), "total")
	h.diagnosticEvent(slog.LevelInfo, "job.admitted", slog.String("trigger", trigger))
}

func (h *jobHandle) diagnosticEvent(level slog.Level, event string, attrs ...slog.Attr) {
	if recorder := diagnostics.FromContext(h.diagnosticCtx); recorder != nil {
		attrs = append(attrs, slog.String("job_id", h.id), slog.String("job_kind", string(h.kind)))
		recorder.Event(h.diagnosticCtx, level, "indexing-lane", event, "", attrs...)
	}
}

func (h *jobHandle) setReason(reason string) {
	h.mu.Lock()
	if !h.finished && !h.finishing && h.reason == "" {
		h.reason = reason
	}
	h.mu.Unlock()
}

func (h *jobHandle) setPreflightReason(err error) {
	reason := "preflight_failed"
	if errors.Is(err, ErrDatabaseReplaced) {
		reason = "database_replaced"
	}
	h.setReason(reason)
}

func (h *jobHandle) publishDiagnostics(err error) {
	if h.finishTotal == nil {
		return // Fake lane handles have no production collector.
	}
	now := time.Now()
	queueWait := h.queueWait
	if h.pickedAt.IsZero() {
		queueWait = now.Sub(h.started)
	}
	h.mu.Lock()
	reason := h.reason
	h.mu.Unlock()
	status := "success"
	if err == nil {
		reason = ""
	}
	switch {
	case errors.Is(err, ErrPreempted):
		status, reason = "preempted", "explicit_index"
	case errors.Is(err, ErrClosed):
		status, reason = "canceled", "runtime_shutdown"
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		status = "canceled"
		if reason == "" {
			reason = "job_canceled"
		}
	case errors.Is(err, ErrSkipped):
		status, reason = "skipped", "job_skipped"
	case err != nil:
		status = "error"
		if reason == "" {
			reason = "job_failed"
		}
	}
	snapshot := h.collector.Snapshot()
	metrics, marshalErr := json.Marshal(snapshot)
	if marshalErr != nil {
		h.diagnosticEvent(slog.LevelWarn, "report.metrics_failed")
	}
	level := slog.LevelInfo
	if status == "error" {
		level = slog.LevelWarn
	}
	h.diagnosticEvent(level, "job.finished", slog.String("status", status), slog.String("reason_code", reason), slog.Int64("queue_wait_ms", queueWait.Milliseconds()), slog.Int64("lock_wait_ms", h.lockWait.Milliseconds()), slog.Int64("execution_ms", h.execution.Milliseconds()))
	if recorder := diagnostics.FromContext(h.diagnosticCtx); recorder != nil {
		errorClass := logging.ClassifyError(err)
		if status == "skipped" {
			errorClass = ""
		}
		if reportErr := recorder.PublishReport(h.diagnosticCtx, diagnostics.Report{
			FinishedAt: now, Status: status, ReasonCode: reason, Error: errorClass,
			QueueWaitMS: queueWait.Milliseconds(), LockWaitMS: h.lockWait.Milliseconds(), ExecutionMS: h.execution.Milliseconds(),
			Attributes: map[string]any{"job_id": h.id, "job_kind": string(h.kind)}, Metrics: metrics,
			Truncated: snapshot.Coverage.RejectedObservations > 0 || snapshot.Coverage.DroppedSamples > 0 || snapshot.Coverage.DroppedIntervals > 0,
		}); reportErr != nil {
			h.diagnosticEvent(slog.LevelWarn, "report.publish_failed")
		}
	}
}
