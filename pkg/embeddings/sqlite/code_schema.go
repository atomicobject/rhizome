package sqlite

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

// EnsureSchema creates tables and indices if needed.
func (s *CodeStore) ensureSchemaStatements() []string {
	return []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS ` + codeTableIndexMeta + ` (
                id INTEGER PRIMARY KEY CHECK (id = 1),
                provider TEXT,
                model TEXT,
                dimensions INTEGER,
                schema_version INTEGER NOT NULL DEFAULT 1,
                created_at INTEGER NOT NULL,
                fingerprint_version INTEGER NOT NULL DEFAULT 0,
                last_sync INTEGER,
                source_high_water INTEGER NOT NULL DEFAULT 0,
                sync_generation INTEGER NOT NULL DEFAULT 0,
                visible_generation INTEGER NOT NULL DEFAULT 0
        );`,
		`CREATE TABLE IF NOT EXISTS intel_code_anchors (
                id INTEGER PRIMARY KEY,
                anchor_id TEXT NOT NULL UNIQUE,
                lang TEXT,
                kind TEXT,
                path TEXT NOT NULL,
                symbol TEXT,
                fqn TEXT
        );`,
		`CREATE INDEX IF NOT EXISTS idx_codeemb_intel_code_anchors_path ON intel_code_anchors(path);`,
		`CREATE INDEX IF NOT EXISTS idx_codeemb_intel_code_anchors_kind_lower ON intel_code_anchors(lower(kind));`,
		`CREATE TABLE IF NOT EXISTS ` + tableItems + ` (
                id           INTEGER PRIMARY KEY,
                anchor_row_id INTEGER NOT NULL UNIQUE REFERENCES intel_code_anchors(id) ON DELETE CASCADE,
                fingerprint  TEXT,
                updated_at   INTEGER NOT NULL,
                sync_generation INTEGER NOT NULL DEFAULT 0
        );`,
		`CREATE TABLE IF NOT EXISTS ` + tableItemEmbeddings + ` (
                id            INTEGER PRIMARY KEY,
                item_row_id   INTEGER NOT NULL REFERENCES ` + tableItems + `(id) ON DELETE CASCADE,
                content_hash  TEXT NOT NULL,
                embedding     BLOB NOT NULL,
                norm          REAL NOT NULL DEFAULT 0,
                dimensions    INTEGER NOT NULL,
                created_at    INTEGER NOT NULL,
                UNIQUE(item_row_id)
        );`,
		`CREATE INDEX IF NOT EXISTS idx_code_item_embeddings_item_row_id ON ` + tableItemEmbeddings + `(item_row_id);`,
		`CREATE TABLE IF NOT EXISTS ` + codeTableChunkEmbeddings + ` (
                id           INTEGER PRIMARY KEY,
                item_row_id  INTEGER NOT NULL REFERENCES ` + tableItems + `(id) ON DELETE CASCADE,
                chunk_index  INTEGER NOT NULL,
                granularity  TEXT NOT NULL,
                breadcrumb   TEXT,
                heading      TEXT,
                start_byte   INTEGER,
                end_byte     INTEGER,
                start_line   INTEGER,
                end_line     INTEGER,
                content_hash TEXT NOT NULL,
                embedding    BLOB NOT NULL,
                norm         REAL NOT NULL DEFAULT 0,
                dimensions   INTEGER NOT NULL,
                created_at   INTEGER NOT NULL,
                UNIQUE(item_row_id, granularity, chunk_index)
        );`,
		`CREATE TABLE IF NOT EXISTS ` + codeTableEmbeddingCache + ` (
                content_hash TEXT PRIMARY KEY,
                embedding    BLOB NOT NULL,
                norm         REAL NOT NULL DEFAULT 0,
                dimensions   INTEGER NOT NULL,
                created_at   INTEGER NOT NULL
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
}

func (s *CodeStore) ensureSchemaNoLock(ctx context.Context, db schemaDB) error {
	for _, stmt := range s.ensureSchemaStatements() {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *CodeStore) resetDomainOn(ctx context.Context, db schemaDB, dropCache bool) error {
	dropViews := []string{codeTableIndexMeta, tableItems, tableItemEmbeddings, codeTableChunkEmbeddings, tableChunkFTS, codeTableEmbeddingCache}
	if !dropCache {
		dropViews = dropViews[:len(dropViews)-1]
	}
	for _, name := range dropViews {
		if err := dropViewIfExists(ctx, db, name); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+tableChunkFTS); err != nil {
		return err
	}
	for _, name := range []string{
		"idx_code_item_embeddings_item_row_id",
		"idx_code_chunk_embeddings_item_row_id",
	} {
		if _, err := db.ExecContext(ctx, `DROP INDEX IF EXISTS `+name); err != nil {
			return err
		}
	}
	dropTables := []string{codeTableChunkEmbeddings, tableItemEmbeddings, tableItems, codeTableIndexMeta}
	if dropCache {
		dropTables = append(dropTables, codeTableEmbeddingCache)
	}
	for _, name := range dropTables {
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+name); err != nil {
			return err
		}
	}
	if err := dropVecTablesByPrefix(ctx, db, codeVecChunkTablePrefix); err != nil {
		return err
	}
	if err := dropVecTablesByPrefix(ctx, db, vecItemTablePrefix); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM rzm_migration_state WHERE domain = ?`, string(migration.DomainCodeEmbeddings)); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return err
		}
	}
	return nil
}

func (s *CodeStore) needsSchemaRebuild(ctx context.Context) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	required := map[string][]string{
		codeTableIndexMeta:       {"id", "provider", "model", "dimensions", "schema_version", "created_at", "fingerprint_version", "last_sync", "sync_generation", "visible_generation"},
		tableItems:               {"id", "anchor_row_id", "fingerprint", "updated_at", "sync_generation"},
		tableItemEmbeddings:      {"id", "item_row_id", "content_hash", "embedding", "norm", "dimensions", "created_at"},
		codeTableChunkEmbeddings: {"id", "item_row_id", "chunk_index", "granularity", "content_hash", "embedding", "norm", "dimensions", "created_at"},
		codeTableEmbeddingCache:  {"content_hash", "embedding", "norm", "dimensions", "created_at"},
	}
	for table, cols := range required {
		exists, err := tableExists(ctx, s.db, table)
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		for _, col := range cols {
			has, err := tableHasColumn(ctx, s.db, table, col)
			if err != nil {
				return false, err
			}
			if !has {
				return true, nil
			}
		}
	}
	return false, nil
}
