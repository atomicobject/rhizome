package indexcore

import (
	"context"
	"log/slog"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
)

func event(ctx context.Context, name, reason string, attrs ...slog.Attr) {
	if recorder := diagnostics.FromContext(ctx); recorder != nil {
		attrs = append(attrs, slog.String("reason_code", reason))
		recorder.Event(ctx, slog.LevelInfo, "indexing", name, "", attrs...)
	}
}
