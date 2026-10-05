package sqlite

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func BenchmarkGraphWebFingerprint(b *testing.B) {
	for _, size := range []int{0, 1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("edges_%d", size), func(b *testing.B) {
			store, err := Open(filepath.Join(b.TempDir(), "benchmark.db"))
			require.NoError(b, err)
			defer store.Close()
			ctx := context.Background()
			tx, err := store.db.BeginTx(ctx, nil)
			require.NoError(b, err)
			stmt, err := tx.PrepareContext(ctx, `INSERT INTO graph_doc_edges(src_path, dst_path, kind, confidence, confidence_score, source_location) VALUES (?, ?, 'wikilink', 'extracted', 1, '')`)
			require.NoError(b, err)
			for i := range size {
				_, err = stmt.ExecContext(ctx, fmt.Sprintf("src/%d", i), fmt.Sprintf("dst/%d", i))
				require.NoError(b, err)
			}
			require.NoError(b, stmt.Close())
			require.NoError(b, tx.Commit())
			b.ResetTimer()
			for range b.N {
				_, err = store.GraphWebFingerprint(ctx)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
		})
	}
}

func BenchmarkGraphRevisionTriggerWriteOverhead(b *testing.B) {
	const rowsPerTransaction = 1_000
	for _, enabled := range []bool{false, true} {
		name := "without_trigger"
		if enabled {
			name = "with_trigger"
		}
		b.Run(name, func(b *testing.B) {
			store, err := Open(filepath.Join(b.TempDir(), "benchmark.db"))
			require.NoError(b, err)
			defer store.Close()
			ctx := context.Background()
			if !enabled {
				for _, op := range []string{"insert", "delete", "update"} {
					_, err = store.db.ExecContext(ctx, `DROP TRIGGER graph_web_revision_graph_doc_edges_`+op)
					require.NoError(b, err)
				}
			}
			b.ResetTimer()
			for iteration := range b.N {
				tx, beginErr := store.db.BeginTx(ctx, nil)
				if beginErr != nil {
					b.Fatal(beginErr)
				}
				stmt, prepareErr := tx.PrepareContext(ctx, `INSERT INTO graph_doc_edges(src_path, dst_path, kind, confidence, confidence_score, source_location) VALUES (?, ?, 'wikilink', 'extracted', 1, '')`)
				if prepareErr != nil {
					b.Fatal(prepareErr)
				}
				for row := range rowsPerTransaction {
					id := iteration*rowsPerTransaction + row
					if _, err = stmt.ExecContext(ctx, fmt.Sprintf("src/%d", id), fmt.Sprintf("dst/%d", id)); err != nil {
						b.Fatal(err)
					}
				}
				if err = stmt.Close(); err != nil {
					b.Fatal(err)
				}
				if err = tx.Commit(); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(rowsPerTransaction, "rows/txn")
		})
	}
}

func BenchmarkGraphRevisionStoreOpen(b *testing.B) {
	path := filepath.Join(b.TempDir(), "open.db")
	store, err := Open(path)
	require.NoError(b, err)
	_, err = store.db.Exec(`WITH RECURSIVE seq(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM seq WHERE x < 1000) INSERT INTO graph_doc_edges(src_path, dst_path, kind) SELECT printf('src/%d', x), printf('dst/%d', x), 'wikilink' FROM seq`)
	require.NoError(b, err)
	require.NoError(b, store.Close())
	b.ResetTimer()
	for range b.N {
		store, err = Open(path)
		b.StopTimer()
		require.NoError(b, err)
		require.NoError(b, store.Close())
		b.StartTimer()
	}
	b.StopTimer()
}
