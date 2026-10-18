package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration/domains"
)

func (s *CodeStore) ensureDomainMigrations(ctx context.Context) error {
	plan := domains.CodeEmbeddingsPlan(codeSchemaVersion, s.migrateDomainStepTx, nil)
	if err := migration.EnsureDomain(ctx, s.db, plan, migration.EnsureOptions{}); err != nil {
		return err
	}
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return sqliteutil.ExecTxWithRetry(ctx, db, func(tx *sql.Tx) error {
			if err := addColumnIfMissingTx(ctx, tx, codeTableIndexMeta, "source_high_water", "INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
			if err := addColumnIfMissingTx(ctx, tx, codeTableIndexMeta, "visible_generation", "INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `UPDATE `+codeTableIndexMeta+` SET visible_generation = sync_generation WHERE visible_generation = 0 AND sync_generation > 0`)
			return err
		})
	})
}

func (s *CodeStore) migrateDomainStepTx(ctx context.Context, tx *sql.Tx, from, to int) error {
	switch to {
	case 1:
		// Initial schema already created by EnsureSchema.
	case 2:
		if err := s.migrateBodyChunkIndicesTx(ctx, tx); err != nil {
			return err
		}
	case 3:
		if err := s.migrateChunkIndexUniquenessTx(ctx, tx); err != nil {
			return err
		}
	case 4:
		if _, err := tx.ExecContext(ctx, `ALTER TABLE `+codeTableIndexMeta+` ADD COLUMN fingerprint_version INTEGER NOT NULL DEFAULT 0`); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
				return err
			}
		}
	case 5:
		if err := s.migrateEmbeddingCacheTx(ctx, tx); err != nil {
			return err
		}
	case 6:
		if err := s.migrateAddNormColumnsTx(ctx, tx); err != nil {
			return err
		}
	case 7:
		if err := s.migrateAddSyncGenerationTx(ctx, tx); err != nil {
			return err
		}
	case 8:
		if err := s.migrateNormalizeEmbeddingStorageTx(ctx, tx); err != nil {
			return err
		}
	case 9:
		if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS `+`idx_code_chunk_embeddings_dimensions`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS `+`idx_code_item_embeddings_dimensions`); err != nil {
			return err
		}
	case 10:
		stmts := []string{
			`DROP TABLE IF EXISTS ` + tableChunkFTS + `;`,
			`DROP TABLE IF EXISTS ` + codeTableChunkEmbeddings + `;`,
			`DROP TABLE IF EXISTS ` + tableItemEmbeddings + `;`,
			`DROP TABLE IF EXISTS ` + tableItems + `;`,
			`CREATE TABLE IF NOT EXISTS ` + tableItems + ` (
				id INTEGER PRIMARY KEY,
				anchor_row_id INTEGER UNIQUE REFERENCES intel_code_anchors(id) ON DELETE CASCADE,
				anchor_id TEXT NOT NULL UNIQUE,
				lang TEXT,
				kind TEXT,
				path TEXT NOT NULL,
				symbol TEXT,
				fqn TEXT,
				fingerprint TEXT,
				updated_at INTEGER NOT NULL,
				sync_generation INTEGER NOT NULL DEFAULT 0
			);`,
			`CREATE TABLE IF NOT EXISTS ` + tableItemEmbeddings + ` (
				id INTEGER PRIMARY KEY,
				item_row_id INTEGER NOT NULL REFERENCES ` + tableItems + `(id) ON DELETE CASCADE,
				content_hash TEXT NOT NULL,
				embedding BLOB NOT NULL,
				norm REAL NOT NULL DEFAULT 0,
				dimensions INTEGER NOT NULL,
				created_at INTEGER NOT NULL,
				UNIQUE(item_row_id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_code_item_embeddings_item_row_id ON ` + tableItemEmbeddings + `(item_row_id);`,
			`CREATE TABLE IF NOT EXISTS ` + codeTableChunkEmbeddings + ` (
				id INTEGER PRIMARY KEY,
				item_row_id INTEGER NOT NULL REFERENCES ` + tableItems + `(id) ON DELETE CASCADE,
				chunk_index INTEGER NOT NULL,
				granularity TEXT NOT NULL,
				breadcrumb TEXT,
				heading TEXT,
				start_byte INTEGER,
				end_byte INTEGER,
				start_line INTEGER,
				end_line INTEGER,
				content_hash TEXT NOT NULL,
				embedding BLOB NOT NULL,
				norm REAL NOT NULL DEFAULT 0,
				dimensions INTEGER NOT NULL,
				created_at INTEGER NOT NULL,
				UNIQUE(item_row_id, granularity, chunk_index)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_code_chunk_embeddings_item_row_id ON ` + codeTableChunkEmbeddings + `(item_row_id);`,
			`CREATE INDEX IF NOT EXISTS idx_code_chunk_embeddings_granularity_lower ON ` + codeTableChunkEmbeddings + `(lower(granularity));`,
			`CREATE INDEX IF NOT EXISTS idx_code_items_anchor_row_id ON ` + tableItems + `(anchor_row_id);`,
			`CREATE VIRTUAL TABLE IF NOT EXISTS ` + tableChunkFTS + ` USING fts5(
				anchor_id,
				chunk_index UNINDEXED,
				path,
				symbol,
				fqn,
				kind,
				granularity,
				breadcrumb,
				heading,
				body
			);`,
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
	case 11:
		stmts := []string{
			`DROP TABLE IF EXISTS ` + tableChunkFTS + `;`,
			`DROP TABLE IF EXISTS ` + codeTableChunkEmbeddings + `;`,
			`DROP TABLE IF EXISTS ` + tableItemEmbeddings + `;`,
			`DROP TABLE IF EXISTS ` + tableItems + `;`,
			`DROP INDEX IF EXISTS idx_code_items_path;`,
			`DROP INDEX IF EXISTS idx_code_items_kind_lower;`,
			`CREATE INDEX IF NOT EXISTS idx_codeemb_intel_code_anchors_kind_lower ON intel_code_anchors(lower(kind));`,
			`CREATE TABLE IF NOT EXISTS ` + tableItems + ` (
				id INTEGER PRIMARY KEY,
				anchor_row_id INTEGER NOT NULL UNIQUE REFERENCES intel_code_anchors(id) ON DELETE CASCADE,
				fingerprint TEXT,
				updated_at INTEGER NOT NULL,
				sync_generation INTEGER NOT NULL DEFAULT 0
			);`,
			`CREATE TABLE IF NOT EXISTS ` + tableItemEmbeddings + ` (
				id INTEGER PRIMARY KEY,
				item_row_id INTEGER NOT NULL REFERENCES ` + tableItems + `(id) ON DELETE CASCADE,
				content_hash TEXT NOT NULL,
				embedding BLOB NOT NULL,
				norm REAL NOT NULL DEFAULT 0,
				dimensions INTEGER NOT NULL,
				created_at INTEGER NOT NULL,
				UNIQUE(item_row_id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_code_item_embeddings_item_row_id ON ` + tableItemEmbeddings + `(item_row_id);`,
			`CREATE TABLE IF NOT EXISTS ` + codeTableChunkEmbeddings + ` (
				id INTEGER PRIMARY KEY,
				item_row_id INTEGER NOT NULL REFERENCES ` + tableItems + `(id) ON DELETE CASCADE,
				chunk_index INTEGER NOT NULL,
				granularity TEXT NOT NULL,
				breadcrumb TEXT,
				heading TEXT,
				start_byte INTEGER,
				end_byte INTEGER,
				start_line INTEGER,
				end_line INTEGER,
				content_hash TEXT NOT NULL,
				embedding BLOB NOT NULL,
				norm REAL NOT NULL DEFAULT 0,
				dimensions INTEGER NOT NULL,
				created_at INTEGER NOT NULL,
				UNIQUE(item_row_id, granularity, chunk_index)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_code_chunk_embeddings_item_row_id ON ` + codeTableChunkEmbeddings + `(item_row_id);`,
			`CREATE INDEX IF NOT EXISTS idx_code_chunk_embeddings_granularity_lower ON ` + codeTableChunkEmbeddings + `(lower(granularity));`,
			`CREATE INDEX IF NOT EXISTS idx_code_items_anchor_row_id ON ` + tableItems + `(anchor_row_id);`,
			`CREATE VIRTUAL TABLE IF NOT EXISTS ` + tableChunkFTS + ` USING fts5(
				anchor_id,
				chunk_index UNINDEXED,
				path,
				symbol,
				fqn,
				kind,
				granularity,
				breadcrumb,
				heading,
				body
			);`,
		}
		for _, stmt := range stmts {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
	case 12:
		if err := addColumnIfMissingTx(ctx, tx, codeTableIndexMeta, "source_high_water", "INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported code embeddings migration %d->%d", from, to)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE `+codeTableIndexMeta+` SET schema_version = ? WHERE id = 1`, to); err != nil {
		return err
	}
	return nil
}

func (s *CodeStore) migrateChunkIndexUniquenessTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM `+codeTableChunkEmbeddings+`
		WHERE id IN (
			SELECT id FROM (
				SELECT id,
				       ROW_NUMBER() OVER (
				           PARTITION BY item_row_id, chunk_index
				           ORDER BY created_at DESC, id DESC
				       ) AS rn
				FROM `+codeTableChunkEmbeddings+`
			)
			WHERE rn > 1
		)
	`)
	return err
}

