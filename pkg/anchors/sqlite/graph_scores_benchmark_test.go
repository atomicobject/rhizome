package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkReplaceGraphDocScores(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("rows_%d", count), func(b *testing.B) {
			store, err := Open(filepath.Join(b.TempDir(), "scores.db"))
			require.NoError(b, err)
			defer store.Close()
			ctx := context.Background()
			scores := make([]GraphDocScore, count)
			for i := range scores {
				kind := "note"
				if i%2 == 1 {
					kind = "code"
				}
				scores[i] = GraphDocScore{DocPath: fmt.Sprintf("%s/%06d", kind, i), DocType: kind, Hub: float64(i%37) / 37, Authority: float64(i%53) / 53, Community: fmt.Sprintf("group-%d", i%7), Inbound: i % 11, Outbound: i % 19, UpdatedAt: 1000}
			}
			require.NoError(b, store.ReplaceGraphDocScores(ctx, scores))
			before, err := store.GraphWebFingerprint(ctx)
			require.NoError(b, err)
			store.writeInfo.mu.Lock()
			holdBefore := store.writeInfo.totalHold
			store.writeInfo.mu.Unlock()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := store.ReplaceGraphDocScores(ctx, scores); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			store.writeInfo.mu.Lock()
			hold := store.writeInfo.totalHold - holdBefore
			store.writeInfo.mu.Unlock()
			b.ReportMetric(float64(hold.Nanoseconds())/float64(b.N), "writer-hold-ns/op")
			b.ReportMetric(float64(count), "rows/replacement")
			after, err := store.GraphWebFingerprint(ctx)
			require.NoError(b, err)
			require.NotEqual(b, before, after)
			got, err := store.GraphDocScores(ctx)
			require.NoError(b, err)
			require.Len(b, got, count)
			byPath := make(map[string]GraphDocScore, count)
			for _, score := range got {
				byPath[score.DocPath] = score
			}
			for _, score := range scores {
				require.Equal(b, score, byPath[score.DocPath])
			}
		})
	}
}

func BenchmarkReplaceAnchorScores(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("rows_%d", count), func(b *testing.B) {
			store, err := Open(filepath.Join(b.TempDir(), "scores.db"))
			require.NoError(b, err)
			defer store.Close()
			ctx := context.Background()
			scores := make([]AnchorScore, count)
			ids := make([]string, count)
			for i := range scores {
				ids[i] = fmt.Sprintf("anchor-%06d", i)
				scores[i] = AnchorScore{AnchorID: ids[i], PageRank: float64(i%53) / 53, Updated: 1000}
			}
			require.NoError(b, store.ReplaceAnchorScores(ctx, scores))
			before, err := store.GraphWebFingerprint(ctx)
			require.NoError(b, err)
			store.writeInfo.mu.Lock()
			holdBefore := store.writeInfo.totalHold
			store.writeInfo.mu.Unlock()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := store.ReplaceAnchorScores(ctx, scores); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			store.writeInfo.mu.Lock()
			hold := store.writeInfo.totalHold - holdBefore
			store.writeInfo.mu.Unlock()
			b.ReportMetric(float64(hold.Nanoseconds())/float64(b.N), "writer-hold-ns/op")
			b.ReportMetric(float64(count), "rows/replacement")
			after, err := store.GraphWebFingerprint(ctx)
			require.NoError(b, err)
			require.NotEqual(b, before, after)
			rows, err := store.db.QueryContext(ctx, `SELECT anchor_id, pagerank, updated_at FROM graph_anchor_scores ORDER BY anchor_id`)
			require.NoError(b, err)
			var got []AnchorScore
			for rows.Next() {
				var score AnchorScore
				require.NoError(b, rows.Scan(&score.AnchorID, &score.PageRank, &score.Updated))
				got = append(got, score)
			}
			require.NoError(b, rows.Err())
			require.NoError(b, rows.Close())
			require.Equal(b, scores, got)
		})
	}
}
