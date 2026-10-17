package store

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/paths"
	"strings"
)

// SparseCallGraphThreshold gates the fallback path in the hotspot + doc_coverage
// reports. Below this resolution-rate (callsResolved / callsExtracted, per language),
// the queries also include rows sourced from intel_symbol_refs (raw extracted refs
// that may not have anchor-resolved on either end).
//
// WHY: 0.60 chosen empirically from wp-tst measurements (2026-06-15). PHP at 0.41
// post-EFF-0037 still produced JS-only hotspots/doc_coverage output because the
// anchor-join scoring path collapses to zero authority when most calls don't resolve.
// TS at 0.0036 hits the same wall. The rhizome repo's own Go index sits near 1.0
// and is unaffected. 0.60 is conservative — anything materially below it indicates
// a sparse-graph language where the fallback signal is more useful than the
// resolved-only signal. Override per-invocation via the FallbackThreshold field on
// the options struct or the --fallback-threshold CLI flag. See EFF-0038 + SPEC-0073.
const SparseCallGraphThreshold = 0.60

type DocCoverageRow struct {
	Lang     string
	Kind     string
	FQN      string
	Path     string
	Calls    int
	Callers  int
	Mentions int
	Links    int
	Resolved bool
	// Fallback is true when this row was surfaced via the sparse-graph fallback
	// path (intel_symbol_refs-derived rather than intel_edges-derived). Downstream
	// consumers (assessor skill, dashboards) use this to distinguish resolved-edge
	// rankings from fallback rankings.
	Fallback bool
}

type DocCoverageOptions struct {
	Limit int
	// Langs restricts results to the named language codes (e.g. "php", "go"). Empty = all.
	Langs []string
	// FallbackThreshold overrides SparseCallGraphThreshold when non-nil. Per-invocation
	// tuning for the sparse-graph fallback gating.
	FallbackThreshold *float64
	// PathPrefixes applies a prefix match against caller paths (vault-relative).
	PathPrefixes []string
	// FQNs narrows results to specific fully-qualified symbols.
	FQNs []string
}

