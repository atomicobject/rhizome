package store

import (
	"context"
	"database/sql"
)

func addOntologyRelocationFingerprintSchema(ctx context.Context, tx *sql.Tx) error {
	present, err := tableHasColumnQuery(ctx, tx, "ontology_nodes", "relocation_fingerprint")
	if err != nil || present {
		return err
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE ontology_nodes ADD COLUMN relocation_fingerprint TEXT NOT NULL DEFAULT ''`)
	return err
}
