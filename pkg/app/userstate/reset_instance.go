package userstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

func familyKey(scope Scope) string {
	scope.WidgetSlot = ""
	encoded, _ := json.Marshal(scope)
	return string(encoded)
}

func familyResetGeneration(ctx context.Context, tx *sql.Tx, scope Scope) (uint64, error) {
	var generation uint64
	err := tx.QueryRowContext(ctx, `SELECT generation FROM view_preference_family_resets WHERE family_key=?`, familyKey(scope)).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return generation, err
}

// ResetInstance resets the host and every persisted widget in one transaction.
// The family marker also rejects imports and stale writes for slots that have
// never been persisted, including widgets that are currently unmounted.
func (s *Store) ResetInstance(ctx context.Context, scope Scope, expected uint64) (Snapshot, error) {
	canonical, key, err := CanonicalScope(s.vaultPath, scope)
	if err != nil {
		return Snapshot{}, err
	}
	if canonical.WidgetSlot != "" {
		return Snapshot{}, invalid("includeSlots requires a host scope without widgetSlot")
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
		if _, err := tx.ExecContext(ctx, `DELETE FROM view_preferences WHERE scope_key IN
		 (SELECT scope_key FROM view_preference_scopes WHERE json_remove(scope_key, '$.widgetSlot')=?)`, key); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE view_preference_scopes SET revision=revision+1, migration_closed=1
		 WHERE json_remove(scope_key, '$.widgetSlot')=?`, key); err != nil {
			return err
		}
		// Establish the host tombstone even when only its widgets existed.
		if err := saveScope(ctx, tx, key, current.Revision+1, true); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO view_preference_family_resets(family_key, generation) VALUES (?, 1)
		 ON CONFLICT(family_key) DO UPDATE SET generation=generation+1`, key); err != nil {
			return err
		}
		result, err = readSnapshot(ctx, tx, canonical, key)
		return err
	})
	if err != nil {
		return Snapshot{}, err
	}
	return result, nil
}
