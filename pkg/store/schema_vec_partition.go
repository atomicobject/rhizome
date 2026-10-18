package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func intelVecTableName(dims int) string {
	return fmt.Sprintf("%s%d", intelVecTablePrefix, dims)
}

func createIntelVecTable(ctx context.Context, db execer, dims int) error {
	if dims <= 0 {
		return fmt.Errorf("invalid vec dimensions: %d", dims)
	}
	table := intelVecTableName(dims)
	indexingperf.AddCount(ctx, indexingperf.AgentStartOpSchemaStatements, 1)
	_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(chunk_id integer primary key, owner_type text partition key, embedding float[%d] distance_metric=cosine);`, table, dims))
	return err
}

func migrateIntelVecOwnerPartitions(ctx context.Context, tx *sql.Tx) error {
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
			return fmt.Errorf("missing vector table %s during owner partition migration", table)
		}
		hasOwnerPartition, err := vecOwnerPartitionReadyTx(ctx, tx, table)
		if err != nil {
			return err
		}
		if hasOwnerPartition {
			continue
		}
		staging := table + "_owner_partition_v67_stage"
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+staging); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `CREATE TABLE `+staging+` (chunk_id INTEGER PRIMARY KEY, owner_type TEXT NOT NULL, embedding BLOB NOT NULL) STRICT`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s(chunk_id, owner_type, embedding)
			SELECT v.chunk_id, COALESCE(c.owner_type, ''), v.embedding
			FROM %s v
			LEFT JOIN intel_chunks c ON c.id = v.chunk_id
		`, staging, table)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE `+table); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`CREATE VIRTUAL TABLE %s USING vec0(chunk_id integer primary key, owner_type text partition key, embedding float[%d] distance_metric=cosine)`, table, dims)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s(chunk_id, owner_type, embedding) SELECT chunk_id, owner_type, embedding FROM %s`, table, staging)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE `+staging); err != nil {
			return err
		}
	}
	return nil
}

func tableHasColumnTx(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	var found int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM pragma_table_info(?)
		WHERE name = ?
		LIMIT 1
	`, table, column).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return found == 1, nil
}

func dropTriggersByPrefixTx(ctx context.Context, tx *sql.Tx, prefix string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'trigger' AND name LIKE ?
	`, prefix+"%")
	if err != nil {
		return err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range names {
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS `+name); err != nil {
			return err
		}
	}
	return nil
}

func dropVecTablesByPrefixTx(ctx context.Context, tx *sql.Tx, prefix string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name LIKE ?
	`, prefix+"%")
	if err != nil {
		return err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range names {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+name); err != nil {
			return err
		}
	}
	return nil
}
