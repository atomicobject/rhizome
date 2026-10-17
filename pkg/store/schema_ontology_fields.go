package store

func ontologyNodeFieldValueSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS ontology_node_field_values (
			id INTEGER PRIMARY KEY,
			node_id TEXT NOT NULL CHECK (node_id != ''),
			note_path TEXT NOT NULL CHECK (note_path != ''),
			type_name TEXT NOT NULL DEFAULT '',
			field_name TEXT NOT NULL CHECK (field_name != ''),
			field_kind TEXT NOT NULL DEFAULT '',
			source_kind TEXT NOT NULL DEFAULT '',
			value_kind TEXT NOT NULL DEFAULT '',
			value_text TEXT NOT NULL DEFAULT '',
			value_norm TEXT NOT NULL DEFAULT '',
			value_bool INTEGER,
			value_int INTEGER,
			value_real REAL,
			value_date TEXT,
			value_datetime TEXT,
			target_node_id TEXT NOT NULL DEFAULT '',
			target_ref_json TEXT NOT NULL DEFAULT '',
			target_note_path TEXT NOT NULL DEFAULT '',
			target_type_name TEXT NOT NULL DEFAULT '',
			target_source_locator TEXT NOT NULL DEFAULT '',
			list_ordinal INTEGER NOT NULL DEFAULT 0,
			schema_hash TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL CHECK (updated_at >= 0),
			FOREIGN KEY(node_id) REFERENCES ontology_nodes(node_id) ON DELETE CASCADE
		) STRICT;`,
		// IMPORTANT: this generic sidecar is node-scoped, not note-scoped like
		// note_property_values. It supports typed ontology predicates without
		// hard-coding ActionItem or per-type columns. Docs: [[ontology-indexed-read-model-contract]]
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ontology_node_field_values_unique ON ontology_node_field_values(node_id, field_name, list_ordinal, value_norm, target_node_id);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_node ON ontology_node_field_values(node_id, field_name, list_ordinal);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_norm ON ontology_node_field_values(type_name, field_name, value_norm, node_id) WHERE value_norm != '';`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_bool ON ontology_node_field_values(type_name, field_name, value_bool, node_id) WHERE value_bool IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_date ON ontology_node_field_values(type_name, field_name, value_date, node_id) WHERE value_date IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_datetime ON ontology_node_field_values(type_name, field_name, value_datetime, node_id) WHERE value_datetime IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_real ON ontology_node_field_values(type_name, field_name, value_real, node_id) WHERE value_real IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_int ON ontology_node_field_values(type_name, field_name, value_int, node_id) WHERE value_int IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_target ON ontology_node_field_values(type_name, field_name, target_node_id, node_id) WHERE target_node_id != '';`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_target_note ON ontology_node_field_values(type_name, field_name, target_note_path, target_type_name, node_id) WHERE target_note_path != '';`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_values_note ON ontology_node_field_values(note_path, node_id);`,
	}
}

func ontologyNodeFieldValueDependencySchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS ontology_node_field_value_dependencies (
			id INTEGER PRIMARY KEY,
			source_note_path TEXT NOT NULL CHECK (source_note_path != ''),
			node_id TEXT NOT NULL DEFAULT '',
			type_name TEXT NOT NULL DEFAULT '',
			field_name TEXT NOT NULL CHECK (field_name != ''),
			target_input TEXT NOT NULL DEFAULT '',
			target_input_norm TEXT NOT NULL DEFAULT '',
			resolved_target_note_path TEXT NOT NULL DEFAULT '',
			resolved_target_type_name TEXT NOT NULL DEFAULT '',
			schema_hash TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
		) STRICT;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ontology_node_field_value_dependencies_unique ON ontology_node_field_value_dependencies(source_note_path, node_id, field_name, target_input_norm);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_value_dependencies_source ON ontology_node_field_value_dependencies(source_note_path);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_value_dependencies_target_input ON ontology_node_field_value_dependencies(target_input_norm, source_note_path);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_field_value_dependencies_resolved_target ON ontology_node_field_value_dependencies(resolved_target_note_path, source_note_path) WHERE resolved_target_note_path != '';`,
	}
}
