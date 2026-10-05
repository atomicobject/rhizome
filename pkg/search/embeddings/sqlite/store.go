package sqlite

// Docs:
// - [Embeddings - SQLite store + locking hazards](docs/reference/analysis/Embeddings - SQLite store + locking hazards.md)
// - [Code Index - Unified SQLite DB](docs/reference/analysis/Code Index - Unified SQLite DB.md)

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/internal/sqlstore"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration/domains"

	_ "github.com/mattn/go-sqlite3"
)

// Store implements embeddings.Index backed by SQLite.
type Store struct {
	db      *sql.DB
	runtime sqlstore.Runtime
}

// DB returns the underlying SQLite handle for unified-index sharing.
func (s *Store) DB() *sql.DB {
	if s == nil {
		return nil
	}
	return s.db
}

// SetWriteMu overrides the default write mutex (used to serialize writes across stores).
func (s *Store) SetWriteMu(mu *sync.Mutex) {
	s.runtime.SetWriteMu(mu)
}

func (s *Store) withWrite(ctx context.Context, fn func(ctx context.Context, db *sql.DB) error) error {
	return s.runtime.WithWrite(ctx, s.db, fn)
}

type embeddingCacheRow struct {
	hash string
	vec  embeddings.Embedding
}

type noteChunkRow struct {
	index      int
	breadcrumb string
	heading    string
	hash       string
	vec        embeddings.Embedding
	norm       float64
}

const currentSchemaVersion = 7

const (
	noteChunkBatchSize      = 100
	embeddingCacheBatchSize = 200
)

const (
	tableIndexMeta       = "emb_index_meta"
	tableNotes           = "emb_notes"
	tableChunkEmbeddings = "emb_chunk_embeddings"
	tableEmbeddingCache  = "emb_embedding_cache"
	vecChunkTablePrefix  = "emb_chunk_embeddings_vec_d"

	// DefaultStaleNoteThreshold is the fraction of stale notes (not seen in current sync)
	// above which we prune them. Below this threshold, notes are kept to avoid re-embedding
	// costs when switching branches. 0.3 means we keep stale notes if they're <30% of total.
	DefaultStaleNoteThreshold = 0.3
)

// OpenOptions controls SQLite store open behavior.
type OpenOptions struct {
	// TxLockMode sets sqlite _txlock mode (supported: immediate, exclusive).
	TxLockMode string
	// Pool controls SQLite connection pooling.
	Pool sqliteutil.Options
}

// DeleteChunksNotIn removes chunk embeddings whose indices are not in the provided set.
func (s *Store) DeleteChunksNotIn(ctx context.Context, id embeddings.NoteID, indices []int) error {
	ctx = indexingperf.WithOp(ctx, "noteemb.delete_chunks_not_in")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		row := db.QueryRowContext(ctx, `SELECT id FROM `+tableNotes+` WHERE note_id = ?`, string(id))
		var rowID int64
		if err := row.Scan(&rowID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		return deleteNoteChunkRowsNotInDB(ctx, db, rowID, indices)
	})
}

