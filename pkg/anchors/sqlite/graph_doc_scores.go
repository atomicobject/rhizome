package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type GraphDocScore struct {
	DocPath   string
	DocType   string // note|code
	Hub       float64
	Authority float64
	Community string
	Inbound   int
	Outbound  int
	UpdatedAt int64
}

type GraphDocScoresState struct {
	Count         int
	MaxUpdatedAt  int64
	NoteCount     int
	NoteUpdatedAt int64
}

func (s *Store) ReplaceGraphDocScores(ctx context.Context, scores []GraphDocScore) error {
	ctx = indexingperf.WithOp(ctx, "graph.replace_doc_scores")
	if err := ctx.Err(); err != nil {
		return err
	}
	values := make([][]any, 0, len(scores))
	for _, sc := range scores {
		if err := ctx.Err(); err != nil {
			return err
		}
		if strings.TrimSpace(sc.DocPath) == "" {
			continue
		}
		if sc.DocType != "note" && sc.DocType != "code" {
			return fmt.Errorf("graph_doc_scores: invalid doc_type %q for %q", sc.DocType, sc.DocPath)
		}
		values = append(values, []any{sc.DocPath, sc.DocType, sc.Hub, sc.Authority, sc.Community, sc.Inbound, sc.Outbound, sc.UpdatedAt})
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM graph_doc_scores`); err != nil {
			return err
		}
		return execValuesBatch(ctx, tx, `
			INSERT OR REPLACE INTO graph_doc_scores (doc_path, doc_type, hub, authority, community, inbound, outbound, updated_at)
			VALUES
		`, values, 8, "")
	})
}

func (s *Store) GraphDocScoresByPaths(ctx context.Context, paths []string) (map[string]GraphDocScore, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return map[string]GraphDocScore{}, nil
	}

	// SQLite param limit can be low; keep this comfortably under typical defaults.
	const batchSize = 400

	out := make(map[string]GraphDocScore, len(paths))
	for start := 0; start < len(paths); start += batchSize {
		end := start + batchSize
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		placeholders := strings.Repeat("?,", len(batch))
		placeholders = strings.TrimSuffix(placeholders, ",")
		query := fmt.Sprintf(`
			SELECT doc_path, doc_type, hub, authority, community, inbound, outbound, updated_at
			FROM graph_doc_scores
			WHERE doc_path IN (%s)
		`, placeholders)

		args := make([]any, 0, len(batch))
		for _, p := range batch {
			args = append(args, p)
		}

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var sc GraphDocScore
			if err := rows.Scan(&sc.DocPath, &sc.DocType, &sc.Hub, &sc.Authority, &sc.Community, &sc.Inbound, &sc.Outbound, &sc.UpdatedAt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[sc.DocPath] = sc
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

func (s *Store) GraphDocScores(ctx context.Context) ([]GraphDocScore, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT doc_path, doc_type, hub, authority, community, inbound, outbound, updated_at
		FROM graph_doc_scores
		ORDER BY doc_path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GraphDocScore
	for rows.Next() {
		var sc GraphDocScore
		if err := rows.Scan(&sc.DocPath, &sc.DocType, &sc.Hub, &sc.Authority, &sc.Community, &sc.Inbound, &sc.Outbound, &sc.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) GraphDocScoresSummary(ctx context.Context) (GraphDocScoresState, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(MAX(updated_at), 0),
			COALESCE(SUM(CASE WHEN doc_type = 'note' THEN 1 ELSE 0 END), 0),
			COALESCE(MAX(CASE WHEN doc_type = 'note' THEN updated_at ELSE 0 END), 0)
		FROM graph_doc_scores
	`)
	var state GraphDocScoresState
	if err := row.Scan(&state.Count, &state.MaxUpdatedAt, &state.NoteCount, &state.NoteUpdatedAt); err != nil {
		return GraphDocScoresState{}, err
	}
	return state, nil
}

func (s *Store) GraphDocNoteScoresByCommunities(ctx context.Context, communityIDs []string, perCommunityLimit int) (map[string][]GraphDocScore, error) {
	communityIDs = normalizeNonEmptyStrings(communityIDs)
	if len(communityIDs) == 0 {
		return map[string][]GraphDocScore{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(communityIDs)), ",")
	limitClause := ""
	args := sliceAny(communityIDs)
	if perCommunityLimit > 0 {
		limitClause = "WHERE rank_in_community <= ?"
		args = append(args, perCommunityLimit)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		WITH ranked AS (
			SELECT
				doc_path, doc_type, hub, authority, community, inbound, outbound, updated_at,
				ROW_NUMBER() OVER (
					PARTITION BY community
					ORDER BY authority DESC, doc_path
				) AS rank_in_community
			FROM graph_doc_scores
			WHERE doc_type = 'note' AND community IN (%s)
		)
		SELECT doc_path, doc_type, hub, authority, community, inbound, outbound, updated_at
		FROM ranked
		%s
		ORDER BY community, authority DESC, doc_path
	`, placeholders, limitClause), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]GraphDocScore, len(communityIDs))
	for rows.Next() {
		var sc GraphDocScore
		if err := rows.Scan(&sc.DocPath, &sc.DocType, &sc.Hub, &sc.Authority, &sc.Community, &sc.Inbound, &sc.Outbound, &sc.UpdatedAt); err != nil {
			return nil, err
		}
		out[sc.Community] = append(out[sc.Community], sc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range communityIDs {
		if _, ok := out[id]; !ok {
			out[id] = []GraphDocScore{}
		}
	}
	for _, scores := range out {
		sort.Slice(scores, func(i, j int) bool {
			if scores[i].Authority != scores[j].Authority {
				return scores[i].Authority > scores[j].Authority
			}
			return scores[i].DocPath < scores[j].DocPath
		})
	}
	return out, nil
}
