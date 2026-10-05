package bootstrap

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/logging"
)

// Compatible lanes can pack requests from several derived tickets into one
// physical call. Their provider counters therefore belong to the node lifetime.
func observePhysicalEmbeddings(ctx context.Context) (context.Context, func()) {
	recorder := diagnostics.FromContext(ctx)
	if recorder == nil {
		return ctx, func() {}
	}
	parent := diagnostics.OperationFromContext(ctx)
	op := diagnostics.NewOperation("runtime.embedding-provider", "shared_provider")
	op.ParentID = parent.ID
	if parent.TraceID != "" {
		op.TraceID = parent.TraceID
	}
	ctx = diagnostics.WithOperation(ctx, op)
	collector := indexingperf.NewBounded()
	ctx = indexingperf.WithCollector(ctx, collector)
	recorder.Event(ctx, slog.LevelInfo, "runtime", "embedding_provider.started", "")
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			snapshot := collector.Snapshot()
			metrics, _ := json.Marshal(snapshot)
			attrs := map[string]any{"scope": "shared_runtime_lifetime"}
			logging.CompleteEventQuiet(ctx, op, "success", "nodes_drained", attrs)
			_ = recorder.PublishReport(ctx, diagnostics.Report{
				FinishedAt: time.Now(), Status: "success", ReasonCode: "nodes_drained", Attributes: attrs, Metrics: metrics,
				Truncated: snapshot.Coverage.RejectedObservations > 0 || snapshot.Coverage.DroppedSamples > 0 || snapshot.Coverage.DroppedIntervals > 0,
			})
		})
	}
}