func deleteNoteChunkRowsNotInDB(ctx context.Context, db *sql.DB, rowID int64, indices []int) error {
	if len(indices) == 0 {
		_, err := execWithRetry(ctx, db, `DELETE FROM `+tableChunkEmbeddings+` WHERE note_row_id = ?`, rowID)
		return err
	}
	holders := make([]string, len(indices))
	args := make([]any, 0, len(indices)+1)
	args = append(args, rowID)
	for i, idx := range indices {
		holders[i] = "?"
		args = append(args, idx)
	}
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE note_row_id = ? AND chunk_index NOT IN (%s)`, tableChunkEmbeddings, strings.Join(holders, ","))
	_, err := execWithRetry(ctx, db, stmt, args...)
	return err
}

func deleteNoteChunkRowsNotInTx(ctx context.Context, tx *sql.Tx, rowID int64, indices []int) error {
	if len(indices) == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM `+tableChunkEmbeddings+` WHERE note_row_id = ?`, rowID)
		return err
	}
	holders := make([]string, len(indices))
	args := make([]any, 0, len(indices)+1)
	args = append(args, rowID)
	for i, idx := range indices {
		holders[i] = "?"
		args = append(args, idx)
	}
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE note_row_id = ? AND chunk_index NOT IN (%s)`, tableChunkEmbeddings, strings.Join(holders, ","))
	_, err := tx.ExecContext(ctx, stmt, args...)
	return err
}

// SyncNoteChunks upserts note chunks and removes stale chunk rows in one write path.
func (s *Store) SyncNoteChunks(ctx context.Context, item embeddings.NoteChunkSync) error {
	return s.SyncNoteChunksBatch(ctx, []embeddings.NoteChunkSync{item})
}

// NoteChunks returns stored embeddings for all chunks of a note.
func (s *Store) NoteChunks(ctx context.Context, id embeddings.NoteID) ([]embeddings.StoredChunk, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id FROM `+tableNotes+` WHERE note_id = ?`, string(id))
	var rowID int64
	if err := row.Scan(&rowID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT chunk_index, breadcrumb, heading, embedding, dimensions
		FROM `+tableChunkEmbeddings+`
		WHERE note_row_id = ?
		ORDER BY chunk_index
	`, rowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []embeddings.StoredChunk
	expectedDims := int(s.runtime.Dimensions.Load())
	for rows.Next() {
		var idx, dims int
		var breadcrumb, heading string
		var blob []byte
		if err := rows.Scan(&idx, &breadcrumb, &heading, &blob, &dims); err != nil {
			return nil, err
		}
		emb := bytesToEmbed(blob)
		if dims > 0 && len(emb) != dims {
			continue
		}
		if expectedDims > 0 && len(emb) != expectedDims {
			continue
		}
		chunks = append(chunks, embeddings.StoredChunk{
			Index:      idx,
			Breadcrumb: breadcrumb,
			Heading:    heading,
			Embedding:  emb,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return chunks, nil
}

// Open opens (or creates) a SQLite index at path with the expected vector dimensions.
func Open(path string, dimensions int) (*Store, error) {
	return OpenWithOptions(path, dimensions, OpenOptions{})
}

// OpenWithOptions opens (or creates) a SQLite index at path with the expected vector dimensions.
func OpenWithOptions(path string, dimensions int, opts OpenOptions) (*Store, error) {
	// One creator or migrator per database file, held from before the first
	// connection (the WAL switch) through schema validation.
	releaseSchemaLock, err := sqliteutil.LockSchemaInit(context.Background(), path)
	if err != nil {
		return nil, err
	}
	defer releaseSchemaLock()
	db, err := sqlstore.OpenDBWithOptions(path, sqlstore.OpenDBOptions{
		TxLockMode: opts.TxLockMode,
		Pool:       opts.Pool,
	})
	if err != nil {
		return nil, err
	}
	return openWithDB(db, dimensions, db.Close)
}

// OpenWithDB opens the store using an existing sqlite handle (shared in unified index).
func OpenWithDB(db *sql.DB, dimensions int) (*Store, error) {
	if db == nil {
		return nil, errors.New("sqlite db is required")
	}
	releaseSchemaLock, err := sqliteutil.LockSchemaInitForDB(context.Background(), db)
	if err != nil {
		return nil, err
	}
	defer releaseSchemaLock()
	return openWithDB(db, dimensions, func() error { return nil })
}

func openWithDB(db *sql.DB, dimensions int, closeFn func() error) (*Store, error) {
	store := &Store{
		db:      db,
		runtime: sqlstore.NewRuntime("note-embeddings", closeFn),
	}
	if err := store.ensureSchemaWithRecovery(context.Background()); err != nil {
		if closeFn != nil {
			_ = closeFn()
		}
		return nil, err
	}
	if dimensions > 0 {
		store.runtime.Dimensions.Store(int64(dimensions))
	}
	return store, nil
}

// Dimensions returns the configured or discovered embedding dimensions.
func (s *Store) Dimensions() int {
	return int(s.runtime.Dimensions.Load())
}

func (s *Store) ensureSchemaStatements() []string {
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

func (s *Store) ensureSchemaNoLock(ctx context.Context, db *sql.DB) error {
	for _, stmt := range s.ensureSchemaStatements() {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// EnsureSchema creates tables and indices if needed.
func (s *Store) EnsureSchema(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "noteemb.ensure_schema")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return s.ensureSchemaNoLock(ctx, db)
	})
}

func chunkVecTableName(dims int) string {
	return fmt.Sprintf("%s%d", vecChunkTablePrefix, dims)
}

func (s *Store) ensureChunkVecMirror(ctx context.Context, dims int) (string, error) {
	if dims <= 0 {
		return "", fmt.Errorf("invalid vec dimensions: %d", dims)
	}
	if _, ok := s.runtime.VecReady.Load(dims); ok {
		return chunkVecTableName(dims), nil
	}
	ctx = indexingperf.WithOp(ctx, "noteemb.ensure_vec_mirror")

	table := chunkVecTableName(dims)
	insertTrigger := fmt.Sprintf("trg_%s_ins", table)
	updateTrigger := fmt.Sprintf("trg_%s_upd", table)
	deleteTrigger := fmt.Sprintf("trg_%s_del", table)
	blobBytes := dims * 4

	if err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		stmts := []string{
			fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(chunk_id integer primary key, embedding float[%d]);`, table, dims),
			fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %s AFTER INSERT ON %s
				WHEN NEW.dimensions = %d AND length(NEW.embedding) = %d
				BEGIN
					INSERT INTO %s(chunk_id, embedding) VALUES (NEW.id, NEW.embedding);
				END;`, insertTrigger, tableChunkEmbeddings, dims, blobBytes, table),
			fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %s AFTER UPDATE OF embedding, dimensions ON %s
				BEGIN
					DELETE FROM %s WHERE chunk_id = OLD.id;
					INSERT INTO %s(chunk_id, embedding)
					SELECT NEW.id, NEW.embedding
					WHERE NEW.dimensions = %d AND length(NEW.embedding) = %d;
				END;`, updateTrigger, tableChunkEmbeddings, table, table, dims, blobBytes),
			fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %s AFTER DELETE ON %s
				BEGIN
					DELETE FROM %s WHERE chunk_id = OLD.id;
				END;`, deleteTrigger, tableChunkEmbeddings, table),
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM `+table); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s(chunk_id, embedding)
			SELECT id, embedding
			FROM %s
			WHERE dimensions = ? AND length(embedding) = ?
		`, table, tableChunkEmbeddings), dims, blobBytes); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return "", err
	}
	s.runtime.VecReady.Store(dims, struct{}{})
	return table, nil
}

func dropVecTablesByPrefix(ctx context.Context, db *sql.DB, prefix string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name LIKE ?
	`, prefix+"%")
	if err != nil {
		return err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range tables {
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+name); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureSchemaWithRecovery(ctx context.Context) error {
	if err := s.EnsureSchema(ctx); err == nil {
		// Some older DBs can have tables that exist but are missing columns; CREATE TABLE IF NOT EXISTS
		// won't fix that. Detect and rebuild while preserving embeddings when possible.
		if needs, checkErr := s.needsSchemaRebuild(ctx); checkErr == nil && needs {
			if err := s.rebuildDomainPreservingEmbeddings(ctx); err == nil {
				return s.ensureDomainMigrations(ctx)
			}
			// Fall through to hard reset if preservation fails.
			if err := s.ResetDomain(ctx); err != nil {
				return err
			}
			if err := s.EnsureSchema(ctx); err != nil {
				return err
			}
			return s.ensureDomainMigrations(ctx)
		}
		return s.ensureDomainMigrations(ctx)
	} else if !isSchemaError(err) {
		return err
	}
	if err := s.rebuildDomainPreservingEmbeddings(ctx); err == nil {
		return s.ensureDomainMigrations(ctx)
	}
	if err := s.ResetDomain(ctx); err != nil {
		return err
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return err
	}
	return s.ensureDomainMigrations(ctx)
}

func (s *Store) ensureDomainMigrations(ctx context.Context) error {
	plan := domains.NoteEmbeddingsPlan(currentSchemaVersion, func(ctx context.Context, tx *sql.Tx, _, to int) error {
		if to == 7 {
			if err := addColumnIfMissingTx(ctx, tx, tableIndexMeta, "source_high_water", "INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE `+tableIndexMeta+`
			SET schema_version = CASE
				WHEN schema_version IS NULL OR schema_version < ? THEN ?
				ELSE schema_version
			END
			WHERE id = 1
		`, to, to)
		return err
	}, nil)
	if err := migration.EnsureDomain(ctx, s.db, plan, migration.EnsureOptions{}); err != nil {
		return err
	}
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return sqliteutil.ExecTxWithRetry(ctx, db, func(tx *sql.Tx) error {
			if err := addColumnIfMissingTx(ctx, tx, tableIndexMeta, "source_high_water", "INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
			if err := addColumnIfMissingTx(ctx, tx, tableIndexMeta, "visible_generation", "INTEGER NOT NULL DEFAULT 0"); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `UPDATE `+tableIndexMeta+` SET visible_generation = sync_generation WHERE visible_generation = 0 AND sync_generation > 0`)
			return err
		})
	})
}

// ResetDomain drops and recreates the semantic embeddings tables. This does not affect code intel
// nor code embeddings tables in a unified DB.
func (s *Store) ResetDomain(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "noteemb.reset_domain")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return s.resetDomainNoLock(ctx, true)
	})
}

// ResetDomainPreservingCache drops semantic embedding tables but keeps the embedding cache.
// This allows rebuilds to reuse embeddings by content hash when provider/model match.
func (s *Store) ResetDomainPreservingCache(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "noteemb.reset_domain_preserve_cache")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return s.resetDomainNoLock(ctx, false)
	})
}

// resetDomainNoLock assumes caller holds writeMu.
func (s *Store) resetDomainNoLock(ctx context.Context, dropCache bool) error {
	// Corrupt/legacy DBs can contain views shadowing table names.
	// Include legacy emb_note_embeddings for backwards compatibility cleanup.
	dropViews := []string{tableIndexMeta, tableNotes, "emb_note_embeddings", tableChunkEmbeddings, tableEmbeddingCache}
	if !dropCache {
		dropViews = dropViews[:len(dropViews)-1]
	}
	for _, name := range dropViews {
		if err := dropViewIfExists(ctx, s.db, name); err != nil {
			return err
		}
	}
	for _, name := range []string{
		"idx_emb_note_embeddings_note_row_id",
		"idx_emb_chunk_embeddings_note_row_id",
	} {
		if _, err := s.db.ExecContext(ctx, `DROP INDEX IF EXISTS `+name); err != nil {
			return err
		}
	}
	// Include legacy emb_note_embeddings for backwards compatibility cleanup.
	dropTables := []string{tableChunkEmbeddings, "emb_note_embeddings", tableNotes, tableIndexMeta}
	if dropCache {
		dropTables = append(dropTables, tableEmbeddingCache)
	}
	for _, name := range dropTables {
		if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS `+name); err != nil {
			return err
		}
	}
	if err := dropVecTablesByPrefix(ctx, s.db, vecChunkTablePrefix); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM rzm_migration_state WHERE domain = ?`, string(migration.DomainNoteEmbeddings)); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return err
		}
	}
	s.runtime.VecReady = sync.Map{}
	return nil
}

func (s *Store) needsSchemaRebuild(ctx context.Context) (bool, error) {
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

func (s *Store) rebuildDomainPreservingEmbeddings(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("nil sqlite store")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	hasChunkNorm, err := tableHasColumn(ctx, s.db, tableChunkEmbeddings, "norm")
	if err != nil {
		return err
	}
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
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

		// Drop any previous temp tables (best-effort).
		for _, name := range []string{b.meta, b.notes, b.chunkE, b.cache} {
			_, _ = s.db.ExecContext(ctx, `DROP TABLE IF EXISTS `+name)
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
			if _, err := s.db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}

		// Check if sync_generation columns exist for graceful migration.
		hasMetaSyncGen, _ := tableHasColumn(ctx, s.db, tableIndexMeta, "sync_generation")
		hasMetaSourceHighWater, _ := tableHasColumn(ctx, s.db, tableIndexMeta, "source_high_water")
		hasMetaVisibleGen, _ := tableHasColumn(ctx, s.db, tableIndexMeta, "visible_generation")
		hasNoteSyncGen, _ := tableHasColumn(ctx, s.db, tableNotes, "sync_generation")

		// Backup best-effort; missing tables/columns are fine (we'll just restore less).
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
		_, _ = s.db.ExecContext(ctx, `
			INSERT INTO `+b.meta+` (provider, model, dimensions, schema_version, created_at, last_sync, source_high_water, sync_generation, visible_generation)
			SELECT provider, model, dimensions, schema_version, created_at, last_sync, `+metaSourceHighWaterSelect+`, `+metaSyncGenSelect+`, `+metaVisibleGenSelect+`
			FROM `+tableIndexMeta+`
			WHERE id = 1
		`)
		noteSyncGenSelect := "0"
		if hasNoteSyncGen {
			noteSyncGenSelect = "COALESCE(sync_generation, 0)"
		}
		_, _ = s.db.ExecContext(ctx, `
			INSERT INTO `+b.notes+` (note_id, title, path, last_seen_mtime, last_seen_size, sync_generation)
			SELECT note_id, title, path, last_seen_mtime, last_seen_size, `+noteSyncGenSelect+`
			FROM `+tableNotes+`
		`)
		chunkNormSelect := "0"
		if hasChunkNorm {
			chunkNormSelect = "COALESCE(c.norm, 0)"
		}
		_, _ = s.db.ExecContext(ctx, `
			INSERT INTO `+b.chunkE+` (note_id, chunk_index, breadcrumb, heading, content_hash, embedding, norm, dimensions, created_at)
			SELECT n.note_id, c.chunk_index, c.breadcrumb, c.heading, c.content_hash, c.embedding, `+chunkNormSelect+`, c.dimensions, c.created_at
			FROM `+tableChunkEmbeddings+` c
			JOIN `+tableNotes+` n ON n.id = c.note_row_id
		`)
		// Check if cache has norm column for graceful migration.
		hasCacheNorm, _ := tableHasColumn(ctx, s.db, tableEmbeddingCache, "norm")
		cacheNormSelect := "0"
		if hasCacheNorm {
			cacheNormSelect = "COALESCE(norm, 0)"
		}
		_, _ = s.db.ExecContext(ctx, `
			INSERT INTO `+b.cache+` (content_hash, embedding, norm, dimensions, created_at)
			SELECT content_hash, embedding, `+cacheNormSelect+`, dimensions, created_at
			FROM `+tableEmbeddingCache+`
		`)

		// Rebuild domain tables.
		if err := s.resetDomainNoLock(ctx, true); err != nil {
			return err
		}
		if err := s.ensureSchemaNoLock(ctx, db); err != nil {
			return err
		}

		// Restore notes first (row ids may change).
		_, _ = s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO `+tableNotes+` (note_id, title, path, last_seen_mtime, last_seen_size, sync_generation)
			SELECT note_id, COALESCE(title, ''), COALESCE(path, ''), COALESCE(last_seen_mtime, 0), COALESCE(last_seen_size, 0), COALESCE(sync_generation, 0)
			FROM `+b.notes+`
		`)
		// Restore chunk embeddings.
		_, _ = s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO `+tableChunkEmbeddings+` (note_row_id, chunk_index, breadcrumb, heading, content_hash, embedding, norm, dimensions, created_at)
			SELECT n.id, b.chunk_index, b.breadcrumb, b.heading, b.content_hash, b.embedding, COALESCE(b.norm, 0), b.dimensions, b.created_at
			FROM `+b.chunkE+` b
			JOIN `+tableNotes+` n ON n.note_id = b.note_id
		`)
		_, _ = s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO `+tableEmbeddingCache+` (content_hash, embedding, norm, dimensions, created_at)
			SELECT content_hash, embedding, COALESCE(norm, 0), dimensions, created_at
			FROM `+b.cache+`
			WHERE content_hash IS NOT NULL AND content_hash != ''
		`)
		_, _ = s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO `+tableEmbeddingCache+` (content_hash, embedding, norm, dimensions, created_at)
			SELECT content_hash, embedding, COALESCE(norm, 0), dimensions, created_at
			FROM `+tableChunkEmbeddings+`
			WHERE content_hash IS NOT NULL AND content_hash != ''
		`)

		// Restore metadata (clamp schema version upward).
		_, _ = s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO `+tableIndexMeta+` (id, provider, model, dimensions, schema_version, created_at, last_sync, source_high_water, sync_generation, visible_generation)
			SELECT 1, provider, model, dimensions, ?, COALESCE(created_at, 0), COALESCE(last_sync, 0), COALESCE(source_high_water, 0), COALESCE(sync_generation, 0), COALESCE(visible_generation, sync_generation, 0)
			FROM `+b.meta+`
			LIMIT 1
		`, currentSchemaVersion)

		for _, name := range []string{b.meta, b.notes, b.chunkE, b.cache} {
			_, _ = s.db.ExecContext(ctx, `DROP TABLE IF EXISTS `+name)
		}
		return nil
	})
}

func tableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var name string
	err := db.QueryRowContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type='table' AND name=?
		LIMIT 1
	`, table).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return name == table, nil
}

func tableHasColumn(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	var found int
	err := db.QueryRowContext(ctx, `
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

func addColumnIfMissingTx(ctx context.Context, tx *sql.Tx, table, column, decl string) error {
	var found int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM pragma_table_info(?)
		WHERE name = ?
		LIMIT 1
	`, table, column).Scan(&found)
	if err == nil && found == 1 {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+decl)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return nil
	}
	return err
}

func isSchemaError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no such column"):
		return true
	case strings.Contains(msg, "no such table"):
		return true
	case strings.Contains(msg, "malformed database schema"):
		return true
	case strings.Contains(msg, "views may not be indexed"):
		return true
	case strings.Contains(msg, "is a view"):
		return true
	default:
		return false
	}
}

func dropViewIfExists(ctx context.Context, db *sql.DB, name string) error {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	_, err := db.ExecContext(ctx, `DROP VIEW IF EXISTS `+name)
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "use drop table") {
		return nil
	}
	return err
}

// Close releases database resources.
func (s *Store) Close() error {
	return s.runtime.Close(s.db)
}

func (s *Store) Metadata(ctx context.Context) (embeddings.IndexMetadata, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT provider, model, dimensions, schema_version, created_at, IFNULL(last_sync, 0), IFNULL(source_high_water, 0) FROM `+tableIndexMeta+` WHERE id = 1`)
	var provider, model string
	var dims, version int
	var created, lastSync, sourceHighWater int64
	if err := row.Scan(&provider, &model, &dims, &version, &created, &lastSync, &sourceHighWater); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return embeddings.IndexMetadata{}, false, nil
		}
		return embeddings.IndexMetadata{}, false, err
	}
	meta := embeddings.IndexMetadata{
		Provider:      provider,
		Model:         model,
		Dimensions:    dims,
		SchemaVersion: version,
		CreatedAt:     time.Unix(created, 0),
	}
	if lastSync > 0 {
		meta.LastSync = time.Unix(lastSync, 0)
	}
	if sourceHighWater > 0 {
		meta.SourceHighWater = time.Unix(sourceHighWater, 0)
	}
	return meta, true, nil
}

