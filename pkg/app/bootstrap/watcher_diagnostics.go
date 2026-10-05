package bootstrap

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func watcherEvent(ctx context.Context, level slog.Level, name string, attrs ...slog.Attr) {
	if recorder := diagnostics.FromContext(ctx); recorder != nil {
		recorder.Event(ctx, level, "watcher", name, "", attrs...)
	}
}

func watcherReasonAttrs(reasons map[string]int64, attrs ...slog.Attr) []slog.Attr {
	keys := make([]string, 0, len(reasons))
	for key := range reasons {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		attrs = append(attrs, slog.Int64("trigger_"+key, reasons[key]))
	}
	return attrs
}

func addWatchReason(reasons map[string]int64, reason string) map[string]int64 {
	if reasons == nil {
		reasons = make(map[string]int64)
	}
	reasons[reason]++
	return reasons
}

func watchPhase(ctx context.Context, name string) (context.Context, func(error)) {
	ctx = indexingperf.WithPhase(ctx, name)
	return ctx, indexingperf.StartSpan(ctx, name)
}

func watchDuration(ctx context.Context, name string) func() {
	started := time.Now()
	return func() { indexingperf.ObserveLatency(ctx, name, time.Since(started)) }
}
