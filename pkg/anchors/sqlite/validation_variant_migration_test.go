package sqlite

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddValidationDiagnosticVariantSchemaUpgradesPersistedDiagnostics(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "validation-variant-migration.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Rebuild the pre-v70 shape with a published diagnostic, as a table that
	// survived ResetDomain would look.
	require.NoError(t, store.withWriteTx(ctx, func(tx *sql.Tx) error {
		for _, stmt := range []string{
			`DROP INDEX idx_validation_diagnostics_variant`,
			`ALTER TABLE validation_diagnostics DROP COLUMN variant_label`,
			`ALTER TABLE validation_diagnostics DROP COLUMN variant_key`,
			`INSERT INTO validation_generations(generation, vault_identity, scope, selected_checks_json, completion)
				VALUES (7, 'vault', 'all', '[]', 'complete')`,
			`INSERT INTO validation_diagnostics(generation, diagnostic_order, issue_key, check_name, code)
				VALUES (7, 0, 'issue-1', 'ontology', 'type_ambiguous')`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	}))

	for range 2 {
		require.NoError(t, store.withWriteTx(ctx, func(tx *sql.Tx) error {
			return addValidationDiagnosticVariantSchema(ctx, tx)
		}), "the step must be idempotent")
	}

	var key, label string
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT variant_key, variant_label FROM validation_diagnostics WHERE generation = 7 AND issue_key = 'issue-1'`,
	).Scan(&key, &label))
	require.Empty(t, key)
	require.Empty(t, label)

	var indexes int
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_validation_diagnostics_variant'`,
	).Scan(&indexes))
	require.Equal(t, 1, indexes)
}
