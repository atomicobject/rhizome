package sqlite

import (
	"context"
	"database/sql"
)

type codeRecoveryTables struct {
	meta  string
	items string
	itemE string
	chunk string
	cache string
}

func (s *CodeStore) recoverCodeTx(ctx context.Context, tx *sql.Tx) error {
	b := codeRecoveryTables{
		meta:  "temp_code_backup_meta",
		items: "temp_code_backup_items",
		itemE: "temp_code_backup_item_embeddings",
		chunk: "temp_code_backup_chunk_embeddings",
		cache: "temp_code_backup_embedding_cache",
	}

	for _, name := range []string{b.meta, b.items, b.itemE, b.chunk, b.cache} {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp.`+name); err != nil {
			return err
		}
	}

	stmts := []string{
		`CREATE TEMP TABLE ` + b.meta + ` (
			provider TEXT,
			model TEXT,
			dimensions INTEGER,
			schema_version INTEGER,
			created_at INTEGER,
			fingerprint_version INTEGER,
			last_sync INTEGER,
			source_high_water INTEGER,
			sync_generation INTEGER,
			visible_generation INTEGER
		);`,
		`CREATE TEMP TABLE ` + b.items + ` (
			anchor_id TEXT,
			lang TEXT,
			kind TEXT,
			path TEXT,
			symbol TEXT,
			fqn TEXT,
			fingerprint TEXT,
			updated_at INTEGER,
			sync_generation INTEGER
		);`,
		`CREATE TEMP TABLE ` + b.itemE + ` (
			anchor_id TEXT,
			content_hash TEXT,
			embedding BLOB,
			norm REAL,
			dimensions INTEGER,
			created_at INTEGER
		);`,
		`CREATE TEMP TABLE ` + b.chunk + ` (
			anchor_id TEXT,
			chunk_index INTEGER,
			granularity TEXT,
			breadcrumb TEXT,
			heading TEXT,
			start_byte INTEGER,
			end_byte INTEGER,
			start_line INTEGER,
			end_line INTEGER,
			content_hash TEXT,
			embedding BLOB,
			norm REAL,
			dimensions INTEGER,
			created_at INTEGER
		);`,
		`CREATE TEMP TABLE ` + b.cache + ` (
			content_hash TEXT,
			embedding BLOB,
			norm REAL,
			dimensions INTEGER,
			created_at INTEGER
		);`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	if err := s.backupCodeTx(ctx, tx, b); err != nil {
		return err
	}
	return s.restoreCodeTx(ctx, tx, b)
}
