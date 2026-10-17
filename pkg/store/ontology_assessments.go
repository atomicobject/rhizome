package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (s *Store) GetOntologyTypeByPath(ctx context.Context, notePath string) (OntologyNoteTypeRow, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT note_path, type_name, schema_hash, updated_at
		FROM ontology_note_types
		WHERE note_path = ?
		LIMIT 1
	`, notePath)
	var out OntologyNoteTypeRow
	if err := row.Scan(&out.NotePath, &out.TypeName, &out.SchemaHash, &out.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return OntologyNoteTypeRow{}, false, nil
		}
		return OntologyNoteTypeRow{}, false, err
	}
	return out, true, nil
}

func (s *Store) GetOntologyAssessmentByPath(ctx context.Context, notePath string) (OntologyNoteAssessmentRow, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT note_path, declared_type, resolved_type, assessment_json, has_issues, type_ambiguous, schema_hash, updated_at
		FROM ontology_note_assessments
		WHERE note_path = ?
		LIMIT 1
	`, notePath)
	var out OntologyNoteAssessmentRow
	if err := row.Scan(&out.NotePath, &out.DeclaredType, &out.ResolvedType, &out.AssessmentJSON, &out.HasIssues, &out.TypeAmbiguous, &out.SchemaHash, &out.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return OntologyNoteAssessmentRow{}, false, nil
		}
		return OntologyNoteAssessmentRow{}, false, err
	}
	return out, true, nil
}

func (s *Store) OntologyAssessmentsByPaths(ctx context.Context, paths []string) (map[string]OntologyNoteAssessmentRow, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return map[string]OntologyNoteAssessmentRow{}, nil
	}
	out := make(map[string]OntologyNoteAssessmentRow, len(paths))
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := start + sqliteValuesBatchMaxParams
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT note_path, declared_type, resolved_type, assessment_json, has_issues, type_ambiguous, schema_hash, updated_at
			FROM ontology_note_assessments
			WHERE note_path IN (%s)
		`, placeholders), sliceAny(batch)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row OntologyNoteAssessmentRow
			if err := rows.Scan(&row.NotePath, &row.DeclaredType, &row.ResolvedType, &row.AssessmentJSON, &row.HasIssues, &row.TypeAmbiguous, &row.SchemaHash, &row.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			out[row.NotePath] = row
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// OntologyAssessmentFlags returns the materialized inventory flags for every
// assessment row in one scan. It never reads assessment_json.
func (s *Store) OntologyAssessmentFlags(ctx context.Context) (map[string]OntologyAssessmentFlags, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT note_path, has_issues, type_ambiguous FROM ontology_note_assessments`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]OntologyAssessmentFlags{}
	for rows.Next() {
		var notePath string
		var flags OntologyAssessmentFlags
		if err := rows.Scan(&notePath, &flags.HasIssues, &flags.TypeAmbiguous); err != nil {
			return nil, err
		}
		out[notePath] = flags
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) OntologyTypesByPaths(ctx context.Context, paths []string) (map[string]OntologyNoteTypeRow, error) {
	paths = normalizeNonEmptyStrings(paths)
	if len(paths) == 0 {
		return map[string]OntologyNoteTypeRow{}, nil
	}
	out := make(map[string]OntologyNoteTypeRow, len(paths))
	for start := 0; start < len(paths); start += sqliteValuesBatchMaxParams {
		end := start + sqliteValuesBatchMaxParams
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT note_path, type_name, schema_hash, updated_at
			FROM ontology_note_types
			WHERE note_path IN (%s)
		`, placeholders), sliceAny(batch)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row OntologyNoteTypeRow
			if err := rows.Scan(&row.NotePath, &row.TypeName, &row.SchemaHash, &row.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			out[row.NotePath] = row
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}