func (s *Store) ValidateOrInitMetadata(ctx context.Context, meta embeddings.IndexMetadata) error {
	ctx = indexingperf.WithOp(ctx, "noteemb.validate_metadata")
	current, ok, err := s.Metadata(ctx)
	if err != nil {
		return err
	}

	// initMetadata inserts a fresh metadata row. Skipped when dimensions are not
	// yet known so we don't persist a zero-dim placeholder that would later look
	// like a corrupted index. The next call (after the provider has learned its
	// output size) will insert the row with the real value.
	initMetadata := func() error {
		if meta.Dimensions == 0 {
			return nil
		}
		if meta.SchemaVersion == 0 {
			meta.SchemaVersion = currentSchemaVersion
		}
		if meta.CreatedAt.IsZero() {
			meta.CreatedAt = time.Now()
		}
		return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `
				INSERT INTO `+tableIndexMeta+` (id, provider, model, dimensions, schema_version, created_at, last_sync, source_high_water)
				VALUES (1, ?, ?, ?, ?, ?, ?, ?)
			`, meta.Provider, meta.Model, meta.Dimensions, meta.SchemaVersion, meta.CreatedAt.Unix(), meta.LastSync.Unix(), meta.SourceHighWater.Unix())
			if err == nil && meta.Dimensions > 0 {
				s.runtime.Dimensions.Store(int64(meta.Dimensions))
			}
			return err
		})
	}

	// resetAndInit drops embedding tables and re-initializes metadata.
	// Used when provider/model/dimensions change (existing embeddings are invalid).
	resetAndInit := func(reasonCode, reason string) error {
		indexingperf.AddCount(ctx, "noteemb.reset.reason."+reasonCode, 1)
		if recorder := diagnostics.FromContext(ctx); recorder != nil {
			recorder.Logger("embeddings").InfoContext(ctx, "Note embedding index reset", slog.String("event", "index.reset"), slog.String("reason", reasonCode))
		} else {
			slog.InfoContext(ctx, "Note embedding index reset", "reason", reasonCode)
		}
		if err := s.ResetDomain(ctx); err != nil {
			return fmt.Errorf("%s: reset domain: %w", reason, err)
		}
		if err := s.EnsureSchema(ctx); err != nil {
			return fmt.Errorf("%s: recreate schema: %w", reason, err)
		}
		if err := initMetadata(); err != nil {
			return err
		}
		return nil
	}

	if !ok {
		return initMetadata()
	}
	if current.SchemaVersion > currentSchemaVersion {
		return &migration.ErrFutureSchema{
			Domain:    migration.DomainNoteEmbeddings,
			Current:   current.SchemaVersion,
			Supported: currentSchemaVersion,
		}
	}
	// A stored dimensions=0 row is a placeholder from a prior call whose provider
	// hadn't yet learned its output size (e.g. Voyage discovers dims lazily from
	// the first embedding response). Heal it via the UPDATE below — the per-chunk
	// `dimensions` column is the authoritative dim for any embeddings that may
	// have been stored, and reads filter mismatched chunks. Reset only on a true
	// dim conflict between two known values.
	if meta.Dimensions > 0 && current.Dimensions > 0 && meta.Dimensions != current.Dimensions {
		return resetAndInit("dimensions_mismatch", fmt.Sprintf("dimensions mismatch: have %d, expected %d", current.Dimensions, meta.Dimensions))
	}
	if meta.Provider != "" && current.Provider != "" && meta.Provider != current.Provider {
		return resetAndInit("provider_mismatch", fmt.Sprintf("provider mismatch: have %s, expected %s", current.Provider, meta.Provider))
	}
	if meta.Model != "" && current.Model != "" && meta.Model != current.Model {
		return resetAndInit("model_mismatch", fmt.Sprintf("model mismatch: have %s, expected %s", current.Model, meta.Model))
	}
	if err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err = db.ExecContext(ctx, `
			UPDATE `+tableIndexMeta+` SET
				provider = COALESCE(NULLIF(provider, ''), ?),
				model = COALESCE(NULLIF(model, ''), ?),
				dimensions = CASE WHEN dimensions IS NULL OR dimensions = 0 THEN ? ELSE dimensions END
				, schema_version = CASE WHEN schema_version IS NULL OR schema_version = 0 THEN ? ELSE schema_version END
			WHERE id = 1
		`, meta.Provider, meta.Model, meta.Dimensions, currentSchemaVersion)
		// Update in-memory dimensions if we just wrote them
		if err == nil && meta.Dimensions > 0 {
			s.runtime.Dimensions.Store(int64(meta.Dimensions))
		}
		return err
	}); err != nil {
		return err
	}
	return nil
}

