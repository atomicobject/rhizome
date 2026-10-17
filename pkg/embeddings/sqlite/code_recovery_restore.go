package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

func (s *CodeStore) restoreCodeTx(ctx context.Context, tx *sql.Tx, b codeRecoveryTables) error {
	if err := s.resetDomainOn(ctx, tx, true); err != nil {
		return err
	}
	if err := s.ensureSchemaNoLock(ctx, tx); err != nil {
		return err
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS rzm_migration_state (
				domain TEXT PRIMARY KEY,
				version INTEGER NOT NULL,
				dirty INTEGER NOT NULL DEFAULT 0,
				updated_at INTEGER NOT NULL
			);`,
		`CREATE TABLE IF NOT EXISTS rzm_migration_log (
				id INTEGER PRIMARY KEY,
				domain TEXT NOT NULL,
				from_version INTEGER NOT NULL,
				to_version INTEGER NOT NULL,
				status TEXT NOT NULL,
				started_at INTEGER NOT NULL,
				finished_at INTEGER,
				error TEXT
			);`,
		`CREATE INDEX IF NOT EXISTS idx_rzm_migration_log_domain_started ON rzm_migration_log(domain, started_at DESC);`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	hasAnchorStartByte, err := tableHasColumn(ctx, tx, "intel_code_anchors", "start_byte")
	if err != nil {
		return err
	}
	hasAnchorFingerprint, err := tableHasColumn(ctx, tx, "intel_code_anchors", "fingerprint")
	if err != nil {
		return err
	}
	hasAnchorUpdatedAt, err := tableHasColumn(ctx, tx, "intel_code_anchors", "updated_at")
	if err != nil {
		return err
	}
	anchorRestoreCols := []string{"anchor_id", "lang", "kind", "path", "symbol", "fqn"}
	anchorRestoreSelect := []string{
		"b.anchor_id",
		"COALESCE(NULLIF(b.lang, ''), 'unknown')",
		"COALESCE(NULLIF(b.kind, ''), 'unknown')",
		"b.path",
		"COALESCE(NULLIF(b.symbol, ''), b.anchor_id)",
		"COALESCE(b.fqn, '')",
	}
	anchorRestoreUpdates := []string{
		"lang = excluded.lang",
		"kind = excluded.kind",
		"path = excluded.path",
		"symbol = excluded.symbol",
		"fqn = excluded.fqn",
	}
	if hasAnchorStartByte {
		anchorRestoreCols = append(anchorRestoreCols, "start_byte", "end_byte", "start_line", "end_line")
		anchorRestoreSelect = append(anchorRestoreSelect, "0", "0", "0", "0")
	}
	if hasAnchorFingerprint {
		anchorRestoreCols = append(anchorRestoreCols, "fingerprint")
		anchorRestoreSelect = append(anchorRestoreSelect, "'codeemb'")
	}
	if hasAnchorUpdatedAt {
		anchorRestoreCols = append(anchorRestoreCols, "updated_at")
		anchorRestoreSelect = append(anchorRestoreSelect, "0")
	}
	anchorRestoreQuery := fmt.Sprintf(`
		INSERT INTO intel_code_anchors (%s)
		SELECT %s
		FROM %s b
		WHERE b.anchor_id IS NOT NULL AND b.anchor_id != ''
		  AND b.path IS NOT NULL AND b.path != ''
		ON CONFLICT(anchor_id) DO UPDATE SET
			%s
	`, strings.Join(anchorRestoreCols, ", "), strings.Join(anchorRestoreSelect, ", "), b.items, strings.Join(anchorRestoreUpdates, ",\n\t\t\t"))
	if _, err := tx.ExecContext(ctx, anchorRestoreQuery); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO `+tableItems+` (anchor_row_id, fingerprint, updated_at, sync_generation)
		SELECT a.id, COALESCE(b.fingerprint, ''), COALESCE(b.updated_at, 0), COALESCE(b.sync_generation, 0)
		FROM `+b.items+` b
		JOIN intel_code_anchors a ON a.anchor_id = b.anchor_id
		WHERE b.anchor_id IS NOT NULL AND b.anchor_id != ''
		  AND b.path IS NOT NULL AND b.path != ''
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
	INSERT OR REPLACE INTO `+tableItemEmbeddings+` (item_row_id, content_hash, embedding, norm, dimensions, created_at)
		SELECT i.id, b.content_hash, b.embedding, COALESCE(b.norm, 0), b.dimensions, b.created_at
		FROM `+b.itemE+` b
		JOIN intel_code_anchors a ON a.anchor_id = b.anchor_id
		JOIN `+tableItems+` i ON i.anchor_row_id = a.id
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO `+codeTableChunkEmbeddings+` (
			item_row_id, chunk_index, granularity, breadcrumb, heading, start_byte, end_byte, start_line, end_line, content_hash, embedding, norm, dimensions, created_at
		)
		SELECT i.id, b.chunk_index, b.granularity, b.breadcrumb, b.heading, b.start_byte, b.end_byte, b.start_line, b.end_line, b.content_hash, b.embedding, COALESCE(b.norm, 0), b.dimensions, b.created_at
		FROM `+b.chunk+` b
		JOIN intel_code_anchors a ON a.anchor_id = b.anchor_id
		JOIN `+tableItems+` i ON i.anchor_row_id = a.id
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO `+codeTableEmbeddingCache+` (content_hash, embedding, norm, dimensions, created_at)
		SELECT content_hash, embedding, COALESCE(norm, 0), dimensions, created_at
		FROM `+b.cache+`
	`); err != nil {
		return err
	}
	if err := verifyPreservedRows(ctx, tx, b.cache, codeTableEmbeddingCache); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO `+codeTableEmbeddingCache+` (content_hash, embedding, norm, dimensions, created_at)
		SELECT content_hash, embedding, norm, dimensions, created_at
		FROM (
			SELECT content_hash, embedding, COALESCE(norm, 0) AS norm, dimensions, created_at FROM `+tableItemEmbeddings+`
			UNION ALL
			SELECT content_hash, embedding, COALESCE(norm, 0) AS norm, dimensions, created_at FROM `+codeTableChunkEmbeddings+`
		)
		WHERE content_hash IS NOT NULL AND content_hash != ''
	`); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO `+codeTableIndexMeta+` (id, provider, model, dimensions, schema_version, created_at, fingerprint_version, last_sync, source_high_water, sync_generation, visible_generation)
		SELECT 1, provider, model, dimensions, ?, COALESCE(created_at, 0), COALESCE(fingerprint_version, 0), COALESCE(last_sync, 0), COALESCE(source_high_water, 0), COALESCE(sync_generation, 0), COALESCE(visible_generation, sync_generation, 0)
		FROM `+b.meta+`
		LIMIT 1
	`, codeSchemaVersion); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO rzm_migration_state(domain, version, dirty, updated_at)
		VALUES (?, ?, 0, ?)
		ON CONFLICT(domain) DO UPDATE SET
			version = excluded.version,
			dirty = 0,
			updated_at = excluded.updated_at
	`, string(migration.DomainCodeEmbeddings), codeSchemaVersion, time.Now().Unix()); err != nil {
		return err
	}
	if err := verifyPreservedRows(ctx, tx, b.meta, codeTableIndexMeta); err != nil {
		return err
	}

	if err := verifyPreservedRows(ctx, tx, b.items, tableItems); err != nil {
		return err
	}

	if err := verifyPreservedRows(ctx, tx, b.itemE, tableItemEmbeddings); err != nil {
		return err
	}

	if err := verifyPreservedRows(ctx, tx, b.chunk, codeTableChunkEmbeddings); err != nil {
		return err
	}

	for _, name := range []string{b.meta, b.items, b.itemE, b.chunk, b.cache} {
		if _, err := tx.ExecContext(ctx, `DROP TABLE temp.`+name); err != nil {
			return err
		}
	}

	return nil
}
