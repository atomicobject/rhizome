package store

import (
	"context"
	"database/sql"
)

func addOntologyRecordContextSchema(ctx context.Context, tx *sql.Tx) error {
	has, err := tableHasColumnQuery(ctx, tx, "ontology_nodes", "record_identity_json")
	if err != nil || has {
		return err
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE ontology_nodes ADD COLUMN record_identity_json TEXT NOT NULL DEFAULT ''`)
	return err
}
