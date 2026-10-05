package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkCodePersistenceRationale(b *testing.B) {
	for _, populated := range []bool{false, true} {
		b.Run(fmt.Sprintf("populated_%t", populated), func(b *testing.B) {
			for _, count := range []int{1, 128, 512} {
				b.Run(fmt.Sprintf("paths_%d", count), func(b *testing.B) {
					ctx := context.Background()
					store, err := Open(filepath.Join(b.TempDir(), "index.db"))
					require.NoError(b, err)
					defer store.Close()
					batch := atomicCodeBatch(b, count, "benchmark")
					batch.Metas = nil
					if !populated {
						for i := range batch.RationaleBatches {
							batch.RationaleBatches[i].Rationales = nil
						}
					}
					require.NoError(b, store.ApplyCodePersistenceBatch(ctx, batch))
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						if err := store.ApplyCodePersistenceBatch(ctx, batch); err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
					b.ReportMetric(float64(count), "paths/replacement")
					expected := 0
					if populated {
						expected = count
					}
					for _, table := range []string{"intel_rationale", "intel_rationale_fts", "intel_rationale_fts_rowid"} {
						var actual int
						require.NoError(b, store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&actual))
						require.Equal(b, expected, actual, table)
					}
					var mismatched int
					require.NoError(b, store.db.QueryRowContext(ctx, `
						SELECT count(*) FROM intel_rationale r
						LEFT JOIN intel_rationale_fts_rowid m ON m.rationale_id = r.rationale_id
						LEFT JOIN intel_rationale_fts f ON f.rowid = m.fts_rowid
						WHERE f.rationale_id IS NULL OR f.rationale_id != r.rationale_id
						 OR f.path != r.path OR f.symbol_fqn != coalesce(r.symbol_fqn, '')
						 OR f.kind != r.kind OR f.content != r.content
					`).Scan(&mismatched))
					require.Zero(b, mismatched)
					found, err := store.SearchRationaleFTS(ctx, "benchmark", nil, count+1)
					require.NoError(b, err)
					require.Len(b, found, expected)
					for _, wanted := range batch.RationaleBatches {
						actual, err := store.RationaleForPath(ctx, wanted.Path)
						require.NoError(b, err)
						require.Equal(b, wanted.Rationales, actual)
					}
				})
			}
		})
	}
}
