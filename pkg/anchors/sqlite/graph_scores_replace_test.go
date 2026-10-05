package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReplaceGraphDocScores_ReplacementContractAcrossBatchBoundaries(t *testing.T) {
	for _, count := range []int{112, 113} {
		t.Run(fmt.Sprintf("rows_%d", count), func(t *testing.T) {
			ctx := context.Background()
			store, _ := openGraphScoreTestStores(t)
			require.NoError(t, store.ReplaceGraphDocScores(ctx, []GraphDocScore{{
				DocPath: "stale.md", DocType: "note", Hub: 9, Authority: 8,
				Community: "stale", Inbound: 7, Outbound: 6, UpdatedAt: 5,
			}}))

			scores := graphDocScoreFixture(count)
			require.NoError(t, store.ReplaceGraphDocScores(ctx, scores))
			require.Equal(t, expectedGraphDocScores(scores), mustGraphDocScores(t, store))

			require.NoError(t, store.ReplaceGraphDocScores(ctx, nil))
			require.Empty(t, mustGraphDocScores(t, store))
		})
	}
}

func TestReplaceAnchorScores_ReplacementContractAcrossBatchBoundaries(t *testing.T) {
	for _, count := range []int{300, 301} {
		t.Run(fmt.Sprintf("rows_%d", count), func(t *testing.T) {
			ctx := context.Background()
			store, _ := openGraphScoreTestStores(t)
			require.NoError(t, store.ReplaceAnchorScores(ctx, []AnchorScore{{
				AnchorID: "stale-anchor", PageRank: 9, Updated: 5,
			}}))

			scores := anchorScoreFixture(count)
			require.NoError(t, store.ReplaceAnchorScores(ctx, scores))
			require.Equal(t, expectedAnchorScores(scores), mustAnchorScores(t, store))

			require.NoError(t, store.ReplaceAnchorScores(ctx, nil))
			require.Empty(t, mustAnchorScores(t, store))
		})
	}
}

func TestReplaceGraphScores_FailureAfterFirstBatchRollsBackRowsAndRevision(t *testing.T) {
	t.Run("document scores", func(t *testing.T) {
		ctx := context.Background()
		store, observer := openGraphScoreTestStores(t)
		previous := []GraphDocScore{{
			DocPath: "previous.md", DocType: "note", Hub: 1.25, Authority: 2.5,
			Community: "previous", Inbound: 3, Outbound: 4, UpdatedAt: 5,
		}}
		require.NoError(t, store.ReplaceGraphDocScores(ctx, previous))
		before := mustGraphFingerprint(t, observer)
		scores := graphDocScoreFixture(113)
		scores[112].UpdatedAt = -1

		require.Error(t, store.ReplaceGraphDocScores(ctx, scores))
		require.Equal(t, previous, mustGraphDocScores(t, observer))
		require.Equal(t, before, mustGraphFingerprint(t, observer))
	})

	t.Run("invalid document type", func(t *testing.T) {
		ctx := context.Background()
		store, observer := openGraphScoreTestStores(t)
		previous := []GraphDocScore{{
			DocPath: "previous.md", DocType: "note", Hub: 1.25, Authority: 2.5,
			Community: "previous", Inbound: 3, Outbound: 4, UpdatedAt: 5,
		}}
		require.NoError(t, store.ReplaceGraphDocScores(ctx, previous))
		before := mustGraphFingerprint(t, observer)
		scores := graphDocScoreFixture(113)
		scores = append(scores, GraphDocScore{DocPath: "invalid.md", DocType: "diagram", UpdatedAt: 1})

		require.ErrorContains(t, store.ReplaceGraphDocScores(ctx, scores), "invalid doc_type")
		require.Equal(t, previous, mustGraphDocScores(t, observer))
		require.Equal(t, before, mustGraphFingerprint(t, observer))
	})

	t.Run("anchor scores", func(t *testing.T) {
		ctx := context.Background()
		store, observer := openGraphScoreTestStores(t)
		previous := []AnchorScore{{AnchorID: "previous-anchor", PageRank: 1.25, Updated: 5}}
		require.NoError(t, store.ReplaceAnchorScores(ctx, previous))
		before := mustGraphFingerprint(t, observer)
		scores := anchorScoreFixture(301)
		scores[300].Updated = -1

		require.Error(t, store.ReplaceAnchorScores(ctx, scores))
		require.Equal(t, previous, mustAnchorScores(t, observer))
		require.Equal(t, before, mustGraphFingerprint(t, observer))
	})
}

