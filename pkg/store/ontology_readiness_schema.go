package store

import (
	"context"
	"fmt"
)

var ontologyReadinessSourceTables = []string{
	"notes", "note_metadata_state", "note_projection_state", "note_projection_payloads",
	"property_keys", "note_property_values", "note_tags", "note_link_inputs",
	"note_fragment_targets", "note_projection_diagnostics", "graph_doc_edges",
	"ontology_schema_state", "ontology_note_assessments", "ontology_note_state",
	"ontology_note_types", "ontology_edges", "ontology_type_policies", "ontology_nodes",
	"ontology_node_field_values", "ontology_node_field_value_dependencies",
}

func createOntologyReadinessSchema(ctx context.Context, db execer) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS ontology_readiness (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		schema_hash TEXT NOT NULL CHECK (length(trim(schema_hash)) > 0),
		notes_hash TEXT NOT NULL CHECK (length(trim(notes_hash)) > 0),
		raw_notes_hash TEXT NOT NULL,
		metadata_loaded_at INTEGER NOT NULL CHECK (metadata_loaded_at > 0),
		materialization_version INTEGER NOT NULL CHECK (materialization_version > 0),
		ontology_loaded_at INTEGER NOT NULL CHECK (ontology_loaded_at > 0),
		provider_hash TEXT NOT NULL CHECK (length(trim(provider_hash)) > 0),
		error_json TEXT NOT NULL
	) STRICT`); err != nil {
		return err
	}
	for _, table := range ontologyReadinessSourceTables {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			if _, err := db.ExecContext(ctx, ontologyReadinessTriggerSQL(table, event)); err != nil {
				return err
			}
		}
	}
	return nil
}

func ontologyReadinessTriggerSQL(table, event string) string {
	when := event
	if table == "ontology_schema_state" && event == "UPDATE" {
		// Cached broken-link diagnostics do not change ontology inputs.
		when += " OF schema_hash, notes_hash, materialization_version, loaded_at, ready, error_json"
	}
	return fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS ontology_readiness_%s_%s
		AFTER %s ON %s BEGIN DELETE FROM ontology_readiness; END`, table, event, when, table)
}
