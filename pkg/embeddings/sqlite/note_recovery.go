package sqlite

import (
	"context"
	"database/sql"
)

func (s *NoteStore) recoverNoteTx(ctx context.Context, tx *sql.Tx) error {
	hasChunkNorm, err := tableHasColumn(ctx, tx, tableChunkEmbeddings, "norm")
	if err != nil {
		return err
	}

	type backupTables struct {
		meta   string
		notes  string
		chunkE string
		cache  string
	}
	b := backupTables{
		meta:   "temp_emb_backup_meta",
		notes:  "temp_emb_backup_notes",
		chunkE: "temp_emb_backup_chunk_embeddings",
		cache:  "temp_emb_backup_embedding_cache",
	}

	// Keep temporary backups on this transaction's connection.
	for _, name := range []string{b.meta, b.notes, b.chunkE, b.cache} {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp.`+name); err != nil {
			return err
		}
	}

	// Create temp backup tables.
	stmts := []string{
		`CREATE TEMP TABLE ` + b.meta + ` (
				provider TEXT,
				model TEXT,
				dimensions INTEGER,
			schema_version INTEGER,
			created_at INTEGER,
			last_sync INTEGER,
			source_high_water INTEGER,
			sync_generation INTEGER,
			visible_generation INTEGER
		);`,
		`CREATE TEMP TABLE ` + b.notes + ` (
				note_id TEXT,
				title TEXT,
				path TEXT,
				last_seen_mtime INTEGER,
				last_seen_size INTEGER,
				sync_generation INTEGER
			);`,
		`CREATE TEMP TABLE ` + b.chunkE + ` (
				note_id TEXT,
				chunk_index INTEGER,
				breadcrumb TEXT,
				heading TEXT,
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

	// Check if sync_generation columns exist for graceful migration.
	hasMetaSyncGen, err := tableHasColumn(ctx, tx, tableIndexMeta, "sync_generation")
	if err != nil {
		return err
	}
	hasMetaSourceHighWater, err := tableHasColumn(ctx, tx, tableIndexMeta, "source_high_water")
	if err != nil {
		return err
	}
	hasMetaVisibleGen, err := tableHasColumn(ctx, tx, tableIndexMeta, "visible_generation")
	if err != nil {
		return err
	}
	hasNoteSyncGen, err := tableHasColumn(ctx, tx, tableNotes, "sync_generation")
	if err != nil {
		return err
	}

	hasMetaSchemaVersion, err := tableHasColumn(ctx, tx, tableIndexMeta, "schema_version")
	if err != nil {
		return err
	}
	hasMetaLastSync, err := tableHasColumn(ctx, tx, tableIndexMeta, "last_sync")
	if err != nil {
		return err
	}
	metaSchemaVersionSelect := "1"
	if hasMetaSchemaVersion {
		metaSchemaVersionSelect = "schema_version"
	}
	metaLastSyncSelect := "0"
	if hasMetaLastSync {
		metaLastSyncSelect = "last_sync"
	}

	// Missing tables contain no vectors; malformed existing tables fail closed.
	metaSyncGenSelect := "0"
	if hasMetaSyncGen {
		metaSyncGenSelect = "COALESCE(sync_generation, 0)"
	}
	metaSourceHighWaterSelect := "0"
	if hasMetaSourceHighWater {
		metaSourceHighWaterSelect = "COALESCE(source_high_water, 0)"
	}
	metaVisibleGenSelect := metaSyncGenSelect
	if hasMetaVisibleGen {
		metaVisibleGenSelect = "COALESCE(visible_generation, 0)"
	}
	{
		exists, err := tableExists(ctx, tx, tableIndexMeta)
		if err != nil {
			return err
		}
		if exists {
			if _, err := tx.ExecContext(ctx, `
			INSERT INTO `+b.meta+` (provider, model, dimensions, schema_version, created_at, last_sync, source_high_water, sync_generation, visible_generation)
			SELECT provider, model, dimensions, `+metaSchemaVersionSelect+`, created_at, `+metaLastSyncSelect+`, `+metaSourceHighWaterSelect+`, `+metaSyncGenSelect+`, `+metaVisibleGenSelect+`
			FROM `+tableIndexMeta+`
			WHERE id = 1
		`); err != nil {
				return err
			}
			if err := verifyPreservedRows(ctx, tx, tableIndexMeta, b.meta); err != nil {
				return err
			}
		}
	}
	noteSyncGenSelect := "0"
	if hasNoteSyncGen {
		noteSyncGenSelect = "COALESCE(sync_generation, 0)"
	}
	{
		exists, err := tableExists(ctx, tx, tableNotes)
		if err != nil {
			return err
		}
		if exists {
			if _, err := tx.ExecContext(ctx, `
			INSERT INTO `+b.notes+` (note_id, title, path, last_seen_mtime, last_seen_size, sync_generation)
			SELECT note_id, title, path, last_seen_mtime, last_seen_size, `+noteSyncGenSelect+`
			FROM `+tableNotes+`
		`); err != nil {
				return err
			}
			if err := verifyPreservedRows(ctx, tx, tableNotes, b.notes); err != nil {
				return err
			}
		}
	}
	chunkNormSelect := "0"
	if hasChunkNorm {
		chunkNormSelect = "COALESCE(c.norm, 0)"
	}
	{
		exists, err := tableExists(ctx, tx, tableChunkEmbeddings)
		if err != nil {
			return err
		}
		if exists {
			if _, err := tx.ExecContext(ctx, `
			INSERT INTO `+b.chunkE+` (note_id, chunk_index, breadcrumb, heading, content_hash, embedding, norm, dimensions, created_at)
			SELECT n.note_id, c.chunk_index, c.breadcrumb, c.heading, c.content_hash, c.embedding, `+chunkNormSelect+`, c.dimensions, c.created_at
			FROM `+tableChunkEmbeddings+` c
			JOIN `+tableNotes+` n ON n.id = c.note_row_id
		`); err != nil {
				return err
			}
			if err := verifyPreservedRows(ctx, tx, tableChunkEmbeddings, b.chunkE); err != nil {
				return err
			}
		}
	}
	// Check if cache has norm column for graceful migration.
	hasCacheNorm, err := tableHasColumn(ctx, tx, tableEmbeddingCache, "norm")
	if err != nil {
		return err
	}
	cacheNormSelect := "0"
	if hasCacheNorm {
		cacheNormSelect = "COALESCE(norm, 0)"
	}
	{
		exists, err := tableExists(ctx, tx, tableEmbeddingCache)
		if err != nil {
			return err
		}
		if exists {
			if _, err := tx.ExecContext(ctx, `
			INSERT INTO `+b.cache+` (content_hash, embedding, norm, dimensions, created_at)
			SELECT content_hash, embedding, `+cacheNormSelect+`, dimensions, created_at
			FROM `+tableEmbeddingCache+`
		`); err != nil {
				return err
			}
			if err := verifyPreservedRows(ctx, tx, tableEmbeddingCache, b.cache); err != nil {
				return err
			}
		}
	}

	// Rebuild domain tables.
	if err := s.resetDomainOn(ctx, tx, true); err != nil {
		return err
	}
	if err := s.ensureSchemaNoLock(ctx, tx); err != nil {
		return err
	}

	// Restore notes first (row ids may change).
	if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO `+tableNotes+` (note_id, title, path, last_seen_mtime, last_seen_size, sync_generation)
			SELECT note_id, COALESCE(title, ''), COALESCE(path, ''), COALESCE(last_seen_mtime, 0), COALESCE(last_seen_size, 0), COALESCE(sync_generation, 0)
			FROM `+b.notes+`
		`); err != nil {
		return err
	}
	// Restore chunk embeddings.
	if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO `+tableChunkEmbeddings+` (note_row_id, chunk_index, breadcrumb, heading, content_hash, embedding, norm, dimensions, created_at)
			SELECT n.id, b.chunk_index, b.breadcrumb, b.heading, b.content_hash, b.embedding, COALESCE(b.norm, 0), b.dimensions, b.created_at
			FROM `+b.chunkE+` b
			JOIN `+tableNotes+` n ON n.note_id = b.note_id
		`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO `+tableEmbeddingCache+` (content_hash, embedding, norm, dimensions, created_at)
			SELECT content_hash, embedding, COALESCE(norm, 0), dimensions, created_at
			FROM `+b.cache+`
		`); err != nil {
		return err
	}
	if err := verifyPreservedRows(ctx, tx, b.cache, tableEmbeddingCache); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO `+tableEmbeddingCache+` (content_hash, embedding, norm, dimensions, created_at)
			SELECT content_hash, embedding, COALESCE(norm, 0), dimensions, created_at
			FROM `+tableChunkEmbeddings+`
			WHERE content_hash IS NOT NULL AND content_hash != ''
		`); err != nil {
		return err
	}

	// Restore metadata (clamp schema version upward).
	if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO `+tableIndexMeta+` (id, provider, model, dimensions, schema_version, created_at, last_sync, source_high_water, sync_generation, visible_generation)
			SELECT 1, provider, model, dimensions, ?, COALESCE(created_at, 0), COALESCE(last_sync, 0), COALESCE(source_high_water, 0), COALESCE(sync_generation, 0), COALESCE(visible_generation, sync_generation, 0)
			FROM `+b.meta+`
			LIMIT 1
		`, noteSchemaVersion); err != nil {
		return err
	}

	if err := verifyPreservedRows(ctx, tx, b.meta, tableIndexMeta); err != nil {
		return err
	}

	if err := verifyPreservedRows(ctx, tx, b.notes, tableNotes); err != nil {
		return err
	}

	if err := verifyPreservedRows(ctx, tx, b.chunkE, tableChunkEmbeddings); err != nil {
		return err
	}

	for _, name := range []string{b.meta, b.notes, b.chunkE, b.cache} {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp.`+name); err != nil {
			return err
		}
	}
	return nil
}
