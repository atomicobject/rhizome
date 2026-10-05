package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// GraphDocEdgesForPath returns note+code edges incident to a single path.
func (s *Store) GraphDocEdgesForPath(ctx context.Context, path string, limit int) ([]GraphDocEdge, error) {
	if limit <= 0 {
		limit = 500
	}
	out := make([]GraphDocEdge, 0, limit)
	appendRows := func(rows *sql.Rows, kind string) error {
		defer rows.Close()
		for rows.Next() {
			var src, dst string
			if err := rows.Scan(&src, &dst); err != nil {
				return err
			}
			out = append(out, GraphDocEdge{SrcPath: src, DstPath: dst, Kind: kind, Weight: 1})
			if len(out) >= limit {
				return nil
			}
		}
		return rows.Err()
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT src_path, dst_path
		FROM graph_doc_edges
		WHERE kind = 'wikilink' AND (src_path = ? OR dst_path = ?)
		ORDER BY src_path, dst_path
		LIMIT ?
	`, path, path, limit)
	if err != nil {
		return nil, err
	}
	if err := appendRows(rows, "wikilink"); err != nil {
		return nil, err
	}
	if len(out) >= limit {
		return out, nil
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT d.path AS src_path, a.path AS dst_path
		FROM intel_edges e
		JOIN intel_doc_sections d
		  ON e.src_type = 'doc_section'
		 AND d.id = e.src_row_id
		JOIN intel_code_anchors a
		  ON e.dst_type = 'anchor'
		 AND a.id = e.dst_row_id
		WHERE e.kind = 'mentions' AND (d.path = ? OR a.path = ?)
		ORDER BY d.path, a.path
		LIMIT ?
	`, path, path, limit-len(out))
	if err != nil {
		return nil, err
	}
	if err := appendRows(rows, "mentions"); err != nil {
		return nil, err
	}
	if len(out) >= limit {
		return out, nil
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT src_path, dst_path
		FROM doc_links
		WHERE src_type = 'code' AND dst_kind = 'note' AND (src_path = ? OR dst_path = ?)
		ORDER BY src_path, dst_path
		LIMIT ?
	`, path, path, limit-len(out))
	if err != nil {
		return nil, err
	}
	if err := appendRows(rows, "coderef"); err != nil {
		return nil, err
	}
	if len(out) >= limit {
		return out, nil
	}

	// Code-to-code edges (calls, imports, tests, etc.) - unified query
	remaining := limit - len(out)
	rows, err = s.db.QueryContext(ctx, `
		SELECT src.path, dst.path, e.kind, COUNT(*) AS weight
		FROM intel_edges e
		JOIN intel_code_anchors src
		  ON e.src_type = 'anchor'
		 AND src.id = e.src_row_id
		JOIN intel_code_anchors dst
		  ON e.dst_type = 'anchor'
		 AND dst.id = e.dst_row_id
		WHERE e.kind NOT IN ('defines', 'mentions')
		  AND (src.path = ? OR dst.path = ?)
		GROUP BY src.path, dst.path, e.kind
		ORDER BY src.path, dst.path
		LIMIT ?
	`, path, path, remaining)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var src, dst, kind string
		var weight int
		if err := rows.Scan(&src, &dst, &kind, &weight); err != nil {
			return nil, err
		}
		out = append(out, GraphDocEdge{SrcPath: src, DstPath: dst, Kind: kind, Weight: weight})
		if len(out) >= limit {
			return out, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

// GraphDocBacklinksForPath returns note->note edges targeting the supplied note path.
func (s *Store) GraphDocBacklinksForPath(ctx context.Context, path string, limit int) ([]GraphDocEdgeRow, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_path, dst_path, kind, confidence, confidence_score
		FROM graph_doc_edges
		WHERE dst_path = ?
		ORDER BY src_path, dst_path, kind
		LIMIT ?
	`, path, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]GraphDocEdgeRow, 0, limit)
	for rows.Next() {
		var row GraphDocEdgeRow
		if err := rows.Scan(&row.SrcPath, &row.DstPath, &row.Kind, &row.Confidence, &row.ConfidenceScore); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// GraphDocEdgesForPaths returns note+code edges incident to any of the provided paths.
// When includeCalls=false, call edges are skipped entirely.
func (s *Store) GraphDocEdgesForPaths(ctx context.Context, paths []string, limit int, includeCalls bool) ([]GraphDocEdge, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}

	// Deduplicate + drop empty paths while preserving order for determinism.
	seen := make(map[string]struct{}, len(paths))
	unique := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		unique = append(unique, p)
	}
	if len(unique) == 0 {
		return nil, nil
	}

	out := make([]GraphDocEdge, 0, min(limit, len(unique)*8))

	appendRows := func(rows *sql.Rows, kind string) error {
		defer rows.Close()
		for rows.Next() {
			var src, dst string
			if err := rows.Scan(&src, &dst); err != nil {
				return err
			}
			out = append(out, GraphDocEdge{SrcPath: src, DstPath: dst, Kind: kind, Weight: 1})
			if len(out) >= limit {
				return nil
			}
		}
		return rows.Err()
	}

	const chunkSize = 64
	for i := 0; i < len(unique) && len(out) < limit; i += chunkSize {
		end := i + chunkSize
		if end > len(unique) {
			end = len(unique)
		}
		chunk := unique[i:end]
		placeholders := strings.Repeat("?,", len(chunk))
		placeholders = strings.TrimRight(placeholders, ",")

		makeArgs := func(remaining int) []any {
			args := make([]any, 0, len(chunk)*2+1)
			args = append(args, sliceAny(chunk)...)
			args = append(args, sliceAny(chunk)...)
			args = append(args, remaining)
			return args
		}

		remaining := limit - len(out)
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT src_path, dst_path
			FROM graph_doc_edges
			WHERE kind = 'wikilink' AND (src_path IN (%s) OR dst_path IN (%s))
			ORDER BY src_path, dst_path
			LIMIT ?
		`, placeholders, placeholders), makeArgs(remaining)...)
		if err != nil {
			return nil, err
		}
		if err := appendRows(rows, "wikilink"); err != nil {
			return nil, err
		}
		if len(out) >= limit {
			break
		}

		remaining = limit - len(out)
		rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT d.path AS src_path, a.path AS dst_path
			FROM intel_edges e
			JOIN intel_doc_sections d
			  ON e.src_type = 'doc_section'
			 AND d.id = e.src_row_id
			JOIN intel_code_anchors a
			  ON e.dst_type = 'anchor'
			 AND a.id = e.dst_row_id
			WHERE e.kind = 'mentions' AND (d.path IN (%s) OR a.path IN (%s))
			ORDER BY d.path, a.path
			LIMIT ?
		`, placeholders, placeholders), makeArgs(remaining)...)
		if err != nil {
			return nil, err
		}
		if err := appendRows(rows, "mentions"); err != nil {
			return nil, err
		}
		if len(out) >= limit {
			break
		}

		remaining = limit - len(out)
		rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT src_path, dst_path
			FROM doc_links
			WHERE src_type = 'code' AND dst_kind = 'note' AND (src_path IN (%s) OR dst_path IN (%s))
			ORDER BY src_path, dst_path
			LIMIT ?
		`, placeholders, placeholders), makeArgs(remaining)...)
		if err != nil {
			return nil, err
		}
		if err := appendRows(rows, "coderef"); err != nil {
			return nil, err
		}
		if len(out) >= limit {
			break
		}

		// Code-to-code edges (calls, imports, tests, etc.) - unified query
		// includeCalls controls whether to include any code-to-code edges
		if includeCalls {
			remaining = limit - len(out)
			rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
				SELECT src.path, dst.path, e.kind, COUNT(*) AS weight
				FROM intel_edges e
				JOIN intel_code_anchors src
				  ON e.src_type = 'anchor'
				 AND src.id = e.src_row_id
				JOIN intel_code_anchors dst
				  ON e.dst_type = 'anchor'
				 AND dst.id = e.dst_row_id
				WHERE e.kind NOT IN ('defines', 'mentions')
				  AND (src.path IN (%s) OR dst.path IN (%s))
				GROUP BY src.path, dst.path, e.kind
				ORDER BY src.path, dst.path
				LIMIT ?
			`, placeholders, placeholders), makeArgs(remaining)...)
			if err != nil {
				return nil, err
			}
			func() {
				defer rows.Close()
				for rows.Next() {
					var src, dst, kind string
					var weight int
					if err := rows.Scan(&src, &dst, &kind, &weight); err != nil {
						return
					}
					out = append(out, GraphDocEdge{SrcPath: src, DstPath: dst, Kind: kind, Weight: weight})
					if len(out) >= limit {
						return
					}
				}
			}()
		}
	}

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// GraphDocEdgesForPrefix returns edges for paths with the given prefix.
func (s *Store) GraphDocEdgesForPrefix(ctx context.Context, prefix string, limit int, includeCode bool) ([]GraphDocEdge, error) {
	if limit <= 0 {
		limit = 1000
	}
	like := prefix + "/%"
	out := make([]GraphDocEdge, 0, limit)
	appendRows := func(rows *sql.Rows, kind string) error {
		defer rows.Close()
		for rows.Next() {
			var src, dst string
			if err := rows.Scan(&src, &dst); err != nil {
				return err
			}
			out = append(out, GraphDocEdge{SrcPath: src, DstPath: dst, Kind: kind, Weight: 1})
			if len(out) >= limit {
				return nil
			}
		}
		return rows.Err()
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT src_path, dst_path
		FROM graph_doc_edges
		WHERE kind = 'wikilink' AND (src_path = ? OR dst_path = ? OR src_path LIKE ? OR dst_path LIKE ?)
		ORDER BY src_path, dst_path
		LIMIT ?
	`, prefix, prefix, like, like, limit)
	if err != nil {
		return nil, err
	}
	if err := appendRows(rows, "wikilink"); err != nil {
		return nil, err
	}
	if len(out) >= limit {
		return out, nil
	}

	if includeCode {
		rows, err = s.db.QueryContext(ctx, `
			SELECT d.path AS src_path, a.path AS dst_path
			FROM intel_edges e
			JOIN intel_doc_sections d
			  ON e.src_type = 'doc_section'
			 AND d.id = e.src_row_id
			JOIN intel_code_anchors a
			  ON e.dst_type = 'anchor'
			 AND a.id = e.dst_row_id
			WHERE e.kind = 'mentions' AND (d.path = ? OR a.path = ? OR d.path LIKE ? OR a.path LIKE ?)
			ORDER BY d.path, a.path
			LIMIT ?
		`, prefix, prefix, like, like, limit-len(out))
		if err != nil {
			return nil, err
		}
		if err := appendRows(rows, "mentions"); err != nil {
			return nil, err
		}
		if len(out) >= limit {
			return out, nil
		}

		rows, err = s.db.QueryContext(ctx, `
			SELECT src_path, dst_path
			FROM doc_links
			WHERE src_type = 'code' AND dst_kind = 'note' AND (src_path = ? OR dst_path = ? OR src_path LIKE ? OR dst_path LIKE ?)
			ORDER BY src_path, dst_path
			LIMIT ?
		`, prefix, prefix, like, like, limit-len(out))
		if err != nil {
			return nil, err
		}
		if err := appendRows(rows, "coderef"); err != nil {
			return nil, err
		}
		if len(out) >= limit {
			return out, nil
		}

		// Code-to-code edges (calls, imports, tests, etc.) - unified query.
		remaining := limit - len(out)
		rows, err = s.db.QueryContext(ctx, `
			SELECT src.path, dst.path, e.kind, COUNT(*) AS weight
			FROM intel_edges e
			JOIN intel_code_anchors src
			  ON e.src_type = 'anchor'
			 AND src.id = e.src_row_id
			JOIN intel_code_anchors dst
			  ON e.dst_type = 'anchor'
			 AND dst.id = e.dst_row_id
			WHERE e.kind NOT IN ('defines', 'mentions')
			  AND (src.path = ? OR dst.path = ? OR src.path LIKE ? OR dst.path LIKE ?)
			GROUP BY src.path, dst.path, e.kind
			ORDER BY src.path, dst.path
			LIMIT ?
		`, prefix, prefix, like, like, remaining)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var src, dst, kind string
			var weight int
			if err := rows.Scan(&src, &dst, &kind, &weight); err != nil {
				return nil, err
			}
			out = append(out, GraphDocEdge{SrcPath: src, DstPath: dst, Kind: kind, Weight: weight})
			if len(out) >= limit {
				return out, nil
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func sliceAny(items []string) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}
