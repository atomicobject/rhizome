package sqlite

import (
	"context"
	"database/sql"
)

func (s *CodeStore) backupCodeTx(ctx context.Context, tx *sql.Tx, b codeRecoveryTables) error {
	hasMetaTable, err := tableExists(ctx, tx, codeTableIndexMeta)
	if err != nil {
		return err
	}
	hasItemsTable, err := tableExists(ctx, tx, tableItems)
	if err != nil {
		return err
	}
	hasItemEmbeddingsTable, err := tableExists(ctx, tx, tableItemEmbeddings)
	if err != nil {
		return err
	}
	hasChunkEmbeddingsTable, err := tableExists(ctx, tx, codeTableChunkEmbeddings)
	if err != nil {
		return err
	}
	hasEmbeddingCacheTable, err := tableExists(ctx, tx, codeTableEmbeddingCache)
	if err != nil {
		return err
	}
	hasItemNorm, err := tableHasColumn(ctx, tx, tableItemEmbeddings, "norm")
	if err != nil {
		return err
	}

	hasChunkNorm, err := tableHasColumn(ctx, tx, codeTableChunkEmbeddings, "norm")
	if err != nil {
		return err
	}

	hasCacheNorm, err := tableHasColumn(ctx, tx, codeTableEmbeddingCache, "norm")
	if err != nil {
		return err
	}

	// Check if optional columns exist for graceful migration from older codeemb schemas.
	hasMetaFingerprintVersion, err := tableHasColumn(ctx, tx, codeTableIndexMeta, "fingerprint_version")
	if err != nil {
		return err
	}
	hasMetaSyncGen, err := tableHasColumn(ctx, tx, codeTableIndexMeta, "sync_generation")
	if err != nil {
		return err
	}
	hasMetaSourceHighWater, err := tableHasColumn(ctx, tx, codeTableIndexMeta, "source_high_water")
	if err != nil {
		return err
	}
	hasMetaVisibleGen, err := tableHasColumn(ctx, tx, codeTableIndexMeta, "visible_generation")
	if err != nil {
		return err
	}
	hasItemSyncGen, err := tableHasColumn(ctx, tx, tableItems, "sync_generation")
	if err != nil {
		return err
	}
	hasAnchorRowID, err := tableHasColumn(ctx, tx, tableItems, "anchor_row_id")
	if err != nil {
		return err
	}
	hasItemAnchorID, err := tableHasColumn(ctx, tx, tableItems, "anchor_id")
	if err != nil {
		return err
	}
	hasItemLang, err := tableHasColumn(ctx, tx, tableItems, "lang")
	if err != nil {
		return err
	}
	hasItemKind, err := tableHasColumn(ctx, tx, tableItems, "kind")
	if err != nil {
		return err
	}
	hasItemPath, err := tableHasColumn(ctx, tx, tableItems, "path")
	if err != nil {
		return err
	}
	hasItemSymbol, err := tableHasColumn(ctx, tx, tableItems, "symbol")
	if err != nil {
		return err
	}
	hasItemFQN, err := tableHasColumn(ctx, tx, tableItems, "fqn")
	if err != nil {
		return err
	}

	metaSyncGenSelect := "0"
	if hasMetaSyncGen {
		metaSyncGenSelect = "COALESCE(sync_generation, 0)"
	}
	metaFingerprintVersionSelect := "0"
	if hasMetaFingerprintVersion {
		metaFingerprintVersionSelect = "COALESCE(fingerprint_version, 0)"
	}
	metaSourceHighWaterSelect := "0"
	if hasMetaSourceHighWater {
		metaSourceHighWaterSelect = "COALESCE(source_high_water, 0)"
	}
	metaVisibleGenSelect := metaSyncGenSelect
	if hasMetaVisibleGen {
		metaVisibleGenSelect = "COALESCE(visible_generation, 0)"
	}
	if hasMetaTable {
		if _, err := tx.ExecContext(ctx, `
		INSERT INTO `+b.meta+` (provider, model, dimensions, schema_version, created_at, fingerprint_version, last_sync, source_high_water, sync_generation, visible_generation)
		SELECT provider, model, dimensions, schema_version, created_at, `+metaFingerprintVersionSelect+`, last_sync, `+metaSourceHighWaterSelect+`, `+metaSyncGenSelect+`, `+metaVisibleGenSelect+`
		FROM `+codeTableIndexMeta+`
		WHERE id = 1
	`); err != nil {
			return err
		}
	}
	itemSyncGenSelect := "0"
	if hasItemSyncGen {
		itemSyncGenSelect = "COALESCE(sync_generation, 0)"
	}
	itemBackupQuery := `
		INSERT INTO ` + b.items + ` (anchor_id, lang, kind, path, symbol, fqn, fingerprint, updated_at, sync_generation)
		SELECT i.anchor_id, i.lang, i.kind, i.path, i.symbol, i.fqn, i.fingerprint, i.updated_at, ` + itemSyncGenSelect + `
		FROM ` + tableItems + ` i
	`
	if hasAnchorRowID && hasItemAnchorID && hasItemLang && hasItemKind && hasItemPath && hasItemSymbol && hasItemFQN {
		itemBackupQuery = `
		INSERT INTO ` + b.items + ` (anchor_id, lang, kind, path, symbol, fqn, fingerprint, updated_at, sync_generation)
		SELECT COALESCE(a.anchor_id, i.anchor_id), COALESCE(a.lang, i.lang), COALESCE(a.kind, i.kind),
		       COALESCE(a.path, i.path), COALESCE(a.symbol, i.symbol), COALESCE(a.fqn, i.fqn),
		       i.fingerprint, i.updated_at, ` + itemSyncGenSelect + `
		FROM ` + tableItems + ` i
		LEFT JOIN intel_code_anchors a ON a.id = i.anchor_row_id
	`
	} else if hasAnchorRowID {
		itemBackupQuery = `
		INSERT INTO ` + b.items + ` (anchor_id, lang, kind, path, symbol, fqn, fingerprint, updated_at, sync_generation)
		SELECT a.anchor_id, a.lang, a.kind, a.path, a.symbol, a.fqn,
		       i.fingerprint, i.updated_at, ` + itemSyncGenSelect + `
		FROM ` + tableItems + ` i
		JOIN intel_code_anchors a ON a.id = i.anchor_row_id
	`
	}
	if hasItemsTable {
		if _, err := tx.ExecContext(ctx, itemBackupQuery); err != nil {
			return err
		}
	}
	itemNormSelect := "0"
	if hasItemNorm {
		itemNormSelect = "COALESCE(e.norm, 0)"
	}
	itemEmbBackupQuery := `
		INSERT INTO ` + b.itemE + ` (anchor_id, content_hash, embedding, norm, dimensions, created_at)
		SELECT i.anchor_id, e.content_hash, e.embedding, ` + itemNormSelect + `, e.dimensions, e.created_at
		FROM ` + tableItemEmbeddings + ` e
		JOIN ` + tableItems + ` i ON i.id = e.item_row_id
	`
	if hasAnchorRowID && hasItemAnchorID {
		itemEmbBackupQuery = `
		INSERT INTO ` + b.itemE + ` (anchor_id, content_hash, embedding, norm, dimensions, created_at)
		SELECT COALESCE(a.anchor_id, i.anchor_id), e.content_hash, e.embedding, ` + itemNormSelect + `, e.dimensions, e.created_at
		FROM ` + tableItemEmbeddings + ` e
		JOIN ` + tableItems + ` i ON i.id = e.item_row_id
		LEFT JOIN intel_code_anchors a ON a.id = i.anchor_row_id
	`
	} else if hasAnchorRowID {
		itemEmbBackupQuery = `
		INSERT INTO ` + b.itemE + ` (anchor_id, content_hash, embedding, norm, dimensions, created_at)
		SELECT a.anchor_id, e.content_hash, e.embedding, ` + itemNormSelect + `, e.dimensions, e.created_at
		FROM ` + tableItemEmbeddings + ` e
		JOIN ` + tableItems + ` i ON i.id = e.item_row_id
		JOIN intel_code_anchors a ON a.id = i.anchor_row_id
	`
	}
	if hasItemsTable && hasItemEmbeddingsTable {
		if _, err := tx.ExecContext(ctx, itemEmbBackupQuery); err != nil {
			return err
		}
	}
	chunkNormSelect := "0"
	if hasChunkNorm {
		chunkNormSelect = "COALESCE(c.norm, 0)"
	}
	chunkBackupQuery := `
		INSERT INTO ` + b.chunk + ` (anchor_id, chunk_index, granularity, breadcrumb, heading, start_byte, end_byte, start_line, end_line, content_hash, embedding, norm, dimensions, created_at)
		SELECT i.anchor_id, c.chunk_index, c.granularity, c.breadcrumb, c.heading, c.start_byte, c.end_byte, c.start_line, c.end_line, c.content_hash, c.embedding, ` + chunkNormSelect + `, c.dimensions, c.created_at
		FROM ` + codeTableChunkEmbeddings + ` c
		JOIN ` + tableItems + ` i ON i.id = c.item_row_id
	`
	if hasAnchorRowID && hasItemAnchorID {
		chunkBackupQuery = `
		INSERT INTO ` + b.chunk + ` (anchor_id, chunk_index, granularity, breadcrumb, heading, start_byte, end_byte, start_line, end_line, content_hash, embedding, norm, dimensions, created_at)
		SELECT COALESCE(a.anchor_id, i.anchor_id), c.chunk_index, c.granularity, c.breadcrumb, c.heading, c.start_byte, c.end_byte, c.start_line, c.end_line, c.content_hash, c.embedding, ` + chunkNormSelect + `, c.dimensions, c.created_at
		FROM ` + codeTableChunkEmbeddings + ` c
		JOIN ` + tableItems + ` i ON i.id = c.item_row_id
		LEFT JOIN intel_code_anchors a ON a.id = i.anchor_row_id
	`
	} else if hasAnchorRowID {
		chunkBackupQuery = `
		INSERT INTO ` + b.chunk + ` (anchor_id, chunk_index, granularity, breadcrumb, heading, start_byte, end_byte, start_line, end_line, content_hash, embedding, norm, dimensions, created_at)
		SELECT a.anchor_id, c.chunk_index, c.granularity, c.breadcrumb, c.heading, c.start_byte, c.end_byte, c.start_line, c.end_line, c.content_hash, c.embedding, ` + chunkNormSelect + `, c.dimensions, c.created_at
		FROM ` + codeTableChunkEmbeddings + ` c
		JOIN ` + tableItems + ` i ON i.id = c.item_row_id
		JOIN intel_code_anchors a ON a.id = i.anchor_row_id
	`
	}
	if hasItemsTable && hasChunkEmbeddingsTable {
		if _, err := tx.ExecContext(ctx, chunkBackupQuery); err != nil {
			return err
		}
	}
	cacheNormSelect := "0"
	if hasCacheNorm {
		cacheNormSelect = "COALESCE(norm, 0)"
	}
	if hasEmbeddingCacheTable {
		if _, err := tx.ExecContext(ctx, `
		INSERT INTO `+b.cache+` (content_hash, embedding, norm, dimensions, created_at)
		SELECT content_hash, embedding, `+cacheNormSelect+`, dimensions, created_at
		FROM `+codeTableEmbeddingCache+`
	`); err != nil {
			return err
		}
	}

	if hasMetaTable {
		if err := verifyPreservedRows(ctx, tx, codeTableIndexMeta, b.meta); err != nil {
			return err
		}
	}

	if hasItemsTable {
		if err := verifyPreservedRows(ctx, tx, tableItems, b.items); err != nil {
			return err
		}
	}

	if hasItemEmbeddingsTable {
		if err := verifyPreservedRows(ctx, tx, tableItemEmbeddings, b.itemE); err != nil {
			return err
		}
	}

	if hasChunkEmbeddingsTable {
		if err := verifyPreservedRows(ctx, tx, codeTableChunkEmbeddings, b.chunk); err != nil {
			return err
		}
	}

	if hasEmbeddingCacheTable {
		if err := verifyPreservedRows(ctx, tx, codeTableEmbeddingCache, b.cache); err != nil {
			return err
		}
	}

	return nil
}
