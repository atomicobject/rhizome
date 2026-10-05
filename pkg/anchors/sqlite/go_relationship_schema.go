package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type goRelationshipSchemaObject struct {
	kind string
	name string
	sql  string
}

var goRelationshipSchemaObjects = []goRelationshipSchemaObject{
	{kind: "table", name: "intel_go_package_relationship_state", sql: `CREATE TABLE IF NOT EXISTS intel_go_package_relationship_state (
		package_key TEXT PRIMARY KEY CHECK (package_key != ''),
		import_path TEXT NOT NULL,
		directory TEXT NOT NULL,
		package_name TEXT NOT NULL,
		build_variant TEXT NOT NULL,
		membership_digest TEXT NOT NULL CHECK (membership_digest != ''),
		analyzer_version TEXT NOT NULL CHECK (analyzer_version != ''),
		complete INTEGER NOT NULL CHECK (complete IN (0, 1)),
		valid INTEGER NOT NULL CHECK (valid IN (0, 1)),
		diagnostics_json TEXT NOT NULL DEFAULT '[]',
		updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
	) WITHOUT ROWID, STRICT;`},
	{kind: "table", name: "intel_go_package_files", sql: `CREATE TABLE IF NOT EXISTS intel_go_package_files (
		source_path TEXT PRIMARY KEY,
		package_key TEXT NOT NULL CHECK (package_key != ''),
		import_path TEXT NOT NULL,
		directory TEXT NOT NULL,
		package_name TEXT NOT NULL,
		build_variant TEXT NOT NULL,
		FOREIGN KEY (source_path) REFERENCES files(path) ON DELETE CASCADE
	) WITHOUT ROWID, STRICT;`},
	{kind: "index", name: "idx_intel_go_package_files_package", sql: `CREATE INDEX IF NOT EXISTS idx_intel_go_package_files_package
		ON intel_go_package_files(package_key, source_path);`},
	{kind: "index", name: "idx_intel_go_package_files_directory", sql: `CREATE INDEX IF NOT EXISTS idx_intel_go_package_files_directory
		ON intel_go_package_files(directory, source_path);`},
	{kind: "table", name: "intel_go_derived_relationships", sql: `CREATE TABLE IF NOT EXISTS intel_go_derived_relationships (
		package_key TEXT NOT NULL,
		kind TEXT NOT NULL CHECK (kind IN ('calls', 'implements')),
		source_path TEXT NOT NULL,
		source_fqn TEXT NOT NULL,
		target_fqn TEXT NOT NULL,
		pointer_only INTEGER NOT NULL DEFAULT 0 CHECK (pointer_only IN (0, 1)),
		PRIMARY KEY (package_key, kind, source_path, source_fqn, target_fqn),
		FOREIGN KEY (package_key)
			REFERENCES intel_go_package_relationship_state(package_key)
			ON DELETE CASCADE
	) WITHOUT ROWID, STRICT;`},
	{kind: "index", name: "idx_intel_go_derived_relationship_target", sql: `CREATE INDEX IF NOT EXISTS idx_intel_go_derived_relationship_target
		ON intel_go_derived_relationships(kind, target_fqn, source_fqn, source_path);`},
	{kind: "index", name: "idx_intel_go_derived_relationship_source", sql: `CREATE INDEX IF NOT EXISTS idx_intel_go_derived_relationship_source
		ON intel_go_derived_relationships(source_path, kind, source_fqn, target_fqn);`},
	{kind: "trigger", name: "intel_go_package_files_insert_invalidate", sql: `CREATE TRIGGER IF NOT EXISTS intel_go_package_files_insert_invalidate
		AFTER INSERT ON intel_go_package_files
		BEGIN
			UPDATE intel_go_package_relationship_state
			SET valid=0, complete=0, diagnostics_json='[{"code":"source_generation_changed"}]', updated_at=unixepoch()
			WHERE package_key=NEW.package_key AND valid=1;
			INSERT INTO index_metadata(key, value)
			SELECT 'intel_go_relationship_generation', '1' WHERE changes() > 0
			ON CONFLICT(key) DO UPDATE SET value=CAST(value AS INTEGER)+1;
		END;`},
	{kind: "trigger", name: "intel_go_package_files_delete_invalidate", sql: `CREATE TRIGGER IF NOT EXISTS intel_go_package_files_delete_invalidate
		AFTER DELETE ON intel_go_package_files
		BEGIN
			UPDATE intel_go_package_relationship_state
			SET valid=0, complete=0, diagnostics_json='[{"code":"source_generation_changed"}]', updated_at=unixepoch()
			WHERE package_key=OLD.package_key AND valid=1;
			INSERT INTO index_metadata(key, value)
			SELECT 'intel_go_relationship_generation', '1' WHERE changes() > 0
			ON CONFLICT(key) DO UPDATE SET value=CAST(value AS INTEGER)+1;
		END;`},
	{kind: "trigger", name: "intel_go_package_files_update_invalidate", sql: `CREATE TRIGGER IF NOT EXISTS intel_go_package_files_update_invalidate
		AFTER UPDATE ON intel_go_package_files
		BEGIN
			UPDATE intel_go_package_relationship_state
			SET valid=0, complete=0, diagnostics_json='[{"code":"source_generation_changed"}]', updated_at=unixepoch()
			WHERE package_key IN (OLD.package_key, NEW.package_key) AND valid=1;
			INSERT INTO index_metadata(key, value)
			SELECT 'intel_go_relationship_generation', '1' WHERE changes() > 0
			ON CONFLICT(key) DO UPDATE SET value=CAST(value AS INTEGER)+1;
		END;`},
}

func createGoRelationshipSchema(ctx context.Context, db execer) error {
	for _, object := range goRelationshipSchemaObjects {
		if _, err := db.ExecContext(ctx, object.sql); err != nil {
			return fmt.Errorf("create Go relationship %s %s: %w", object.kind, object.name, err)
		}
	}
	return nil
}

// validateGoRelationshipSchema compares complete definitions. Column-presence
// checks cannot detect weakened CHECK/FK clauses or an invalidation trigger that
// still exists by name but no longer hides stale derived relationships.
func validateGoRelationshipSchema(ctx context.Context, tx *sql.Tx) error {
	for _, object := range goRelationshipSchemaObjects {
		var got string
		if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = ? AND name = ?`, object.kind, object.name).Scan(&got); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return intelSchemaDrift("missing Go relationship %s %s", object.kind, object.name)
			}
			return fmt.Errorf("validate Go relationship %s %s: %w", object.kind, object.name, err)
		}
		if normalizeGoRelationshipSchemaSQL(got) != normalizeGoRelationshipSchemaSQL(object.sql) {
			return intelSchemaDrift("invalid Go relationship %s %s", object.kind, object.name)
		}
	}
	return nil
}

func normalizeGoRelationshipSchemaSQL(sqlText string) string {
	normalized := strings.TrimSuffix(strings.Join(strings.Fields(sqlText), " "), ";")
	for _, prefix := range []string{
		"CREATE TABLE IF NOT EXISTS",
		"CREATE INDEX IF NOT EXISTS",
		"CREATE TRIGGER IF NOT EXISTS",
	} {
		normalized = strings.Replace(normalized, prefix, strings.Replace(prefix, " IF NOT EXISTS", "", 1), 1)
	}
	return normalized
}
