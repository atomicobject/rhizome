package store

import (
	"context"
	"database/sql"
	"strings"
)

func insertOntologyNoteTypes(ctx context.Context, tx *sql.Tx, rows []OntologyNoteTypeRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO ontology_note_types (note_path, type_name, schema_hash, updated_at)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if strings.TrimSpace(row.NotePath) == "" || strings.TrimSpace(row.TypeName) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, row.NotePath, row.TypeName, row.SchemaHash, row.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func insertOntologyAssessments(ctx context.Context, tx *sql.Tx, rows []OntologyNoteAssessmentRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO ontology_note_assessments (note_path, declared_type, resolved_type, assessment_json, has_issues, type_ambiguous, schema_hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx, row.NotePath, row.DeclaredType, row.ResolvedType, row.AssessmentJSON, boolToInt(row.HasIssues), boolToInt(row.TypeAmbiguous), row.SchemaHash, row.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func insertOntologyEdges(ctx context.Context, tx *sql.Tx, rows []OntologyEdgeRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO ontology_edges (src_path, src_node_id, relation_name, dst_path, dst_node_id, dst_type, provenance, structural, schema_hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if strings.TrimSpace(row.SrcPath) == "" || strings.TrimSpace(row.RelationName) == "" || strings.TrimSpace(row.DstPath) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, row.SrcPath, row.SrcNodeID, row.RelationName, row.DstPath, row.DstNodeID, row.DstType, row.Provenance, boolToInt(row.Structural), row.SchemaHash, row.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func insertOntologyTypePolicies(ctx context.Context, tx *sql.Tx, rows []OntologyTypePolicyRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO ontology_type_policies (type_name, policy_json, schema_hash, updated_at)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if strings.TrimSpace(row.TypeName) == "" || strings.TrimSpace(row.PolicyJSON) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, row.TypeName, row.PolicyJSON, row.SchemaHash, row.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