func (s *CodeStore) migrateBodyChunkIndicesTx(ctx context.Context, tx *sql.Tx) error {
	type chunkRow struct {
		id       int64
		anchorID string
		index    int
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT c.id, a.anchor_id, c.chunk_index
		FROM `+codeTableChunkEmbeddings+` c
		JOIN `+tableItems+` i ON c.item_row_id = i.id
		JOIN intel_code_anchors a ON a.id = i.anchor_row_id
		WHERE c.granularity = 'body'
		ORDER BY a.anchor_id, c.chunk_index, c.id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var chunks []chunkRow
	for rows.Next() {
		var r chunkRow
		if err := rows.Scan(&r.id, &r.anchorID, &r.index); err != nil {
			return err
		}
		chunks = append(chunks, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(chunks) == 0 {
		return nil
	}

	const tempOffset = 1000000
	type update struct {
		id        int64
		anchorID  string
		oldIndex  int
		tempIndex int
		newIndex  int
	}

	var updates []update
	var currentAnchor string
	nextIndex := 1
	for _, ch := range chunks {
		if ch.anchorID != currentAnchor {
			currentAnchor = ch.anchorID
			nextIndex = 1
		}
		if ch.index != nextIndex {
			updates = append(updates, update{
				id:        ch.id,
				anchorID:  ch.anchorID,
				oldIndex:  ch.index,
				tempIndex: nextIndex + tempOffset,
				newIndex:  nextIndex,
			})
		}
		nextIndex++
	}
	if len(updates) == 0 {
		return nil
	}

	for _, u := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE `+codeTableChunkEmbeddings+` SET chunk_index = ? WHERE id = ?`, u.tempIndex, u.id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+tableChunkFTS+` SET chunk_index = ? WHERE anchor_id = ? AND granularity = 'body' AND chunk_index = ?`, u.tempIndex, u.anchorID, u.oldIndex); err != nil {
			return err
		}
	}
	for _, u := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE `+codeTableChunkEmbeddings+` SET chunk_index = ? WHERE id = ?`, u.newIndex, u.id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+tableChunkFTS+` SET chunk_index = ? WHERE anchor_id = ? AND granularity = 'body' AND chunk_index = ?`, u.newIndex, u.anchorID, u.tempIndex); err != nil {
			return err
		}
	}
	return nil
}

func (s *CodeStore) migrateEmbeddingCacheTx(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS `+codeTableEmbeddingCache+` (
			content_hash TEXT PRIMARY KEY,
			embedding    BLOB NOT NULL,
			dimensions   INTEGER NOT NULL,
			created_at   INTEGER NOT NULL
		)
	`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO `+codeTableEmbeddingCache+` (content_hash, embedding, dimensions, created_at)
		SELECT content_hash, embedding, dimensions, created_at
		FROM (
			SELECT content_hash, embedding, dimensions, created_at
			FROM `+tableItemEmbeddings+`
			WHERE content_hash IS NOT NULL AND content_hash != ''
			UNION ALL
			SELECT content_hash, embedding, dimensions, created_at
			FROM `+codeTableChunkEmbeddings+`
			WHERE content_hash IS NOT NULL AND content_hash != ''
		)
		WHERE content_hash IS NOT NULL AND content_hash != ''
		ON CONFLICT(content_hash) DO UPDATE SET
			embedding = excluded.embedding,
			dimensions = excluded.dimensions,
			created_at = MAX(`+codeTableEmbeddingCache+`.created_at, excluded.created_at)
	`)
	return err
}

