package domains

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

// UserStatePlan versions durable personal state independently of the index.
func UserStatePlan() migration.DomainPlan {
	return migration.DomainPlan{
		Domain: migration.DomainUserState,
		Target: 2,
		Steps: []migration.Step{{To: 1, Name: "view_preferences", Run: func(ctx context.Context, tx *sql.Tx) error {
			for _, statement := range []string{
				`CREATE TABLE view_preference_scopes (
				 scope_key TEXT NOT NULL PRIMARY KEY,
				 revision INTEGER NOT NULL CHECK(revision >= 0),
				 migration_closed INTEGER NOT NULL CHECK(migration_closed IN (0, 1)))`,
				`CREATE TABLE view_preferences (
				 scope_key TEXT NOT NULL REFERENCES view_preference_scopes(scope_key),
				 preference_key TEXT NOT NULL,
				 value_json TEXT NOT NULL CHECK(json_valid(value_json)),
				 PRIMARY KEY(scope_key, preference_key))`,
				`CREATE TABLE view_preference_imports (
				 migration_id TEXT NOT NULL PRIMARY KEY,
				 scope_key TEXT NOT NULL REFERENCES view_preference_scopes(scope_key))`,
			} {
				if _, err := tx.ExecContext(ctx, statement); err != nil {
					return err
				}
			}
			return nil
		}}, {To: 2, Name: "view_preference_family_resets", Run: func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `CREATE TABLE view_preference_family_resets (
			 family_key TEXT NOT NULL PRIMARY KEY,
			 generation INTEGER NOT NULL CHECK(generation > 0))`); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `CREATE INDEX view_preference_scope_family ON view_preference_scopes(json_remove(scope_key, '$.widgetSlot'))`)
			return err
		}}},
		Validate: validateUserState,
	}
}

func validateUserState(ctx context.Context, tx *sql.Tx) error {
	type column struct {
		name, kind string
		primary    int
	}
	tables := map[string][]column{
		"view_preference_scopes":        {{"scope_key", "TEXT", 1}, {"revision", "INTEGER", 0}, {"migration_closed", "INTEGER", 0}},
		"view_preferences":              {{"scope_key", "TEXT", 1}, {"preference_key", "TEXT", 2}, {"value_json", "TEXT", 0}},
		"view_preference_imports":       {{"migration_id", "TEXT", 1}, {"scope_key", "TEXT", 0}},
		"view_preference_family_resets": {{"family_key", "TEXT", 1}, {"generation", "INTEGER", 0}},
	}
	for table, expected := range tables {
		rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+table+")")
		if err != nil {
			return err
		}
		var actual []column
		for rows.Next() {
			var cid, notNull int
			var defaultValue sql.NullString
			var c column
			if err := rows.Scan(&cid, &c.name, &c.kind, &notNull, &defaultValue, &c.primary); err != nil {
				_ = rows.Close()
				return err
			}
			if notNull != 1 {
				_ = rows.Close()
				return &migration.ErrSchemaDrift{Domain: migration.DomainUserState, Err: fmt.Errorf("%s.%s must be NOT NULL", table, c.name)}
			}
			actual = append(actual, c)
		}
		readErr := rows.Err()
		_ = rows.Close()
		if readErr != nil {
			return readErr
		}
		if fmt.Sprint(actual) != fmt.Sprint(expected) {
			return &migration.ErrSchemaDrift{Domain: migration.DomainUserState, Err: fmt.Errorf("unexpected columns in %s", table)}
		}
	}
	return nil
}