func TestReplaceGraphScores_CanceledCallPreservesRowsAndRevision(t *testing.T) {
	t.Run("document scores", func(t *testing.T) {
		ctx := context.Background()
		store, observer := openGraphScoreTestStores(t)
		previous := []GraphDocScore{{DocPath: "previous.md", DocType: "note", Authority: 1, UpdatedAt: 2}}
		require.NoError(t, store.ReplaceGraphDocScores(ctx, previous))
		before := mustGraphFingerprint(t, observer)
		canceled, cancel := context.WithCancel(ctx)
		cancel()

		require.ErrorIs(t, store.ReplaceGraphDocScores(canceled, graphDocScoreFixture(113)), context.Canceled)
		require.Equal(t, previous, mustGraphDocScores(t, observer))
		require.Equal(t, before, mustGraphFingerprint(t, observer))
	})

	t.Run("anchor scores", func(t *testing.T) {
		ctx := context.Background()
		store, observer := openGraphScoreTestStores(t)
		previous := []AnchorScore{{AnchorID: "previous-anchor", PageRank: 1, Updated: 2}}
		require.NoError(t, store.ReplaceAnchorScores(ctx, previous))
		before := mustGraphFingerprint(t, observer)
		canceled, cancel := context.WithCancel(ctx)
		cancel()

		require.ErrorIs(t, store.ReplaceAnchorScores(canceled, anchorScoreFixture(301)), context.Canceled)
		require.Equal(t, previous, mustAnchorScores(t, observer))
		require.Equal(t, before, mustGraphFingerprint(t, observer))
	})
}

func TestReplaceGraphScores_PreTransactionFailuresDoNotWaitForWriterLock(t *testing.T) {
	tests := []struct {
		name   string
		call   func(*Store) error
		assert func(require.TestingT, error)
	}{
		{
			name: "canceled empty document replacement",
			call: func(store *Store) error {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return store.ReplaceGraphDocScores(ctx, nil)
			},
			assert: func(t require.TestingT, err error) { require.ErrorIs(t, err, context.Canceled) },
		},
		{
			name: "canceled nonempty document replacement",
			call: func(store *Store) error {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return store.ReplaceGraphDocScores(ctx, []GraphDocScore{{DocPath: "a.md", DocType: "note"}})
			},
			assert: func(t require.TestingT, err error) { require.ErrorIs(t, err, context.Canceled) },
		},
		{
			name: "canceled empty anchor replacement",
			call: func(store *Store) error {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return store.ReplaceAnchorScores(ctx, nil)
			},
			assert: func(t require.TestingT, err error) { require.ErrorIs(t, err, context.Canceled) },
		},
		{
			name: "canceled nonempty anchor replacement",
			call: func(store *Store) error {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return store.ReplaceAnchorScores(ctx, []AnchorScore{{AnchorID: "anchor-a"}})
			},
			assert: func(t require.TestingT, err error) { require.ErrorIs(t, err, context.Canceled) },
		},
		{
			name: "invalid document type",
			call: func(store *Store) error {
				return store.ReplaceGraphDocScores(context.Background(), []GraphDocScore{
					{DocPath: " ", DocType: "also-invalid"},
					{DocPath: "invalid.md", DocType: "diagram"},
				})
			},
			assert: func(t require.TestingT, err error) { require.ErrorContains(t, err, "invalid doc_type") },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, _ := openGraphScoreTestStores(t)
			err := graphScoreCallWithWriterLocked(t, store, func() error { return test.call(store) })
			test.assert(t, err)
		})
	}
}

func TestReplaceGraphScores_ChangedCommitAdvancesCrossHandleFingerprint(t *testing.T) {
	t.Run("document scores", func(t *testing.T) {
		ctx := context.Background()
		store, observer := openGraphScoreTestStores(t)
		beforeScore := GraphDocScore{DocPath: "changed.md", DocType: "note", Hub: 1, Authority: 2, Community: "old", Inbound: 3, Outbound: 4, UpdatedAt: 5}
		require.NoError(t, store.ReplaceGraphDocScores(ctx, []GraphDocScore{beforeScore}))
		before := mustGraphFingerprint(t, observer)
		afterScore := GraphDocScore{DocPath: "changed.md", DocType: "code", Hub: 6, Authority: 7, Community: "new", Inbound: 8, Outbound: 9, UpdatedAt: 10}

		require.NoError(t, store.ReplaceGraphDocScores(ctx, []GraphDocScore{afterScore}))
		require.Equal(t, []GraphDocScore{afterScore}, mustGraphDocScores(t, observer))
		require.NotEqual(t, before, mustGraphFingerprint(t, observer))
	})

	t.Run("anchor scores", func(t *testing.T) {
		ctx := context.Background()
		store, observer := openGraphScoreTestStores(t)
		require.NoError(t, store.ReplaceAnchorScores(ctx, []AnchorScore{{AnchorID: "changed-anchor", PageRank: 1, Updated: 2}}))
		before := mustGraphFingerprint(t, observer)
		afterScore := AnchorScore{AnchorID: "changed-anchor", PageRank: 3, Updated: 4}

		require.NoError(t, store.ReplaceAnchorScores(ctx, []AnchorScore{afterScore}))
		require.Equal(t, []AnchorScore{afterScore}, mustAnchorScores(t, observer))
		require.NotEqual(t, before, mustGraphFingerprint(t, observer))
	})
}

