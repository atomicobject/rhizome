package sqlite

import (
	"context"
	"database/sql"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration/domains"
)

func (s *NoteStore) ensureDomainMigrations(ctx context.Context) error {
	plan := domains.NoteEmbeddingsPlan(noteSchemaVersion, func(ctx context.Context, tx *sql.Tx, _, to int) error {
		if to == 7 {
			if err := addColumnIfMissingTx(ctx, tx, tableIndexMeta, "source_high_water", "INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE `+tableIndexMeta+`
			SET schema_version = CASE
				WHEN schema_version IS NULL OR schema_version < ? THEN ?
				ELSE schema_version
			END
			WHERE id = 1
		`, to, to)
		return err
	}, nil)
	if err := migration.EnsureDomain(ctx, s.db, plan, migration.EnsureOptions{}); err != nil {
		return err
	}
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return sqliteutil.ExecTxWithRetry(ctx, db, func(tx *sql.Tx) error {
			if err := addColumnIfMissingTx(ctx, tx, tableIndexMeta, "source_high_water", "INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
			if err := addColumnIfMissingTx(ctx, tx, tableIndexMeta, "visible_generation", "INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `UPDATE `+tableIndexMeta+` SET visible_generation = sync_generation WHERE visible_generation = 0 AND sync_generation > 0`)
			return err
		})
	})
}
