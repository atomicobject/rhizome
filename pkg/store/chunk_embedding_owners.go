package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

func repartitionIntelEmbeddingOwnersTx(ctx context.Context, tx *sql.Tx, chunkIDs map[string]struct{}, useKeepTable bool) error {
	if len(chunkIDs) == 0 {
		return nil
	}
	var scopeSQL string
	var scopeArgs []any
	if useKeepTable {
		scopeSQL = `SELECT chunk_id FROM temp_intel_keep_chunks`
	} else {
		placeholders := make([]string, 0, len(chunkIDs))
		ids := make([]string, 0, len(chunkIDs))
		for id := range chunkIDs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			placeholders = append(placeholders, "?")
			scopeArgs = append(scopeArgs, id)
		}
		scopeSQL = `SELECT chunk_id FROM intel_chunks WHERE chunk_id IN (` + strings.Join(placeholders, ",") + `)`
	}

	dimensionArgs := append([]any(nil), scopeArgs...)
	dimensionRows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT e.dimensions
		FROM intel_embeddings e
		WHERE e.chunk_id IN (`+scopeSQL+`)
		ORDER BY e.dimensions
	`, dimensionArgs...)
	if err != nil {
		return err
	}
	var dimensions []int
	for dimensionRows.Next() {
		var dims int
		if err := dimensionRows.Scan(&dims); err != nil {
			_ = dimensionRows.Close()
			return err
		}
		dimensions = append(dimensions, dims)
	}
	if err := dimensionRows.Err(); err != nil {
		_ = dimensionRows.Close()
		return err
	}
	_ = dimensionRows.Close()
	if len(dimensions) == 0 {
		return nil
	}

	const staging = `temp_intel_vec_owner_repartition`
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+staging); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+staging+` (chunk_id INTEGER PRIMARY KEY, owner_type TEXT NOT NULL, embedding BLOB NOT NULL) STRICT`); err != nil {
		return err
	}
	defer tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+staging) //nolint:errcheck

	for _, dims := range dimensions {
		table := intelVecTableName(dims)
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+staging); err != nil {
			return err
		}
		args := []any{dims}
		args = append(args, scopeArgs...)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s(chunk_id, owner_type, embedding)
			SELECT c.id, c.owner_type, v.embedding
			FROM intel_chunks c
			JOIN intel_embeddings e ON e.chunk_id = c.chunk_id AND e.dimensions = ?
			JOIN %s v ON v.chunk_id = c.id
			WHERE c.chunk_id IN (%s) AND v.owner_type <> c.owner_type
		`, staging, table, scopeSQL), args...); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE chunk_id IN (SELECT chunk_id FROM `+staging+`)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+table+`(chunk_id, owner_type, embedding) SELECT chunk_id, owner_type, embedding FROM `+staging); err != nil {
			return err
		}
	}
	return nil
}