func (s *Store) UpdateLastSync(ctx context.Context, ts time.Time) error {
	ctx = indexingperf.WithOp(ctx, "noteemb.update_last_sync")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, `UPDATE `+tableIndexMeta+` SET last_sync = ? WHERE id = 1`, ts.Unix())
		return err
	})
}

func (s *Store) UpdateSourceHighWater(ctx context.Context, ts time.Time) error {
	if ts.IsZero() {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "noteemb.update_source_high_water")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, `UPDATE `+tableIndexMeta+` SET source_high_water = MAX(IFNULL(source_high_water, 0), ?) WHERE id = 1`, ts.Unix())
		return err
	})
}

// SyncGeneration returns the current sync generation number.
func (s *Store) SyncGeneration(ctx context.Context) (int64, error) {
	var gen int64
	err := s.db.QueryRowContext(ctx, `SELECT IFNULL(sync_generation, 0) FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&gen)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return gen, err
}

// IncrementSyncGeneration bumps the sync generation and returns the new value.
// Call this at the start of a sync to mark which notes are "active" in this sync.
func (s *Store) IncrementSyncGeneration(ctx context.Context) (int64, error) {
	ctx = indexingperf.WithOp(ctx, "noteemb.increment_sync_generation")
	var newGen int64
	err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if err := ensureNoteEmbeddingMetadataRowForSyncGeneration(ctx, db); err != nil {
			return err
		}
		if _, err := execWithRetry(ctx, db, `UPDATE `+tableIndexMeta+` SET sync_generation = sync_generation + 1 WHERE id = 1`); err != nil {
			return err
		}
		return db.QueryRowContext(ctx, `SELECT sync_generation FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&newGen)
	})
	return newGen, err
}

func ensureNoteEmbeddingMetadataRowForSyncGeneration(ctx context.Context, db *sql.DB) error {
	_, err := execWithRetry(ctx, db, `
		INSERT OR IGNORE INTO `+tableIndexMeta+` (id, provider, model, dimensions, schema_version, created_at, last_sync, source_high_water, sync_generation, visible_generation)
		VALUES (1, '', '', 0, ?, ?, 0, 0, 0, 0)
	`, currentSchemaVersion, time.Now().Unix())
	return err
}

// CommitSyncGeneration makes the current sync generation visible to search.
func (s *Store) CommitSyncGeneration(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "noteemb.commit_sync_generation")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if err := ensureNoteEmbeddingMetadataRowForSyncGeneration(ctx, db); err != nil {
			return err
		}
		_, err := execWithRetry(ctx, db, `UPDATE `+tableIndexMeta+` SET visible_generation = sync_generation WHERE id = 1`)
		return err
	})
}

// PruneStaleNotes removes notes that weren't seen in the current sync generation.
// If threshold > 0, only prunes when stale notes exceed threshold fraction of total.
// Returns the number of notes pruned.
func (s *Store) PruneStaleNotes(ctx context.Context, threshold float64) (int, error) {
	ctx = indexingperf.WithOp(ctx, "noteemb.prune_stale_notes")
	gen, err := s.SyncGeneration(ctx)
	if err != nil {
		return 0, err
	}

	var total, stale int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableNotes).Scan(&total); err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableNotes+` WHERE sync_generation < ?`, gen).Scan(&stale); err != nil {
		return 0, err
	}
	if stale == 0 {
		return 0, nil
	}

	// If stale fraction is below threshold, keep notes around for potential reuse.
	staleFraction := float64(stale) / float64(total)
	if threshold > 0 && staleFraction < threshold {
		return 0, nil // Notes preserved for branch-switching reuse
	}

	// Prune stale notes and their embeddings (via cascade).
	var rows int64
	err = s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		result, err := execWithRetry(ctx, db, `DELETE FROM `+tableNotes+` WHERE sync_generation < ?`, gen)
		if err != nil {
			return err
		}
		rows, _ = result.RowsAffected()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int(rows), nil
}

func (s *Store) Stats(ctx context.Context) (int, int, error) {
	var notes, chunks int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableNotes).Scan(&notes); err != nil {
		return 0, 0, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableChunkEmbeddings).Scan(&chunks); err != nil {
		return 0, 0, err
	}
	return notes, chunks, nil
}

// ChunkHashes returns existing chunk hashes for a note keyed by chunk index.
func (s *Store) ChunkHashes(ctx context.Context, id embeddings.NoteID) (map[int]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.chunk_index, c.content_hash
		FROM `+tableChunkEmbeddings+` c
		JOIN `+tableNotes+` n ON c.note_row_id = n.id
		WHERE n.note_id = ?
	`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[int]string)
	for rows.Next() {
		var idx int
		var hash string
		if err := rows.Scan(&idx, &hash); err != nil {
			return nil, err
		}
		res[idx] = hash
	}
	return res, rows.Err()
}

// ChunkHashesBatch returns existing chunk hashes for multiple notes keyed by note ID and chunk index.
func (s *Store) ChunkHashesBatch(ctx context.Context, ids []embeddings.NoteID) (map[embeddings.NoteID]map[int]string, error) {
	out := make(map[embeddings.NoteID]map[int]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	const maxVars = 900
	for batch := range slices.Chunk(ids, maxVars) {
		holders := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch))
		for _, id := range batch {
			holders = append(holders, "?")
			args = append(args, string(id))
		}
		query := fmt.Sprintf(`
			SELECT n.note_id, c.chunk_index, c.content_hash
			FROM %s c
			JOIN %s n ON c.note_row_id = n.id
			WHERE n.note_id IN (%s)
		`, tableChunkEmbeddings, tableNotes, strings.Join(holders, ","))
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var noteID string
			var idx int
			var hash sql.NullString
			if err := rows.Scan(&noteID, &idx, &hash); err != nil {
				rows.Close()
				return nil, err
			}
			id := embeddings.NoteID(noteID)
			if _, ok := out[id]; !ok {
				out[id] = make(map[int]string)
			}
			if hash.Valid {
				out[id][idx] = hash.String
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// UpsertNoteMeta inserts or updates metadata for a note.
// The note is automatically marked with the current sync generation.
func (s *Store) UpsertNoteMeta(ctx context.Context, info embeddings.NoteFileInfo) error {
	ctx = indexingperf.WithOp(ctx, "noteemb.upsert_note_meta")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		// Get current sync generation to mark this note as active.
		var gen int64
		_ = db.QueryRowContext(ctx, `SELECT IFNULL(sync_generation, 0) FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&gen)

		_, err := execWithRetry(ctx, db, `
			INSERT INTO `+tableNotes+` (note_id, title, path, last_seen_mtime, last_seen_size, sync_generation)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(note_id) DO UPDATE SET
				title = excluded.title,
				path = excluded.path,
				last_seen_mtime = excluded.last_seen_mtime,
				last_seen_size = excluded.last_seen_size,
				sync_generation = excluded.sync_generation
		`, string(info.ID), info.Title, info.Path, info.Mtime.Unix(), info.Size, gen)
		return err
	})
}

