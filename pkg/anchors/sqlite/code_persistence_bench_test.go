package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// BenchmarkCodePersistenceBatch measures complete replacement of a warm index.
// Every source has symbols, refs, external evidence, an anchor, FTS and rationale.
func BenchmarkCodePersistenceBatch(b *testing.B) {
	for _, count := range []int{1, 128, 512} {
		b.Run(fmt.Sprintf("paths=%d", count), func(b *testing.B) {
			store, err := Open(filepath.Join(b.TempDir(), "index.db"))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = store.Close() })
			batch := atomicCodeBatch(b, count, "bench")
			batch.Metas = nil
			if err := store.ApplyCodePersistenceBatch(context.Background(), batch); err != nil {
				b.Fatal(err)
			}
			collector := indexingperf.New()
			ctx := indexingperf.WithCollector(context.Background(), collector)
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				if err := store.ApplyCodePersistenceBatch(ctx, batch); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.Log(collector.RenderSummary())
		})
	}
}
