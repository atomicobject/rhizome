package store

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
	LinkText        string  // see GraphDocEdgeRow.LinkText
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