// DocCoverage returns the most-used called symbols, joined with doc mention counts when resolvable.
//
// Usage is derived from `intel_edges(kind='calls')`; documentation coverage comes from `mentions` edges
// in `intel_edges` and requires a matching `intel_code_anchors` row (by lang+fqn).
//
// RATIONALE: When a language's call-resolution rate (callsResolved / callsExtracted from
// intel_symbol_refs) falls below the threshold (default SparseCallGraphThreshold, override
// via opts.FallbackThreshold), the query supplements its scoring with rows derived from
// intel_symbol_refs. This is the sparse-graph fallback: the existing anchor-join path
// produces zero rows for sparse-graph languages (e.g. wp-tst PHP at 0.41 — the anchor
// joins drop most calls because neither end resolves to an anchor). Surfacing the raw
// extracted refs gives the assessor skill a meaningful ranking even when the resolved-
// edge call graph is thin. Languages above threshold use the existing query path unchanged.
func (s *Store) DocCoverage(ctx context.Context, opts DocCoverageOptions) ([]DocCoverageRow, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 25
	}
	threshold := SparseCallGraphThreshold
	if opts.FallbackThreshold != nil {
		threshold = *opts.FallbackThreshold
	}
	if err := s.ensureAnalyticsTables(ctx); err != nil {
		return nil, err
	}

	args := []any{threshold}

	callWhere := []string{
		"e.kind = 'calls'",
		"callee.fqn IS NOT NULL AND callee.fqn != ''",
	}
	// Path filter shared between the resolved path and the fallback path. Each path
	// applies it to its own caller-file column (caller.path vs. intel_symbol_ref_files.path)
	// so the user-visible filter is consistent regardless of which scoring branch runs.
	var pathPrefixes []string
	for _, p := range opts.PathPrefixes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = string(paths.NormalizeCode(p))
		if p == "" {
			continue
		}
		pathPrefixes = append(pathPrefixes, p)
	}
	if len(pathPrefixes) > 0 {
		var pathConds []string
		for _, p := range pathPrefixes {
			pathConds = append(pathConds, "caller.path LIKE ?")
			args = append(args, p+"%")
		}
		callWhere = append(callWhere, fmt.Sprintf("(%s)", strings.Join(pathConds, " OR ")))
	}
	callWhereClause := strings.Join(callWhere, " AND ")

	var langFilter string
	if len(opts.Langs) > 0 {
		placeholders := strings.Repeat("?,", len(opts.Langs))
		placeholders = strings.TrimSuffix(placeholders, ",")
		langFilter = fmt.Sprintf(" AND u.lang IN (%s)", placeholders)
	}

	// Fallback-side path filter binds: same prefixes against intel_symbol_ref_files.path.
	var fallbackPathFilter string
	if len(pathPrefixes) > 0 {
		var conds []string
		for _, p := range pathPrefixes {
			conds = append(conds, "f.path LIKE ?")
			args = append(args, p+"%")
		}
		fallbackPathFilter = " AND (" + strings.Join(conds, " OR ") + ")"
	}

	// Lang filter args after threshold (so the order matches the SQL).
	for _, l := range opts.Langs {
		args = append(args, l)
	}

	var fqnFilter string
	if len(opts.FQNs) > 0 {
		placeholders := strings.Repeat("?,", len(opts.FQNs))
		placeholders = strings.TrimSuffix(placeholders, ",")
		fqnFilter = fmt.Sprintf(" AND u.fqn IN (%s)", placeholders)
		for _, f := range opts.FQNs {
			args = append(args, f)
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH lang_resolution AS (
			-- Per-language resolution rate from intel_symbol_refs; mirrors rzm code stats.
			SELECT
				t.dst_lang AS lang,
				CAST(COUNT(*) AS REAL) AS extracted,
				CAST(SUM(CASE WHEN sym.id IS NOT NULL THEN 1 ELSE 0 END) AS REAL) AS resolved
			FROM intel_symbol_refs r
			JOIN intel_symbol_ref_targets t ON t.target_id = r.dst_target_id
			LEFT JOIN symbols sym ON sym.lang = t.dst_lang AND sym.fqn = t.dst_fqn
			WHERE r.ref_kind = 'calls'
			GROUP BY t.dst_lang
		),
		sparse_langs AS (
			-- Languages where the fallback path should activate.
			SELECT lang FROM lang_resolution
			WHERE extracted > 0 AND (resolved / extracted) < ?
		),
		call_usage AS (
			SELECT
				callee.lang AS lang,
				callee.fqn AS fqn,
				COUNT(*) AS calls,
				COUNT(DISTINCT caller.path) AS callers,
				0 AS fallback
			FROM intel_edges e
			JOIN intel_code_anchors caller
			  ON e.src_type = 'anchor'
			 AND caller.id = e.src_row_id
			JOIN intel_code_anchors callee
			  ON e.dst_type = 'anchor'
			 AND callee.id = e.dst_row_id
			WHERE `+callWhereClause+`
			GROUP BY callee.lang, callee.fqn
		),
		fallback_usage AS (
			SELECT
				t.dst_lang AS lang,
				t.dst_fqn AS fqn,
				COUNT(*) AS calls,
				COUNT(DISTINCT r.src_file_id) AS callers,
				1 AS fallback
			FROM intel_symbol_refs r
			JOIN intel_symbol_ref_targets t ON t.target_id = r.dst_target_id
			JOIN sparse_langs sl ON sl.lang = t.dst_lang
			LEFT JOIN intel_symbol_ref_files f ON f.file_id = r.src_file_id
			WHERE r.ref_kind = 'calls' AND t.dst_fqn IS NOT NULL AND t.dst_fqn != ''`+fallbackPathFilter+`
			GROUP BY t.dst_lang, t.dst_fqn
		),
		all_usage AS (
			SELECT lang, fqn, calls, callers, fallback FROM call_usage
			UNION ALL
			SELECT lang, fqn, calls, callers, fallback FROM fallback_usage
		),
		ranked_usage AS (
			-- One row per (lang, fqn): keep whichever signal has the stronger
			-- usage count. Ties prefer resolved rows so high-resolution languages
			-- keep their original behavior.
			SELECT lang, fqn, calls, callers, fallback
			FROM all_usage u1
			WHERE NOT EXISTS (
			        SELECT 1 FROM all_usage u2
			        WHERE u2.lang = u1.lang
			          AND u2.fqn = u1.fqn
			          AND (
			            u2.calls > u1.calls
			            OR (u2.calls = u1.calls AND u2.callers > u1.callers)
			            OR (u2.calls = u1.calls AND u2.callers = u1.callers AND u2.fallback < u1.fallback)
			          )
			      )
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
		)
		SELECT
			u.lang,
			COALESCE(a.kind, '') AS kind,
			u.fqn,
			COALESCE(a.path, '') AS path,
			u.calls,
			u.callers,
			COALESCE(m.mentions, 0) AS mentions,
			COALESCE(dl.links, 0) AS links,
			CASE WHEN a.anchor_id IS NULL THEN 0 ELSE 1 END AS resolved,
			u.fallback
		FROM ranked_usage u
		LEFT JOIN intel_code_anchors a
			ON a.lang = u.lang AND a.fqn = u.fqn
		LEFT JOIN mentions m
			ON m.anchor_id = a.anchor_id
		LEFT JOIN doc_links_counts dl
			ON dl.anchor_id = a.anchor_id
		WHERE u.fqn IS NOT NULL
		`+langFilter+fqnFilter+`
		ORDER BY u.calls DESC, resolved DESC, u.fallback ASC, mentions ASC, u.fqn ASC
		LIMIT ?
	`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DocCoverageRow
	for rows.Next() {
		var r DocCoverageRow
		var resolved, fallback int
		if err := rows.Scan(&r.Lang, &r.Kind, &r.FQN, &r.Path, &r.Calls, &r.Callers, &r.Mentions, &r.Links, &resolved, &fallback); err != nil {
			return nil, err
		}
		r.Resolved = resolved != 0
		r.Fallback = fallback != 0
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
