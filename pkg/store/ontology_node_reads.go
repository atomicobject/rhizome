package store

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"slices"
	"strings"
)

// OntologyNodesByIDs returns ontology-node owners keyed by node_id.
func (s *Store) OntologyNodesByIDs(ctx context.Context, nodeIDs []string) (map[string]readmodel.NodeRow, error) {
	nodeIDs = normalizeNonEmptyStrings(nodeIDs)
	out := make(map[string]readmodel.NodeRow, len(nodeIDs))
	if len(nodeIDs) == 0 {
		return out, nil
	}
	for batch := range slices.Chunk(nodeIDs, 400) {
		holders, args := placeholders(batch)
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT node_id, note_path, node_ref_json, node_kind, COALESCE(type_name, ''),
				COALESCE(parent_node_id, ''), COALESCE(parent_type_name, ''), COALESCE(title, ''),
				COALESCE(source_locator, ''), COALESCE(fragment, ''), COALESCE(block_id, ''),
				COALESCE(display_label, ''), COALESCE(locator_status, ''),
				start_byte, end_byte, COALESCE(structural_fingerprint, ''), COALESCE(relocation_fingerprint, ''), COALESCE(schema_hash, ''), updated_at
			FROM ontology_nodes
			WHERE node_id IN (%s)
		`, holders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var node readmodel.NodeRow
			if err := scanOntologyNode(rows, &node); err != nil {
				rows.Close()
				return nil, err
			}
			out[node.NodeID] = node
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// AllOntologyNodes returns every persisted ontology-node owner.
func (s *Store) AllOntologyNodes(ctx context.Context) ([]readmodel.NodeRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id,
			parent_type_name, title, source_locator, fragment, block_id, display_label,
			locator_status, start_byte, end_byte, structural_fingerprint, relocation_fingerprint, schema_hash, updated_at
		FROM ontology_nodes
		ORDER BY note_path, node_kind, node_id
	`)
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

// OntologyNodesByType returns ontology-node owners for the supplied type.
func (s *Store) OntologyNodesByType(ctx context.Context, typeName string) ([]readmodel.NodeRow, error) {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id,
			parent_type_name, title, source_locator, fragment, block_id, display_label,
			locator_status, start_byte, end_byte, structural_fingerprint, relocation_fingerprint, schema_hash, updated_at
		FROM ontology_nodes
		WHERE type_name = ?
		ORDER BY note_path, node_kind, node_id
	`, typeName)
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
