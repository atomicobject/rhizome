package store

import (
	"context"
	"database/sql"
	"errors"
)

func createNoteProjectionStateSchema(ctx context.Context, db execer) error {
	query, ok := db.(queryRower)
	if !ok {
		return errors.New("note projection migration requires schema query support")
	}
	hasFormatID, err := tableHasColumnQuery(ctx, query, "notes", "format_id")
	if err != nil {
		return err
	}
	if !hasFormatID {
		if _, err := db.ExecContext(ctx, `ALTER TABLE notes ADD COLUMN format_id TEXT NOT NULL DEFAULT 'markdown' CHECK (format_id != '' AND format_id = lower(trim(format_id)))`); err != nil {
			return err
		}
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS note_projection_state (
			note_id INTEGER PRIMARY KEY,
			provider_version TEXT NOT NULL DEFAULT '',
			projection_version TEXT NOT NULL DEFAULT '',
			source_content_hash TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'stale' CHECK (status IN ('stale', 'current', 'fatal')),
			diagnostic_code TEXT NOT NULL DEFAULT '',
			diagnostic_detail TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT 0 CHECK (updated_at >= 0),
			CHECK (
				(status = 'current'
					AND length(trim(provider_version)) > 0
					AND length(trim(projection_version)) > 0
					AND length(trim(source_content_hash)) > 0
					AND diagnostic_code = '' AND diagnostic_detail = '')
				OR (status = 'fatal'
					AND length(trim(provider_version)) > 0
					AND length(trim(projection_version)) > 0
					AND length(trim(diagnostic_code)) > 0
					AND length(trim(diagnostic_detail)) > 0)
				OR (status = 'stale' AND diagnostic_code = '' AND diagnostic_detail = '')
			),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
		) STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_note_projection_state_status
			ON note_projection_state(status, note_id);`,
		`INSERT OR IGNORE INTO note_projection_state(
			note_id, provider_version, projection_version, source_content_hash,
			status, diagnostic_code, diagnostic_detail, updated_at
		)
		SELECT id, '', '', '', 'stale', '', '', indexed_at
		FROM notes;`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func createNoteFragmentTargetsSchema(ctx context.Context, db execer) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS note_fragment_targets (
			note_id INTEGER NOT NULL,
			target_kind TEXT NOT NULL CHECK (target_kind IN ('heading', 'block', 'element_id', 'legacy_name')),
			target_text TEXT NOT NULL CHECK (target_text != ''),
			target_norm TEXT NOT NULL CHECK (target_norm != ''),
			ordinal INTEGER NOT NULL CHECK (ordinal >= 1),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
		) STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_note_fragment_targets_note ON note_fragment_targets(note_id, target_kind, ordinal);`,
		`CREATE INDEX IF NOT EXISTS idx_note_fragment_targets_lookup ON note_fragment_targets(note_id, target_kind, target_norm COLLATE BINARY, ordinal);`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func migrateNotePublicationFactsSchema(ctx context.Context, tx *sql.Tx) error {
	var oldTargets int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'note_markdown_targets'`).Scan(&oldTargets); err != nil {
		return err
	}
	if err := createNoteFragmentTargetsSchema(ctx, tx); err != nil {
		return err
	}
	if oldTargets > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO note_fragment_targets(note_id, target_kind, target_text, target_norm, ordinal)
			SELECT note_id, target_kind, target_text, target_norm, ordinal FROM note_markdown_targets
		`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE note_markdown_targets`); err != nil {
			return err
		}
	}
	return createNoteProjectionFactsSchema(ctx, tx)
}

func createNoteProjectionFactsSchema(ctx context.Context, db execer) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS note_projection_diagnostics (
			note_id INTEGER NOT NULL,
			ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
			code TEXT NOT NULL CHECK (code != ''),
			category TEXT NOT NULL CHECK (category != ''),
			message TEXT NOT NULL CHECK (message != ''),
			range_present INTEGER NOT NULL CHECK (range_present IN (0, 1)),
			start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
			end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
			blocking INTEGER NOT NULL CHECK (blocking IN (0, 1)),
			affected_operation TEXT NOT NULL CHECK (affected_operation != ''),
			PRIMARY KEY(note_id, ordinal),
			CHECK (range_present = 1 OR (start_byte = 0 AND end_byte = 0)),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_note_projection_diagnostics_operation ON note_projection_diagnostics(affected_operation, blocking, note_id);`,
		`CREATE TABLE IF NOT EXISTS note_search_regions (
			note_id INTEGER NOT NULL,
			ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
			origin TEXT NOT NULL CHECK (origin IN ('authored', 'derived')),
			region_kind TEXT NOT NULL CHECK (region_kind IN ('visible', 'supplemental')),
			region_text TEXT NOT NULL CHECK (region_text != ''),
			media_type TEXT NOT NULL CHECK (media_type != ''),
			range_present INTEGER NOT NULL CHECK (range_present IN (0, 1)),
			start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
			end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
			PRIMARY KEY(note_id, ordinal),
			CHECK (range_present = 1 OR (start_byte = 0 AND end_byte = 0)),
			FOREIGN KEY(note_id) REFERENCES notes(id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_note_search_regions_kind ON note_search_regions(region_kind, origin, note_id, ordinal);`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
