package sqlite

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
)

func (s *NoteStore) ensureSchemaStatements() []string {
	return []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS ` + tableIndexMeta + ` (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			provider TEXT,
			model TEXT,
			dimensions INTEGER,
			schema_version INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			last_sync INTEGER,
			source_high_water INTEGER NOT NULL DEFAULT 0,
			sync_generation INTEGER NOT NULL DEFAULT 0,
			visible_generation INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS ` + tableNotes + ` (
			id              INTEGER PRIMARY KEY,
			note_id         TEXT NOT NULL UNIQUE,
			title           TEXT NOT NULL,
			path            TEXT NOT NULL,
			last_seen_mtime INTEGER NOT NULL,
			last_seen_size  INTEGER NOT NULL,
			sync_generation INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS ` + tableChunkEmbeddings + ` (
			id           INTEGER PRIMARY KEY,
			note_row_id  INTEGER NOT NULL REFERENCES ` + tableNotes + `(id) ON DELETE CASCADE,
			chunk_index  INTEGER NOT NULL,
			breadcrumb   TEXT,
			heading      TEXT,
			content_hash TEXT NOT NULL,
			embedding    BLOB NOT NULL,
			norm         REAL NOT NULL DEFAULT 0,
			dimensions   INTEGER NOT NULL,
			created_at   INTEGER NOT NULL,
			UNIQUE(note_row_id, chunk_index)
		);`,
		`CREATE TABLE IF NOT EXISTS ` + tableEmbeddingCache + ` (
			content_hash TEXT PRIMARY KEY,
			embedding    BLOB NOT NULL,
			norm         REAL NOT NULL DEFAULT 0,
			dimensions   INTEGER NOT NULL,
			created_at   INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_emb_chunk_embeddings_note_row_id ON ` + tableChunkEmbeddings + `(note_row_id);`,
	}
}

func (s *NoteStore) ensureSchemaNoLock(ctx context.Context, db schemaDB) error {
	for _, stmt := range s.ensureSchemaStatements() {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *NoteStore) resetDomainOn(ctx context.Context, db schemaDB, dropCache bool) error {
	// Corrupt/legacy DBs can contain views shadowing table names.
	// Include legacy emb_note_embeddings for backwards compatibility cleanup.
	dropViews := []string{tableIndexMeta, tableNotes, "emb_note_embeddings", tableChunkEmbeddings, tableEmbeddingCache}
	if !dropCache {
		dropViews = dropViews[:len(dropViews)-1]
	}
	for _, name := range dropViews {
		if err := dropViewIfExists(ctx, db, name); err != nil {
			return err
		}
	}
	for _, name := range []string{
		"idx_emb_note_embeddings_note_row_id",
		"idx_emb_chunk_embeddings_note_row_id",
	} {
		if _, err := db.ExecContext(ctx, `DROP INDEX IF EXISTS `+name); err != nil {
			return err
		}
	}
	// Include legacy emb_note_embeddings for backwards compatibility cleanup.
	dropTables := []string{tableChunkEmbeddings, "emb_note_embeddings", tableNotes, tableIndexMeta}
	if dropCache {
		dropTables = append(dropTables, tableEmbeddingCache)
	}
	for _, name := range dropTables {
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+name); err != nil {
			return err
		}
	}
	if err := dropVecTablesByPrefix(ctx, db, vecChunkTablePrefix); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM rzm_migration_state WHERE domain = ?`, string(migration.DomainNoteEmbeddings)); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return err
		}
	}
	return nil
}

func (s *NoteStore) needsSchemaRebuild(ctx context.Context) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	required := map[string][]string{
		tableIndexMeta:       {"id", "provider", "model", "dimensions", "schema_version", "created_at", "last_sync", "sync_generation", "visible_generation"},
		tableNotes:           {"id", "note_id", "title", "path", "last_seen_mtime", "last_seen_size", "sync_generation"},
		tableChunkEmbeddings: {"id", "note_row_id", "chunk_index", "breadcrumb", "heading", "content_hash", "embedding", "norm", "dimensions", "created_at"},
		tableEmbeddingCache:  {"content_hash", "embedding", "dimensions", "created_at"},
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
