package store

import (
	"context"
	"strings"
)

func intelBaselineStep26(ctx context.Context, db execer) error { // v26 -> v27
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS ontology_note_types (
				note_path TEXT NOT NULL,
				type_name TEXT NOT NULL,
				schema_hash TEXT NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (note_path, type_name)
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_note_types_type ON ontology_note_types(type_name, note_path);`,
		`CREATE TABLE IF NOT EXISTS ontology_edges (
				src_path TEXT NOT NULL,
				src_node_id TEXT NOT NULL DEFAULT '',
				relation_name TEXT NOT NULL,
				dst_path TEXT NOT NULL,
				dst_node_id TEXT NOT NULL DEFAULT '',
				dst_type TEXT NOT NULL,
				provenance TEXT NOT NULL,
				structural INTEGER NOT NULL,
				schema_hash TEXT NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (src_path, src_node_id, relation_name, dst_path, dst_node_id, provenance)
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_edges_src ON ontology_edges(src_path, src_node_id, structural, relation_name);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_edges_dst ON ontology_edges(dst_path, dst_node_id, structural, relation_name);`,
		`CREATE TABLE IF NOT EXISTS ontology_schema_state (
				schema_hash TEXT NOT NULL,
				notes_hash TEXT NOT NULL,
				materialization_version INTEGER NOT NULL DEFAULT 0 CHECK (materialization_version >= 0),
				loaded_at INTEGER NOT NULL,
				ready INTEGER NOT NULL,
				error_json TEXT,
				PRIMARY KEY (schema_hash, notes_hash)
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_schema_state_loaded ON ontology_schema_state(loaded_at DESC);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep27(ctx context.Context, db execer) error { // v27 -> v28
	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_ontology_edges_src_query ON ontology_edges(src_path, provenance, relation_name, dst_type, dst_path);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_edges_dst_query ON ontology_edges(dst_path, provenance, relation_name, src_path);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep31(ctx context.Context, db execer) error { // v31 -> v32
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS ontology_note_assessments (
				note_path TEXT NOT NULL PRIMARY KEY,
				declared_type TEXT NOT NULL,
				resolved_type TEXT NOT NULL,
				assessment_json TEXT NOT NULL,
				schema_hash TEXT NOT NULL,
				updated_at INTEGER NOT NULL
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_note_assessments_resolved ON ontology_note_assessments(resolved_type, note_path);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep32(ctx context.Context, db execer) error { // v32 -> v33
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS ontology_note_state (
				note_path TEXT NOT NULL PRIMARY KEY,
				input_fingerprint TEXT NOT NULL,
				schema_hash TEXT NOT NULL,
				resolved_type TEXT NOT NULL,
				updated_at INTEGER NOT NULL
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_note_state_schema ON ontology_note_state(schema_hash, note_path);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep33(ctx context.Context, db execer) error { // v33 -> v34
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS ontology_type_policies (
					type_name TEXT NOT NULL PRIMARY KEY,
					policy_json TEXT NOT NULL,
					schema_hash TEXT NOT NULL,
					updated_at INTEGER NOT NULL
				) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_type_policies_schema ON ontology_type_policies(schema_hash, type_name);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep42(ctx context.Context, db execer) error { // v42 -> v43
	_, err := db.ExecContext(ctx, `ALTER TABLE ontology_schema_state ADD COLUMN broken_links_json TEXT;`)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
		return nil
	}
	return err
}

func intelBaselineStep46(ctx context.Context, db execer) error { // v46 -> v47
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS ontology_nodes (
				id INTEGER PRIMARY KEY,
				node_id TEXT NOT NULL UNIQUE CHECK (node_id != ''),
				note_path TEXT NOT NULL CHECK (note_path != ''),
				node_ref_json TEXT NOT NULL CHECK (node_ref_json != ''),
				node_kind TEXT NOT NULL CHECK (node_kind != ''),
				type_name TEXT,
				parent_node_id TEXT,
				parent_type_name TEXT,
				title TEXT,
				source_locator TEXT NOT NULL DEFAULT '',
				fragment TEXT NOT NULL DEFAULT '',
				block_id TEXT NOT NULL DEFAULT '',
				display_label TEXT NOT NULL DEFAULT '',
				locator_status TEXT NOT NULL DEFAULT '',
				start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
				end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
				structural_fingerprint TEXT,
				schema_hash TEXT,
				updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
			) STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_note_path ON ontology_nodes(note_path);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_type ON ontology_nodes(type_name);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_source_locator ON ontology_nodes(source_locator);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_note_fragment ON ontology_nodes(note_path, fragment);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_note_block ON ontology_nodes(note_path, block_id);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := recreateIntelChunksWithOntologyNode(ctx, db); err != nil {
		return err
	}
	sidecarStmts := []string{
		`CREATE TABLE IF NOT EXISTS ontology_node_embedding_state (
				chunk_id TEXT PRIMARY KEY CHECK (chunk_id != ''),
				node_id TEXT NOT NULL CHECK (node_id != ''),
				note_path TEXT NOT NULL CHECK (note_path != ''),
				type_name TEXT,
				node_kind TEXT NOT NULL CHECK (node_kind != ''),
				embedding_schema_signature TEXT NOT NULL,
				node_structure_fingerprint TEXT NOT NULL,
				source_content_hash TEXT NOT NULL,
				chunk_text_hash TEXT NOT NULL,
				chunk_granularity TEXT NOT NULL,
				provider TEXT,
				model TEXT,
				updated_at INTEGER NOT NULL CHECK (updated_at >= 0),
				FOREIGN KEY(chunk_id) REFERENCES intel_chunks(chunk_id) ON DELETE CASCADE
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_embedding_state_node ON ontology_node_embedding_state(node_id);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_embedding_state_note ON ontology_node_embedding_state(note_path);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_node_embedding_state_type ON ontology_node_embedding_state(type_name);`,
	}
	for _, stmt := range sidecarStmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep47(ctx context.Context, db execer) error { // v47 -> v48
	stmts := []string{
		`ALTER TABLE ontology_edges RENAME TO ontology_edges_legacy_node_scope`,
		`CREATE TABLE ontology_edges (
				src_path TEXT NOT NULL,
				src_node_id TEXT NOT NULL DEFAULT '',
				relation_name TEXT NOT NULL,
				dst_path TEXT NOT NULL,
				dst_node_id TEXT NOT NULL DEFAULT '',
				dst_type TEXT NOT NULL,
				provenance TEXT NOT NULL,
				structural INTEGER NOT NULL,
				schema_hash TEXT NOT NULL,
				updated_at INTEGER NOT NULL,
				PRIMARY KEY (src_path, src_node_id, relation_name, dst_path, dst_node_id, provenance)
			) WITHOUT ROWID, STRICT;`,
		`INSERT OR REPLACE INTO ontology_edges (src_path, src_node_id, relation_name, dst_path, dst_node_id, dst_type, provenance, structural, schema_hash, updated_at)
			 SELECT src_path, '', relation_name, dst_path, '', dst_type, provenance, structural, schema_hash, updated_at
			 FROM ontology_edges_legacy_node_scope`,
		`DROP TABLE ontology_edges_legacy_node_scope`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_edges_src ON ontology_edges(src_path, src_node_id, structural, relation_name);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_edges_dst ON ontology_edges(dst_path, dst_node_id, structural, relation_name);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_edges_src_query ON ontology_edges(src_path, src_node_id, provenance, relation_name, dst_type, dst_path);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_edges_dst_query ON ontology_edges(dst_path, dst_node_id, provenance, relation_name, src_path);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "no such table: ontology_edges") ||
				strings.Contains(strings.ToLower(err.Error()), "there is already another table or index with this name") {
				continue
			}
			return err
		}
	}
	return nil
}

func intelBaselineStep48(ctx context.Context, db execer) error { // v48 -> v49
	stmts := []string{
		`ALTER TABLE ontology_nodes ADD COLUMN source_locator TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE ontology_nodes ADD COLUMN fragment TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE ontology_nodes ADD COLUMN block_id TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE ontology_nodes ADD COLUMN display_label TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE ontology_nodes ADD COLUMN locator_status TEXT NOT NULL DEFAULT '';`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_source_locator ON ontology_nodes(source_locator);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_note_fragment ON ontology_nodes(note_path, fragment);`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_nodes_note_block ON ontology_nodes(note_path, block_id);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}

func intelBaselineStep49(ctx context.Context, db execer) error { // v49 -> v50
	for _, stmt := range ontologyNodeFieldValueSchemaStatements() {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep50(ctx context.Context, db execer) error { // v50 -> v51
	for _, stmt := range ontologyNodeFieldValueDependencySchemaStatements() {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep51(ctx context.Context, db execer) error { // v51 -> v52
	// v49/v50 created ontology_node_field_values{,_dependencies} empty.
	// EnsureIndex's fast path skips when ontology_schema_state.SchemaHash and
	// NotesHash are unchanged, so an upgrade against an existing vault would
	// leave the new field tables empty and indexed reads would be wrong.
	// Clear the schema state so the next index/serve recomputes the read
	// model and populates the field/dependency rows.
	if _, err := db.ExecContext(ctx, `DELETE FROM ontology_schema_state`); err != nil {
		return err
	}
	return nil
}

func intelBaselineStep55(ctx context.Context, db execer) error { // v55 -> v56
	// Generated answer cards were retired in favor of enriched primary
	// chunks. Remove their durable rows so embeddings and invalidation state
	// cascade away instead of lingering as unreachable index data.
	if _, err := db.ExecContext(ctx, `
			DELETE FROM intel_chunks
			WHERE chunk_family = 'ontology_card'
			   OR granularity IN ('card', 'node_card')`); err != nil {
		return err
	}
	for _, table := range []string{
		"indexed_card_context_diagnostics",
		"indexed_card_context_targets",
		"indexed_card_facts",
		"indexed_cards",
		"indexed_card_specs",
	} {
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
			return err
		}
	}
	// The primary node-body format changed with card removal. Clearing the
	// cached schema state makes the next index rebuild unchanged projections,
	// replacing old ordinals and preserving bodyless-node retrieval.
	if _, err := db.ExecContext(ctx, `DELETE FROM ontology_schema_state`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM ontology_node_embedding_state`); err != nil {
		return err
	}
	return nil
}
