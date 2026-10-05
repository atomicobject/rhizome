package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type AnchorScore struct {
	AnchorID string
	PageRank float64
	Updated  int64
}

type AnchorScoresState struct {
	Count        int
	MaxUpdatedAt int64
}

func (s *Store) ReplaceAnchorScores(ctx context.Context, scores []AnchorScore) error {
	ctx = indexingperf.WithOp(ctx, "graph.replace_anchor_scores")
	if err := ctx.Err(); err != nil {
		return err
	}
	values := make([][]any, 0, len(scores))
	for _, sc := range scores {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.TrimSpace(sc.AnchorID) == "" {
			continue
		}
		values = append(values, []any{sc.AnchorID, sc.PageRank, sc.Updated})
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM graph_anchor_scores`); err != nil {
			return err
		}
		return execValuesBatch(ctx, tx, `
			INSERT OR REPLACE INTO graph_anchor_scores (anchor_id, pagerank, updated_at)
			VALUES
		`, values, 3, "")
	})
}

func (s *Store) AnchorScoresByIDs(ctx context.Context, anchorIDs []string) (map[string]float64, error) {
	ids := normalizeNonEmptyStrings(anchorIDs)
	if len(ids) == 0 {
		return map[string]float64{}, nil
	}
	const batchSize = 400
	out := make(map[string]float64, len(ids))
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		holders := strings.Repeat("?,", len(batch))
		holders = strings.TrimSuffix(holders, ",")
		query := fmt.Sprintf(`
			SELECT anchor_id, pagerank, updated_at
			FROM graph_anchor_scores
			WHERE anchor_id IN (%s)
		`, holders)
		args := make([]any, 0, len(batch))
		for _, id := range batch {
			args = append(args, id)
		}
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var pr float64
			var updated int64
			if err := rows.Scan(&id, &pr, &updated); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[id] = pr
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

func (s *Store) AnchorScoresSummary(ctx context.Context) (AnchorScoresState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(MAX(updated_at), 0)
		FROM graph_anchor_scores
	`)
	var state AnchorScoresState
	if err := row.Scan(&state.Count, &state.MaxUpdatedAt); err != nil {
		return AnchorScoresState{}, err
	}
	return state, nil
}

// CodePageRankByPaths returns the max anchor PageRank per code path.
func (s *Store) CodePageRankByPaths(ctx context.Context, paths []string) (map[string]float64, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return map[string]float64{}, nil
	}

	const batchSize = 400
	out := make(map[string]float64, len(paths))
	for start := 0; start < len(paths); start += batchSize {
		end := start + batchSize
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		holders := strings.Repeat("?,", len(batch))
		holders = strings.TrimSuffix(holders, ",")
		query := fmt.Sprintf(`
			SELECT a.path, MAX(COALESCE(s.pagerank, 0)) AS pr
			FROM intel_code_anchors a
			LEFT JOIN graph_anchor_scores s ON s.anchor_id = a.anchor_id
			WHERE a.path IN (%s)
			GROUP BY a.path
		`, holders)

		args := make([]any, 0, len(batch))
		for _, p := range batch {
			args = append(args, p)
		}

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var path string
			var pr float64
			if err := rows.Scan(&path, &pr); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[path] = pr
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}
