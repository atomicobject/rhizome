package store

import (
	"context"
	"database/sql"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

// loadOntologyReadinessOutputs includes orphan rows: completeness must reject
// stale sources instead of hiding them behind a join to current metadata.
func loadOntologyReadinessOutputs(ctx context.Context, tx *sql.Tx, snapshot *OntologyReadinessSnapshot) error {
	if err := queryOntologyReadinessRows(ctx, tx, `
		SELECT note_path, type_name, schema_hash, updated_at
		FROM ontology_note_types ORDER BY note_path, type_name
	`, func(rows *sql.Rows) error {
		var row OntologyNoteTypeRow
		if err := rows.Scan(&row.NotePath, &row.TypeName, &row.SchemaHash, &row.UpdatedAt); err != nil {
			return err
		}
		snapshot.NoteTypes = append(snapshot.NoteTypes, row)
		return nil
	}); err != nil {
		return err
	}
	if err := queryOntologyReadinessRows(ctx, tx, `
		SELECT src_path, COALESCE(src_node_id, ''), relation_name, dst_path, COALESCE(dst_node_id, ''),
			dst_type, provenance, structural, schema_hash, updated_at
		FROM ontology_edges ORDER BY src_path, src_node_id, relation_name, dst_path, dst_node_id, provenance
	`, func(rows *sql.Rows) error {
		var row OntologyEdgeRow
		if err := rows.Scan(&row.SrcPath, &row.SrcNodeID, &row.RelationName, &row.DstPath, &row.DstNodeID,
			&row.DstType, &row.Provenance, &row.Structural, &row.SchemaHash, &row.UpdatedAt); err != nil {
			return err
		}
		snapshot.Edges = append(snapshot.Edges, row)
		return nil
	}); err != nil {
		return err
	}
	if err := queryOntologyReadinessRows(ctx, tx, `
		SELECT source_note_path, node_id, type_name, field_name, target_input, target_input_norm,
			resolved_target_note_path, resolved_target_type_name, schema_hash, updated_at
		FROM ontology_node_field_value_dependencies ORDER BY source_note_path, node_id, field_name, target_input_norm
	`, func(rows *sql.Rows) error {
		var row readmodel.LinkDependencyRow
		if err := rows.Scan(&row.SourceNotePath, &row.NodeID, &row.TypeName, &row.FieldName, &row.TargetInput,
			&row.TargetInputNorm, &row.ResolvedTargetNotePath, &row.ResolvedTargetTypeName, &row.SchemaHash, &row.UpdatedAt); err != nil {
			return err
		}
		snapshot.LinkDependencies = append(snapshot.LinkDependencies, row)
		return nil
	}); err != nil {
		return err
	}
	return queryOntologyReadinessRows(ctx, tx, `
		SELECT type_name, policy_json, schema_hash, updated_at
		FROM ontology_type_policies ORDER BY type_name
	`, func(rows *sql.Rows) error {
		var row OntologyTypePolicyRow
		if err := rows.Scan(&row.TypeName, &row.PolicyJSON, &row.SchemaHash, &row.UpdatedAt); err != nil {
			return err
		}
		snapshot.TypePolicies = append(snapshot.TypePolicies, row)
		return nil
	})
}
