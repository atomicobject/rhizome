package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func validateIntelVecOwnerPartitionsTx(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT dimensions FROM intel_embeddings WHERE dimensions > 0 ORDER BY dimensions`)
	if err != nil {
		return err
	}
	var dimensions []int
	for rows.Next() {
		var dims int
		if err := rows.Scan(&dims); err != nil {
			_ = rows.Close()
			return err
		}
		dimensions = append(dimensions, dims)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, dims := range dimensions {
		table := intelVecTableName(dims)
		exists, err := tableExistsQuery(ctx, tx, table)
		if err != nil {
			return err
		}
		if !exists {
			return intelSchemaDrift("missing vector table %s", table)
		}
		hasOwnerPartition, err := vecOwnerPartitionReadyTx(ctx, tx, table)
		if err != nil {
			return err
		}
		if !hasOwnerPartition {
			return intelSchemaDrift("vector table %s is missing owner_type partition", table)
		}
	}
	return nil
}

func vecOwnerPartitionReadyTx(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var ddl string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&ddl); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	normalized := strings.ToLower(strings.Join(strings.Fields(ddl), " "))
	return strings.Contains(normalized, "owner_type text partition key"), nil
}
