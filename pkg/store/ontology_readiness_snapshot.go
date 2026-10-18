package store

import (
	"context"
	"database/sql"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

func ontologyReadinessSnapshotTx(ctx context.Context, tx *sql.Tx, state OntologyReadinessState) (OntologyReadinessSnapshot, error) {
	snapshot := OntologyReadinessSnapshot{
		SchemaState: state.SchemaState, MetadataState: state.MetadataState,
		NoteStates: make(map[string]OntologyNoteStateRow), Assessments: make(map[string]OntologyNoteAssessmentRow),
	}
	err := queryOntologyReadinessRows(ctx, tx, `
		SELECT n.id, n.path, COALESCE(n.title, ''), n.content_hash, n.indexer_version,
			n.mtime, n.size, n.indexed_at, n.format_id,
			COALESCE(p.provider_version, ''), COALESCE(p.projection_version, ''), COALESCE(p.source_content_hash, ''),
			COALESCE(p.status, 'stale'), COALESCE(p.diagnostic_code, ''), COALESCE(p.diagnostic_detail, ''), COALESCE(p.updated_at, 0)
		FROM notes n LEFT JOIN note_projection_state p ON p.note_id = n.id
		WHERE n.indexed_at > 0 ORDER BY n.path
	`, func(rows *sql.Rows) error {
		var row NoteMetadataRow
		if err := scanNoteMetadataRow(rows, &row); err != nil {
			return err
		}
		snapshot.Metadata = append(snapshot.Metadata, row)
		return nil
	})
	if err != nil {
		return OntologyReadinessSnapshot{}, err
	}
	err = queryOntologyReadinessRows(ctx, tx, `
		SELECT note_path, input_fingerprint, schema_hash, resolved_type, updated_at FROM ontology_note_state
	`, func(rows *sql.Rows) error {
		var row OntologyNoteStateRow
		if err := rows.Scan(&row.NotePath, &row.InputFingerprint, &row.SchemaHash, &row.ResolvedType, &row.UpdatedAt); err != nil {
			return err
		}
		snapshot.NoteStates[row.NotePath] = row
		return nil
	})
	if err != nil {
		return OntologyReadinessSnapshot{}, err
	}
	err = queryOntologyReadinessRows(ctx, tx, `
		SELECT note_path, COALESCE(declared_type, ''), COALESCE(resolved_type, ''), assessment_json,
			has_issues, type_ambiguous, schema_hash, updated_at FROM ontology_note_assessments
	`, func(rows *sql.Rows) error {
		var row OntologyNoteAssessmentRow
		if err := rows.Scan(&row.NotePath, &row.DeclaredType, &row.ResolvedType, &row.AssessmentJSON,
			&row.HasIssues, &row.TypeAmbiguous, &row.SchemaHash, &row.UpdatedAt); err != nil {
			return err
		}
		snapshot.Assessments[row.NotePath] = row
		return nil
	})
	if err != nil {
		return OntologyReadinessSnapshot{}, err
	}
	err = queryOntologyReadinessRows(ctx, tx, `
		SELECT node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id,
			parent_type_name, title, source_locator, fragment, block_id, display_label,
			locator_status, start_byte, end_byte, structural_fingerprint, relocation_fingerprint, schema_hash, updated_at
		FROM ontology_nodes ORDER BY note_path, node_id
	`, func(rows *sql.Rows) error {
		var row readmodel.NodeRow
		if err := scanOntologyNode(rows, &row); err != nil {
			return err
		}
		snapshot.Nodes = append(snapshot.Nodes, row)
		return nil
	})
	if err != nil {
		return OntologyReadinessSnapshot{}, err
	}
	if err := loadOntologyReadinessOutputs(ctx, tx, &snapshot); err != nil {
		return OntologyReadinessSnapshot{}, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT node_id, note_path, type_name, field_name, field_kind, source_kind,
			value_kind, value_text, value_norm, value_bool, value_int, value_real,
			value_date, value_datetime, target_node_id, target_ref_json, target_note_path,
			target_type_name, target_source_locator, list_ordinal, schema_hash, updated_at
		FROM ontology_node_field_values ORDER BY node_id, field_name, list_ordinal, id
	`)
	if err != nil {
		return OntologyReadinessSnapshot{}, err
	}
	defer rows.Close()
	snapshot.FieldValues, err = scanOntologyNodeFieldValueRows(rows)
	return snapshot, err
}

func queryOntologyReadinessRows(ctx context.Context, tx *sql.Tx, query string, scan func(*sql.Rows) error) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
