package store

import (
	"context"
	"database/sql"
)

func addOntologyStructureSchema(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []struct{ table, name string }{
		{"ontology_edges", "occurrences_json"},
		{"ontology_node_field_values", "source_presence"},
		{"ontology_node_field_values", "source_value_kind"},
		{"ontology_node_field_values", "reference_at"},
	} {
		has, err := tableHasColumnQuery(ctx, tx, column.table, column.name)
		if err != nil {
			return err
		}
		if !has {
			if _, err := tx.ExecContext(ctx, "ALTER TABLE "+column.table+" ADD COLUMN "+column.name+" TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
		}
	}
	_, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_presence ON ontology_node_field_values(type_name,field_name,node_id) WHERE source_presence = 'value'`)
	return err
}