// UpsertNoteMetaBatch inserts or updates metadata for multiple notes in a single transaction.
func (s *Store) UpsertNoteMetaBatch(ctx context.Context, infos []embeddings.NoteFileInfo) error {
	ctx = indexingperf.WithOp(ctx, "noteemb.upsert_note_meta")
	if len(infos) == 0 {
		return nil
	}
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return sqliteutil.ExecTxWithRetry(ctx, db, func(tx *sql.Tx) error {
			var gen int64
			_ = tx.QueryRowContext(ctx, `SELECT IFNULL(sync_generation, 0) FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&gen)

			stmt, err := tx.PrepareContext(ctx, `
				INSERT INTO `+tableNotes+` (note_id, title, path, last_seen_mtime, last_seen_size, sync_generation)
				VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT(note_id) DO UPDATE SET
					title = excluded.title,
					path = excluded.path,
					last_seen_mtime = excluded.last_seen_mtime,
					last_seen_size = excluded.last_seen_size,
					sync_generation = excluded.sync_generation
			`)
			if err != nil {
				return err
			}
			defer stmt.Close()

			for _, info := range infos {
				if _, err := stmt.ExecContext(ctx, string(info.ID), info.Title, info.Path, info.Mtime.Unix(), info.Size, gen); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// DeleteNotesNotIn removes notes (and embeddings via cascade) not present in existingIDs.
func (s *Store) DeleteNotesNotIn(ctx context.Context, existingIDs []embeddings.NoteID) error {
	ctx = indexingperf.WithOp(ctx, "noteemb.delete_notes_not_in")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if len(existingIDs) == 0 {
			_, err := execWithRetry(ctx, db, `DELETE FROM `+tableNotes)
			return err
		}

		if len(existingIDs) <= 900 {
			args := make([]any, len(existingIDs))
			holders := make([]string, len(existingIDs))
			for i, id := range existingIDs {
				args[i] = string(id)
				holders[i] = "?"
			}
			query := fmt.Sprintf(`DELETE FROM %s WHERE note_id NOT IN (%s)`, tableNotes, strings.Join(holders, ","))
			_, err := execWithRetry(ctx, db, query, args...)
			return err
		}

		existingSet := make(map[string]struct{}, len(existingIDs))
		for _, id := range existingIDs {
			existingSet[string(id)] = struct{}{}
		}

		rows, err := db.QueryContext(ctx, `SELECT note_id FROM `+tableNotes)
		if err != nil {
			return err
		}
		defer rows.Close()

		var toDelete []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			if _, ok := existingSet[id]; !ok {
				toDelete = append(toDelete, id)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(toDelete) == 0 {
			return nil
		}

		const batch = 500
		for chunk := range slices.Chunk(toDelete, batch) {
			args := make([]any, len(chunk))
			holders := make([]string, len(chunk))
			for i, id := range chunk {
				args[i] = id
				holders[i] = "?"
			}
			query := fmt.Sprintf(`DELETE FROM %s WHERE note_id IN (%s)`, tableNotes, strings.Join(holders, ","))
			if _, err := execWithRetry(ctx, db, query, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

func execWithRetry(ctx context.Context, db *sql.DB, query string, args ...any) (sql.Result, error) {
	const maxAttempts = 5
	const baseBackoff = 50 * time.Millisecond
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := db.ExecContext(ctx, query, args...)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !isBusyError(err) || attempt == maxAttempts {
			return nil, err
		}
		time.Sleep(baseBackoff * time.Duration(attempt))
	}
	return nil, lastErr
}

func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "SQLITE_BUSY") ||
		strings.Contains(msg, "locked") ||
		strings.Contains(msg, "busy") ||
		strings.Contains(msg, "transaction within a transaction")
}

// ListNotes returns metadata for all tracked notes.
func (s *Store) ListNotes(ctx context.Context) ([]embeddings.NoteFileInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT note_id, title, path, last_seen_mtime, last_seen_size
		FROM `+tableNotes+`
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []embeddings.NoteFileInfo
	for rows.Next() {
		var id, title, path string
		var mtimeUnix int64
		var size int64
		if err := rows.Scan(&id, &title, &path, &mtimeUnix, &size); err != nil {
			return nil, err
		}
		res = append(res, embeddings.NoteFileInfo{
			ID:    embeddings.NoteID(id),
			Title: title,
			Path:  path,
			Mtime: time.Unix(mtimeUnix, 0),
			Size:  size,
		})
	}
	return res, rows.Err()
}

// EmbeddingByHash returns a cached embedding by content hash, if present and dimension-compatible.
func (s *Store) EmbeddingByHash(ctx context.Context, hash string) (embeddings.Embedding, bool, error) {
	if strings.TrimSpace(hash) == "" {
		return nil, false, nil
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT embedding, dimensions
		FROM `+tableEmbeddingCache+`
		WHERE content_hash = ?
	`, hash)
	var blob []byte
	var dims int
	if err := row.Scan(&blob, &dims); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	emb := bytesToEmbed(blob)
	if dims > 0 && len(emb) != dims {
		return nil, false, nil
	}
	if expected := int(s.runtime.Dimensions.Load()); expected > 0 && len(emb) != expected {
		return nil, false, nil
	}
	if s.runtime.Dimensions.Load() == 0 && len(emb) > 0 {
		s.runtime.Dimensions.Store(int64(len(emb)))
	}
	return emb, true, nil
}

// CacheEmbedding stores an embedding keyed by content hash for reuse across notes.
func (s *Store) CacheEmbedding(ctx context.Context, hash string, emb embeddings.Embedding) error {
	ctx = indexingperf.WithOp(ctx, "noteemb.cache_embedding")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return s.cacheEmbeddingLocked(ctx, hash, emb)
	})
}

