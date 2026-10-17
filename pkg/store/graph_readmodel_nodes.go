package store

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"strings"
)

func (s *Store) ontologyNodesByPathPrefixes(ctx context.Context, prefixes []string, limit int) ([]readmodel.NodeRow, error) {
	conditions, args := graphPrefixConditions("note_path", prefixes)
	if len(conditions) == 0 {
		return nil, nil
	}
	query := fmt.Sprintf(`
		SELECT node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id,
			parent_type_name, title, source_locator, fragment, block_id, display_label,
			locator_status, start_byte, end_byte, structural_fingerprint, relocation_fingerprint, schema_hash, updated_at
		FROM ontology_nodes
		WHERE %s
		ORDER BY note_path, node_kind, node_id
	`, strings.Join(conditions, " OR "))
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []readmodel.NodeRow
	for rows.Next() {
		var node readmodel.NodeRow
		if err := scanOntologyNode(rows, &node); err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}

func (s *Store) ontologyNodesByEndpointSelectors(ctx context.Context, selectors []readmodel.GraphEndpointSelector, limit int) ([]readmodel.NodeRow, error) {
	conditions := make([]string, 0, len(selectors))
	args := make([]any, 0, len(selectors)*4)
	for _, selector := range selectors {
		condition, conditionArgs := graphEndpointSelectorNodeCondition("", selector)
		if condition == "" {
			continue
		}
		conditions = append(conditions, "("+condition+")")
		args = append(args, conditionArgs...)
	}
	if len(conditions) == 0 {
		return nil, nil
	}
	query := fmt.Sprintf(`
		SELECT node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id,
			parent_type_name, title, source_locator, fragment, block_id, display_label,
			locator_status, start_byte, end_byte, structural_fingerprint, relocation_fingerprint, schema_hash, updated_at
		FROM ontology_nodes
		WHERE %s
		ORDER BY note_path, node_kind, node_id
	`, strings.Join(conditions, " OR "))
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []readmodel.NodeRow
	for rows.Next() {
		var node readmodel.NodeRow
		if err := scanOntologyNode(rows, &node); err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}
