package sqlite

import (
	"context"
	"fmt"
	"strings"
)

// OntologyFieldValueCounts counts the nodes of the given types per normalized
// value of one field, in one indexed GROUP BY over
// idx_ontology_node_field_values_norm. Views use it to choose a type's default
// layout without loading its records. Empty values are not counted.
func (s *Store) OntologyFieldValueCounts(ctx context.Context, typeNames []string, fieldName string) (map[string]int, error) {
	typeNames = normalizeNonEmptyStrings(typeNames)
	fieldName = strings.ToLower(strings.TrimSpace(fieldName))
	out := map[string]int{}
	if len(typeNames) == 0 || fieldName == "" {
		return out, nil
	}
	args := make([]any, 0, len(typeNames)+1)
	for _, name := range typeNames {
		args = append(args, name)
	}
	args = append(args, fieldName)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT value_norm, COUNT(DISTINCT node_id)
		FROM ontology_node_field_values
		WHERE type_name IN (%s) AND field_name = ? AND value_norm != ''
		GROUP BY value_norm
	`, strings.TrimSuffix(strings.Repeat("?,", len(typeNames)), ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var value string
		var count int
		if err := rows.Scan(&value, &count); err != nil {
			return nil, err
		}
		out[value] = count
	}
	return out, rows.Err()
}