func (s *Store) cacheEmbeddingLocked(ctx context.Context, hash string, emb embeddings.Embedding) error {
	if strings.TrimSpace(hash) == "" || len(emb) == 0 {
		return nil
	}
	norm := math.Sqrt(dotFloat64(emb, emb))
	now := time.Now().Unix()
	_, err := execWithRetry(ctx, s.db, `
		INSERT OR IGNORE INTO `+tableEmbeddingCache+` (content_hash, embedding, norm, dimensions, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, hash, embedToBytes(emb), norm, len(emb), now)
	if err == nil && s.runtime.Dimensions.Load() == 0 {
		s.runtime.Dimensions.Store(int64(len(emb)))
	}
	return err
}

// PruneStaleCache removes embedding cache entries that are no longer referenced
// by any chunk. Only prunes when orphaned entries exceed the threshold
// fraction of total cache entries.
// Returns the number of cache entries pruned.
func (s *Store) PruneStaleCache(ctx context.Context, threshold float64) (int, error) {
	ctx = indexingperf.WithOp(ctx, "noteemb.prune_stale_cache")
	var total, orphaned int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableEmbeddingCache).Scan(&total); err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}

	// Count orphaned cache entries (not referenced by any chunk).
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM `+tableEmbeddingCache+`
		WHERE content_hash NOT IN (
			SELECT content_hash FROM `+tableChunkEmbeddings+`
			WHERE content_hash IS NOT NULL AND content_hash != ''
		)
	`).Scan(&orphaned); err != nil {
		return 0, err
	}
	if orphaned == 0 {
		return 0, nil
	}

	// If orphaned fraction is below threshold, keep entries around for potential reuse.
	orphanedFraction := float64(orphaned) / float64(total)
	if threshold > 0 && orphanedFraction < threshold {
		return 0, nil // Cache entries preserved for branch-switching reuse
	}

	// Prune orphaned cache entries.
	pruned := 0
	err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		result, err := execWithRetry(ctx, db, `
			DELETE FROM `+tableEmbeddingCache+`
			WHERE content_hash NOT IN (
				SELECT content_hash FROM `+tableChunkEmbeddings+`
				WHERE content_hash IS NOT NULL AND content_hash != ''
			)
		`)
		if err != nil {
			return err
		}
		rows, _ := result.RowsAffected()
		pruned = int(rows)
		return nil
	})
	return pruned, err
}

// DeleteNote removes a note and cascades embeddings.
func (s *Store) DeleteNote(ctx context.Context, id embeddings.NoteID) error {
	ctx = indexingperf.WithOp(ctx, "noteemb.delete_note")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := execWithRetry(ctx, db, `DELETE FROM `+tableNotes+` WHERE note_id = ?`, string(id))
		return err
	})
}

// NoteCentroid returns the average chunk embedding for a note, when no note-level
// embedding is stored.
func (s *Store) NoteCentroid(ctx context.Context, id embeddings.NoteID) (embeddings.Embedding, bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cache.embedding
		FROM `+tableChunkEmbeddings+` c
		JOIN `+tableNotes+` n ON c.note_row_id = n.id
		JOIN `+tableEmbeddingCache+` cache ON c.content_hash = cache.content_hash
		WHERE n.note_id = ?
	`, string(id))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var sum embeddings.Embedding
	count := 0
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			return nil, false, err
		}
		emb := bytesToEmbed(blob)
		if len(emb) == 0 {
			continue
		}
		if sum == nil {
			sum = make(embeddings.Embedding, len(emb))
		}
		if len(sum) != len(emb) {
			continue
		}
		for i := range sum {
			sum[i] += emb[i]
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if count == 0 || sum == nil {
		return nil, false, nil
	}
	for i := range sum {
		sum[i] /= float32(count)
	}
	return sum, true, nil
}

func bulkUpsertEmbeddingCacheTx(ctx context.Context, tx *sql.Tx, table string, rows []embeddingCacheRow, now int64) error {
	if len(rows) == 0 {
		return nil
	}
	for batch := range slices.Chunk(rows, embeddingCacheBatchSize) {
		args := make([]any, 0, len(batch)*5)
		valueSQL := make([]string, 0, len(batch))
		for _, r := range batch {
			if strings.TrimSpace(r.hash) == "" || len(r.vec) == 0 {
				continue
			}
			norm := math.Sqrt(dotFloat64(r.vec, r.vec))
			valueSQL = append(valueSQL, "(?, ?, ?, ?, ?)")
			args = append(args, r.hash, embedToBytes(r.vec), norm, len(r.vec), now)
		}
		if len(valueSQL) == 0 {
			continue
		}
		stmt := fmt.Sprintf(`
			INSERT OR IGNORE INTO %s (content_hash, embedding, norm, dimensions, created_at)
			VALUES %s
		`, table, strings.Join(valueSQL, ","))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

func bulkUpsertNoteChunkRowsTx(ctx context.Context, tx *sql.Tx, rowID int64, rows []noteChunkRow, now int64) error {
	if len(rows) == 0 {
		return nil
	}
	for batch := range slices.Chunk(rows, noteChunkBatchSize) {
		args := make([]any, 0, len(batch)*9)
		valueSQL := make([]string, 0, len(batch))
		for _, r := range batch {
			if len(r.vec) == 0 {
				continue
			}
			valueSQL = append(valueSQL, "(?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, rowID, r.index, r.breadcrumb, r.heading, r.hash, embedToBytes(r.vec), r.norm, len(r.vec), now)
		}
		if len(valueSQL) == 0 {
			continue
		}
		stmt := fmt.Sprintf(`
			INSERT INTO %s (note_row_id, chunk_index, breadcrumb, heading, content_hash, embedding, norm, dimensions, created_at)
			VALUES %s
			ON CONFLICT(note_row_id, chunk_index) DO UPDATE SET
				breadcrumb = excluded.breadcrumb,
				heading = excluded.heading,
				content_hash = excluded.content_hash,
				embedding = excluded.embedding,
				norm = excluded.norm,
				dimensions = excluded.dimensions,
				created_at = excluded.created_at
		`, tableChunkEmbeddings, strings.Join(valueSQL, ","))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

// SearchChunksByVector performs cosine similarity search across chunks using sqlite-vec.
func (s *Store) SearchChunksByVector(ctx context.Context, query embeddings.Embedding, k int) ([]embeddings.SimilarChunk, int, error) {
	return s.searchChunksByVector(ctx, query, k, nil)
}

func (s *Store) searchChunksByVector(ctx context.Context, query embeddings.Embedding, k int, allow map[string]struct{}) ([]embeddings.SimilarChunk, int, error) {
	if len(query) == 0 {
		return nil, 0, errors.New("query embedding is empty")
	}
	if math.Sqrt(dotFloat64(query, query)) == 0 {
		return nil, 0, errors.New("query embedding has zero norm")
	}
	queryDims := len(query)
	queryBlob := embedToBytes(query)
	vecTable, err := s.ensureChunkVecMirror(ctx, queryDims)
	if err != nil {
		return nil, 0, err
	}

	allowedRowIDs, err := s.allowedNoteRowIDs(ctx, allow)
	if err != nil {
		return nil, 0, err
	}
	if allow != nil && len(allowedRowIDs) == 0 {
		return nil, 0, nil
	}

	querySQL := `
		SELECT c.id, c.note_row_id, c.chunk_index, 1.0 - vec_distance_cosine(v.embedding, ?) AS score
		FROM ` + vecTable + ` v
		JOIN ` + tableChunkEmbeddings + ` c ON c.id = v.chunk_id
		JOIN ` + tableNotes + ` n ON n.id = c.note_row_id
		WHERE c.dimensions = ?`
	args := make([]any, 0, 2+len(allowedRowIDs))
	args = append(args, queryBlob, queryDims)
	if gen, ok, err := s.currentVisibleGeneration(ctx); err != nil {
		return nil, 0, err
	} else if ok {
		querySQL += ` AND n.sync_generation = ?`
		args = append(args, gen)
	}
	if len(allowedRowIDs) > 0 {
		querySQL += ` AND c.note_row_id IN (` + makePlaceholders(len(allowedRowIDs)) + `)`
		for _, id := range allowedRowIDs {
			args = append(args, id)
		}
	}
	querySQL += ` ORDER BY score DESC`
	if k > 0 {
		querySQL += ` LIMIT ?`
		args = append(args, k)
	}

	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	capHint := 0
	if k > 0 {
		capHint = k
	}
	scored := make([]scoredChunk, 0, capHint)
	for rows.Next() {
		var sc scoredChunk
		if err := rows.Scan(&sc.chunkID, &sc.noteRowID, &sc.index, &sc.score); err != nil {
			return nil, 0, err
		}
		scored = append(scored, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if k > 0 && len(scored) > k {
		scored = scored[:k]
	}

	chunkMeta, err := s.chunkMetadata(ctx, scored)
	if err != nil {
		return nil, 0, err
	}

	out := make([]embeddings.SimilarChunk, 0, len(scored))
	for _, sc := range scored {
		meta, ok := chunkMeta[sc.chunkID]
		if !ok {
			continue
		}
		out = append(out, embeddings.SimilarChunk{
			NoteID:     embeddings.NoteID(meta.noteID),
			Title:      meta.title,
			ChunkIndex: meta.index,
			Breadcrumb: meta.breadcrumb,
			Heading:    meta.heading,
			Score:      sc.score,
		})
	}
	return out, 0, nil
}

// SearchNotesByText performs a lightweight lexical search over note titles.
func (s *Store) SearchNotesByText(ctx context.Context, query string, k int) ([]embeddings.SimilarNote, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if k <= 0 {
		k = 25
	}
	pat := "%" + strings.ToLower(q) + "%"

	querySQL := `
		SELECT note_id, title
		FROM ` + tableNotes + `
		WHERE lower(title) LIKE ?`
	args := []any{pat}
	if gen, ok, err := s.currentVisibleGeneration(ctx); err != nil {
		return nil, err
	} else if ok {
		querySQL += ` AND sync_generation = ?`
		args = append(args, gen)
	}
	querySQL += ` LIMIT ?`
	args = append(args, k*2)
	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []embeddings.SimilarNote
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		score := lexicalScore(title, q)
		out = append(out, embeddings.SimilarNote{
			ID:    embeddings.NoteID(id),
			Title: title,
			Score: score,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

func (s *Store) allowedNoteRowIDs(ctx context.Context, allow map[string]struct{}) ([]int64, error) {
	if allow == nil {
		return nil, nil
	}
	if len(allow) == 0 {
		return []int64{}, nil
	}
	ids := make([]string, 0, len(allow))
	for id := range allow {
		ids = append(ids, id)
	}
	const batchSize = 500
	var rowIDs []int64
	for batch := range slices.Chunk(ids, batchSize) {
		args := make([]any, 0, len(batch))
		for _, id := range batch {
			args = append(args, id)
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT id
			FROM `+tableNotes+`
			WHERE note_id IN (`+makePlaceholders(len(batch))+`)
		`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var rowID int64
			if err := rows.Scan(&rowID); err != nil {
				_ = rows.Close()
				return nil, err
			}
			rowIDs = append(rowIDs, rowID)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return rowIDs, nil
}

