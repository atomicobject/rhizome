package sqlite

import (
	"context"
	"fmt"
	"strings"
)

type GraphDocEdge struct {
	SrcPath         string
	DstPath         string
	SourceKind      string
	TargetKind      string
	Kind            string
	Weight          int
	Confidence      string  // "extracted", "inferred", "ambiguous"
	ConfidenceScore float64 // 0.0–1.0
	SourceLocation  string  // "L{line}" or ""
}

type GraphDocDegree struct {
	Inbound  int
	Outbound int
}

type GraphDocNoteNeighborhood struct {
	Edges   []GraphDocEdge
	Degrees map[string]GraphDocDegree
}

// GraphWebFingerprint returns a compact cache key seed for the web graph
// endpoints. It changes when graph structure inputs or attached score tables
// change.
func (s *Store) GraphWebFingerprint(ctx context.Context) (string, error) {
	var incarnation string
	var revision int64
	if err := s.db.QueryRowContext(ctx, `SELECT incarnation, revision FROM graph_web_revision WHERE id = 1`).Scan(&incarnation, &revision); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", incarnation, revision), nil
}

// GraphDocNoteEdges returns note-path -> note-path edges (wikilinks) persisted in graph_doc_edges(kind='wikilink').
func (s *Store) GraphDocNoteEdges(ctx context.Context) ([]GraphDocEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_path, dst_path
		FROM graph_doc_edges
		WHERE kind = 'wikilink'
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
		out = append(out, GraphDocEdge{SrcPath: src, DstPath: dst, Kind: "wikilink", Weight: 1})
	}
	return out, rows.Err()
}

func (s *Store) GraphDocNoteEdgesBySources(ctx context.Context, srcPaths []string) ([]GraphDocEdge, error) {
	srcPaths = normalizeNonEmptyStrings(srcPaths)
	if len(srcPaths) == 0 {
		return []GraphDocEdge{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(srcPaths)), ",")
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_path, dst_path
		FROM graph_doc_edges
		WHERE kind = 'wikilink' AND src_path IN (`+placeholders+`)
		ORDER BY src_path, dst_path
	`, sliceAny(srcPaths)...)
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
		out = append(out, GraphDocEdge{SrcPath: src, DstPath: dst, Kind: "wikilink", Weight: 1})
	}
	return out, rows.Err()
}

func (s *Store) GraphDocNoteEdgesForPaths(ctx context.Context, paths []string) ([]GraphDocEdge, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return []GraphDocEdge{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(paths)), ",")
	args := make([]any, 0, len(paths)*2)
	args = append(args, sliceAny(paths)...)
	args = append(args, sliceAny(paths)...)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT src_path, dst_path
		FROM graph_doc_edges
		WHERE kind = 'wikilink' AND (src_path IN (%s) OR dst_path IN (%s))
		ORDER BY src_path, dst_path
	`, placeholders, placeholders), args...)
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
		out = append(out, GraphDocEdge{SrcPath: src, DstPath: dst, Kind: "wikilink", Weight: 1})
	}
	return out, rows.Err()
}

// GraphDocNoteNeighborhoodForPaths returns at most perDirectionLimit incident
// edges in each direction for every requested path. Ranking uses the neighbor's
// persisted authority while window counts preserve the complete incident
// degree even when the returned edge set is bounded.
func (s *Store) GraphDocNoteNeighborhoodForPaths(ctx context.Context, paths []string, perDirectionLimit int) (GraphDocNoteNeighborhood, error) {
	paths = normalizeNonEmptyStrings(paths)
	out := GraphDocNoteNeighborhood{
		Edges:   []GraphDocEdge{},
		Degrees: make(map[string]GraphDocDegree, len(paths)),
	}
	for _, path := range paths {
		out.Degrees[path] = GraphDocDegree{}
	}
	if len(paths) == 0 {
		return out, nil
	}
	if perDirectionLimit <= 0 {
		return GraphDocNoteNeighborhood{}, fmt.Errorf("per-direction limit must be positive")
	}

	const targetBatchSize = 300
	seenEdges := make(map[string]struct{}, len(paths)*perDirectionLimit*2)
	for start := 0; start < len(paths); start += targetBatchSize {
		end := min(start+targetBatchSize, len(paths))
		batch := paths[start:end]
		values := strings.TrimSuffix(strings.Repeat("(?),", len(batch)), ",")
		args := append(sliceAny(batch), perDirectionLimit)
		rows, err := s.db.QueryContext(ctx, `
			WITH targets(path) AS (
				VALUES `+values+`
			),
			incident AS (
				SELECT DISTINCT
					e.src_path,
					e.dst_path,
					t.path AS focus_path,
					'outbound' AS direction,
					e.dst_path AS neighbor_path,
					COALESCE(score.authority, 0) AS neighbor_authority
				FROM targets t
				JOIN graph_doc_edges e ON e.src_path = t.path
				LEFT JOIN graph_doc_scores score ON score.doc_path = e.dst_path
				WHERE e.kind = 'wikilink' AND e.src_path <> e.dst_path

				UNION ALL

				SELECT DISTINCT
					e.src_path,
					e.dst_path,
					t.path AS focus_path,
					'inbound' AS direction,
					e.src_path AS neighbor_path,
					COALESCE(score.authority, 0) AS neighbor_authority
				FROM targets t
				JOIN graph_doc_edges e ON e.dst_path = t.path
				LEFT JOIN graph_doc_scores score ON score.doc_path = e.src_path
				WHERE e.kind = 'wikilink' AND e.src_path <> e.dst_path
			),
			ranked AS (
				SELECT
					src_path,
					dst_path,
					focus_path,
					direction,
					COUNT(*) OVER (PARTITION BY focus_path, direction) AS degree,
					ROW_NUMBER() OVER (
						PARTITION BY focus_path, direction
						ORDER BY neighbor_authority DESC, neighbor_path
					) AS ordinal
				FROM incident
			)
			SELECT src_path, dst_path, focus_path, direction, degree
			FROM ranked
			WHERE ordinal <= ?
			ORDER BY focus_path, direction, ordinal
		`, args...)
		if err != nil {
			return GraphDocNoteNeighborhood{}, err
		}

		for rows.Next() {
			var src, dst, focus, direction string
			var degree int
			if err := rows.Scan(&src, &dst, &focus, &direction, &degree); err != nil {
				_ = rows.Close()
				return GraphDocNoteNeighborhood{}, err
			}
			counts := out.Degrees[focus]
			switch direction {
			case "inbound":
				counts.Inbound = degree
			case "outbound":
				counts.Outbound = degree
			}
			out.Degrees[focus] = counts

			key := src + "\x00" + dst
			if _, ok := seenEdges[key]; ok {
				continue
			}
			seenEdges[key] = struct{}{}
			out.Edges = append(out.Edges, GraphDocEdge{
				SrcPath: src,
				DstPath: dst,
				Kind:    "wikilink",
				Weight:  1,
			})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return GraphDocNoteNeighborhood{}, err
		}
		_ = rows.Close()
	}
	return out, nil
}