func (s *CodeStore) migrateAddNormColumnsTx(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range []string{
		`ALTER TABLE ` + tableItemEmbeddings + ` ADD COLUMN norm REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE ` + codeTableChunkEmbeddings + ` ADD COLUMN norm REAL NOT NULL DEFAULT 0`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "duplicate column") {
				continue
			}
			return err
		}
	}
	return nil
}

func (s *CodeStore) migrateAddSyncGenerationTx(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range []string{
		`ALTER TABLE ` + codeTableIndexMeta + ` ADD COLUMN sync_generation INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE ` + tableItems + ` ADD COLUMN sync_generation INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			msg := strings.ToLower(err.Error())
			if strings.Contains(msg, "duplicate column") {
				continue
			}
			return err
		}
	}
	return nil
}

// migrateNormalizeEmbeddingStorage adds norm column to the embedding cache
// and ensures all embeddings are in the cache with their norm values.
// This enables queries to use the cache as the single source of truth.
func (s *CodeStore) migrateNormalizeEmbeddingStorageTx(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE `+codeTableEmbeddingCache+` ADD COLUMN norm REAL NOT NULL DEFAULT 0`); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "duplicate column") {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE `+codeTableEmbeddingCache+` SET norm = (
			SELECT COALESCE(e.norm, 0)
			FROM `+tableItemEmbeddings+` e
			WHERE e.content_hash = `+codeTableEmbeddingCache+`.content_hash
			LIMIT 1
		)
		WHERE norm = 0 AND EXISTS (
			SELECT 1 FROM `+tableItemEmbeddings+` e
			WHERE e.content_hash = `+codeTableEmbeddingCache+`.content_hash AND e.norm > 0
		)
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE `+codeTableEmbeddingCache+` SET norm = (
			SELECT COALESCE(c.norm, 0)
			FROM `+codeTableChunkEmbeddings+` c
			WHERE c.content_hash = `+codeTableEmbeddingCache+`.content_hash AND c.norm > 0
			LIMIT 1
		)
		WHERE norm = 0 AND EXISTS (
			SELECT 1 FROM `+codeTableChunkEmbeddings+` c
			WHERE c.content_hash = `+codeTableEmbeddingCache+`.content_hash AND c.norm > 0
		)
	`); err != nil {
		return err
	}
	return nil
}