func (s *Store) currentVisibleGeneration(ctx context.Context) (int64, bool, error) {
	var gen int64
	err := s.db.QueryRowContext(ctx, `SELECT IFNULL(visible_generation, 0) FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&gen)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return gen, gen > 0, nil
}

// VisibleGeneration returns the committed generation used by search queries.
// An in-progress sync changes sync_generation first and becomes visible only
// when CommitSyncGeneration publishes it.
func (s *Store) VisibleGeneration(ctx context.Context) (int64, bool, error) {
	return s.currentVisibleGeneration(ctx)
}

type chunkMetadataRow struct {
	noteID     string
	title      string
	index      int
	breadcrumb string
	heading    string
}

func (s *Store) chunkMetadata(ctx context.Context, scored []scoredChunk) (map[int64]chunkMetadataRow, error) {
	if len(scored) == 0 {
		return map[int64]chunkMetadataRow{}, nil
	}
	ids := make([]int64, 0, len(scored))
	for _, c := range scored {
		ids = append(ids, c.chunkID)
	}

	meta := make(map[int64]chunkMetadataRow, len(ids))
	const batchSize = 500
	for batch := range slices.Chunk(ids, batchSize) {
		args := make([]any, 0, len(batch))
		for _, id := range batch {
			args = append(args, id)
		}
		query := `
			SELECT c.id, n.note_id, n.title, c.chunk_index, c.breadcrumb, c.heading
			FROM ` + tableChunkEmbeddings + ` c
			JOIN ` + tableNotes + ` n ON c.note_row_id = n.id
			WHERE c.id IN (` + makePlaceholders(len(batch)) + `)
		`
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var (
				chunkID    int64
				noteID     string
				title      string
				idx        int
				breadcrumb string
				heading    string
			)
			if err := rows.Scan(&chunkID, &noteID, &title, &idx, &breadcrumb, &heading); err != nil {
				_ = rows.Close()
				return nil, err
			}
			meta[chunkID] = chunkMetadataRow{
				noteID:     noteID,
				title:      title,
				index:      idx,
				breadcrumb: breadcrumb,
				heading:    heading,
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return meta, nil
}

type scoredChunk struct {
	chunkID   int64
	noteRowID int64
	index     int
	score     float64
}

func lexicalScore(title, query string) float64 {
	titleLower := strings.ToLower(strings.TrimSpace(title))
	queryLower := strings.ToLower(strings.TrimSpace(query))
	if queryLower == "" || titleLower == "" {
		return 0
	}
	if titleLower == queryLower {
		return 1.0
	}
	if strings.HasPrefix(titleLower, queryLower) {
		return 0.8
	}
	if strings.Contains(titleLower, queryLower) {
		return 0.6
	}
	return 0
}

func makePlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	holders := make([]string, n)
	for i := range holders {
		holders[i] = "?"
	}
	return strings.Join(holders, ",")
}

func embedToBytes(e embeddings.Embedding) []byte {
	b := make([]byte, 4*len(e))
	for i, f := range e {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func bytesToEmbed(b []byte) embeddings.Embedding {
	if len(b)%4 != 0 {
		return nil
	}
	n := len(b) / 4
	e := make(embeddings.Embedding, n)
	for i := 0; i < n; i++ {
		e[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return e
}

func dotFloat64(a, b embeddings.Embedding) float64 {
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

var _ embeddings.Index = (*Store)(nil)
