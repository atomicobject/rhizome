package store

import (
	"context"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"strings"
)

func (s *Store) ensureAnalyticsTables(ctx context.Context) error {
	// Cheap existence check that yields a helpful error instead of "no such table" deep in queries.
	_, err := s.db.ExecContext(ctx, `SELECT 1 FROM intel_code_anchors LIMIT 1`)
	if err != nil {
		return fmt.Errorf("intel tables unavailable (run `rzm index`): %w", err)
	}
	_, err = s.db.ExecContext(ctx, `SELECT 1 FROM intel_edges LIMIT 1`)
	if err != nil {
		return fmt.Errorf("intel edges unavailable (run `rzm index`): %w", err)
	}
	_, err = s.db.ExecContext(ctx, `SELECT 1 FROM intel_fts LIMIT 1`)
	if err != nil {
		return fmt.Errorf("intel FTS unavailable (run `rzm index`): %w", err)
	}
	return nil
}

// DocStatsForFQNs returns documentation coverage stats for the provided FQNs, keyed by fqn.
// Requires intel tables (see ensureAnalyticsTables).
func (s *Store) DocStatsForFQNs(ctx context.Context, lang string, fqns []string) (map[string]codeanchor.DocStats, error) {
	if len(fqns) == 0 {
		return map[string]codeanchor.DocStats{}, nil
	}
	if err := s.ensureAnalyticsTables(ctx); err != nil {
		return nil, err
	}

	placeholders := strings.Repeat("?,", len(fqns))
	placeholders = strings.TrimSuffix(placeholders, ",")

	args := make([]any, 0, 1+len(fqns))
	args = append(args, lang)
	for _, f := range fqns {
		args = append(args, f)
	}

	query := fmt.Sprintf(`
		WITH mentions AS (
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
			a.fqn,
			COALESCE(a.kind, '') AS kind,
			COALESCE(a.path, '') AS path,
			COALESCE(m.mentions, 0) AS mentions,
			COALESCE(dl.links, 0) AS links,
			CASE WHEN a.anchor_id IS NULL THEN 0 ELSE 1 END AS resolved
		FROM intel_code_anchors a
		LEFT JOIN mentions m
			ON m.anchor_id = a.anchor_id
		LEFT JOIN doc_links_counts dl
			ON dl.anchor_id = a.anchor_id
		WHERE a.lang = ?
		  AND a.fqn IN (%s)
	`, placeholders)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]codeanchor.DocStats, len(fqns))
	for rows.Next() {
		var fqn, kind, path string
		var mentions, links int
		var resolvedInt int
		if err := rows.Scan(&fqn, &kind, &path, &mentions, &links, &resolvedInt); err != nil {
			return nil, err
		}
		out[fqn] = codeanchor.DocStats{
			Kind:     kind,
			Path:     path,
			Mentions: mentions,
			Links:    links,
			Resolved: resolvedInt != 0,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SymbolMetaForFQNs returns symbol metadata for the provided FQNs, keyed by fqn.
func (s *Store) SymbolMetaForFQNs(ctx context.Context, lang string, fqns []string) (map[string]codeanchor.SymbolMeta, error) {
	if len(fqns) == 0 {
		return map[string]codeanchor.SymbolMeta{}, nil
	}

	placeholders := strings.Repeat("?,", len(fqns))
	placeholders = strings.TrimSuffix(placeholders, ",")

	args := make([]any, 0, 1+len(fqns))
	args = append(args, lang)
	for _, f := range fqns {
		args = append(args, f)
	}

	query := fmt.Sprintf(`
		SELECT fqn, kind, file, COALESCE(pkg, ''), name
		FROM symbols
		WHERE lang = ?
		  AND fqn IN (%s)
	`, placeholders)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]codeanchor.SymbolMeta, len(fqns))
	for rows.Next() {
		var fqn, kind, file, pkg, name string
		if err := rows.Scan(&fqn, &kind, &file, &pkg, &name); err != nil {
			return nil, err
		}
		out[fqn] = codeanchor.SymbolMeta{
			Lang:     codeanchor.Lang(lang),
			Kind:     codeanchor.SymbolKind(kind),
			File:     file,
			Pkg:      pkg,
			Name:     name,
			FQN:      fqn,
			Exported: isExportedGoName(name),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func isExportedGoName(name string) bool {
	if name == "" {
		return false
	}
	r := rune(name[0])
	return r >= 'A' && r <= 'Z'
}
