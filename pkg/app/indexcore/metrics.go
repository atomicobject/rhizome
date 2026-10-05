package indexcore

import (
	"context"
	"log/slog"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func runPhase(ctx context.Context, name string, fn func(context.Context) error) error {
	ctx = indexingperf.WithPhase(ctx, name)
	done := indexingperf.StartSpan(ctx, name)
	err := fn(ctx)
	done(err)
	return err
}

func event(ctx context.Context, name, reason string, attrs ...slog.Attr) {
	if recorder := diagnostics.FromContext(ctx); recorder != nil {
		attrs = append(attrs, slog.String("reason_code", reason))
		recorder.Event(ctx, slog.LevelInfo, "indexing", name, "", attrs...)
	}
}
