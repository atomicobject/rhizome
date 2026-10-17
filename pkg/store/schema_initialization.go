package store

import (
	"context"
)

func (s *Store) ensureSchema(ctx context.Context, schemaVersion int) error {
	stmts := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS files (
			path TEXT PRIMARY KEY,
			lang TEXT NOT NULL,
			hash TEXT,
			indexer_version TEXT,
			parse_status TEXT NOT NULL DEFAULT 'ok',
			call_edges_stale INTEGER NOT NULL DEFAULT 0,
			mtime INTEGER
		);`,
		`CREATE TABLE IF NOT EXISTS symbols (
			id INTEGER PRIMARY KEY,
			lang TEXT NOT NULL,
			kind TEXT NOT NULL,
			file TEXT NOT NULL,
			pkg TEXT,
			name TEXT NOT NULL,
			fqn TEXT NOT NULL UNIQUE,
			fqn_reversed TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(file);`,
		`CREATE INDEX IF NOT EXISTS idx_symbols_lang_pkg_name ON symbols(lang, pkg, name);`,
		`CREATE INDEX IF NOT EXISTS idx_symbols_fqn_reversed ON symbols(fqn_reversed);`,
		`CREATE INDEX IF NOT EXISTS idx_symbols_lang_fqn_reversed ON symbols(lang, fqn_reversed);`,
		`CREATE TABLE IF NOT EXISTS super_edges (
			child_fqn TEXT NOT NULL,
			parent_fqn TEXT NOT NULL,
			kind TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_super_child ON super_edges(child_fqn);`,
		`CREATE INDEX IF NOT EXISTS idx_super_parent ON super_edges(parent_fqn);`,
		`CREATE TABLE IF NOT EXISTS annotations (
			id INTEGER PRIMARY KEY,
			owner_fqn TEXT NOT NULL,
			ann_lang TEXT,
			ann_pkg TEXT,
			ann_name TEXT,
			args_json TEXT,
			confidence REAL DEFAULT 1.0
		);`,
		`CREATE INDEX IF NOT EXISTS idx_annotations_type ON annotations(ann_lang, ann_pkg, ann_name);`,
		`CREATE TABLE IF NOT EXISTS anchors (
			id INTEGER PRIMARY KEY,
			label TEXT NOT NULL UNIQUE,
			kind TEXT NOT NULL,
			lang TEXT,
			base_lang TEXT,
			base_pkg TEXT,
			base_name TEXT,
			base_member INTEGER NOT NULL DEFAULT 0,
			ann_lang TEXT,
			ann_pkg TEXT,
			ann_name TEXT,
			ann_args_json TEXT,
			path_prefix TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_anchors_callee ON anchors(kind, base_lang, base_name, base_pkg);`,
		`CREATE TABLE IF NOT EXISTS anchor_globs (
			anchor_id INTEGER NOT NULL,
			pattern TEXT NOT NULL,
			UNIQUE(anchor_id, pattern),
			FOREIGN KEY(anchor_id) REFERENCES anchors(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_anchor_globs_anchor ON anchor_globs(anchor_id);`,
		`CREATE TABLE IF NOT EXISTS notes (
			id INTEGER PRIMARY KEY,
			path TEXT NOT NULL UNIQUE,
			title TEXT,
			content_hash TEXT NOT NULL DEFAULT '',
			indexer_version TEXT NOT NULL DEFAULT '',
			mtime INTEGER NOT NULL DEFAULT 0,
			size INTEGER NOT NULL DEFAULT 0,
			indexed_at INTEGER NOT NULL DEFAULT 0,
			note_key_full TEXT NOT NULL DEFAULT '',
			note_key_base TEXT NOT NULL DEFAULT '',
			path_len INTEGER NOT NULL DEFAULT 0,
			first_segment_norm TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_indexed_at ON notes(indexed_at, path);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_note_key_full ON notes(note_key_full);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_note_key_base ON notes(note_key_base, path_len, path);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_first_segment_norm ON notes(first_segment_norm, path);`,
		`CREATE TABLE IF NOT EXISTS property_keys (
			property_id INTEGER PRIMARY KEY,
			property_name TEXT NOT NULL UNIQUE,
			updated_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_property_keys_name ON property_keys(property_name);`,
		`CREATE TABLE IF NOT EXISTS note_property_values (
			note_id INTEGER NOT NULL,
			property_id INTEGER NOT NULL,
			source INTEGER NOT NULL CHECK (source IN (1, 2)),
			value_text TEXT NOT NULL,
			value_norm TEXT NOT NULL,
			value_kind INTEGER NOT NULL,
			is_list INTEGER NOT NULL,
			list_ordinal INTEGER NOT NULL,
			PRIMARY KEY (note_id, property_id, source, list_ordinal, value_norm),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE,
			FOREIGN KEY(property_id) REFERENCES property_keys(property_id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_note_property_values_lookup ON note_property_values(property_id, value_norm, note_id);`,
		`CREATE INDEX IF NOT EXISTS idx_note_property_values_note ON note_property_values(note_id, property_id, source);`,
		`CREATE TABLE IF NOT EXISTS note_tags (
			note_id INTEGER NOT NULL,
			tag_norm TEXT NOT NULL,
			PRIMARY KEY (note_id, tag_norm),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_note_tags_tag ON note_tags(tag_norm, note_id);`,
		`CREATE TABLE IF NOT EXISTS note_search_terms (
			note_id INTEGER NOT NULL,
			scope INTEGER NOT NULL,
			segment_ordinal INTEGER NOT NULL,
			token_ordinal INTEGER NOT NULL,
			token_text TEXT NOT NULL,
			PRIMARY KEY (note_id, scope, segment_ordinal, token_ordinal, token_text),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_note_search_terms_lookup ON note_search_terms(scope, token_text, note_id, segment_ordinal, token_ordinal);`,
		`CREATE INDEX IF NOT EXISTS idx_note_search_terms_note ON note_search_terms(note_id, scope, segment_ordinal, token_ordinal);`,
		`CREATE TABLE IF NOT EXISTS note_metadata_state (
			notes_hash TEXT NOT NULL,
			raw_notes_hash TEXT NOT NULL DEFAULT '',
			loaded_at INTEGER NOT NULL,
			ready INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE INDEX IF NOT EXISTS idx_note_metadata_state_loaded ON note_metadata_state(loaded_at DESC);`,
		`CREATE TABLE IF NOT EXISTS note_anchors (
			note_id INTEGER NOT NULL,
			anchor_id INTEGER NOT NULL,
			UNIQUE(note_id, anchor_id),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE,
			FOREIGN KEY(anchor_id) REFERENCES anchors(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS anchor_scopes (
			anchor_id INTEGER NOT NULL,
			symbol_fqn TEXT,
			call_file TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_anchor_scopes_anchor ON anchor_scopes(anchor_id);`,
		`CREATE INDEX IF NOT EXISTS idx_anchor_scopes_symbol ON anchor_scopes(symbol_fqn);`,
		`CREATE INDEX IF NOT EXISTS idx_anchor_scopes_call_file ON anchor_scopes(call_file);`,
		`CREATE TABLE IF NOT EXISTS index_metadata (
			key TEXT PRIMARY KEY,
			value TEXT
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.bootstrapIntelBaseline(ctx); err != nil {
		return err
	}
	// Create indices that depend on repaired columns.
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_annotations_owner ON annotations(owner_fqn);`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_annotations_type_owner ON annotations(ann_lang, ann_pkg, ann_name, owner_fqn);`); err != nil {
		return err
	}
	if err := s.ensureSchemaVersion(ctx, schemaVersion); err != nil {
		return err
	}
	return nil
}
