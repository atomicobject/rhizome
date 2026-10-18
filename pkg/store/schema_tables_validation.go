package store

import (
	"context"
	"database/sql"
)

// addValidationDiagnosticVariantSchema adds the optional issue variant to
// published diagnostics. Existing rows default to no variant until the next
// validation run publishes a new generation. The index lives here, not in the
// baseline statements, because ResetDomain keeps validation tables and a
// surviving pre-v70 table lacks the columns until this step runs.
func addValidationDiagnosticVariantSchema(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []string{"variant_key", "variant_label"} {
		hasColumn, err := tableHasColumnQuery(ctx, tx, "validation_diagnostics", column)
		if err != nil {
			return err
		}
		if hasColumn {
			continue
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE validation_diagnostics ADD COLUMN `+column+` TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_validation_diagnostics_variant ON validation_diagnostics(generation, code, variant_key)`)
	return err
}

func ensureValidationAndRationaleSchema(ctx context.Context, db execer) error {
	if err := ensureRationaleFTSSchema(ctx, db); err != nil {
		return err
	}
	for _, stmt := range validationStateSchemaStatements() {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func validationStateSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS validation_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			status TEXT NOT NULL DEFAULT 'never_ran' CHECK (status IN ('never_ran', 'running', 'ok', 'error')),
			result_json TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			generation INTEGER NOT NULL DEFAULT 0,
			published_generation INTEGER NOT NULL DEFAULT 0,
			started_at INTEGER,
			finished_at INTEGER,
			duration_ms INTEGER NOT NULL DEFAULT 0
		) STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_generations (
			generation INTEGER PRIMARY KEY,
			vault_identity TEXT NOT NULL,
			scope TEXT NOT NULL,
			selected_checks_json TEXT NOT NULL,
			schema_identity TEXT NOT NULL DEFAULT '',
			config_identity TEXT NOT NULL DEFAULT '',
			input_revision TEXT NOT NULL DEFAULT '',
			started_at INTEGER NOT NULL DEFAULT 0,
			finished_at INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
			completion TEXT NOT NULL CHECK (completion IN ('complete', 'incomplete')),
			stale_reason TEXT NOT NULL DEFAULT '',
			issue_count INTEGER NOT NULL DEFAULT 0 CHECK (issue_count >= 0),
			error_count INTEGER NOT NULL DEFAULT 0 CHECK (error_count >= 0),
			affected_file_count INTEGER NOT NULL DEFAULT 0 CHECK (affected_file_count >= 0),
			affected_note_count INTEGER NOT NULL DEFAULT 0 CHECK (affected_note_count >= 0),
			repair_action_count INTEGER NOT NULL DEFAULT 0 CHECK (repair_action_count >= 0),
			repair_plan_fingerprint TEXT NOT NULL DEFAULT ''
		) STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_checks (
			generation INTEGER NOT NULL,
			check_order INTEGER NOT NULL CHECK (check_order >= 0),
			check_name TEXT NOT NULL,
			outcome TEXT NOT NULL CHECK (outcome IN ('completed', 'failed', 'blocked', 'not_applicable', 'skipped')),
			issue_count INTEGER NOT NULL DEFAULT 0 CHECK (issue_count >= 0),
			summary TEXT NOT NULL DEFAULT '',
			notes_json TEXT NOT NULL DEFAULT '[]',
			error TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
			PRIMARY KEY (generation, check_name),
			FOREIGN KEY(generation) REFERENCES validation_generations(generation) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_diagnostics (
			generation INTEGER NOT NULL,
			diagnostic_order INTEGER NOT NULL CHECK (diagnostic_order >= 0),
			issue_key TEXT NOT NULL,
			check_name TEXT NOT NULL,
			code TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			evidence_json TEXT NOT NULL DEFAULT '',
			primary_path TEXT NOT NULL DEFAULT '',
			type_name TEXT NOT NULL DEFAULT '',
			field_name TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			target TEXT NOT NULL DEFAULT '',
			location_unit TEXT NOT NULL DEFAULT '' CHECK (location_unit IN ('', 'utf8_bytes', 'line')),
			location_start INTEGER NOT NULL DEFAULT 0 CHECK (location_start >= 0),
			location_end INTEGER NOT NULL DEFAULT 0 CHECK (location_end >= location_start),
			node_id TEXT NOT NULL DEFAULT '',
			location_field TEXT NOT NULL DEFAULT '',
			location_relation TEXT NOT NULL DEFAULT '',
			variant_key TEXT NOT NULL DEFAULT '',
			variant_label TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (generation, issue_key),
			FOREIGN KEY(generation) REFERENCES validation_generations(generation) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_diagnostic_paths (
			generation INTEGER NOT NULL,
			issue_key TEXT NOT NULL,
			path TEXT NOT NULL,
			membership_kind TEXT NOT NULL CHECK (membership_kind IN ('affected', 'note')),
			PRIMARY KEY (generation, issue_key, path, membership_kind),
			FOREIGN KEY(generation, issue_key) REFERENCES validation_diagnostics(generation, issue_key) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_diagnostic_scopes (
			generation INTEGER NOT NULL,
			issue_key TEXT NOT NULL,
			scope_kind TEXT NOT NULL CHECK (scope_kind IN ('node', 'type', 'interface')),
			scope_key TEXT NOT NULL,
			PRIMARY KEY (generation, issue_key, scope_kind, scope_key),
			FOREIGN KEY(generation, issue_key) REFERENCES validation_diagnostics(generation, issue_key) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_actions (
			generation INTEGER NOT NULL,
			action_id TEXT NOT NULL,
			check_name TEXT NOT NULL,
			issue_code TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL,
			safety TEXT NOT NULL CHECK (safety IN ('safe', 'needs_confirmation', 'agent_required')),
			title TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			question TEXT NOT NULL DEFAULT '',
			instance_count INTEGER NOT NULL DEFAULT 0 CHECK (instance_count >= 0),
			candidate_paths_json TEXT NOT NULL DEFAULT '[]',
			PRIMARY KEY (generation, action_id),
			FOREIGN KEY(generation) REFERENCES validation_generations(generation) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_action_issues (
			generation INTEGER NOT NULL,
			action_id TEXT NOT NULL,
			issue_key TEXT NOT NULL,
			PRIMARY KEY (generation, action_id, issue_key),
			FOREIGN KEY(generation, action_id) REFERENCES validation_actions(generation, action_id) ON DELETE CASCADE,
			FOREIGN KEY(generation, issue_key) REFERENCES validation_diagnostics(generation, issue_key) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS validation_action_paths (
			generation INTEGER NOT NULL,
			action_id TEXT NOT NULL,
			path TEXT NOT NULL,
			PRIMARY KEY (generation, action_id, path),
			FOREIGN KEY(generation, action_id) REFERENCES validation_actions(generation, action_id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_validation_checks_order ON validation_checks(generation, check_order);`,
		`CREATE INDEX IF NOT EXISTS idx_validation_diagnostics_page ON validation_diagnostics(generation, diagnostic_order);`,
		`CREATE INDEX IF NOT EXISTS idx_validation_diagnostics_check_code ON validation_diagnostics(generation, check_name, code, diagnostic_order);`,
		`CREATE INDEX IF NOT EXISTS idx_validation_diagnostic_paths_scope ON validation_diagnostic_paths(generation, path, issue_key);`,
		`CREATE INDEX IF NOT EXISTS idx_validation_diagnostic_scopes_scope ON validation_diagnostic_scopes(generation, scope_kind, scope_key, issue_key);`,
		`CREATE INDEX IF NOT EXISTS idx_validation_action_issues_issue ON validation_action_issues(generation, issue_key, action_id);`,
		`CREATE INDEX IF NOT EXISTS idx_validation_action_paths_scope ON validation_action_paths(generation, path, action_id);`,
	}
}

func migrateValidationDiagnosticSchema(ctx context.Context, tx *sql.Tx) error {
	hasPublished, err := tableHasColumnQuery(ctx, tx, "validation_state", "published_generation")
	if err != nil {
		return err
	}
	if !hasPublished {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE validation_state ADD COLUMN published_generation INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	for _, stmt := range validationStateSchemaStatements()[1:] {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	// Legacy result_json was rendered with a per-check cap and cannot prove a
	// complete diagnostic identity set. Force an ordinary refresh instead of
	// promoting it into the generation tables.
	_, err = tx.ExecContext(ctx, `
		UPDATE validation_state
		SET status = 'never_ran', result_json = '', error = '', published_generation = 0,
			started_at = NULL, finished_at = NULL, duration_ms = 0
	`)
	return err
}
