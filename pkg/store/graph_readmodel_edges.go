package store

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"strings"
)

func (s *Store) ontologyEdgesForPathPrefixes(ctx context.Context, prefixes []string, includeAmbient bool, limit int) ([]OntologyEdgeRow, error) {
	srcConditions, args := graphPrefixConditions("src_path", prefixes)
	dstConditions, dstArgs := graphPrefixConditions("dst_path", prefixes)
	args = append(args, dstArgs...)
	if len(srcConditions) == 0 && len(dstConditions) == 0 {
		return nil, nil
	}
	conditions := []string{"(" + strings.Join(append(srcConditions, dstConditions...), " OR ") + ")"}
	if !includeAmbient {
		conditions = append(conditions, "structural = 1")
	}
	query := fmt.Sprintf(`
		SELECT src_path, COALESCE(src_node_id, ''), relation_name, dst_path, COALESCE(dst_node_id, ''), dst_type, provenance, structural, schema_hash, updated_at
		FROM ontology_edges
		WHERE %s
		ORDER BY structural DESC, src_path, relation_name, dst_path, src_node_id, dst_node_id
	`, strings.Join(conditions, " AND "))
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OntologyEdgeRow
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

func (s *Store) ontologyEdgesForEndpointSelectors(ctx context.Context, selectors []readmodel.GraphEndpointSelector, includeAmbient bool, limit int) ([]OntologyEdgeRow, error) {
	conditions := make([]string, 0, len(selectors))
	args := make([]any, 0, len(selectors)*8)
	for _, selector := range selectors {
		condition, conditionArgs := graphEndpointSelectorEdgeCondition(selector)
		if condition == "" {
			continue
		}
		conditions = append(conditions, "("+condition+")")
		args = append(args, conditionArgs...)
	}
	if len(conditions) == 0 {
		return nil, nil
	}
	where := []string{"(" + strings.Join(conditions, " OR ") + ")"}
	if !includeAmbient {
		where = append(where, "structural = 1")
	}
	query := fmt.Sprintf(`
		SELECT src_path, COALESCE(src_node_id, ''), relation_name, dst_path, COALESCE(dst_node_id, ''), dst_type, provenance, structural, schema_hash, updated_at
		FROM ontology_edges
		WHERE %s
		ORDER BY structural DESC, src_path, relation_name, dst_path, src_node_id, dst_node_id
	`, strings.Join(where, " AND "))
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OntologyEdgeRow
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

func graphEndpointSelectorEdgeCondition(selector readmodel.GraphEndpointSelector) (string, []any) {
	nodeID := strings.TrimSpace(selector.NodeID)
	path := strings.TrimSpace(selector.Path)
	conditions := []string{}
	args := []any{}
	if nodeID != "" {
		if path != "" {
			conditions = append(conditions, "((src_path = ? AND src_node_id = ?) OR (dst_path = ? AND dst_node_id = ?))")
			args = append(args, path, nodeID, path, nodeID)
		} else {
			conditions = append(conditions, "(src_node_id = ? OR dst_node_id = ?)")
			args = append(args, nodeID, nodeID)
		}
	}
	srcCondition, srcArgs := graphEndpointSelectorNodeCondition("src_node", selector)
	dstCondition, dstArgs := graphEndpointSelectorNodeCondition("dst_node", selector)
	if srcCondition == "" && dstCondition == "" && len(conditions) == 0 {
		return "", nil
	}
	if srcCondition != "" {
		conditions = append(conditions, fmt.Sprintf("EXISTS (SELECT 1 FROM ontology_nodes src_node WHERE src_node.node_id = ontology_edges.src_node_id AND %s)", srcCondition))
		args = append(args, srcArgs...)
	}
	if dstCondition != "" {
		conditions = append(conditions, fmt.Sprintf("EXISTS (SELECT 1 FROM ontology_nodes dst_node WHERE dst_node.node_id = ontology_edges.dst_node_id AND %s)", dstCondition))
		args = append(args, dstArgs...)
	}
	return strings.Join(conditions, " OR "), args
}

func graphEndpointSelectorNodeCondition(alias string, selector readmodel.GraphEndpointSelector) (string, []any) {
	col := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	conditions := []string{}
	args := []any{}
	if path := strings.TrimSpace(selector.Path); path != "" {
		conditions = append(conditions, col("note_path")+" = ?")
		args = append(args, path)
	}
	if nodeID := strings.TrimSpace(selector.NodeID); nodeID != "" {
		nodeIDConditions := []string{
			col("node_id") + " = ?",
			col("block_id") + " = ?",
			col("fragment") + " = ?",
			col("fragment") + " = ?",
		}
		nodeIDArgs := []any{nodeID, strings.TrimPrefix(nodeID, "^"), nodeID, strings.TrimPrefix(nodeID, "^")}
		if encoded, err := json.Marshal(nodeID); err == nil {
			nodeIDConditions = append(nodeIDConditions, col("node_ref_json")+" LIKE ?")
			nodeIDArgs = append(nodeIDArgs, `%"nodeId":`+string(encoded)+`%`)
		}
		if path := strings.TrimSpace(selector.Path); path != "" {
			block := strings.TrimPrefix(nodeID, "^")
			nodeIDConditions = append(nodeIDConditions, col("source_locator")+" = ?", col("source_locator")+" = ?")
			nodeIDArgs = append(nodeIDArgs, path+"#"+nodeID, path+"#^"+block)
		}
		conditions = append(conditions, "("+strings.Join(nodeIDConditions, " OR ")+")")
		args = append(args, nodeIDArgs...)
	}
	if kind := strings.TrimSpace(selector.Kind); kind != "" {
		conditions = append(conditions, col("node_kind")+" = ?")
		args = append(args, kind)
	}
	if locator := strings.TrimSpace(selector.SourceLocator); locator != "" {
		conditions = append(conditions, col("source_locator")+" = ?")
		args = append(args, locator)
	}
	if fragment := strings.TrimPrefix(strings.TrimSpace(selector.Fragment), "#"); fragment != "" {
		block := strings.TrimPrefix(fragment, "^")
		fragmentConditions := []string{col("fragment") + " = ?", col("block_id") + " = ?"}
		fragmentArgs := []any{fragment, block}
		if path := strings.TrimSpace(selector.Path); path != "" {
			fragmentConditions = append(fragmentConditions, col("source_locator")+" = ?", col("source_locator")+" = ?")
			fragmentArgs = append(fragmentArgs, path+"#"+fragment, path+"#^"+block)
		}
		conditions = append(conditions, "("+strings.Join(fragmentConditions, " OR ")+")")
		args = append(args, fragmentArgs...)
	}
	if structural := strings.TrimSpace(selector.Structural); structural != "" {
		conditions = append(conditions, "("+col("structural_fingerprint")+" = ? OR "+col("fragment")+" = ? OR "+col("block_id")+" = ?)")
		args = append(args, structural, structural, strings.TrimPrefix(structural, "^"))
	}
	if len(conditions) == 0 {
		return "", nil
	}
	return strings.Join(conditions, " AND "), args
}

func graphPrefixConditions(column string, prefixes []string) ([]string, []any) {
	prefixes = normalizeNonEmptyStrings(prefixes)
	conditions := make([]string, 0, len(prefixes))
	args := make([]any, 0, len(prefixes)*2)
	for _, prefix := range prefixes {
		prefix = strings.Trim(strings.TrimSpace(prefix), "/")
		if prefix == "" {
			continue
		}
		conditions = append(conditions, fmt.Sprintf("(%s = ? OR %s LIKE ?)", column, column))
		args = append(args, prefix, prefix+"/%")
	}
	return conditions, args
}
