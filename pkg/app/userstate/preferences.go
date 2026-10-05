package userstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Snapshot is a consistent committed state of one view instance.
type Snapshot struct {
	Scope           Scope                      `json:"scope"`
	Revision        uint64                     `json:"revision"`
	Values          map[string]json.RawMessage `json:"values"`
	MigrationClosed bool                       `json:"migrationClosed"`
}

// ConflictError includes the snapshot that rejected a stale write.
type ConflictError struct{ Current Snapshot }

func (e *ConflictError) Error() string { return "view preferences changed" }

func readSnapshot(ctx context.Context, tx *sql.Tx, scope Scope, key string) (Snapshot, error) {
	result := Snapshot{Scope: scope, Values: make(map[string]json.RawMessage)}
	err := tx.QueryRowContext(ctx, `SELECT revision, migration_closed FROM view_preference_scopes WHERE scope_key = ?`, key).Scan(&result.Revision, &result.MigrationClosed)
	if errors.Is(err, sql.ErrNoRows) {
		generation, err := familyResetGeneration(ctx, tx, scope)
		if err != nil {
			return Snapshot{}, err
		}
		result.Revision = generation
		result.MigrationClosed = generation > 0
		return result, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT preference_key, value_json FROM view_preferences WHERE scope_key = ?`, key)
	if err != nil {
		return Snapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var value []byte
		if err := rows.Scan(&name, &value); err != nil {
			return Snapshot{}, err
		}
		result.Values[name] = json.RawMessage(value)
	}
	return result, rows.Err()
}

func (s *Store) Read(ctx context.Context, scope Scope) (Snapshot, error) {
	canonical, key, err := CanonicalScope(s.vaultPath, scope)
	if err != nil {
		return Snapshot{}, err
	}
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback()
	result, err := readSnapshot(ctx, tx, canonical, key)
	if err != nil {
		return Snapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, err
	}
	return result, nil
}

func validatePatch(set map[string]json.RawMessage, unset []string) error {
	if err := validateValues(set); err != nil {
		return err
	}
	if len(unset) > MaxKeys {
		return invalid("too many unset keys")
	}
	seen := make(map[string]bool, len(unset))
	for _, key := range unset {
		if !validIdentifier(key, MaxKeyBytes) || seen[key] {
			return invalid("invalid or duplicate unset key")
		}
		if _, exists := set[key]; exists {
			return invalid("cannot set and unset %q", key)
		}
		seen[key] = true
	}
	return nil
}

func saveScope(ctx context.Context, tx *sql.Tx, key string, revision uint64, closed bool) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO view_preference_scopes(scope_key, revision, migration_closed) VALUES (?, ?, ?)
	 ON CONFLICT(scope_key) DO UPDATE SET revision=excluded.revision, migration_closed=excluded.migration_closed`, key, revision, closed)
	return err
}

func setValues(ctx context.Context, tx *sql.Tx, key string, values map[string]json.RawMessage) error {
	for name, value := range values {
		if _, err := tx.ExecContext(ctx, `INSERT INTO view_preferences(scope_key, preference_key, value_json) VALUES (?, ?, ?)
		 ON CONFLICT(scope_key, preference_key) DO UPDATE SET value_json=excluded.value_json`, key, name, string(value)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Patch(ctx context.Context, scope Scope, expected uint64, set map[string]json.RawMessage, unset []string) (Snapshot, error) {
	if err := validatePatch(set, unset); err != nil {
		return Snapshot{}, err
	}
	return s.change(ctx, scope, expected, func(ctx context.Context, tx *sql.Tx, key string) error {
		if err := setValues(ctx, tx, key, set); err != nil {
			return err
		}
		for _, name := range unset {
			if _, err := tx.ExecContext(ctx, `DELETE FROM view_preferences WHERE scope_key=? AND preference_key=?`, key, name); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) Reset(ctx context.Context, scope Scope, expected uint64) (Snapshot, error) {
	return s.change(ctx, scope, expected, func(ctx context.Context, tx *sql.Tx, key string) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM view_preferences WHERE scope_key=?`, key)
		return err
	})
}

func (s *Store) change(ctx context.Context, scope Scope, expected uint64, mutate func(context.Context, *sql.Tx, string) error) (Snapshot, error) {
	canonical, key, err := CanonicalScope(s.vaultPath, scope)
	if err != nil {
		return Snapshot{}, err
	}
	var result Snapshot
	err = s.withWrite(ctx, func(tx *sql.Tx) error {
		current, err := readSnapshot(ctx, tx, canonical, key)
		if err != nil {
			return err
		}
		if current.Revision != expected {
			return &ConflictError{Current: current}
		}
		if err := saveScope(ctx, tx, key, current.Revision+1, true); err != nil {
			return err
		}
		if err := mutate(ctx, tx, key); err != nil {
			return err
		}
		result, err = readSnapshot(ctx, tx, canonical, key)
		if err != nil {
			return err
		}
		return validateValues(result.Values)
	})
	if err != nil {
		return Snapshot{}, err
	}
	return result, nil
}

// ImportIfAbsent claims an old browser source once across all scopes. Even a
// suppressed import claims its source so another context cannot inherit it.
func (s *Store) ImportIfAbsent(ctx context.Context, scope Scope, migrationID string, values map[string]json.RawMessage) (Snapshot, bool, error) {
	if !validIdentifier(migrationID, MaxMigrationIDBytes) {
		return Snapshot{}, false, invalid("migrationId is required and must be bounded")
	}
	if err := validateValues(values); err != nil {
		return Snapshot{}, false, err
	}
	canonical, key, err := CanonicalScope(s.vaultPath, scope)
	if err != nil {
		return Snapshot{}, false, err
	}
	var result Snapshot
	var imported bool
	err = s.withWrite(ctx, func(tx *sql.Tx) error {
		imported = false
		var err error
		result, err = readSnapshot(ctx, tx, canonical, key)
		if err != nil {
			return err
		}
		var claimed int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM view_preference_imports WHERE migration_id=?)`, migrationID).Scan(&claimed); err != nil {
			return err
		}
		if claimed != 0 {
			return nil
		}
		if !result.MigrationClosed {
			if err := saveScope(ctx, tx, key, result.Revision+1, false); err != nil {
				return err
			}
			for name, value := range values {
				if _, err := tx.ExecContext(ctx, `INSERT INTO view_preferences(scope_key, preference_key, value_json) VALUES (?, ?, ?)
				 ON CONFLICT(scope_key, preference_key) DO NOTHING`, key, name, string(value)); err != nil {
					return err
				}
			}
			imported = true
		} else {
			// A family reset also closes never-persisted widget slots. Materialize
			// that inherited tombstone before recording its claimed source.
			if err := saveScope(ctx, tx, key, result.Revision, true); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO view_preference_imports(migration_id, scope_key) VALUES (?, ?)`, migrationID, key); err != nil {
			return err
		}
		result, err = readSnapshot(ctx, tx, canonical, key)
		if err != nil {
			return err
		}
		return validateValues(result.Values)
	})
	if err != nil {
		return Snapshot{}, false, err
	}
	return result, imported, nil
}
