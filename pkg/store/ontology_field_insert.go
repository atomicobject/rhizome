package store

import (
	"context"
	"database/sql"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"strings"
)

func insertOntologyNodeFieldValuesTx(ctx context.Context, tx *sql.Tx, fieldValues []readmodel.FieldValueRow) (int, error) {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO ontology_node_field_values (
			node_id, note_path, type_name, field_name, field_kind, source_kind,
			value_kind, value_text, value_norm, value_bool, value_int, value_real,
			value_date, value_datetime, target_node_id, target_ref_json, target_note_path,
			target_type_name, target_source_locator, list_ordinal, schema_hash, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id, field_name, list_ordinal, value_norm, target_node_id) DO UPDATE SET
			note_path = excluded.note_path,
			type_name = excluded.type_name,
			field_kind = excluded.field_kind,
			source_kind = excluded.source_kind,
			value_kind = excluded.value_kind,
			value_text = excluded.value_text,
			value_bool = excluded.value_bool,
			value_int = excluded.value_int,
			value_real = excluded.value_real,
			value_date = excluded.value_date,
			value_datetime = excluded.value_datetime,
			target_ref_json = excluded.target_ref_json,
			target_note_path = excluded.target_note_path,
			target_type_name = excluded.target_type_name,
			target_source_locator = excluded.target_source_locator,
			schema_hash = excluded.schema_hash,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	written := 0
	for _, row := range fieldValues {
		if strings.TrimSpace(row.NodeID) == "" || strings.TrimSpace(row.NotePath) == "" || strings.TrimSpace(row.FieldName) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx,
			row.NodeID,
			row.NotePath,
			row.TypeName,
			row.FieldName,
			row.FieldKind,
			row.SourceKind,
			row.ValueKind,
			row.ValueText,
			row.ValueNorm,
			nullableBool(row.ValueBool),
			nullableInt64(row.ValueInt),
			nullableFloat64(row.ValueReal),
			nullableString(row.ValueDate),
			nullableString(row.ValueDateTime),
			row.TargetNodeID,
			row.TargetRefJSON,
			row.TargetNotePath,
			row.TargetTypeName,
			row.TargetSourceLocator,
			row.ListOrdinal,
			row.SchemaHash,
			row.UpdatedAt,
		); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}
