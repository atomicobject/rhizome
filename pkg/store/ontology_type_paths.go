package store

import (
	"context"
	"fmt"
	"strings"
)

func (s *Store) OntologyPathsByType(ctx context.Context, typeName string, limit int) ([]string, error) {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return nil, nil
	}
	query := `
		SELECT note_path
		FROM ontology_note_types
		WHERE type_name = ?
		ORDER BY note_path
	`
	args := []any{typeName}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, rows.Err()
}

func (s *Store) OntologyTypePoliciesForPaths(ctx context.Context, paths []string) (map[string]OntologyTypePolicyRow, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return map[string]OntologyTypePolicyRow{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(paths)), ",")
	query := fmt.Sprintf(`
		SELECT nt.note_path, tp.type_name, tp.policy_json, tp.schema_hash, tp.updated_at
		FROM ontology_note_types nt
		JOIN ontology_type_policies tp ON tp.type_name = nt.type_name
		WHERE nt.note_path IN (%s)
	`, placeholders)
	rows, err := s.db.QueryContext(ctx, query, sliceAny(paths)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]OntologyTypePolicyRow, len(paths))
	for rows.Next() {
		var notePath string
		var row OntologyTypePolicyRow
		if err := rows.Scan(&notePath, &row.TypeName, &row.PolicyJSON, &row.SchemaHash, &row.UpdatedAt); err != nil {
			return nil, err
		}
		out[notePath] = row
	}
	return out, rows.Err()
}
