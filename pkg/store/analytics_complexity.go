package store

import (
	"context"
	"fmt"
	"strings"
)

// ComplexityHotspots ranks symbol-level hotspots by size and call fan-in/out.
func (s *Store) ComplexityHotspots(ctx context.Context, opts ComplexityOptions) ([]ComplexityRow, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 25
	}
	if err := s.ensureAnalyticsTables(ctx); err != nil {
		return nil, err
	}

	anchorWhere := []string{
		"a.fqn IS NOT NULL AND a.fqn != ''",
		"a.kind IN ('function', 'method')",
	}
	var args []any
	if len(opts.PathPrefixes) > 0 {
		var pathConds []string
		for _, p := range opts.PathPrefixes {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			pathConds = append(pathConds, "a.path LIKE ?")
			args = append(args, p+"%")
		}
		if len(pathConds) > 0 {
			anchorWhere = append(anchorWhere, fmt.Sprintf("(%s)", strings.Join(pathConds, " OR ")))
		}
	}
	anchorWhereClause := strings.Join(anchorWhere, " AND ")

	minLinesClause := ""
	if opts.MinLines > 0 {
		minLinesClause = "WHERE b.span_lines >= ?"
		args = append(args, opts.MinLines)
	}

	query := fmt.Sprintf(`
		WITH anchor_base AS (
			SELECT
				a.anchor_id,
				a.lang,
				a.kind,
				a.fqn,
				a.path,
				CASE
					WHEN a.end_line >= a.start_line AND a.start_line > 0 THEN (a.end_line - a.start_line + 1)
					ELSE 1
				END AS span_lines
			FROM intel_code_anchors a
			WHERE %s
		),
		call_in AS (
			SELECT
				callee.lang AS lang,
				callee.fqn AS fqn,
				COUNT(*) AS calls_in,
				COUNT(DISTINCT caller.path) AS callers
			FROM intel_edges e
			JOIN intel_code_anchors caller
			  ON e.src_type = 'anchor'
			 AND caller.id = e.src_row_id
			JOIN intel_code_anchors callee
			  ON e.dst_type = 'anchor'
			 AND callee.id = e.dst_row_id
			WHERE e.kind = 'calls'
			  AND callee.lang IS NOT NULL AND callee.lang != ''
			  AND callee.fqn IS NOT NULL AND callee.fqn != ''
			GROUP BY callee.lang, callee.fqn
		),
		call_out AS (
			SELECT
				caller.lang AS lang,
				caller.fqn AS fqn,
				COUNT(*) AS calls_out,
				COUNT(DISTINCT callee.fqn) AS callees
			FROM intel_edges e
			JOIN intel_code_anchors caller
			  ON e.src_type = 'anchor'
			 AND caller.id = e.src_row_id
			JOIN intel_code_anchors callee
			  ON e.dst_type = 'anchor'
			 AND callee.id = e.dst_row_id
			WHERE e.kind = 'calls'
			  AND caller.fqn IS NOT NULL AND caller.fqn != ''
			  AND callee.fqn IS NOT NULL AND callee.fqn != ''
			GROUP BY caller.lang, caller.fqn
		)
		SELECT
			b.lang,
			b.kind,
			b.fqn,
			b.path,
			b.span_lines,
			COALESCE(ci.calls_in, 0) AS calls_in,
			COALESCE(ci.callers, 0) AS callers,
			COALESCE(co.calls_out, 0) AS calls_out,
			COALESCE(co.callees, 0) AS callees,
			(b.span_lines * (1 + COALESCE(ci.callers, 0)) * (1 + (COALESCE(co.callees, 0) / 5.0))) AS score
		FROM anchor_base b
		LEFT JOIN call_in ci
			ON ci.lang = b.lang AND ci.fqn = b.fqn
		LEFT JOIN call_out co
			ON co.lang = b.lang AND co.fqn = b.fqn
		%s
		ORDER BY score DESC, b.span_lines DESC, callers DESC, callees DESC, b.path ASC, b.fqn ASC
		LIMIT ?
	`, anchorWhereClause, minLinesClause)

	rows, err := s.db.QueryContext(ctx, query, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ComplexityRow
	for rows.Next() {
		var r ComplexityRow
		if err := rows.Scan(
			&r.Lang, &r.Kind, &r.FQN, &r.Path, &r.SpanLines,
			&r.CallsIn, &r.Callers, &r.CallsOut, &r.Callees, &r.Score,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