// GraphDocNoteEdgeCount returns the number of persisted note->note edges.
func (s *Store) GraphDocNoteEdgeCount(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_doc_edges WHERE kind = 'wikilink'`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

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

// GraphDocPaths returns distinct doc paths seen in intel docs and doc links.
func (s *Store) GraphDocPaths(ctx context.Context) (notes []string, code []string, err error) {
	noteRows, err := s.db.QueryContext(ctx, `SELECT DISTINCT path FROM intel_doc_sections ORDER BY path`)
	if err != nil {
		return nil, nil, err
	}
	defer noteRows.Close()
	for noteRows.Next() {
		var p string
		if err := noteRows.Scan(&p); err != nil {
			return nil, nil, err
		}
		notes = append(notes, p)
	}
	if err := noteRows.Err(); err != nil {
		return nil, nil, err
	}

	codeRows, err := s.db.QueryContext(ctx, `
		WITH linked_code AS (
			SELECT DISTINCT src_path AS path
			FROM doc_links
			WHERE src_type = 'code'
			UNION
			SELECT DISTINCT a.path AS path
			FROM intel_edges e
			JOIN intel_code_anchors a
			  ON e.dst_type = 'anchor'
			 AND a.id = e.dst_row_id
			WHERE e.kind = 'mentions'
		)
		SELECT DISTINCT path FROM linked_code ORDER BY path
	`)
	if err != nil {
		return notes, nil, err
	}
	defer codeRows.Close()
	for codeRows.Next() {
		var p string
		if err := codeRows.Scan(&p); err != nil {
			return notes, nil, err
		}
		code = append(code, p)
	}
	if err := codeRows.Err(); err != nil {
		return notes, nil, err
	}

	return notes, code, nil
}

// GraphDocPathsForPrefix returns distinct note/code doc paths that match the prefix.
func (s *Store) GraphDocPathsForPrefix(ctx context.Context, prefix string) (notes []string, code []string, err error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return s.GraphDocPaths(ctx)
	}
	like := prefix + "/%"

	noteRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT path
		FROM intel_doc_sections
		WHERE path = ? OR path LIKE ?
		ORDER BY path
	`, prefix, like)
	if err != nil {
		return nil, nil, err
	}
	defer noteRows.Close()
	for noteRows.Next() {
		var p string
		if err := noteRows.Scan(&p); err != nil {
			return nil, nil, err
		}
		notes = append(notes, p)
	}
	if err := noteRows.Err(); err != nil {
		return nil, nil, err
	}

	codeRows, err := s.db.QueryContext(ctx, `
		WITH linked_code AS (
			SELECT DISTINCT src_path AS path
			FROM doc_links
			WHERE src_type = 'code' AND (src_path = ? OR src_path LIKE ?)
			UNION
			SELECT DISTINCT a.path AS path
			FROM intel_edges e
			JOIN intel_code_anchors a
			  ON e.dst_type = 'anchor'
			 AND a.id = e.dst_row_id
			WHERE e.kind = 'mentions' AND (a.path = ? OR a.path LIKE ?)
		)
		SELECT DISTINCT path FROM linked_code ORDER BY path
	`, prefix, like, prefix, like)
	if err != nil {
		return notes, nil, err
	}
	defer codeRows.Close()
	for codeRows.Next() {
		var p string
		if err := codeRows.Scan(&p); err != nil {
			return notes, nil, err
		}
		code = append(code, p)
	}
	if err := codeRows.Err(); err != nil {
		return notes, nil, err
	}

	return notes, code, nil
}
