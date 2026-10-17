package store

import (
	"context"
	"strings"
)

// GraphDocMentionEdges returns note-path -> code-path edges derived from intel_edges(kind='mentions').
func (s *Store) GraphDocMentionEdges(ctx context.Context) ([]GraphDocEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.path AS src_path, a.path AS dst_path
		FROM intel_edges e
		JOIN intel_doc_sections d
		  ON e.src_type = 'doc_section'
		 AND d.id = e.src_row_id
		JOIN intel_code_anchors a
		  ON e.dst_type = 'anchor'
		 AND a.id = e.dst_row_id
		WHERE e.kind = 'mentions'
		ORDER BY d.path, a.path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GraphDocEdge
	for rows.Next() {
		var src, dst string
		if err := rows.Scan(&src, &dst); err != nil {
			return nil, err
		}
		out = append(out, GraphDocEdge{SrcPath: src, DstPath: dst, Kind: "mentions", Weight: 1})
	}
	return out, rows.Err()
}

// GraphDocCodeRefEdges returns code-path -> note-path edges derived from doc_links(src_type='code', dst_kind='note').
func (s *Store) GraphDocCodeRefEdges(ctx context.Context) ([]GraphDocEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_path, dst_path
		FROM doc_links
		WHERE src_type = 'code' AND dst_kind = 'note'
		ORDER BY src_path, dst_path
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GraphDocEdge
	for rows.Next() {
		var src, dst string
		if err := rows.Scan(&src, &dst); err != nil {
			return nil, err
		}
		out = append(out, GraphDocEdge{SrcPath: src, DstPath: dst, Kind: "coderef", Weight: 1})
	}
	return out, rows.Err()
}

// GraphDocCodeEdges returns all code-path -> code-path edges from intel_edges,
// aggregated at the file level. Includes calls, imports, tests, and any future edge kinds.
// Excludes 'defines' edges (self-references) and 'mentions' (note->code, handled separately).
func (s *Store) GraphDocCodeEdges(ctx context.Context) ([]GraphDocEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT src.path, dst.path, e.kind, COUNT(*) AS weight
		FROM intel_edges e
		JOIN intel_code_anchors src
		  ON e.src_type = 'anchor'
		 AND src.id = e.src_row_id
		JOIN intel_code_anchors dst
		  ON e.dst_type = 'anchor'
		 AND dst.id = e.dst_row_id
		WHERE e.kind NOT IN ('defines', 'mentions')
		GROUP BY src.path, dst.path, e.kind
		ORDER BY src.path, dst.path, e.kind
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GraphDocEdge
	for rows.Next() {
		var src, dst, kind string
		var weight int
		if err := rows.Scan(&src, &dst, &kind, &weight); err != nil {
			return nil, err
		}
		if strings.TrimSpace(src) == "" || strings.TrimSpace(dst) == "" {
			continue
		}
		out = append(out, GraphDocEdge{SrcPath: src, DstPath: dst, Kind: kind, Weight: weight})
	}
	return out, rows.Err()
}

// AllGraphDocEdges returns the full doc+code graph in one round trip, with
// code edges already aggregated at the file level.
func (s *Store) AllGraphDocEdges(ctx context.Context) ([]GraphDocEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_path, dst_path, kind, weight
		FROM (
			SELECT src_path, dst_path, 'wikilink' AS kind, COUNT(*) AS weight
			FROM graph_doc_edges
			WHERE kind = 'wikilink'
			GROUP BY src_path, dst_path
			UNION ALL
			SELECT d.path AS src_path, a.path AS dst_path, 'mentions' AS kind, COUNT(*) AS weight
			FROM intel_edges e
			JOIN intel_doc_sections d
			  ON e.src_type = 'doc_section'
			 AND d.id = e.src_row_id
			JOIN intel_code_anchors a
			  ON e.dst_type = 'anchor'
			 AND a.id = e.dst_row_id
			WHERE e.kind = 'mentions'
			GROUP BY d.path, a.path
			UNION ALL
			SELECT src_path, dst_path, 'coderef' AS kind, COUNT(*) AS weight
			FROM doc_links
			WHERE src_type = 'code' AND dst_kind = 'note'
			GROUP BY src_path, dst_path
			UNION ALL
			SELECT src.path AS src_path, dst.path AS dst_path, e.kind AS kind, COUNT(*) AS weight
			FROM intel_edges e
			JOIN intel_code_anchors src
			  ON e.src_type = 'anchor'
			 AND src.id = e.src_row_id
			JOIN intel_code_anchors dst
			  ON e.dst_type = 'anchor'
			 AND dst.id = e.dst_row_id
			WHERE e.kind NOT IN ('defines', 'mentions')
			GROUP BY src.path, dst.path, e.kind
		)
		ORDER BY src_path, dst_path, kind
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GraphDocEdge
	for rows.Next() {
		var edge GraphDocEdge
		if err := rows.Scan(&edge.SrcPath, &edge.DstPath, &edge.Kind, &edge.Weight); err != nil {
			return nil, err
		}
		out = append(out, edge)
	}
	return out, rows.Err()
}
