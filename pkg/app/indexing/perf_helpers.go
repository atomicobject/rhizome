package indexing

import (
	"context"
	"errors"
	"log/slog"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func runPhase(ctx context.Context, name string, fn func(context.Context) error) error {
	phaseCtx := indexingperf.WithPhase(ctx, name)
	done := indexingperf.StartSpan(phaseCtx, name)
	err := fn(phaseCtx)
	done(err)
	if err != nil {
		reason := "phase_failed"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			reason = "canceled"
		}
		indexEvent(phaseCtx, slog.LevelWarn, "phase.failed", slog.String("phase", name), slog.String("reason_code", reason))
	}
	return err
}

func indexEvent(ctx context.Context, level slog.Level, name string, attrs ...slog.Attr) {
	if recorder := diagnostics.FromContext(ctx); recorder != nil {
		recorder.Event(ctx, level, "indexing", name, "", attrs...)
	}
}
