package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/logging"
)

const indexTimingsWindow = time.Second

type indexDiagnosticOptions struct {
	Trigger         string
	Rebuild, Vacuum bool
}

// Docs:
// - [[indexing-observability-maintenance-policy#^spec-0047-us2]]
// WHY: --timings is the stable diagnostic surface for indexing phase, queue,
// provider, DB-write, graph, and maintenance bottlenecks; keep setup centralized.
func withIndexTimings(ctx context.Context, out io.Writer, enabled bool, options ...indexDiagnosticOptions) (context.Context, func(error)) {
	if ctx == nil {
		ctx = context.Background()
	}
	parent := diagnostics.OperationFromContext(ctx)
	opts := indexDiagnosticOptions{Trigger: "in-process"}
	if len(options) > 0 {
		opts = options[0]
	}
	op := diagnostics.NewOperation("index", opts.Trigger)
	op.ParentID = parent.ID
	if parent.ID != "" {
		op.TraceID = parent.TraceID
		if op.TraceID == "" {
			op.TraceID = parent.ID
		}
	}
	ctx = diagnostics.WithOperation(ctx, op)
	if recorder := diagnostics.FromContext(ctx); recorder != nil {
		recorder.Event(ctx, slog.LevelInfo, "indexing", "index.started", "", slog.String("trigger", opts.Trigger), slog.Bool("rebuild", opts.Rebuild), slog.Bool("vacuum", opts.Vacuum))
	}
	collector := indexingperf.FromContext(ctx)
	if collector == nil {
		collector = indexingperf.NewBounded()
	}
	ctx = indexingperf.WithCollector(ctx, collector)
	totalCtx := indexingperf.WithPhase(ctx, "total")
	doneTotal := indexingperf.StartSpan(totalCtx, "total")

	reportCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	if enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(indexTimingsWindow)
			defer ticker.Stop()
			for {
				select {
				case <-reportCtx.Done():
					return
				case <-ticker.C:
					if line := strings.TrimSpace(collector.RenderWindow(indexTimingsWindow)); line != "" {
						fmt.Fprintln(out, line)
					}
				}
			}
		}()
	}

	var once sync.Once
	return ctx, func(err error) {
		once.Do(func() {
			cancel()
			wg.Wait()
			doneTotal(err)
			if enabled {
				if summary := strings.TrimSpace(collector.RenderSummary()); summary != "" {
					fmt.Fprintln(out, summary)
				}
			}
			if recorder := diagnostics.FromContext(ctx); recorder != nil {
				status, reason := "success", ""
				if err != nil {
					status, reason = "error", "index_failed"
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					status, reason = "canceled", "caller_canceled"
				}
				snapshot := collector.Snapshot()
				metrics, marshalErr := json.Marshal(snapshot)
				if marshalErr != nil {
					recorder.Event(ctx, slog.LevelWarn, "indexing", "report.metrics_failed", "")
				}
				var lockWait, execution int64
				for _, metric := range snapshot.Latencies {
					if metric.Name == "index.lock_wait" {
						lockWait += metric.Total / int64(time.Millisecond)
					}
				}
				for _, span := range snapshot.Spans {
					if span.Name == "unified_core" {
						execution += span.DurationNs / int64(time.Millisecond)
					}
				}
				finished := time.Now()
				level := slog.LevelInfo
				if status == "error" {
					level = slog.LevelWarn
				}
				recorder.EventQuiet(ctx, level, "indexing", "index.finished", "", slog.String("status", status), slog.String("reason_code", reason), slog.Int64("lock_wait_ms", lockWait), slog.Int64("execution_ms", execution))
				if reportErr := recorder.PublishReport(ctx, diagnostics.Report{FinishedAt: finished, Status: status, ReasonCode: reason, Error: logging.ClassifyError(err), LockWaitMS: lockWait, ExecutionMS: execution, Metrics: metrics,
					Attributes: map[string]any{"rebuild": opts.Rebuild, "vacuum": opts.Vacuum},
					Truncated:  snapshot.Coverage.RejectedObservations > 0 || snapshot.Coverage.DroppedSamples > 0 || snapshot.Coverage.DroppedIntervals > 0,
				}); reportErr != nil {
					recorder.Event(ctx, slog.LevelWarn, "indexing", "report.publish_failed", "")
				}
			}
		})
	}
}
