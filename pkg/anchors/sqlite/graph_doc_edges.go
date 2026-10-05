package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

type GraphDocEdgeRow struct {
	SrcPath         string
	DstPath         string
	Kind            string
	Confidence      string
	ConfidenceScore float64
}

const (
	GraphDocEdgeKindWikilink       = "wikilink"
	GraphDocEdgeKindMarkdownLink   = "mdlink"
	graphDocEdgeKindNoteLinkPrefix = "note_link:"
)

func NoteLinkKind(linkType, subtype string) string {
	linkType = strings.TrimSpace(linkType)
	subtype = strings.TrimSpace(subtype)
	if linkType == "" || subtype == "" {
		return ""
	}
	return graphDocEdgeKindNoteLinkPrefix + linkType + ":" + subtype
}

func ParseNoteLinkKind(kind string) (linkType, subtype string, ok bool) {
	if !strings.HasPrefix(kind, graphDocEdgeKindNoteLinkPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(kind, graphDocEdgeKindNoteLinkPrefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// GraphDocEdgeConfidenceDefaults returns the canonical confidence tier and score for a
// graph_doc_edges kind before caller-provided values are normalized.
func GraphDocEdgeConfidenceDefaults(kind string) (string, float64) {
	kind = strings.TrimSpace(kind)
	switch kind {
	case GraphDocEdgeKindWikilink:
		return EdgeConfidenceExtracted, 1.0
	case GraphDocEdgeKindMarkdownLink:
		return EdgeConfidenceExtracted, 0.9
	}
	if linkType, _, ok := ParseNoteLinkKind(kind); ok {
		return GraphDocEdgeConfidenceDefaults(linkType)
	}
	if strings.HasPrefix(kind, graphDocEdgeKindNoteLinkPrefix) {
		return EdgeConfidenceExtracted, 0.9
	}
	return EdgeConfidenceExtracted, 1.0
}

// NormalizeGraphDocEdgeConfidence clamps scores and falls back to kind-specific defaults
// when the stored tier/score pair is empty or invalid.
func NormalizeGraphDocEdgeConfidence(kind, confidence string, confidenceScore float64) (string, float64) {
	defaultConfidence, defaultScore := GraphDocEdgeConfidenceDefaults(kind)
	confidence = strings.ToLower(strings.TrimSpace(confidence))
	switch confidence {
	case "", EdgeConfidenceExtracted, EdgeConfidenceInferred, EdgeConfidenceAmbiguous:
		if confidence == "" {
			confidence = defaultConfidence
		}
	default:
		confidence = defaultConfidence
	}
	if math.IsNaN(confidenceScore) || math.IsInf(confidenceScore, 0) || confidenceScore <= 0 {
		confidenceScore = defaultScore
	}
	if confidenceScore > 1 {
		confidenceScore = 1
	}
	if confidenceScore < 0 {
		confidenceScore = 0
	}
	return confidence, confidenceScore
}

func (s *Store) ReplaceGraphDocEdgesForPath(ctx context.Context, srcPath string, kind string, dstPaths []string) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_graph_doc_edges")
	srcPath = strings.TrimSpace(srcPath)
	kind = strings.TrimSpace(kind)
	if srcPath == "" {
		return fmt.Errorf("graph_doc_edges: empty src_path")
	}
	if kind == "" {
		return fmt.Errorf("graph_doc_edges: empty kind")
	}

	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM graph_doc_edges WHERE src_path = ? AND kind = ?`, srcPath, kind); err != nil {
			return err
		}
		if len(dstPaths) == 0 {
			return nil
		}
		stmt, err := tx.PrepareContext(ctx, `
			INSERT OR REPLACE INTO graph_doc_edges (src_path, dst_path, kind, confidence, confidence_score, source_location)
			VALUES (?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		confidence, confidenceScore := GraphDocEdgeConfidenceDefaults(kind)
		for _, dst := range dstPaths {
			dst = strings.TrimSpace(dst)
			if dst == "" || dst == srcPath {
				continue
			}
			if _, err := stmt.ExecContext(ctx, srcPath, dst, kind, confidence, confidenceScore, ""); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReplaceGraphDocEdgesWithConfidence replaces all edges for a given (src, kind) pair,
// storing full confidence metadata on each edge.
func (s *Store) ReplaceGraphDocEdgesWithConfidence(ctx context.Context, srcPath string, kind string, edges []GraphDocEdge) error {
	ctx = indexingperf.WithOp(ctx, "intel.replace_graph_doc_edges")
	srcPath = strings.TrimSpace(srcPath)
	kind = strings.TrimSpace(kind)
	if srcPath == "" {
		return fmt.Errorf("graph_doc_edges: empty src_path")
	}
	if kind == "" {
		return fmt.Errorf("graph_doc_edges: empty kind")
	}

	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM graph_doc_edges WHERE src_path = ? AND kind = ?`, srcPath, kind); err != nil {
			return err
		}
		if len(edges) == 0 {
			return nil
		}
		stmt, err := tx.PrepareContext(ctx, `
			INSERT OR REPLACE INTO graph_doc_edges (src_path, dst_path, kind, confidence, confidence_score, source_location)
			VALUES (?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, e := range edges {
			dst := strings.TrimSpace(e.DstPath)
			if dst == "" || dst == srcPath {
				continue
			}
			conf, score := NormalizeGraphDocEdgeConfidence(kind, e.Confidence, e.ConfidenceScore)
			if _, err := stmt.ExecContext(ctx, srcPath, dst, kind, conf, score, e.SourceLocation); err != nil {
				return err
			}
		}
		return nil
	})
}

// GraphDocEdgesWithConfidenceForPaths returns graph_doc_edges rows incident to the provided
// paths, including confidence metadata. Used by the ranker.
func (s *Store) GraphDocEdgesWithConfidenceForPaths(ctx context.Context, paths []string) ([]GraphDocEdge, error) {
	return s.GraphDocEdgesWithConfidenceForPathsLimit(ctx, paths, 0)
}

// GraphDocEdgesWithConfidenceForPathsLimit returns graph_doc_edges rows incident to the provided
// paths, including confidence metadata. When limit > 0, at most limit rows are returned.
func (s *Store) GraphDocEdgesWithConfidenceForPathsLimit(ctx context.Context, paths []string, limit int) ([]GraphDocEdge, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(paths)), ",")
	args := make([]any, 0, len(paths)*2)
	args = append(args, sliceAny(paths)...)
	args = append(args, sliceAny(paths)...)
	query := fmt.Sprintf(`
		SELECT src_path, dst_path, kind, confidence, confidence_score, source_location
		FROM graph_doc_edges
		WHERE src_path IN (%s) OR dst_path IN (%s)
		ORDER BY src_path, dst_path, kind
	`, placeholders, placeholders)
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GraphDocEdge
	for rows.Next() {
		var e GraphDocEdge
		if err := rows.Scan(&e.SrcPath, &e.DstPath, &e.Kind, &e.Confidence, &e.ConfidenceScore, &e.SourceLocation); err != nil {
			return nil, err
		}
		e.Weight = 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) DeleteGraphDocEdgesByPath(ctx context.Context, path string) error {
	ctx = indexingperf.WithOp(ctx, "intel.delete_graph_doc_edges")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, `DELETE FROM graph_doc_edges WHERE src_path = ? OR dst_path = ?`, path, path)
		return err
	})
}

