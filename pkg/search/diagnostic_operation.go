package search

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/logging"
)

type searchOperationKey struct{}

const slowSearchReportThreshold = 250 * time.Millisecond

type searchOperation struct {
	operation    diagnostics.Operation
	started      time.Time
	metricsScope string
	partial      atomic.Bool
}

// Search owns a child operation even when its caller already owns a request.
// Reused collectors remain cumulative; the report explicitly discloses that
// scope rather than pretending their snapshot contains only this search.
func beginSearchOperation(ctx context.Context) (context.Context, *searchOperation) {
	operation := &searchOperation{started: time.Now(), metricsScope: "context"}
	if recorder := diagnostics.FromContext(ctx); recorder != nil {
		parent := diagnostics.OperationFromContext(ctx)
		operation.operation = diagnostics.NewOperation("search", "service")
		operation.operation.ParentID = parent.ID
		if parent.TraceID != "" {
			operation.operation.TraceID = parent.TraceID
		}
		ctx = diagnostics.WithOperation(ctx, operation.operation)
		if indexingperf.FromContext(ctx) == nil {
			ctx = indexingperf.WithCollector(ctx, indexingperf.NewBounded())
			operation.metricsScope = "search_operation"
		}
		recorder.EventQuiet(ctx, slog.LevelInfo, "search", "search.started", "")
	}
	ctx = context.WithValue(ctx, searchOperationKey{}, operation)
	// --timings may have been installed before the bounded collector above.
	// Bind it locally without replacing its shared display events or renderer.
	if timings := TimingsFromContext(ctx); timings != nil {
		ctx = WithTimings(ctx, timings)
	}
	return withMetricsTimings(ctx), operation
}

func (operation *searchOperation) finish(ctx context.Context, response Response, err error, panicked bool) {
	operation.finishAt(ctx, response, err, panicked, time.Now())
}

func (operation *searchOperation) finishAt(ctx context.Context, response Response, err error, panicked bool, finished time.Time) {
	for _, warning := range response.Warnings {
		if diagnosticFallbackWarning(warning) {
			operation.partial.Store(true)
		}
	}
	outcome := searchResultStatus(ctx, err)
	if panicked {
		outcome = "error"
	}
	indexingperf.AddCount(ctx, "search.outcome."+outcome, 1)
	if timings := TimingsFromContext(ctx); timings != nil {
		timings.Add(TimingEvent{
			Name: "search", Kind: "stage", Started: operation.started,
			Duration: finished.Sub(operation.started), Status: outcome, Err: errString(err),
		})
	}
	recorder := diagnostics.FromContext(ctx)
	if recorder == nil {
		return
	}
	status, reason := "success", ""
	switch {
	case panicked:
		status, reason = "error", "handler_panicked"
	case err != nil:
		status, reason = "error", logging.ClassifyError(err)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = "canceled"
		}
	case ctx.Err() != nil:
		status, reason = "canceled", logging.ClassifyError(ctx.Err())
	case operation.partial.Load():
		reason = "fallback_used"
	}
	attrs := map[string]any{"outcome": outcome, "metrics_scope": operation.metricsScope, "result_count": len(response.Results)}
	if status != "success" || operation.partial.Load() || finished.Sub(operation.started) >= slowSearchReportThreshold {
		metrics, _ := json.Marshal(indexingperf.FromContext(ctx).Snapshot())
		_ = recorder.PublishReport(ctx, diagnostics.Report{
			StartedAt: operation.started, FinishedAt: finished, Status: status,
			ReasonCode: reason, Attributes: attrs, Metrics: metrics,
		})
	}
	logging.CompleteEventQuiet(ctx, operation.operation, status, reason, attrs)
}

func diagnosticFallbackWarning(warning Warning) bool {
	if warning.Kind == "degraded" {
		return true
	}
	switch warning.Code {
	case "retriever_degraded", "indexed_graph_unavailable", "typo_fallback", "missing_definition", "missing_call_edges", "missing_tests", "subsystem_overview_fallback":
		return true
	default:
		return false
	}
}
