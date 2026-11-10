package store

import (
	"context"
	"database/sql"
)

// createWriteGenerationSchema adds the one-row counter every withWrite and
// withWriteTx advances (SPEC-0136 R4).
func createWriteGenerationSchema(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS write_generation (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			value INTEGER NOT NULL
		);
		INSERT OR IGNORE INTO write_generation (id, value) VALUES (1, 0);`)
	return err
}