func (s *Store) GraphDocEdgesByKind(ctx context.Context, kind string) ([]GraphDocEdgeRow, error) {
	if kind == "" {
		return nil, fmt.Errorf("graph_doc_edges: empty kind")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_path, dst_path, kind, confidence, confidence_score
		FROM graph_doc_edges
		WHERE kind = ?
		ORDER BY src_path, dst_path
	`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GraphDocEdgeRow
	for rows.Next() {
		var r GraphDocEdgeRow
		if err := rows.Scan(&r.SrcPath, &r.DstPath, &r.Kind, &r.Confidence, &r.ConfidenceScore); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GraphDocNoteLinkEdges(ctx context.Context) ([]GraphDocEdgeRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_path, dst_path, kind, confidence, confidence_score
		FROM graph_doc_edges
		WHERE kind GLOB 'note_link:*'
		ORDER BY src_path, dst_path, kind
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GraphDocEdgeRow
	for rows.Next() {
		var r GraphDocEdgeRow
		if err := rows.Scan(&r.SrcPath, &r.DstPath, &r.Kind, &r.Confidence, &r.ConfidenceScore); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AllGraphDocEdgesWithConfidence returns all edges in graph_doc_edges with confidence data.
func (s *Store) AllGraphDocEdgesWithConfidence(ctx context.Context) ([]GraphDocEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT src_path, dst_path, kind, confidence, confidence_score, source_location
		FROM graph_doc_edges
		ORDER BY src_path, dst_path, kind
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GraphDocEdge
	for rows.Next() {
		var e GraphDocEdge
		if err := rows.Scan(&e.SrcPath, &e.DstPath, &e.Kind, &e.Confidence, &e.ConfidenceScore, &e.SourceLocation); err != nil {
			return nil, err
		}
		e.Weight = 1
		out = append(out, e)
	}
	return out, rows.Err()
}

// Confidence tier constants for graph_doc_edges.
const (
	EdgeConfidenceExtracted = "extracted" // deterministic: explicit link in source
	EdgeConfidenceInferred  = "inferred"  // heuristic: indirect relationship
	EdgeConfidenceAmbiguous = "ambiguous" // uncertain: direction/nature unclear
)
