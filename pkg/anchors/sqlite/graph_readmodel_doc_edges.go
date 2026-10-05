package sqlite

import (
	"context"
	"fmt"
	"strings"
)

func (s *Store) graphDocEdgesForPrefixReadmodel(ctx context.Context, prefix string, limit int, includeCode, includeCodeEdges bool) ([]GraphDocEdge, error) {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return nil, nil
	}
	like := prefix + "/%"
	where := "(src_path = ? OR dst_path = ? OR src_path LIKE ? OR dst_path LIKE ?)"
	args := []any{prefix, prefix, like, like}
	return s.graphDocEdgesReadmodelQuery(ctx, where, args, limit, includeCode, includeCodeEdges)
}

func (s *Store) graphDocEdgesForPathsReadmodel(ctx context.Context, paths []string, limit int, includeCode, includeCodeEdges bool) ([]GraphDocEdge, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(paths)), ",")
	where := fmt.Sprintf("(src_path IN (%s) OR dst_path IN (%s))", placeholders, placeholders)
	args := make([]any, 0, len(paths)*2)
	args = append(args, sliceAny(paths)...)
	args = append(args, sliceAny(paths)...)
	return s.graphDocEdgesReadmodelQuery(ctx, where, args, limit, includeCode, includeCodeEdges)
}

func (s *Store) allGraphDocEdgesReadmodel(ctx context.Context, limit int, includeCode, includeCodeEdges bool) ([]GraphDocEdge, error) {
	return s.graphDocEdgesReadmodelQuery(ctx, "", nil, limit, includeCode, includeCodeEdges)
}

func (s *Store) graphDocEdgesReadmodelQuery(ctx context.Context, filter string, filterArgs []any, limit int, includeCode, includeCodeEdges bool) ([]GraphDocEdge, error) {
	// Docs: [[ontology-indexed-read-model-contract#^spec-0040-table-ownership-docedges]]
	// defines these rows as fallback doc/code graph evidence, not typed ontology edges.
	out := make([]GraphDocEdge, 0)
	limited := limit > 0
	remaining := func() int {
		if !limited {
			return 1
		}
		return limit - len(out)
	}
	queryFilter := func(alias string) string {
		if strings.TrimSpace(filter) == "" {
			return ""
		}
		if alias == "" {
			return " AND " + filter
		}
		replacer := strings.NewReplacer("src_path", alias+".src_path", "dst_path", alias+".dst_path")
		return " AND " + replacer.Replace(filter)
	}
	appendRows := func(rows scannerRows) error {
		defer rows.Close()
		for rows.Next() {
			var edge GraphDocEdge
			if err := rows.Scan(&edge.SrcPath, &edge.DstPath, &edge.Kind, &edge.Weight, &edge.Confidence, &edge.ConfidenceScore, &edge.SourceKind, &edge.TargetKind); err != nil {
				return err
			}
			if edge.Weight <= 0 {
				edge.Weight = 1
			}
			out = append(out, edge)
			if limited && len(out) >= limit {
				return nil
			}
		}
		return rows.Err()
	}
	withLimit := func(query string, args []any) (string, []any) {
		if !limited {
			return query, args
		}
		return query + "\n\t\t\t\tLIMIT ?", append(args, remaining())
	}
	if remaining() > 0 {
		args := append([]any{}, filterArgs...)
		query, args := withLimit(`
			SELECT src_path, dst_path, kind, COUNT(*) AS weight,
					COALESCE(NULLIF(MAX(confidence), ''), 'extracted') AS confidence,
					COALESCE(NULLIF(MAX(confidence_score), 0), 1.0) AS confidence_score,
					'note' AS source_kind, 'note' AS target_kind
			FROM graph_doc_edges
				WHERE (kind = 'wikilink' OR kind = 'mdlink' OR kind GLOB 'note_link:*')`+queryFilter("")+`
				GROUP BY src_path, dst_path, kind
				ORDER BY src_path, dst_path, kind
			`, args)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		if err := appendRows(rows); err != nil {
			return nil, err
		}
	}
	if includeCode && remaining() > 0 {
		args := append([]any{}, filterArgs...)
		query, args := withLimit(`
			SELECT d.path AS src_path, a.path AS dst_path, 'mentions' AS kind, COUNT(*) AS weight,
					'extracted' AS confidence, 1.0 AS confidence_score,
					'note' AS source_kind, 'code' AS target_kind
				FROM intel_edges e
			JOIN intel_doc_sections d ON e.src_type = 'doc_section' AND d.id = e.src_row_id
			JOIN intel_code_anchors a ON e.dst_type = 'anchor' AND a.id = e.dst_row_id
				WHERE e.kind = 'mentions'`+strings.NewReplacer("src_path", "d.path", "dst_path", "a.path").Replace(queryFilter(""))+`
				GROUP BY d.path, a.path
				ORDER BY d.path, a.path
			`, args)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		if err := appendRows(rows); err != nil {
			return nil, err
		}
	}
	if includeCode && remaining() > 0 {
		args := append([]any{}, filterArgs...)
		query, args := withLimit(`
			SELECT l.src_path, l.dst_path, 'coderef' AS kind, COUNT(*) AS weight,
					'extracted' AS confidence, 1.0 AS confidence_score,
					'code' AS source_kind, 'note' AS target_kind
				FROM doc_links l
				WHERE l.src_type = 'code' AND l.dst_kind = 'note'`+queryFilter("l")+`
				GROUP BY l.src_path, l.dst_path
				ORDER BY l.src_path, l.dst_path
			`, args)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		if err := appendRows(rows); err != nil {
			return nil, err
		}
	}
	if includeCode && includeCodeEdges && remaining() > 0 {
		args := append([]any{}, filterArgs...)
		query, args := withLimit(`
			SELECT src.path AS src_path, dst.path AS dst_path, e.kind, COUNT(*) AS weight,
					'extracted' AS confidence, 1.0 AS confidence_score,
					'code' AS source_kind, 'code' AS target_kind
				FROM intel_edges e
			JOIN intel_code_anchors src ON e.src_type = 'anchor' AND src.id = e.src_row_id
			JOIN intel_code_anchors dst ON e.dst_type = 'anchor' AND dst.id = e.dst_row_id
				WHERE e.kind NOT IN ('defines', 'mentions')`+strings.NewReplacer("src_path", "src.path", "dst_path", "dst.path").Replace(queryFilter(""))+`
				GROUP BY src.path, dst.path, e.kind
				ORDER BY src.path, dst.path, e.kind
			`, args)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		if err := appendRows(rows); err != nil {
			return nil, err
		}
	}
	return out, nil
}

type scannerRows interface {
	Close() error
	Next() bool
	Scan(...any) error
	Err() error
}
