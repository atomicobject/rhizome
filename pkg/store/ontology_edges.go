package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func (s *Store) OntologyEdgesForPath(ctx context.Context, path string, includeAmbient bool, relationFilter string, limit int) ([]OntologyEdgeRow, error) {
	return s.OntologyEdgesForPaths(ctx, []string{path}, includeAmbient, relationFilter, limit)
}

func (s *Store) AllOntologyEdges(ctx context.Context, includeAmbient bool, limit int) ([]OntologyEdgeRow, error) {
	conditions := []string{"1 = 1"}
	if !includeAmbient {
		conditions = append(conditions, "structural = 1")
	}
	query := fmt.Sprintf(`
		SELECT src_path, COALESCE(src_node_id, ''), relation_name, dst_path, COALESCE(dst_node_id, ''), dst_type, provenance, structural, schema_hash, updated_at
		FROM ontology_edges
		WHERE %s
		ORDER BY structural DESC, src_path, relation_name, dst_path, src_node_id, dst_node_id
	`, strings.Join(conditions, " AND "))
	args := []any{}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OntologyEdgeRow, 0)
	for rows.Next() {
		var row OntologyEdgeRow
		var structural int
		if err := rows.Scan(&row.SrcPath, &row.SrcNodeID, &row.RelationName, &row.DstPath, &row.DstNodeID, &row.DstType, &row.Provenance, &structural, &row.SchemaHash, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.Structural = structural != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) OntologyEdgesForPaths(ctx context.Context, paths []string, includeAmbient bool, relationFilter string, limit int) ([]OntologyEdgeRow, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil, nil
	}
	outCap := len(paths) * 8
	if limit > 0 && limit < outCap {
		outCap = limit
	}
	out := make([]OntologyEdgeRow, 0, outCap)
	batchSize := sqliteValuesBatchMaxParams / 2
	if batchSize < 1 {
		batchSize = 1
	}
	for start := 0; start < len(paths); start += batchSize {
		end := start + batchSize
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		conditions := []string{fmt.Sprintf("(src_path IN (%s) OR dst_path IN (%s))", placeholders, placeholders)}
		args := make([]any, 0, len(batch)*2+2)
		args = append(args, sliceAny(batch)...)
		args = append(args, sliceAny(batch)...)
		if !includeAmbient {
			conditions = append(conditions, "structural = 1")
		}
		if strings.TrimSpace(relationFilter) != "" {
			conditions = append(conditions, "relation_name = ?")
			args = append(args, relationFilter)
		}
		query := fmt.Sprintf(`
			SELECT src_path, COALESCE(src_node_id, ''), relation_name, dst_path, COALESCE(dst_node_id, ''), dst_type, provenance, structural, schema_hash, updated_at
			FROM ontology_edges
			WHERE %s
			ORDER BY structural DESC, src_path, relation_name, dst_path
		`, strings.Join(conditions, " AND "))

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row OntologyEdgeRow
			var structural int
			if err := rows.Scan(&row.SrcPath, &row.SrcNodeID, &row.RelationName, &row.DstPath, &row.DstNodeID, &row.DstType, &row.Provenance, &structural, &row.SchemaHash, &row.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			row.Structural = structural != 0
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return ontologyEdgePathOrderLess(out[i], out[j])
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) OntologyStructuralEdgesBySources(ctx context.Context, paths []string, relation string, limit int) ([]OntologyEdgeRow, error) {
	return s.ontologyEdgesByEndpoint(ctx, "src_path", paths, true, relation, limit)
}

func (s *Store) OntologyAmbientEdgesBySources(ctx context.Context, paths []string, relation string, limit int) ([]OntologyEdgeRow, error) {
	return s.ontologyEdgesByEndpoint(ctx, "src_path", paths, false, relation, limit)
}

func (s *Store) OntologyAmbientEdgesByTargets(ctx context.Context, paths []string, relation string, limit int) ([]OntologyEdgeRow, error) {
	return s.ontologyEdgesByEndpoint(ctx, "dst_path", paths, false, relation, limit)
}

func (s *Store) ontologyEdgesByEndpoint(ctx context.Context, column string, paths []string, structural bool, relation string, limit int) ([]OntologyEdgeRow, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil, nil
	}
	var out []OntologyEdgeRow
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := start + sqliteValuesBatchMaxParams
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		conditions := []string{fmt.Sprintf("%s IN (%s)", column, placeholders), "structural = ?"}
		args := make([]any, 0, len(batch)+3)
		args = append(args, sliceAny(batch)...)
		args = append(args, boolToInt(structural))
		if strings.TrimSpace(relation) != "" {
			conditions = append(conditions, "relation_name = ?")
			args = append(args, relation)
		}
		query := fmt.Sprintf(`
			SELECT src_path, COALESCE(src_node_id, ''), relation_name, dst_path, COALESCE(dst_node_id, ''), dst_type, provenance, structural, schema_hash, updated_at
			FROM ontology_edges
			WHERE %s
			ORDER BY src_path, src_node_id, provenance, relation_name, dst_path, dst_node_id
		`, strings.Join(conditions, " AND "))

		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row OntologyEdgeRow
			var structuralInt int
			if err := rows.Scan(&row.SrcPath, &row.SrcNodeID, &row.RelationName, &row.DstPath, &row.DstNodeID, &row.DstType, &row.Provenance, &structuralInt, &row.SchemaHash, &row.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			row.Structural = structuralInt != 0
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	sort.SliceStable(out, func(i, j int) bool {
		return ontologyEdgeEndpointOrderLess(out[i], out[j])
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func ontologyEdgeEndpointOrderLess(a, b OntologyEdgeRow) bool {
	if a.SrcPath != b.SrcPath {
		return a.SrcPath < b.SrcPath
	}
	if a.SrcNodeID != b.SrcNodeID {
		return a.SrcNodeID < b.SrcNodeID
	}
	if a.Provenance != b.Provenance {
		return a.Provenance < b.Provenance
	}
	if a.RelationName != b.RelationName {
		return a.RelationName < b.RelationName
	}
	if a.DstPath != b.DstPath {
		return a.DstPath < b.DstPath
	}
	return a.DstNodeID < b.DstNodeID
}

func ontologyEdgePathOrderLess(a, b OntologyEdgeRow) bool {
	if a.Structural != b.Structural {
		return a.Structural
	}
	if a.SrcPath != b.SrcPath {
		return a.SrcPath < b.SrcPath
	}
	if a.RelationName != b.RelationName {
		return a.RelationName < b.RelationName
	}
	if a.DstPath != b.DstPath {
		return a.DstPath < b.DstPath
	}
	if a.SrcNodeID != b.SrcNodeID {
		return a.SrcNodeID < b.SrcNodeID
	}
	return a.DstNodeID < b.DstNodeID
}
