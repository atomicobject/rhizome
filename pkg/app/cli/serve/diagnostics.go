package serve

import (
	"context"
	"log/slog"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/logging"
)

func recordServeDiagnostic(ctx context.Context, level slog.Level, name string, err error) {
	if recorder := diagnostics.FromContext(ctx); recorder != nil {
		reason := logging.ClassifyError(err)
		recorder.Event(ctx, level, "runtime", name, name+": "+reason, slog.String("reason_code", reason))
	}
}