func graphDocScoreFixture(count int) []GraphDocScore {
	scores := make([]GraphDocScore, count)
	for i := range scores {
		docType := "note"
		if i%2 == 1 {
			docType = "code"
		}
		scores[i] = GraphDocScore{
			DocPath: fmt.Sprintf("%s/%03d", docType, i), DocType: docType,
			Hub: float64(i) + 0.25, Authority: float64(i) + 0.5,
			Community: fmt.Sprintf("community-%d", i%7), Inbound: i + 1, Outbound: i + 2, UpdatedAt: int64(i + 3),
		}
	}
	if count == 112 {
		scores[110].DocPath = "duplicate.md"
		scores[111] = GraphDocScore{DocPath: "duplicate.md", DocType: "code", Hub: 91, Authority: 92, Community: "last", Inbound: 93, Outbound: 94, UpdatedAt: 95}
	}
	if count == 113 {
		scores[111].DocPath = "duplicate.md"
		scores[112] = GraphDocScore{DocPath: "duplicate.md", DocType: "code", Hub: 91, Authority: 92, Community: "last", Inbound: 93, Outbound: 94, UpdatedAt: 95}
	}
	return append(scores, GraphDocScore{DocPath: "   ", DocType: "invalid", UpdatedAt: -1})
}

func anchorScoreFixture(count int) []AnchorScore {
	scores := make([]AnchorScore, count)
	for i := range scores {
		scores[i] = AnchorScore{AnchorID: fmt.Sprintf("anchor-%03d", i), PageRank: float64(i) + 0.25, Updated: int64(i + 1)}
	}
	if count == 300 {
		scores[298].AnchorID = "duplicate-anchor"
		scores[299] = AnchorScore{AnchorID: "duplicate-anchor", PageRank: 91, Updated: 92}
	}
	if count == 301 {
		scores[299].AnchorID = "duplicate-anchor"
		scores[300] = AnchorScore{AnchorID: "duplicate-anchor", PageRank: 91, Updated: 92}
	}
	return append(scores, AnchorScore{AnchorID: "   ", PageRank: 99, Updated: -1})
}

func expectedGraphDocScores(scores []GraphDocScore) []GraphDocScore {
	byPath := make(map[string]GraphDocScore, len(scores))
	for _, score := range scores {
		if score.DocPath != "" && score.DocPath != "   " {
			byPath[score.DocPath] = score
		}
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]GraphDocScore, 0, len(paths))
	for _, path := range paths {
		out = append(out, byPath[path])
	}
	return out
}

func expectedAnchorScores(scores []AnchorScore) []AnchorScore {
	byID := make(map[string]AnchorScore, len(scores))
	for _, score := range scores {
		if score.AnchorID != "" && score.AnchorID != "   " {
			byID[score.AnchorID] = score
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]AnchorScore, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out
}

func openGraphScoreTestStores(t *testing.T) (*Store, *Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graph-scores.db")
	store, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	observer, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = observer.Close() })
	return store, observer
}

func mustGraphDocScores(t *testing.T, store *Store) []GraphDocScore {
	t.Helper()
	scores, err := store.GraphDocScores(context.Background())
	require.NoError(t, err)
	return scores
}

func mustAnchorScores(t *testing.T, store *Store) []AnchorScore {
	t.Helper()
	rows, err := store.db.QueryContext(context.Background(), `
		SELECT anchor_id, pagerank, updated_at
		FROM graph_anchor_scores
		ORDER BY anchor_id
	`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var scores []AnchorScore
	for rows.Next() {
		var score AnchorScore
		require.NoError(t, rows.Scan(&score.AnchorID, &score.PageRank, &score.Updated))
		scores = append(scores, score)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	return scores
}

func mustGraphFingerprint(t *testing.T, store *Store) string {
	t.Helper()
	fingerprint, err := store.GraphWebFingerprint(context.Background())
	require.NoError(t, err)
	return fingerprint
}

func graphScoreCallWithWriterLocked(t *testing.T, store *Store, call func() error) error {
	t.Helper()
	var writerMu sync.Mutex
	writerMu.Lock()
	store.SetWriteMu(&writerMu)
	done := make(chan error, 1)
	go func() { done <- call() }()

	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		writerMu.Unlock()
		return err
	case <-timer.C:
		writerMu.Unlock()
	}

	cleanupTimer := time.NewTimer(time.Second)
	defer cleanupTimer.Stop()
	select {
	case <-done:
	case <-cleanupTimer.C:
		t.Fatal("score replacement goroutine did not exit after releasing writer lock")
	}
	t.Fatal("score replacement waited for the writer lock before returning its pre-transaction error")
	return nil
}
