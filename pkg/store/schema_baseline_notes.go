package store

import (
	"context"
	"strings"
)

func intelBaselineStep28(ctx context.Context, db execer) error { // v28 -> v29
	stmts := []string{
		`ALTER TABLE notes ADD COLUMN size INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE notes ADD COLUMN indexed_at INTEGER NOT NULL DEFAULT 0;`,
		`CREATE TABLE IF NOT EXISTS property_keys (
				property_id INTEGER PRIMARY KEY,
				property_name TEXT NOT NULL UNIQUE,
				updated_at INTEGER NOT NULL
			);`,
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
		`CREATE TABLE IF NOT EXISTS note_tags (
				note_id INTEGER NOT NULL,
				tag_norm TEXT NOT NULL,
				PRIMARY KEY (note_id, tag_norm),
				FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
			);`,
		`CREATE TABLE IF NOT EXISTS note_metadata_state (
				notes_hash TEXT NOT NULL,
				raw_notes_hash TEXT NOT NULL DEFAULT '',
				loaded_at INTEGER NOT NULL,
				ready INTEGER NOT NULL DEFAULT 0
			);`,
		`CREATE INDEX IF NOT EXISTS idx_property_keys_name ON property_keys(property_name);`,
		`CREATE INDEX IF NOT EXISTS idx_note_property_values_lookup ON note_property_values(property_id, value_norm, note_id);`,
		`CREATE INDEX IF NOT EXISTS idx_note_property_values_note ON note_property_values(note_id, property_id, source);`,
		`CREATE INDEX IF NOT EXISTS idx_note_tags_tag ON note_tags(tag_norm, note_id);`,
		`CREATE INDEX IF NOT EXISTS idx_note_metadata_state_loaded ON note_metadata_state(loaded_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_notes_indexed_at ON notes(indexed_at, path);`,
		`CREATE INDEX IF NOT EXISTS idx_files_call_edges_stale ON files(call_edges_stale, path);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}

func intelBaselineStep29(ctx context.Context, db execer) error { // v29 -> v30
	_, err := db.ExecContext(ctx, `ALTER TABLE note_metadata_state ADD COLUMN raw_notes_hash TEXT NOT NULL DEFAULT '';`)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
		return nil
	}
	return err
}
