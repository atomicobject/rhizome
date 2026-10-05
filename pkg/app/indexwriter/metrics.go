package indexwriter

import (
	"context"
	"errors"
	"log/slog"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func metricOutcome(err error) string {
	if err == nil {
		return "success"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "canceled"
	}
	return "error"
}

func recordReconciliationAcknowledged(ctx context.Context, generation int64) {
	indexingperf.AddCount(ctx, "ownership.reconciliation_acknowledged", 1)
	if recorder := diagnostics.FromContext(ctx); recorder != nil {
		recorder.Event(ctx, slog.LevelInfo, "indexing", "reconciliation.acknowledged", "", slog.Int64("generation", generation))
	}
}
