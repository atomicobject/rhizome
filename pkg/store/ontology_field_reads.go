package store

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"slices"
	"strings"
)

func (s *Store) OntologyNodeFieldValuesByNodeIDs(ctx context.Context, nodeIDs []string, fieldNames []string) ([]readmodel.FieldValueRow, error) {
	nodeIDs = normalizeNonEmptyStrings(nodeIDs)
	if len(nodeIDs) == 0 {
		return []readmodel.FieldValueRow{}, nil
	}
	var out []readmodel.FieldValueRow
	for batch := range slices.Chunk(nodeIDs, 400) {
		rows, err := s.ontologyNodeFieldValuesByNodeIDsBatch(ctx, batch, fieldNames)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

func (s *Store) ontologyNodeFieldValuesByNodeIDsBatch(ctx context.Context, nodeIDs []string, fieldNames []string) ([]readmodel.FieldValueRow, error) {
	conditions := []string{fmt.Sprintf("node_id IN (%s)", strings.TrimSuffix(strings.Repeat("?,", len(nodeIDs)), ","))}
	args := sliceAny(nodeIDs)
	normalized := normalizeNonEmptyStrings(fieldNames)
	if len(normalized) > 0 {
		// Copy + lowercase so we never mutate the caller's slice.
		lowered := make([]string, len(normalized))
		for i, name := range normalized {
			lowered[i] = strings.ToLower(strings.TrimSpace(name))
		}
		conditions = append(conditions, fmt.Sprintf("field_name IN (%s)", strings.TrimSuffix(strings.Repeat("?,", len(lowered)), ",")))
		args = append(args, sliceAny(lowered)...)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT node_id, note_path, type_name, field_name, field_kind, source_kind,
			value_kind, value_text, value_norm, value_bool, value_int, value_real,
			value_date, value_datetime, target_node_id, target_ref_json, target_note_path,
			target_type_name, target_source_locator, list_ordinal, schema_hash, updated_at
		FROM ontology_node_field_values
		WHERE %s
		ORDER BY node_id, field_name, list_ordinal, value_norm, target_node_id
	`, strings.Join(conditions, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOntologyNodeFieldValueRows(rows)
}
