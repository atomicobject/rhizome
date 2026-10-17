package store

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateIntelEmbeddingsToVecPrimaryTx(ctx context.Context, tx *sql.Tx) ([]int, error) {
	hasEmbeddingBlob, err := tableHasColumnTx(ctx, tx, "intel_embeddings", "embedding")
	if err != nil {
		return nil, err
	}
	if !hasEmbeddingBlob {
		return nil, nil
	}
	hasChunkRowID, err := tableHasColumnTx(ctx, tx, "intel_chunks", "id")
	if err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT dimensions
		FROM intel_embeddings
		WHERE dimensions > 0
		ORDER BY dimensions
	`)
	if err != nil {
		return nil, err
	}

	var dimsList []int
	for rows.Next() {
		var dims int
		if err := rows.Scan(&dims); err != nil {
			_ = rows.Close()
			return nil, err
		}
		dimsList = append(dimsList, dims)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	for _, dims := range dimsList {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+intelVecTableName(dims)); err != nil {
			return nil, err
		}
		if err := createIntelVecTable(ctx, tx, dims); err != nil {
			return nil, err
		}
		table := intelVecTableName(dims)
		if hasChunkRowID {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
				DELETE FROM %s
				WHERE chunk_id IN (
					SELECT c.id
					FROM intel_embeddings e
					JOIN intel_chunks c ON c.chunk_id = e.chunk_id
					WHERE e.dimensions = ? AND length(e.embedding) = ?
				)
			`, table), dims, dims*4); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
				INSERT INTO %s(chunk_id, owner_type, embedding)
				SELECT c.id, c.owner_type, e.embedding
				FROM intel_embeddings e
				JOIN intel_chunks c ON c.chunk_id = e.chunk_id
				WHERE e.dimensions = ? AND length(e.embedding) = ?
			`, table), dims, dims*4); err != nil {
				return nil, err
			}
		} else {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
				DELETE FROM %s
				WHERE chunk_id IN (
					SELECT chunk_id
					FROM intel_embeddings
					WHERE dimensions = ? AND length(embedding) = ?
				)
			`, table), dims, dims*4); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
				INSERT INTO %s(chunk_id, owner_type, embedding)
				SELECT e.chunk_id, COALESCE(c.owner_type, ''), e.embedding
				FROM intel_embeddings e
				LEFT JOIN intel_chunks c ON c.chunk_id = e.chunk_id
				WHERE dimensions = ? AND length(embedding) = ?
			`, table), dims, dims*4); err != nil {
				return nil, err
			}
		}
	}

	if err := dropTriggersByPrefixTx(ctx, tx, "trg_"+intelVecTablePrefix); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `ALTER TABLE intel_embeddings RENAME TO intel_embeddings_legacy`); err != nil {
		return nil, err
	}
	if hasChunkRowID {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE intel_embeddings (
			chunk_row_id INTEGER PRIMARY KEY,
			chunk_id TEXT NOT NULL UNIQUE CHECK (chunk_id != ''),
			norm REAL NOT NULL DEFAULT 0,
			dimensions INTEGER NOT NULL CHECK (dimensions > 0),
			created_at INTEGER NOT NULL CHECK (created_at >= 0),
			FOREIGN KEY(chunk_row_id) REFERENCES intel_chunks(id) ON DELETE CASCADE
		) STRICT;`); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO intel_embeddings (chunk_row_id, chunk_id, norm, dimensions, created_at)
			SELECT c.id, e.chunk_id, e.norm, e.dimensions, e.created_at
			FROM intel_embeddings_legacy e
			JOIN intel_chunks c ON c.chunk_id = e.chunk_id
		`); err != nil {
			return nil, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE intel_embeddings (
			chunk_id TEXT PRIMARY KEY CHECK (chunk_id != ''),
			norm REAL NOT NULL DEFAULT 0,
			dimensions INTEGER NOT NULL CHECK (dimensions > 0),
			created_at INTEGER NOT NULL CHECK (created_at >= 0),
			FOREIGN KEY(chunk_id) REFERENCES intel_chunks(chunk_id) ON DELETE CASCADE
		) WITHOUT ROWID, STRICT;`); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO intel_embeddings (chunk_id, norm, dimensions, created_at)
			SELECT chunk_id, norm, dimensions, created_at
			FROM intel_embeddings_legacy
		`); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE intel_embeddings_legacy`); err != nil {
		return nil, err
	}
	return dimsList, nil
}
