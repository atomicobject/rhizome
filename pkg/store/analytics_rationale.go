package store

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/paths"
	"strings"
	"unicode/utf8"
)

func (s *Store) RationaleAttention(ctx context.Context, opts RationaleAttentionOptions) ([]RationaleAttentionRow, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 25
	}
	if err := s.ensureAnalyticsTables(ctx); err != nil {
		return nil, err
	}

	where := []string{"1=1"}
	var args []any
	if len(opts.PathPrefixes) > 0 {
		var pathConds []string
		for _, p := range opts.PathPrefixes {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			p = string(paths.NormalizeCode(p))
			if p == "" {
				continue
			}
			pathConds = append(pathConds, "r.path LIKE ?")
			args = append(args, p+"%")
		}
		if len(pathConds) > 0 {
			where = append(where, fmt.Sprintf("(%s)", strings.Join(pathConds, " OR ")))
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH call_in AS (
			SELECT
				callee.anchor_id,
				COUNT(*) AS calls,
				COUNT(DISTINCT caller.path) AS callers
			FROM intel_edges e
			JOIN intel_code_anchors caller
			  ON e.src_type = 'anchor'
			 AND caller.id = e.src_row_id
			JOIN intel_code_anchors callee
			  ON e.dst_type = 'anchor'
			 AND callee.id = e.dst_row_id
			WHERE e.kind = 'calls'
			GROUP BY callee.anchor_id
		),
		mentions AS (
			SELECT a.anchor_id AS anchor_id, COUNT(*) AS mentions
			FROM intel_edges e
			JOIN intel_code_anchors a
			  ON e.dst_type = 'anchor'
			 AND a.id = e.dst_row_id
			WHERE e.kind = 'mentions'
			GROUP BY a.anchor_id
		),
		doc_links_counts AS (
			SELECT dst_id AS anchor_id, COUNT(*) AS links
			FROM doc_links
			WHERE dst_kind = 'anchor'
			GROUP BY dst_id
		),
		rationale_anchor_stats AS (
			SELECT
				r.rationale_id,
				MAX(COALESCE(ci.calls, 0)) AS calls,
				MAX(COALESCE(ci.callers, 0)) AS callers,
				MAX(COALESCE(m.mentions, 0)) AS mentions,
				MAX(COALESCE(dl.links, 0)) AS links
			FROM intel_rationale r
			LEFT JOIN intel_code_anchors a
			  ON r.symbol_fqn IS NOT NULL
			 AND r.symbol_fqn != ''
			 AND (a.fqn = r.symbol_fqn OR a.fqn LIKE ('%.' || r.symbol_fqn))
			LEFT JOIN call_in ci ON ci.anchor_id = a.anchor_id
			LEFT JOIN mentions m ON m.anchor_id = a.anchor_id
			LEFT JOIN doc_links_counts dl ON dl.anchor_id = a.anchor_id
			GROUP BY r.rationale_id
		)
		SELECT
			r.path,
			COALESCE(r.symbol_fqn, '') AS symbol_fqn,
			r.kind,
			r.start_line,
			r.content,
			COALESCE(ras.calls, 0) AS calls,
			COALESCE(ras.callers, 0) AS callers,
			COALESCE(ras.mentions, 0) AS mentions,
			COALESCE(ras.links, 0) AS links
		FROM intel_rationale r
		LEFT JOIN rationale_anchor_stats ras ON ras.rationale_id = r.rationale_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY
			CASE r.kind
				WHEN 'fixme' THEN 0
				WHEN 'todo' THEN 1
				WHEN 'hack' THEN 2
				WHEN 'important' THEN 3
				ELSE 4
			END,
			callers DESC,
			calls DESC,
			r.path ASC,
			r.start_line ASC
		LIMIT ?
	`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]RationaleAttentionRow, 0, limit)
	for rows.Next() {
		var r RationaleAttentionRow
		if err := rows.Scan(&r.Path, &r.SymbolFQN, &r.Kind, &r.Line, &r.Content, &r.Calls, &r.Callers, &r.Mentions, &r.Links); err != nil {
			return nil, err
		}
		r.Content = trimAnalyticsSnippet(r.Content, 240)
		r.Reasons, r.Attention = rationaleAttentionReasons(r)
		if len(r.Reasons) == 0 {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func rationaleAttentionReasons(r RationaleAttentionRow) ([]string, int) {
	var reasons []string
	score := 0
	switch strings.ToLower(strings.TrimSpace(r.Kind)) {
	case "todo", "fixme":
		reasons = append(reasons, "todo_or_fixme")
		score += 5
	case "hack":
		reasons = append(reasons, "hack")
		score += 4
	case "important":
		reasons = append(reasons, "important_marker")
		score += 3
	}
	if strings.TrimSpace(r.SymbolFQN) == "" {
		reasons = append(reasons, "file_level_rationale")
		score++
	}
	if r.Callers >= 2 || r.Calls >= 3 {
		reasons = append(reasons, "high_fanin_symbol")
		score += 3
	}
	if strings.TrimSpace(r.SymbolFQN) != "" && r.Mentions == 0 && r.Links == 0 {
		reasons = append(reasons, "undocumented_symbol")
		score += 2
	}
	return reasons, score
}

func trimAnalyticsSnippet(s string, limit int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if limit <= 0 || len(s) <= limit {
		return s
	}
	trimAt := limit
	for trimAt > 0 && !utf8.RuneStart(s[trimAt]) {
		trimAt--
	}
	return s[:trimAt] + "..."
}
