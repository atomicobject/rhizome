package diagnostics

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func BenchmarkEventPersistence(b *testing.B) {
	for _, disabled := range []bool{true, false} {
		name := "enabled"
		if disabled {
			name = "disabled"
		}
		b.Run(name, func(b *testing.B) {
			r, err := Open(b.TempDir(), Options{Disabled: disabled, Stderr: io.Discard})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = r.Close() })
			ctx := WithOperation(context.Background(), NewOperation("index", "benchmark"))
			attrs := []slog.Attr{slog.String("phase", "discovery"), slog.Int("files", 100)}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r.Event(ctx, slog.LevelInfo, "indexing", "phase.finished", "", attrs...)
			}
		})
	}
}
