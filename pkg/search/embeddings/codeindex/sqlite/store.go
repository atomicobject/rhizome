package sqlite

// Docs:
// - [Embeddings - SQLite store + locking hazards](docs/reference/analysis/Embeddings - SQLite store + locking hazards.md)

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/internal/sqlstore"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration/domains"

	_ "github.com/mattn/go-sqlite3"
)

// Store implements codeindex.Index backed by SQLite.
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

type embeddingCacheRow struct {
	hash string
	vec  embeddings.Embedding
}

type codeChunkRow struct {
	index       int
	granularity string
	breadcrumb  string
	heading     string
	startByte   int
	endByte     int
	startLine   int
	endLine     int
	hash        string
	vec         embeddings.Embedding
	norm        float64
}

type codeFTSRow struct {
	anchorID    string
	chunkIndex  int
	path        string
	symbol      string
	fqn         string
	kind        string
	granularity string
	breadcrumb  string
	heading     string
	body        string
}

func (s *Store) withWrite(ctx context.Context, fn func(ctx context.Context, db *sql.DB) error) error {
	return s.runtime.WithWrite(ctx, s.db, fn)
}

func (s *Store) withWriteTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return s.runtime.WithWriteTx(ctx, s.db, sqliteutil.ExecTxWithRetry, fn)
}

const (
	currentSchemaVersion = 12
	tableIndexMeta       = "code_index_meta"
	tableItems           = "code_items"
	tableItemEmbeddings  = "code_item_embeddings"
	tableChunkEmbeddings = "code_chunk_embeddings"
	tableChunkFTS        = "code_chunk_fts"
	tableEmbeddingCache  = "code_embedding_cache"
	vecChunkTablePrefix  = "code_chunk_embeddings_vec_d"
	vecItemTablePrefix   = "code_item_embeddings_vec_d"

	// DefaultStaleItemThreshold is the fraction of stale items (not seen in current sync)
	// above which we prune them. Below this threshold, items are kept to avoid re-embedding
	// costs when switching branches. 0.3 means we keep stale items if they're <30% of total.
	DefaultStaleItemThreshold = 0.3

	codeChunkBatchSize      = 20
	embeddingCacheBatchSize = 50
	hashLookupBatchSize     = 100
)

// OpenOptions controls SQLite store open behavior.
type OpenOptions struct {
	// TxLockMode sets sqlite _txlock mode (supported: immediate, exclusive).
	TxLockMode string
	// Pool controls SQLite connection pooling.
	Pool sqliteutil.Options
}

// Open opens (or creates) a SQLite code embeddings index at path with expected dimensions.
func Open(path string, dimensions int) (*Store, error) {
	return OpenWithOptions(path, dimensions, OpenOptions{})
}

// OpenWithOptions opens (or creates) a SQLite code embeddings index at path with expected dimensions.
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
		runtime: sqlstore.NewRuntime("code-embeddings", closeFn),
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

// Close closes the underlying DB handle.
func (s *Store) Close() error {
	return s.runtime.Close(s.db)
}

// EnsureSchema creates tables and indices if needed.
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
		`CREATE TABLE IF NOT EXISTS ` + tableChunkEmbeddings + ` (
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
		`CREATE TABLE IF NOT EXISTS ` + tableEmbeddingCache + ` (
                content_hash TEXT PRIMARY KEY,
                embedding    BLOB NOT NULL,
                norm         REAL NOT NULL DEFAULT 0,
                dimensions   INTEGER NOT NULL,
                created_at   INTEGER NOT NULL
        );`,
		`CREATE INDEX IF NOT EXISTS idx_code_chunk_embeddings_item_row_id ON ` + tableChunkEmbeddings + `(item_row_id);`,
		`CREATE INDEX IF NOT EXISTS idx_code_chunk_embeddings_granularity_lower ON ` + tableChunkEmbeddings + `(lower(granularity));`,
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
	ctx = indexingperf.WithOp(ctx, "codeemb.ensure_schema")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return s.ensureSchemaNoLock(ctx, db)
	})
}

func chunkVecTableName(dims int) string {
	return fmt.Sprintf("%s%d", vecChunkTablePrefix, dims)
}

func itemVecTableName(dims int) string {
	return fmt.Sprintf("%s%d", vecItemTablePrefix, dims)
}

func (s *Store) ensureVecMirrors(ctx context.Context, dims int) error {
	if dims <= 0 {
		return fmt.Errorf("invalid vec dimensions: %d", dims)
	}
	if _, ok := s.runtime.VecReady.Load(dims); ok {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "codeemb.ensure_vec_mirrors")

	chunkTable := chunkVecTableName(dims)
	itemTable := itemVecTableName(dims)
	chunkInsertTrigger := fmt.Sprintf("trg_%s_ins", chunkTable)
	chunkUpdateTrigger := fmt.Sprintf("trg_%s_upd", chunkTable)
	chunkDeleteTrigger := fmt.Sprintf("trg_%s_del", chunkTable)
	itemInsertTrigger := fmt.Sprintf("trg_%s_ins", itemTable)
	itemUpdateTrigger := fmt.Sprintf("trg_%s_upd", itemTable)
	itemDeleteTrigger := fmt.Sprintf("trg_%s_del", itemTable)
	blobBytes := dims * 4

	if err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		stmts := []string{
			fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(chunk_id integer primary key, embedding float[%d]);`, chunkTable, dims),
			fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(item_id integer primary key, embedding float[%d]);`, itemTable, dims),
			fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %s AFTER INSERT ON %s
				WHEN NEW.dimensions = %d AND length(NEW.embedding) = %d
				BEGIN
					INSERT INTO %s(chunk_id, embedding) VALUES (NEW.id, NEW.embedding);
				END;`, chunkInsertTrigger, tableChunkEmbeddings, dims, blobBytes, chunkTable),
			fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %s AFTER UPDATE OF embedding, dimensions ON %s
				BEGIN
					DELETE FROM %s WHERE chunk_id = OLD.id;
					INSERT INTO %s(chunk_id, embedding)
					SELECT NEW.id, NEW.embedding
					WHERE NEW.dimensions = %d AND length(NEW.embedding) = %d;
				END;`, chunkUpdateTrigger, tableChunkEmbeddings, chunkTable, chunkTable, dims, blobBytes),
			fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %s AFTER DELETE ON %s
				BEGIN
					DELETE FROM %s WHERE chunk_id = OLD.id;
				END;`, chunkDeleteTrigger, tableChunkEmbeddings, chunkTable),
			fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %s AFTER INSERT ON %s
				WHEN NEW.dimensions = %d AND length(NEW.embedding) = %d
				BEGIN
					INSERT INTO %s(item_id, embedding) VALUES (NEW.id, NEW.embedding);
				END;`, itemInsertTrigger, tableItemEmbeddings, dims, blobBytes, itemTable),
			fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %s AFTER UPDATE OF embedding, dimensions ON %s
				BEGIN
					DELETE FROM %s WHERE item_id = OLD.id;
					INSERT INTO %s(item_id, embedding)
					SELECT NEW.id, NEW.embedding
					WHERE NEW.dimensions = %d AND length(NEW.embedding) = %d;
				END;`, itemUpdateTrigger, tableItemEmbeddings, itemTable, itemTable, dims, blobBytes),
			fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %s AFTER DELETE ON %s
				BEGIN
					DELETE FROM %s WHERE item_id = OLD.id;
				END;`, itemDeleteTrigger, tableItemEmbeddings, itemTable),
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM `+chunkTable); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s(chunk_id, embedding)
			SELECT id, embedding
			FROM %s
			WHERE dimensions = ? AND length(embedding) = ?
		`, chunkTable, tableChunkEmbeddings), dims, blobBytes); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM `+itemTable); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s(item_id, embedding)
			SELECT id, embedding
			FROM %s
			WHERE dimensions = ? AND length(embedding) = ?
		`, itemTable, tableItemEmbeddings), dims, blobBytes); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	s.runtime.VecReady.Store(dims, struct{}{})
	return nil
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
		if needs, checkErr := s.needsSchemaRebuild(ctx); checkErr == nil && needs {
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
	plan := domains.CodeEmbeddingsPlan(currentSchemaVersion, s.migrateDomainStepTx, nil)
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

func (s *Store) migrateDomainStepTx(ctx context.Context, tx *sql.Tx, from, to int) error {
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
		if _, err := tx.ExecContext(ctx, `ALTER TABLE `+tableIndexMeta+` ADD COLUMN fingerprint_version INTEGER NOT NULL DEFAULT 0`); err != nil {
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
			`DROP TABLE IF EXISTS ` + tableChunkEmbeddings + `;`,
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
			`CREATE TABLE IF NOT EXISTS ` + tableChunkEmbeddings + ` (
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
			`CREATE INDEX IF NOT EXISTS idx_code_chunk_embeddings_item_row_id ON ` + tableChunkEmbeddings + `(item_row_id);`,
			`CREATE INDEX IF NOT EXISTS idx_code_chunk_embeddings_granularity_lower ON ` + tableChunkEmbeddings + `(lower(granularity));`,
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
			`DROP TABLE IF EXISTS ` + tableChunkEmbeddings + `;`,
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
			`CREATE TABLE IF NOT EXISTS ` + tableChunkEmbeddings + ` (
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
			`CREATE INDEX IF NOT EXISTS idx_code_chunk_embeddings_item_row_id ON ` + tableChunkEmbeddings + `(item_row_id);`,
			`CREATE INDEX IF NOT EXISTS idx_code_chunk_embeddings_granularity_lower ON ` + tableChunkEmbeddings + `(lower(granularity));`,
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
		if err := addColumnIfMissingTx(ctx, tx, tableIndexMeta, "source_high_water", "INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported code embeddings migration %d->%d", from, to)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE `+tableIndexMeta+` SET schema_version = ? WHERE id = 1`, to); err != nil {
		return err
	}
	return nil
}

// ResetDomain drops and recreates the code embeddings tables (`code_*`). This does not affect
// code intel tables nor semantic note embeddings in a unified DB.
func (s *Store) ResetDomain(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.reset_domain")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return s.resetDomainNoLock(ctx, true)
	})
}

// ResetDomainPreservingCache drops code-embedding tables but keeps the embedding cache.
// This allows rebuilds to reuse embeddings by content hash when provider/model match.
func (s *Store) ResetDomainPreservingCache(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.reset_domain_preserve_cache")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return s.resetDomainNoLock(ctx, false)
	})
}

func (s *Store) resetDomainNoLock(ctx context.Context, dropCache bool) error {
	dropViews := []string{tableIndexMeta, tableItems, tableItemEmbeddings, tableChunkEmbeddings, tableChunkFTS, tableEmbeddingCache}
	if !dropCache {
		dropViews = dropViews[:len(dropViews)-1]
	}
	for _, name := range dropViews {
		if err := dropViewIfExists(ctx, s.db, name); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `DROP TABLE IF EXISTS `+tableChunkFTS); err != nil {
		return err
	}
	for _, name := range []string{
		"idx_code_item_embeddings_item_row_id",
		"idx_code_chunk_embeddings_item_row_id",
	} {
		if _, err := s.db.ExecContext(ctx, `DROP INDEX IF EXISTS `+name); err != nil {
			return err
		}
	}
	dropTables := []string{tableChunkEmbeddings, tableItemEmbeddings, tableItems, tableIndexMeta}
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
	if err := dropVecTablesByPrefix(ctx, s.db, vecItemTablePrefix); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM rzm_migration_state WHERE domain = ?`, string(migration.DomainCodeEmbeddings)); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return err
		}
	}
	s.runtime.VecReady = sync.Map{}
	return nil
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

func (s *Store) needsSchemaRebuild(ctx context.Context) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	required := map[string][]string{
		tableIndexMeta:       {"id", "provider", "model", "dimensions", "schema_version", "created_at", "fingerprint_version", "last_sync", "sync_generation", "visible_generation"},
		tableItems:           {"id", "anchor_row_id", "fingerprint", "updated_at", "sync_generation"},
		tableItemEmbeddings:  {"id", "item_row_id", "content_hash", "embedding", "norm", "dimensions", "created_at"},
		tableChunkEmbeddings: {"id", "item_row_id", "chunk_index", "granularity", "content_hash", "embedding", "norm", "dimensions", "created_at"},
		tableEmbeddingCache:  {"content_hash", "embedding", "norm", "dimensions", "created_at"},
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

	hasItemNorm, err := tableHasColumn(ctx, s.db, tableItemEmbeddings, "norm")
	if err != nil {
		return err
	}
	hasChunkNorm, err := tableHasColumn(ctx, s.db, tableChunkEmbeddings, "norm")
	if err != nil {
		return err
	}
	hasCacheNorm, err := tableHasColumn(ctx, s.db, tableEmbeddingCache, "norm")
	if err != nil {
		return err
	}

	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		type backupTables struct {
			meta  string
			items string
			itemE string
			chunk string
			cache string
		}
		b := backupTables{
			meta:  "temp_code_backup_meta",
			items: "temp_code_backup_items",
			itemE: "temp_code_backup_item_embeddings",
			chunk: "temp_code_backup_chunk_embeddings",
			cache: "temp_code_backup_embedding_cache",
		}

		for _, name := range []string{b.meta, b.items, b.itemE, b.chunk, b.cache} {
			_, _ = s.db.ExecContext(ctx, `DROP TABLE IF EXISTS `+name)
		}
		defer func() {
			for _, name := range []string{b.meta, b.items, b.itemE, b.chunk, b.cache} {
				_, _ = s.db.ExecContext(ctx, `DROP TABLE IF EXISTS `+name)
			}
		}()

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
			if _, err := s.db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}

		hasMetaTable, err := tableExists(ctx, s.db, tableIndexMeta)
		if err != nil {
			return err
		}
		hasItemsTable, err := tableExists(ctx, s.db, tableItems)
		if err != nil {
			return err
		}
		hasItemEmbeddingsTable, err := tableExists(ctx, s.db, tableItemEmbeddings)
		if err != nil {
			return err
		}
		hasChunkEmbeddingsTable, err := tableExists(ctx, s.db, tableChunkEmbeddings)
		if err != nil {
			return err
		}
		hasEmbeddingCacheTable, err := tableExists(ctx, s.db, tableEmbeddingCache)
		if err != nil {
			return err
		}

		// Check if optional columns exist for graceful migration from older codeemb schemas.
		hasMetaFingerprintVersion, _ := tableHasColumn(ctx, s.db, tableIndexMeta, "fingerprint_version")
		hasMetaSyncGen, _ := tableHasColumn(ctx, s.db, tableIndexMeta, "sync_generation")
		hasMetaSourceHighWater, _ := tableHasColumn(ctx, s.db, tableIndexMeta, "source_high_water")
		hasMetaVisibleGen, _ := tableHasColumn(ctx, s.db, tableIndexMeta, "visible_generation")
		hasItemSyncGen, _ := tableHasColumn(ctx, s.db, tableItems, "sync_generation")
		hasAnchorRowID, _ := tableHasColumn(ctx, s.db, tableItems, "anchor_row_id")
		hasItemAnchorID, _ := tableHasColumn(ctx, s.db, tableItems, "anchor_id")
		hasItemLang, _ := tableHasColumn(ctx, s.db, tableItems, "lang")
		hasItemKind, _ := tableHasColumn(ctx, s.db, tableItems, "kind")
		hasItemPath, _ := tableHasColumn(ctx, s.db, tableItems, "path")
		hasItemSymbol, _ := tableHasColumn(ctx, s.db, tableItems, "symbol")
		hasItemFQN, _ := tableHasColumn(ctx, s.db, tableItems, "fqn")

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
			if _, err := s.db.ExecContext(ctx, `
		INSERT INTO `+b.meta+` (provider, model, dimensions, schema_version, created_at, fingerprint_version, last_sync, source_high_water, sync_generation, visible_generation)
		SELECT provider, model, dimensions, schema_version, created_at, `+metaFingerprintVersionSelect+`, last_sync, `+metaSourceHighWaterSelect+`, `+metaSyncGenSelect+`, `+metaVisibleGenSelect+`
		FROM `+tableIndexMeta+`
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
			if _, err := s.db.ExecContext(ctx, itemBackupQuery); err != nil {
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
			if _, err := s.db.ExecContext(ctx, itemEmbBackupQuery); err != nil {
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
		FROM ` + tableChunkEmbeddings + ` c
		JOIN ` + tableItems + ` i ON i.id = c.item_row_id
	`
		if hasAnchorRowID && hasItemAnchorID {
			chunkBackupQuery = `
		INSERT INTO ` + b.chunk + ` (anchor_id, chunk_index, granularity, breadcrumb, heading, start_byte, end_byte, start_line, end_line, content_hash, embedding, norm, dimensions, created_at)
		SELECT COALESCE(a.anchor_id, i.anchor_id), c.chunk_index, c.granularity, c.breadcrumb, c.heading, c.start_byte, c.end_byte, c.start_line, c.end_line, c.content_hash, c.embedding, ` + chunkNormSelect + `, c.dimensions, c.created_at
		FROM ` + tableChunkEmbeddings + ` c
		JOIN ` + tableItems + ` i ON i.id = c.item_row_id
		LEFT JOIN intel_code_anchors a ON a.id = i.anchor_row_id
	`
		} else if hasAnchorRowID {
			chunkBackupQuery = `
		INSERT INTO ` + b.chunk + ` (anchor_id, chunk_index, granularity, breadcrumb, heading, start_byte, end_byte, start_line, end_line, content_hash, embedding, norm, dimensions, created_at)
		SELECT a.anchor_id, c.chunk_index, c.granularity, c.breadcrumb, c.heading, c.start_byte, c.end_byte, c.start_line, c.end_line, c.content_hash, c.embedding, ` + chunkNormSelect + `, c.dimensions, c.created_at
		FROM ` + tableChunkEmbeddings + ` c
		JOIN ` + tableItems + ` i ON i.id = c.item_row_id
		JOIN intel_code_anchors a ON a.id = i.anchor_row_id
	`
		}
		if hasItemsTable && hasChunkEmbeddingsTable {
			if _, err := s.db.ExecContext(ctx, chunkBackupQuery); err != nil {
				return err
			}
		}
		cacheNormSelect := "0"
		if hasCacheNorm {
			cacheNormSelect = "COALESCE(norm, 0)"
		}
		if hasEmbeddingCacheTable {
			if _, err := s.db.ExecContext(ctx, `
		INSERT INTO `+b.cache+` (content_hash, embedding, norm, dimensions, created_at)
		SELECT content_hash, embedding, `+cacheNormSelect+`, dimensions, created_at
		FROM `+tableEmbeddingCache+`
	`); err != nil {
				return err
			}
		}

		if err := s.resetDomainNoLock(ctx, true); err != nil {
			return err
		}
		if err := s.ensureSchemaNoLock(ctx, db); err != nil {
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
			if _, err := s.db.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		hasAnchorStartByte, err := tableHasColumn(ctx, s.db, "intel_code_anchors", "start_byte")
		if err != nil {
			return err
		}
		hasAnchorFingerprint, err := tableHasColumn(ctx, s.db, "intel_code_anchors", "fingerprint")
		if err != nil {
			return err
		}
		hasAnchorUpdatedAt, err := tableHasColumn(ctx, s.db, "intel_code_anchors", "updated_at")
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
		if _, err := s.db.ExecContext(ctx, anchorRestoreQuery); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO `+tableItems+` (anchor_row_id, fingerprint, updated_at, sync_generation)
		SELECT a.id, COALESCE(b.fingerprint, ''), COALESCE(b.updated_at, 0), COALESCE(b.sync_generation, 0)
		FROM `+b.items+` b
		JOIN intel_code_anchors a ON a.anchor_id = b.anchor_id
		WHERE b.anchor_id IS NOT NULL AND b.anchor_id != ''
		  AND b.path IS NOT NULL AND b.path != ''
	`); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `
	INSERT OR REPLACE INTO `+tableItemEmbeddings+` (item_row_id, content_hash, embedding, norm, dimensions, created_at)
		SELECT i.id, b.content_hash, b.embedding, COALESCE(b.norm, 0), b.dimensions, b.created_at
		FROM `+b.itemE+` b
		JOIN intel_code_anchors a ON a.anchor_id = b.anchor_id
		JOIN `+tableItems+` i ON i.anchor_row_id = a.id
	`); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO `+tableChunkEmbeddings+` (
			item_row_id, chunk_index, granularity, breadcrumb, heading, start_byte, end_byte, start_line, end_line, content_hash, embedding, norm, dimensions, created_at
		)
		SELECT i.id, b.chunk_index, b.granularity, b.breadcrumb, b.heading, b.start_byte, b.end_byte, b.start_line, b.end_line, b.content_hash, b.embedding, COALESCE(b.norm, 0), b.dimensions, b.created_at
		FROM `+b.chunk+` b
		JOIN intel_code_anchors a ON a.anchor_id = b.anchor_id
		JOIN `+tableItems+` i ON i.anchor_row_id = a.id
	`); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO `+tableEmbeddingCache+` (content_hash, embedding, norm, dimensions, created_at)
		SELECT content_hash, embedding, COALESCE(norm, 0), dimensions, created_at
		FROM `+b.cache+`
		WHERE content_hash IS NOT NULL AND content_hash != ''
	`); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO `+tableEmbeddingCache+` (content_hash, embedding, norm, dimensions, created_at)
		SELECT content_hash, embedding, norm, dimensions, created_at
		FROM (
			SELECT content_hash, embedding, COALESCE(norm, 0) AS norm, dimensions, created_at FROM `+tableItemEmbeddings+`
			UNION ALL
			SELECT content_hash, embedding, COALESCE(norm, 0) AS norm, dimensions, created_at FROM `+tableChunkEmbeddings+`
		)
		WHERE content_hash IS NOT NULL AND content_hash != ''
	`); err != nil {
			return err
		}

		if _, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO `+tableIndexMeta+` (id, provider, model, dimensions, schema_version, created_at, fingerprint_version, last_sync, source_high_water, sync_generation, visible_generation)
		SELECT 1, provider, model, dimensions, ?, COALESCE(created_at, 0), COALESCE(fingerprint_version, 0), COALESCE(last_sync, 0), COALESCE(source_high_water, 0), COALESCE(sync_generation, 0), COALESCE(visible_generation, sync_generation, 0)
		FROM `+b.meta+`
		LIMIT 1
	`, currentSchemaVersion); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `
		INSERT INTO rzm_migration_state(domain, version, dirty, updated_at)
		VALUES (?, ?, 0, ?)
		ON CONFLICT(domain) DO UPDATE SET
			version = excluded.version,
			dirty = 0,
			updated_at = excluded.updated_at
	`, string(migration.DomainCodeEmbeddings), currentSchemaVersion, time.Now().Unix()); err != nil {
			return err
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

type tableColumnQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type statementPreparer interface {
	PrepareContext(context.Context, string) (*sql.Stmt, error)
}

type anchorUpsertPreparer interface {
	tableColumnQueryer
	statementPreparer
}

func tableHasColumn(ctx context.Context, db tableColumnQueryer, table, column string) (bool, error) {
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

func prepareIntelAnchorUpsertStmt(ctx context.Context, db anchorUpsertPreparer) (*sql.Stmt, error) {
	hasStartByte, err := tableHasColumn(ctx, db, "intel_code_anchors", "start_byte")
	if err != nil {
		return nil, err
	}
	hasFingerprint, err := tableHasColumn(ctx, db, "intel_code_anchors", "fingerprint")
	if err != nil {
		return nil, err
	}
	hasUpdatedAt, err := tableHasColumn(ctx, db, "intel_code_anchors", "updated_at")
	if err != nil {
		return nil, err
	}

	cols := []string{"anchor_id", "lang", "kind", "path", "symbol", "fqn"}
	vals := []string{"?", "?", "?", "?", "?", "?"}
	updates := []string{
		"lang = excluded.lang",
		"kind = excluded.kind",
		"path = excluded.path",
		"symbol = excluded.symbol",
		"fqn = excluded.fqn",
	}
	if hasStartByte {
		cols = append(cols, "start_byte", "end_byte", "start_line", "end_line")
		vals = append(vals, "0", "0", "0", "0")
	}
	if hasFingerprint {
		cols = append(cols, "fingerprint")
		vals = append(vals, "'codeemb'")
	}
	if hasUpdatedAt {
		cols = append(cols, "updated_at")
		vals = append(vals, "0")
	}
	query := fmt.Sprintf(`
		INSERT INTO intel_code_anchors (%s)
		VALUES (%s)
		ON CONFLICT(anchor_id) DO UPDATE SET
			%s
	`, strings.Join(cols, ", "), strings.Join(vals, ", "), strings.Join(updates, ",\n\t\t\t"))
	return db.PrepareContext(ctx, query)
}

type anchorRowIDQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func selectCodeItemRowIDsByAnchorIDs(ctx context.Context, q anchorRowIDQueryer, anchorIDs []codeindex.AnchorID) (map[string]int64, error) {
	anchorIDs = dedupeAnchorIDs(anchorIDs)
	if len(anchorIDs) == 0 {
		return map[string]int64{}, nil
	}
	out := make(map[string]int64, len(anchorIDs))
	const batchSize = 50
	for batch := range slices.Chunk(anchorIDs, batchSize) {
		holders := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, id := range batch {
			holders[i] = "?"
			args[i] = string(id)
		}
		rows, err := q.QueryContext(ctx, fmt.Sprintf(`
			SELECT a.anchor_id, i.id
			FROM `+tableItems+` i
			JOIN intel_code_anchors a ON a.id = i.anchor_row_id
			WHERE a.anchor_id IN (%s)
		`, strings.Join(holders, ",")), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var anchorID string
			var rowID int64
			if err := rows.Scan(&anchorID, &rowID); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[anchorID] = rowID
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

func dedupeAnchorIDs(anchorIDs []codeindex.AnchorID) []codeindex.AnchorID {
	if len(anchorIDs) == 0 {
		return nil
	}
	seen := make(map[codeindex.AnchorID]struct{}, len(anchorIDs))
	out := make([]codeindex.AnchorID, 0, len(anchorIDs))
	for _, id := range anchorIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// ValidateOrInitMetadata ensures stored metadata matches expected provider/model/dimensions.
func (s *Store) ValidateOrInitMetadata(ctx context.Context, meta embeddings.IndexMetadata) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.validate_metadata")
	current, ok, err := s.Metadata(ctx)
	if err != nil {
		return err
	}

	resetAndInit := func(reason string) error {
		if err := s.ResetDomain(ctx); err != nil {
			return fmt.Errorf("%s: reset code index domain: %w", reason, err)
		}
		if err := s.EnsureSchema(ctx); err != nil {
			return fmt.Errorf("%s: recreate code index schema: %w", reason, err)
		}
		if err := s.initMetadata(ctx, meta); err != nil {
			return err
		}
		return nil
	}

	if !ok {
		return s.initMetadata(ctx, meta)
	}
	if current.SchemaVersion > currentSchemaVersion {
		return &migration.ErrFutureSchema{
			Domain:    migration.DomainCodeEmbeddings,
			Current:   current.SchemaVersion,
			Supported: currentSchemaVersion,
		}
	}
	// A stored dimensions=0 row is a placeholder from a prior call whose provider
	// hadn't yet learned its output size (e.g. Voyage discovers dims lazily from
	// the first embedding response). Heal it in place — per-chunk dimensions are
	// authoritative and chunks with mismatched dims are filtered at read time.
	// Reset only on a true conflict between two known values.
	if current.Dimensions == 0 && meta.Dimensions > 0 {
		if err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
			_, err := execWithRetry(ctx, db, `UPDATE `+tableIndexMeta+` SET dimensions = ? WHERE id = 1`, meta.Dimensions)
			if err == nil {
				s.runtime.Dimensions.Store(int64(meta.Dimensions))
			}
			return err
		}); err != nil {
			return err
		}
		current.Dimensions = meta.Dimensions
	}
	if current.Provider == "" && meta.Provider != "" {
		if err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
			_, err := execWithRetry(ctx, db, `UPDATE `+tableIndexMeta+` SET provider = ? WHERE id = 1`, meta.Provider)
			return err
		}); err != nil {
			return err
		}
		current.Provider = meta.Provider
	}
	if current.Model == "" && meta.Model != "" {
		if err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
			_, err := execWithRetry(ctx, db, `UPDATE `+tableIndexMeta+` SET model = ? WHERE id = 1`, meta.Model)
			return err
		}); err != nil {
			return err
		}
		current.Model = meta.Model
	}
	// Treat empty/zero expected metadata as "don't care" so configs that omit dimensions
	// (provider auto-detect or model-dependent dims) don't force rebuilds.
	if meta.Dimensions > 0 && current.Dimensions > 0 && current.Dimensions != meta.Dimensions {
		return resetAndInit("code index dimensions mismatch")
	}
	if meta.Provider != "" && current.Provider != "" && current.Provider != meta.Provider {
		return resetAndInit("code index provider mismatch")
	}
	if meta.Model != "" && current.Model != "" && current.Model != meta.Model {
		return resetAndInit("code index model mismatch")
	}
	return nil
}

// initMetadata inserts a fresh metadata row. Skipped when dimensions are not
// yet known so we don't persist a zero-dim placeholder that would later look
// like a corrupted index. The next call (after the provider has learned its
// output size) will insert the row with the real value.
func (s *Store) initMetadata(ctx context.Context, meta embeddings.IndexMetadata) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.init_metadata")
	if meta.Dimensions == 0 {
		return nil
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now()
	}
	if meta.SchemaVersion == 0 {
		meta.SchemaVersion = currentSchemaVersion
	}
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := execWithRetry(ctx, db, `INSERT INTO `+tableIndexMeta+` (id, provider, model, dimensions, schema_version, created_at, fingerprint_version, last_sync, source_high_water) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)`, meta.Provider, meta.Model, meta.Dimensions, meta.SchemaVersion, meta.CreatedAt.Unix(), meta.FingerprintVersion, meta.LastSync.Unix(), meta.SourceHighWater.Unix())
		if err == nil {
			s.runtime.Dimensions.Store(int64(meta.Dimensions))
		}
		return err
	})
}

func (s *Store) migrateChunkIndexUniquenessTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM `+tableChunkEmbeddings+`
		WHERE id IN (
			SELECT id FROM (
				SELECT id,
				       ROW_NUMBER() OVER (
				           PARTITION BY item_row_id, chunk_index
				           ORDER BY created_at DESC, id DESC
				       ) AS rn
				FROM `+tableChunkEmbeddings+`
			)
			WHERE rn > 1
		)
	`)
	return err
}

func (s *Store) migrateBodyChunkIndicesTx(ctx context.Context, tx *sql.Tx) error {
	type chunkRow struct {
		id       int64
		anchorID string
		index    int
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT c.id, a.anchor_id, c.chunk_index
		FROM `+tableChunkEmbeddings+` c
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
		if _, err := tx.ExecContext(ctx, `UPDATE `+tableChunkEmbeddings+` SET chunk_index = ? WHERE id = ?`, u.tempIndex, u.id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+tableChunkFTS+` SET chunk_index = ? WHERE anchor_id = ? AND granularity = 'body' AND chunk_index = ?`, u.tempIndex, u.anchorID, u.oldIndex); err != nil {
			return err
		}
	}
	for _, u := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE `+tableChunkEmbeddings+` SET chunk_index = ? WHERE id = ?`, u.newIndex, u.id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE `+tableChunkFTS+` SET chunk_index = ? WHERE anchor_id = ? AND granularity = 'body' AND chunk_index = ?`, u.newIndex, u.anchorID, u.tempIndex); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrateEmbeddingCacheTx(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS `+tableEmbeddingCache+` (
			content_hash TEXT PRIMARY KEY,
			embedding    BLOB NOT NULL,
			dimensions   INTEGER NOT NULL,
			created_at   INTEGER NOT NULL
		)
	`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO `+tableEmbeddingCache+` (content_hash, embedding, dimensions, created_at)
		SELECT content_hash, embedding, dimensions, created_at
		FROM (
			SELECT content_hash, embedding, dimensions, created_at
			FROM `+tableItemEmbeddings+`
			WHERE content_hash IS NOT NULL AND content_hash != ''
			UNION ALL
			SELECT content_hash, embedding, dimensions, created_at
			FROM `+tableChunkEmbeddings+`
			WHERE content_hash IS NOT NULL AND content_hash != ''
		)
		WHERE content_hash IS NOT NULL AND content_hash != ''
		ON CONFLICT(content_hash) DO UPDATE SET
			embedding = excluded.embedding,
			dimensions = excluded.dimensions,
			created_at = MAX(`+tableEmbeddingCache+`.created_at, excluded.created_at)
	`)
	return err
}

func (s *Store) migrateAddNormColumnsTx(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range []string{
		`ALTER TABLE ` + tableItemEmbeddings + ` ADD COLUMN norm REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE ` + tableChunkEmbeddings + ` ADD COLUMN norm REAL NOT NULL DEFAULT 0`,
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

func (s *Store) migrateAddSyncGenerationTx(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range []string{
		`ALTER TABLE ` + tableIndexMeta + ` ADD COLUMN sync_generation INTEGER NOT NULL DEFAULT 0`,
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
func (s *Store) migrateNormalizeEmbeddingStorageTx(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE `+tableEmbeddingCache+` ADD COLUMN norm REAL NOT NULL DEFAULT 0`); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "duplicate column") {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE `+tableEmbeddingCache+` SET norm = (
			SELECT COALESCE(e.norm, 0)
			FROM `+tableItemEmbeddings+` e
			WHERE e.content_hash = `+tableEmbeddingCache+`.content_hash
			LIMIT 1
		)
		WHERE norm = 0 AND EXISTS (
			SELECT 1 FROM `+tableItemEmbeddings+` e
			WHERE e.content_hash = `+tableEmbeddingCache+`.content_hash AND e.norm > 0
		)
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE `+tableEmbeddingCache+` SET norm = (
			SELECT COALESCE(c.norm, 0)
			FROM `+tableChunkEmbeddings+` c
			WHERE c.content_hash = `+tableEmbeddingCache+`.content_hash AND c.norm > 0
			LIMIT 1
		)
		WHERE norm = 0 AND EXISTS (
			SELECT 1 FROM `+tableChunkEmbeddings+` c
			WHERE c.content_hash = `+tableEmbeddingCache+`.content_hash AND c.norm > 0
		)
	`); err != nil {
		return err
	}
	return nil
}

// Metadata returns stored index metadata if present.
func (s *Store) Metadata(ctx context.Context) (embeddings.IndexMetadata, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT provider, model, dimensions, schema_version, created_at, fingerprint_version, IFNULL(last_sync, 0), IFNULL(source_high_water, 0) FROM `+tableIndexMeta+` WHERE id = 1`)
	var meta embeddings.IndexMetadata
	var schemaVersion int
	var fingerprintVersion int
	var createdAt, lastSync, sourceHighWater int64
	if err := row.Scan(&meta.Provider, &meta.Model, &meta.Dimensions, &schemaVersion, &createdAt, &fingerprintVersion, &lastSync, &sourceHighWater); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return embeddings.IndexMetadata{}, false, nil
		}
		return embeddings.IndexMetadata{}, false, err
	}
	meta.SchemaVersion = schemaVersion
	meta.FingerprintVersion = fingerprintVersion
	meta.CreatedAt = time.Unix(createdAt, 0)
	if lastSync > 0 {
		meta.LastSync = time.Unix(lastSync, 0)
	}
	if sourceHighWater > 0 {
		meta.SourceHighWater = time.Unix(sourceHighWater, 0)
	}
	if s.runtime.Dimensions.Load() == 0 && meta.Dimensions > 0 {
		s.runtime.Dimensions.Store(int64(meta.Dimensions))
	}
	return meta, true, nil
}

// UpdateLastSync updates the last_sync timestamp in metadata.
func (s *Store) UpdateLastSync(ctx context.Context, ts time.Time) error {
	if ts.IsZero() {
		ts = time.Now()
	}
	ctx = indexingperf.WithOp(ctx, "codeemb.update_last_sync")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := execWithRetry(ctx, db, `UPDATE `+tableIndexMeta+` SET last_sync = ? WHERE id = 1`, ts.Unix())
		return err
	})
}

func (s *Store) UpdateSourceHighWater(ctx context.Context, ts time.Time) error {
	if ts.IsZero() {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "codeemb.update_source_high_water")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := execWithRetry(ctx, db, `UPDATE `+tableIndexMeta+` SET source_high_water = MAX(IFNULL(source_high_water, 0), ?) WHERE id = 1`, ts.Unix())
		return err
	})
}

// SetFingerprintVersion updates the stored fingerprint version metadata.
func (s *Store) SetFingerprintVersion(ctx context.Context, version int) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.set_fingerprint_version")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := execWithRetry(ctx, db, `UPDATE `+tableIndexMeta+` SET fingerprint_version = ? WHERE id = 1`, version)
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
// Call this at the start of a sync to mark which items are "active" in this sync.
func (s *Store) IncrementSyncGeneration(ctx context.Context) (int64, error) {
	ctx = indexingperf.WithOp(ctx, "codeemb.increment_sync_generation")
	var newGen int64
	err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if err := ensureCodeEmbeddingMetadataRowForSyncGeneration(ctx, db); err != nil {
			return err
		}
		if _, err := execWithRetry(ctx, db, `UPDATE `+tableIndexMeta+` SET sync_generation = sync_generation + 1 WHERE id = 1`); err != nil {
			return err
		}
		return db.QueryRowContext(ctx, `SELECT sync_generation FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&newGen)
	})
	return newGen, err
}

func ensureCodeEmbeddingMetadataRowForSyncGeneration(ctx context.Context, db *sql.DB) error {
	_, err := execWithRetry(ctx, db, `
		INSERT OR IGNORE INTO `+tableIndexMeta+` (id, provider, model, dimensions, schema_version, created_at, fingerprint_version, last_sync, source_high_water, sync_generation, visible_generation)
		VALUES (1, '', '', 0, ?, ?, 0, 0, 0, 0, 0)
	`, currentSchemaVersion, time.Now().Unix())
	return err
}

// CommitSyncGeneration makes the current sync generation visible to search.
func (s *Store) CommitSyncGeneration(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.commit_sync_generation")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if err := ensureCodeEmbeddingMetadataRowForSyncGeneration(ctx, db); err != nil {
			return err
		}
		_, err := execWithRetry(ctx, db, `UPDATE `+tableIndexMeta+` SET visible_generation = sync_generation WHERE id = 1`)
		return err
	})
}

// PruneStaleItems removes items that weren't seen in the current sync generation.
// If threshold > 0, only prunes when stale items exceed threshold fraction of total.
// Returns the number of items pruned.
func (s *Store) PruneStaleItems(ctx context.Context, threshold float64) (int, error) {
	ctx = indexingperf.WithOp(ctx, "codeemb.prune_stale_items")
	gen, err := s.SyncGeneration(ctx)
	if err != nil {
		return 0, err
	}

	var total, stale int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableItems).Scan(&total); err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableItems+` WHERE sync_generation < ?`, gen).Scan(&stale); err != nil {
		return 0, err
	}
	if stale == 0 {
		return 0, nil
	}

	// If stale fraction is below threshold, keep items around for potential reuse.
	staleFraction := float64(stale) / float64(total)
	if threshold > 0 && staleFraction < threshold {
		return 0, nil // Items preserved for branch-switching reuse
	}

	// Prune stale items and their embeddings.
	pruned := 0
	err = s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		result, err := execWithRetry(ctx, db, `DELETE FROM `+tableItems+` WHERE sync_generation < ?`, gen)
		if err != nil {
			return err
		}
		rows, _ := result.RowsAffected()
		pruned = int(rows)

		// Also clean up orphaned FTS entries.
		_, err = execWithRetry(ctx, db, `
			DELETE FROM `+tableChunkFTS+`
			WHERE anchor_id NOT IN (
				SELECT a.anchor_id
				FROM `+tableItems+` i
				JOIN intel_code_anchors a ON a.id = i.anchor_row_id
			)
		`)
		return err
	})
	return pruned, err
}

// PruneStaleCache removes embedding cache entries that are no longer referenced
// by any item or chunk. Only prunes when orphaned entries exceed the threshold
// fraction of total cache entries.
// Returns the number of cache entries pruned.
func (s *Store) PruneStaleCache(ctx context.Context, threshold float64) (int, error) {
	ctx = indexingperf.WithOp(ctx, "codeemb.prune_stale_cache")
	var total, orphaned int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableEmbeddingCache).Scan(&total); err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}

	// Count orphaned cache entries (not referenced by any item or chunk).
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM `+tableEmbeddingCache+`
		WHERE content_hash NOT IN (
			SELECT content_hash FROM `+tableItemEmbeddings+`
			WHERE content_hash IS NOT NULL AND content_hash != ''
			UNION
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
				SELECT content_hash FROM `+tableItemEmbeddings+`
				WHERE content_hash IS NOT NULL AND content_hash != ''
				UNION
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

// UpsertItemMeta inserts or updates code item metadata.
// The item is automatically marked with the current sync generation.
func (s *Store) UpsertItemMeta(ctx context.Context, item codeindex.Item) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.upsert_item_meta")
	now := time.Now().Unix()
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		// Get current sync generation to mark this item as active.
		var gen int64
		_ = db.QueryRowContext(ctx, `SELECT IFNULL(sync_generation, 0) FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&gen)

		anchorStmt, err := prepareIntelAnchorUpsertStmt(ctx, db)
		if err != nil {
			return err
		}
		defer anchorStmt.Close()
		if _, err := anchorStmt.ExecContext(ctx, string(item.AnchorID), item.Lang, item.Kind, item.Path, item.Symbol, item.FQN); err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `
                INSERT INTO `+tableItems+` (anchor_row_id, fingerprint, updated_at, sync_generation)
                SELECT id, ?, ?, ?
                FROM intel_code_anchors
                WHERE anchor_id = ?
                ON CONFLICT(anchor_row_id) DO UPDATE SET
                        fingerprint = excluded.fingerprint,
                        updated_at = excluded.updated_at,
                        sync_generation = excluded.sync_generation
        `, item.Fingerprint, now, gen, string(item.AnchorID))
		return err
	})
}

// UpsertItemMetaBatch inserts or updates metadata for multiple code items in one transaction.
func (s *Store) UpsertItemMetaBatch(ctx context.Context, items []codeindex.Item) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.upsert_item_meta_batch")
	if len(items) == 0 {
		return nil
	}
	now := time.Now().Unix()
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		// Get current sync generation to mark items as active.
		var gen int64
		_ = tx.QueryRowContext(ctx, `SELECT IFNULL(sync_generation, 0) FROM `+tableIndexMeta+` WHERE id = 1`).Scan(&gen)

		anchorUpsertStarted := time.Now()
		if err := bulkUpsertIntelAnchorsTx(ctx, tx, items); err != nil {
			return err
		}
		indexingperf.ObserveLatency(ctx, "codeemb.meta.anchor_upsert", time.Since(anchorUpsertStarted))

		itemUpsertStarted := time.Now()
		if err := bulkUpsertCodeItemsTx(ctx, tx, items, now, gen); err != nil {
			return err
		}
		indexingperf.AddCount(ctx, "codeemb.meta.item_rows.count", int64(len(items)))
		indexingperf.ObserveLatency(ctx, "codeemb.meta.item_upsert", time.Since(itemUpsertStarted))
		return nil
	})
}

func bulkUpsertIntelAnchorsTx(ctx context.Context, tx *sql.Tx, items []codeindex.Item) error {
	if len(items) == 0 {
		return nil
	}
	hasStartByte, err := tableHasColumn(ctx, tx, "intel_code_anchors", "start_byte")
	if err != nil {
		return err
	}
	hasFingerprint, err := tableHasColumn(ctx, tx, "intel_code_anchors", "fingerprint")
	if err != nil {
		return err
	}
	hasUpdatedAt, err := tableHasColumn(ctx, tx, "intel_code_anchors", "updated_at")
	if err != nil {
		return err
	}

	cols := []string{"anchor_id", "lang", "kind", "path", "symbol", "fqn"}
	updates := []string{
		"lang = excluded.lang",
		"kind = excluded.kind",
		"path = excluded.path",
		"symbol = excluded.symbol",
		"fqn = excluded.fqn",
	}
	if hasStartByte {
		cols = append(cols, "start_byte", "end_byte", "start_line", "end_line")
	}
	if hasFingerprint {
		cols = append(cols, "fingerprint")
	}
	if hasUpdatedAt {
		cols = append(cols, "updated_at")
	}

	const batchSize = 100
	for batch := range slices.Chunk(items, batchSize) {
		valueSQL := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch)*6)
		for _, item := range batch {
			vals := []string{"?", "?", "?", "?", "?", "?"}
			args = append(args, string(item.AnchorID), item.Lang, item.Kind, item.Path, item.Symbol, item.FQN)
			if hasStartByte {
				vals = append(vals, "0", "0", "0", "0")
			}
			if hasFingerprint {
				vals = append(vals, "'codeemb'")
			}
			if hasUpdatedAt {
				vals = append(vals, "0")
			}
			valueSQL = append(valueSQL, "("+strings.Join(vals, ", ")+")")
		}
		stmt := fmt.Sprintf(`
			INSERT INTO intel_code_anchors (%s)
			VALUES %s
			ON CONFLICT(anchor_id) DO UPDATE SET
				%s
		`, strings.Join(cols, ", "), strings.Join(valueSQL, ","), strings.Join(updates, ",\n\t\t\t\t"))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

func bulkUpsertCodeItemsTx(ctx context.Context, tx *sql.Tx, items []codeindex.Item, now, gen int64) error {
	if len(items) == 0 {
		return nil
	}
	const batchSize = 200
	for batch := range slices.Chunk(items, batchSize) {
		valueSQL := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch)*4)
		for _, item := range batch {
			valueSQL = append(valueSQL, "(?, ?, ?, ?)")
			args = append(args, string(item.AnchorID), item.Fingerprint, now, gen)
		}
		stmt := fmt.Sprintf(`
			WITH input(anchor_id, fingerprint, updated_at, sync_generation) AS (VALUES %s)
			INSERT INTO `+tableItems+` (anchor_row_id, fingerprint, updated_at, sync_generation)
			SELECT a.id, i.fingerprint, i.updated_at, i.sync_generation
			FROM input i
			JOIN intel_code_anchors a ON a.anchor_id = i.anchor_id
			ON CONFLICT(anchor_row_id) DO UPDATE SET
				fingerprint = excluded.fingerprint,
				updated_at = excluded.updated_at,
				sync_generation = excluded.sync_generation
		`, strings.Join(valueSQL, ","))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

// DeleteItemsNotIn removes metadata and embeddings for items not present in anchorIDs.
func (s *Store) DeleteItemsNotIn(ctx context.Context, anchorIDs []codeindex.AnchorID) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.delete_items_not_in")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if len(anchorIDs) == 0 {
			if _, err := execWithRetry(ctx, db, `DELETE FROM `+tableItems); err != nil {
				return err
			}
			_, err := execWithRetry(ctx, db, `DELETE FROM `+tableChunkFTS)
			return err
		}

		// SQLite has a limit on bound parameters; fall back to a two-pass strategy when the list is large.
		if len(anchorIDs) <= 900 {
			holders := make([]string, len(anchorIDs))
			args := make([]any, 0, len(anchorIDs))
			for i, id := range anchorIDs {
				holders[i] = "?"
				args = append(args, string(id))
			}
			stmt := fmt.Sprintf(`DELETE FROM %s WHERE anchor_row_id NOT IN (SELECT id FROM intel_code_anchors WHERE anchor_id IN (%s))`, tableItems, strings.Join(holders, ","))
			if _, err := execWithRetry(ctx, db, stmt, args...); err != nil {
				return err
			}
			stmt = fmt.Sprintf(`DELETE FROM %s WHERE anchor_id NOT IN (%s)`, tableChunkFTS, strings.Join(holders, ","))
			_, err := execWithRetry(ctx, db, stmt, args...)
			return err
		}

		existing := make(map[string]struct{})
		rows, err := db.QueryContext(ctx, `SELECT a.anchor_id FROM `+tableItems+` i JOIN intel_code_anchors a ON a.id = i.anchor_row_id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if scanErr := rows.Scan(&id); scanErr != nil {
				_ = rows.Close()
				return scanErr
			}
			existing[id] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()

		keep := make(map[string]struct{}, len(anchorIDs))
		for _, id := range anchorIDs {
			keep[string(id)] = struct{}{}
		}

		var toDelete []string
		for id := range existing {
			if _, ok := keep[id]; !ok {
				toDelete = append(toDelete, id)
			}
		}
		if len(toDelete) == 0 {
			return nil
		}

		const batch = 500
		for chunk := range slices.Chunk(toDelete, batch) {
			holders := make([]string, len(chunk))
			args := make([]any, len(chunk))
			for i, id := range chunk {
				holders[i] = "?"
				args[i] = id
			}
			stmt := fmt.Sprintf(`DELETE FROM %s WHERE anchor_row_id IN (SELECT id FROM intel_code_anchors WHERE anchor_id IN (%s))`, tableItems, strings.Join(holders, ","))
			if _, err := execWithRetry(ctx, db, stmt, args...); err != nil {
				return err
			}
			stmt = fmt.Sprintf(`DELETE FROM %s WHERE anchor_id IN (%s)`, tableChunkFTS, strings.Join(holders, ","))
			if _, err := execWithRetry(ctx, db, stmt, args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteItemsForPathNotIn removes metadata and embeddings for items under one path
// that are not present in anchorIDs.
func (s *Store) DeleteItemsForPathNotIn(ctx context.Context, path string, anchorIDs []codeindex.AnchorID) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.delete_items_for_path_not_in")
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return nil
	}
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return deleteItemsForPathNotIn(ctx, db, path, anchorIDs)
	})
}

// DeleteItemsForPathsNotIn removes metadata and embeddings for many paths in one write transaction.
func (s *Store) DeleteItemsForPathsNotIn(ctx context.Context, anchorIDsByPath map[string][]codeindex.AnchorID) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.delete_items_for_paths_not_in_batch")
	if len(anchorIDsByPath) == 0 {
		return nil
	}
	paths := make([]string, 0, len(anchorIDsByPath))
	for path := range anchorIDsByPath {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		for _, path := range paths {
			if err := deleteItemsForPathNotIn(ctx, db, path, anchorIDsByPath[path]); err != nil {
				return err
			}
		}
		return nil
	})
}

func deleteItemsForPathNotIn(ctx context.Context, db *sql.DB, path string, anchorIDs []codeindex.AnchorID) error {
	if len(anchorIDs) == 0 {
		if _, err := execWithRetry(ctx, db, `DELETE FROM `+tableItems+` WHERE anchor_row_id IN (SELECT id FROM intel_code_anchors WHERE path = ?)`, path); err != nil {
			return err
		}
		_, err := execWithRetry(ctx, db, `DELETE FROM `+tableChunkFTS+` WHERE path = ?`, path)
		return err
	}
	holders := make([]string, len(anchorIDs))
	args := make([]any, 0, len(anchorIDs)+1)
	args = append(args, path)
	for i, id := range anchorIDs {
		holders[i] = "?"
		args = append(args, string(id))
	}
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE anchor_row_id IN (SELECT id FROM intel_code_anchors WHERE path = ?) AND anchor_row_id NOT IN (SELECT id FROM intel_code_anchors WHERE anchor_id IN (%s))`, tableItems, strings.Join(holders, ","))
	if _, err := execWithRetry(ctx, db, stmt, args...); err != nil {
		return err
	}
	stmt = fmt.Sprintf(`DELETE FROM %s WHERE path = ? AND anchor_id NOT IN (%s)`, tableChunkFTS, strings.Join(holders, ","))
	_, err := execWithRetry(ctx, db, stmt, args...)
	return err
}

// ListItems returns all stored item metadata.
func (s *Store) ListItems(ctx context.Context) ([]codeindex.Item, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.anchor_id, a.lang, a.kind, a.path, a.symbol, a.fqn,
		       i.fingerprint, i.updated_at
		FROM `+tableItems+` i
		JOIN intel_code_anchors a ON a.id = i.anchor_row_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []codeindex.Item
	for rows.Next() {
		var item codeindex.Item
		var updated int64
		if err := rows.Scan(&item.AnchorID, &item.Lang, &item.Kind, &item.Path, &item.Symbol, &item.FQN, &item.Fingerprint, &updated); err != nil {
			return nil, err
		}
		item.UpdatedAt = time.Unix(updated, 0)
		out = append(out, item)
	}
	return out, rows.Err()
}

// AnchorIDsByPathPrefix returns up to limit anchor IDs whose stored path equals prefix or is nested under it.
// Prefix matching is directory-boundary aware (prefix "pkg" matches "pkg/foo.go" but not "pkgx/foo.go").
//
// This is used to make directory/file seed expansion efficient without scanning the entire item list.
func (s *Store) AnchorIDsByPathPrefix(ctx context.Context, prefix string, limit int) ([]codeindex.AnchorID, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}

	prefix = filepath.ToSlash(filepath.Clean(prefix))
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "." || prefix == "/" {
		return nil, nil
	}
	pattern := prefix + "/%"

	rows, err := s.db.QueryContext(ctx, `
		SELECT a.anchor_id
		FROM `+tableItems+` i
		JOIN intel_code_anchors a ON a.id = i.anchor_row_id
		WHERE a.path = ? OR a.path LIKE ?
		ORDER BY a.path
		LIMIT ?
	`, prefix, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]codeindex.AnchorID, 0, min(limit, 256))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if strings.TrimSpace(id) == "" {
			continue
		}
		out = append(out, codeindex.AnchorID(id))
	}
	return out, rows.Err()
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
	return s.embeddingFromBlob(blob, dims)
}

// EmbeddingByHashes returns cached embeddings by content hash, skipping missing
// or dimension-incompatible rows.
func (s *Store) EmbeddingByHashes(ctx context.Context, hashes []string) (map[string]embeddings.Embedding, error) {
	unique := dedupeStrings(hashes)
	rowsByHash := make(map[string]embeddings.Embedding, len(unique))
	if len(unique) == 0 {
		return rowsByHash, nil
	}

	for batch := range slices.Chunk(unique, hashLookupBatchSize) {
		holders := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, hash := range batch {
			holders[i] = "?"
			args[i] = hash
		}
		inClause := strings.Join(holders, ",")
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT content_hash, embedding, dimensions
			FROM %s
			WHERE content_hash IN (%s)
		`, tableEmbeddingCache, inClause), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var (
				hash string
				blob []byte
				dims int
			)
			if err := rows.Scan(&hash, &blob, &dims); err != nil {
				_ = rows.Close()
				return nil, err
			}
			emb, ok, err := s.embeddingFromBlob(blob, dims)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			if ok {
				rowsByHash[hash] = emb
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}

	return rowsByHash, nil
}

// CacheEmbedding stores an embedding keyed by content hash for reuse across anchors.
func (s *Store) CacheEmbedding(ctx context.Context, hash string, emb embeddings.Embedding) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.cache_embedding")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return s.cacheEmbeddingLocked(ctx, hash, emb)
	})
}

// ClearEmbeddingCache removes all entries from the embedding cache.
// Used for testing to force re-embedding via API.
func (s *Store) ClearEmbeddingCache(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.clear_embedding_cache")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := execWithRetry(ctx, s.db, `DELETE FROM `+tableEmbeddingCache)
		return err
	})
}

func (s *Store) cacheEmbeddingLocked(ctx context.Context, hash string, emb embeddings.Embedding) error {
	if strings.TrimSpace(hash) == "" || len(emb) == 0 {
		return nil
	}
	norm := math.Sqrt(dotFloat64(emb, emb))
	now := time.Now().Unix()
	_, err := execWithRetry(ctx, s.db, `
		INSERT INTO `+tableEmbeddingCache+` (content_hash, embedding, norm, dimensions, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(content_hash) DO UPDATE SET
			embedding = excluded.embedding,
			norm = excluded.norm,
			dimensions = excluded.dimensions,
			created_at = excluded.created_at
	`, hash, embedToBytes(emb), norm, len(emb), now)
	if err == nil && s.runtime.Dimensions.Load() == 0 {
		s.runtime.Dimensions.Store(int64(len(emb)))
	}
	return err
}

func cacheEmbeddingTx(ctx context.Context, tx *sql.Tx, table string, hash string, emb embeddings.Embedding, now int64) error {
	if strings.TrimSpace(hash) == "" || len(emb) == 0 {
		return nil
	}
	norm := math.Sqrt(dotFloat64(emb, emb))
	_, err := tx.ExecContext(ctx, `
		INSERT INTO `+table+` (content_hash, embedding, norm, dimensions, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(content_hash) DO UPDATE SET
			embedding = excluded.embedding,
			norm = excluded.norm,
			dimensions = excluded.dimensions,
			created_at = excluded.created_at
	`, hash, embedToBytes(emb), norm, len(emb), now)
	return err
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
			INSERT INTO %s (content_hash, embedding, norm, dimensions, created_at)
			VALUES %s
			ON CONFLICT(content_hash) DO UPDATE SET
				embedding = excluded.embedding,
				norm = excluded.norm,
				dimensions = excluded.dimensions,
				created_at = excluded.created_at
		`, table, strings.Join(valueSQL, ","))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

func bulkUpsertCodeChunkRowsTx(ctx context.Context, tx *sql.Tx, rowID int64, rows []codeChunkRow, now int64) error {
	if len(rows) == 0 {
		return nil
	}
	for batch := range slices.Chunk(rows, codeChunkBatchSize) {
		args := make([]any, 0, len(batch)*14)
		valueSQL := make([]string, 0, len(batch))
		for _, r := range batch {
			if len(r.vec) == 0 {
				continue
			}
			valueSQL = append(valueSQL, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, rowID, r.index, r.granularity, r.breadcrumb, r.heading, r.startByte, r.endByte, r.startLine, r.endLine, r.hash, embedToBytes(r.vec), r.norm, len(r.vec), now)
		}
		if len(valueSQL) == 0 {
			continue
		}
		stmt := fmt.Sprintf(`
			INSERT INTO %s (item_row_id, chunk_index, granularity, breadcrumb, heading, start_byte, end_byte, start_line, end_line, content_hash, embedding, norm, dimensions, created_at)
			VALUES %s
			ON CONFLICT(item_row_id, granularity, chunk_index) DO UPDATE SET
				breadcrumb = excluded.breadcrumb,
				heading = excluded.heading,
				start_byte = excluded.start_byte,
				end_byte = excluded.end_byte,
				start_line = excluded.start_line,
				end_line = excluded.end_line,
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

func bulkInsertChunkFTSTx(ctx context.Context, tx *sql.Tx, rows []codeFTSRow) error {
	if len(rows) == 0 {
		return nil
	}
	for batch := range slices.Chunk(rows, codeChunkBatchSize) {
		args := make([]any, 0, len(batch)*10)
		valueSQL := make([]string, 0, len(batch))
		for _, r := range batch {
			valueSQL = append(valueSQL, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, r.anchorID, r.chunkIndex, r.path, r.symbol, r.fqn, r.kind, r.granularity, r.breadcrumb, r.heading, r.body)
		}
		stmt := fmt.Sprintf(`
			INSERT INTO %s (anchor_id, chunk_index, path, symbol, fqn, kind, granularity, breadcrumb, heading, body)
			VALUES %s
		`, tableChunkFTS, strings.Join(valueSQL, ","))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

func deleteChunkFTSForIndices(ctx context.Context, tx *sql.Tx, anchorID string, indices []int) error {
	if len(indices) == 0 {
		return nil
	}
	if len(indices) <= 900 {
		args := make([]any, 0, len(indices)+1)
		holders := make([]string, 0, len(indices))
		args = append(args, anchorID)
		for _, idx := range indices {
			holders = append(holders, "?")
			args = append(args, idx)
		}
		stmt := fmt.Sprintf(`DELETE FROM %s WHERE anchor_id = ? AND chunk_index IN (%s)`, tableChunkFTS, strings.Join(holders, ","))
		_, err := tx.ExecContext(ctx, stmt, args...)
		return err
	}

	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_chunk_fts_idx`)
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_delete_chunk_fts_idx (chunk_index INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_delete_chunk_fts_idx(chunk_index) VALUES (?)`)
	if err != nil {
		return err
	}
	for _, idx := range indices {
		if _, err := stmt.ExecContext(ctx, idx); err != nil {
			_ = stmt.Close()
			return err
		}
	}
	_ = stmt.Close()
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM %s
		WHERE anchor_id = ?
		  AND chunk_index IN (SELECT chunk_index FROM temp_delete_chunk_fts_idx)
	`, tableChunkFTS), anchorID)
	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_chunk_fts_idx`)
	return err
}

func deleteChunkEmbeddingsForOtherGranularity(ctx context.Context, tx *sql.Tx, rowID int64, granularity string, indices []int) error {
	if len(indices) == 0 {
		return nil
	}
	if len(indices) <= 900 {
		args := make([]any, 0, len(indices)+2)
		holders := make([]string, 0, len(indices))
		args = append(args, rowID)
		for _, idx := range indices {
			holders = append(holders, "?")
			args = append(args, idx)
		}
		args = append(args, granularity)
		stmt := fmt.Sprintf(`DELETE FROM %s WHERE item_row_id = ? AND chunk_index IN (%s) AND granularity != ?`, tableChunkEmbeddings, strings.Join(holders, ","))
		_, err := tx.ExecContext(ctx, stmt, args...)
		return err
	}

	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_chunk_embeddings_idx`)
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_delete_chunk_embeddings_idx (chunk_index INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_delete_chunk_embeddings_idx(chunk_index) VALUES (?)`)
	if err != nil {
		return err
	}
	for _, idx := range indices {
		if _, err := stmt.ExecContext(ctx, idx); err != nil {
			_ = stmt.Close()
			return err
		}
	}
	_ = stmt.Close()
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM %s
		WHERE item_row_id = ?
		  AND chunk_index IN (SELECT chunk_index FROM temp_delete_chunk_embeddings_idx)
		  AND granularity != ?
	`, tableChunkEmbeddings), rowID, granularity)
	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_chunk_embeddings_idx`)
	return err
}

// GetItemEmbedding returns the embedding for a code item if present.
func (s *Store) GetItemEmbedding(ctx context.Context, anchorID codeindex.AnchorID) (embeddings.Embedding, string, bool, error) {
	row := s.db.QueryRowContext(ctx, `
                SELECT e.embedding, e.content_hash, e.dimensions
                FROM `+tableItemEmbeddings+` e
                JOIN `+tableItems+` i ON e.item_row_id = i.id
                JOIN intel_code_anchors a ON a.id = i.anchor_row_id
                WHERE a.anchor_id = ?
        `, string(anchorID))
	var blob []byte
	var contentHash string
	var dims int
	if err := row.Scan(&blob, &contentHash, &dims); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", false, nil
		}
		return nil, "", false, err
	}
	emb := bytesToEmbed(blob)
	if dims > 0 && len(emb) != dims {
		return nil, "", false, nil
	}
	if s.runtime.Dimensions.Load() == 0 && len(emb) > 0 {
		s.runtime.Dimensions.Store(int64(len(emb)))
	}
	return emb, contentHash, true, nil
}

// UpsertItemEmbedding stores the embedding for a code item.
func (s *Store) UpsertItemEmbedding(ctx context.Context, anchorID codeindex.AnchorID, contentHash string, emb embeddings.Embedding) error {
	if len(emb) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "codeemb.upsert_item_embedding")
	norm := math.Sqrt(dotFloat64(emb, emb))
	now := time.Now().Unix()
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if err := cacheEmbeddingTx(ctx, tx, tableEmbeddingCache, contentHash, emb, now); err != nil {
			return err
		}
		var rowID int64
		if err := tx.QueryRowContext(ctx, `SELECT i.id FROM `+tableItems+` i JOIN intel_code_anchors a ON a.id = i.anchor_row_id WHERE a.anchor_id = ?`, string(anchorID)).Scan(&rowID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO `+tableItemEmbeddings+` (item_row_id, content_hash, embedding, norm, dimensions, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(item_row_id) DO UPDATE SET
				content_hash = excluded.content_hash,
				embedding = excluded.embedding,
				norm = excluded.norm,
				dimensions = excluded.dimensions,
				created_at = excluded.created_at
		`, rowID, contentHash, embedToBytes(emb), norm, len(emb), now)
		return err
	})
	if err == nil && s.runtime.Dimensions.Load() == 0 && len(emb) > 0 {
		s.runtime.Dimensions.Store(int64(len(emb)))
	}
	return err
}

// UpsertItemEmbeddingBatch stores embeddings for multiple code items in one transaction.
func (s *Store) UpsertItemEmbeddingBatch(ctx context.Context, items []codeindex.ItemEmbeddingUpsert) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.upsert_item_embeddings")
	if len(items) == 0 {
		return nil
	}
	var dims int
	for _, item := range items {
		if len(item.Embedding) == 0 {
			continue
		}
		dims = len(item.Embedding)
		break
	}
	if dims == 0 {
		return nil
	}
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		cacheRows := make([]embeddingCacheRow, 0, len(items))
		anchorIDs := make([]codeindex.AnchorID, 0, len(items))
		for _, item := range items {
			if len(item.Embedding) == 0 {
				continue
			}
			cacheRows = append(cacheRows, embeddingCacheRow{hash: item.Hash, vec: item.Embedding})
			anchorIDs = append(anchorIDs, item.AnchorID)
		}
		cacheStarted := time.Now()
		if err := bulkUpsertEmbeddingCacheTx(ctx, tx, tableEmbeddingCache, cacheRows, now); err != nil {
			return fmt.Errorf("upsert item embedding cache rows=%d: %w", len(cacheRows), err)
		}
		indexingperf.ObserveLatency(ctx, "codeemb.embed.cache_upsert", time.Since(cacheStarted))

		insertStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO `+tableItemEmbeddings+` (item_row_id, content_hash, embedding, norm, dimensions, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(item_row_id) DO UPDATE SET
				content_hash = excluded.content_hash,
				embedding = excluded.embedding,
				norm = excluded.norm,
				dimensions = excluded.dimensions,
				created_at = excluded.created_at
		`)
		if err != nil {
			return err
		}
		defer insertStmt.Close()

		rowResolveStarted := time.Now()
		rowIDs, err := selectCodeItemRowIDsByAnchorIDs(ctx, tx, anchorIDs)
		if err != nil {
			return fmt.Errorf("select item row ids anchors=%d: %w", len(anchorIDs), err)
		}
		indexingperf.AddCount(ctx, "codeemb.embed.item_rowid_lookup.count", int64(len(anchorIDs)))
		indexingperf.ObserveLatency(ctx, "codeemb.embed.item_rowid_resolve", time.Since(rowResolveStarted))

		insertStarted := time.Now()
		for _, item := range items {
			vec := item.Embedding
			if len(vec) == 0 {
				continue
			}
			dims := s.runtime.Dimensions.Load()
			if dims > 0 && len(vec) != int(dims) {
				return fmt.Errorf("embedding dimension mismatch: have %d want %d", len(vec), dims)
			}
			rowID := rowIDs[string(item.AnchorID)]
			if rowID <= 0 {
				continue
			}
			norm := math.Sqrt(dotFloat64(vec, vec))
			if _, err := insertStmt.ExecContext(ctx, rowID, item.Hash, embedToBytes(vec), norm, len(vec), now); err != nil {
				return fmt.Errorf("upsert item embedding anchor=%s: %w", item.AnchorID, err)
			}
		}
		indexingperf.ObserveLatency(ctx, "codeemb.embed.insert", time.Since(insertStarted))
		return nil
	})
	if err == nil && s.runtime.Dimensions.Load() == 0 && dims > 0 {
		s.runtime.Dimensions.Store(int64(dims))
	}
	return err
}

// ChunkHashes returns content hashes for stored chunks of a code item.
func (s *Store) ChunkHashes(ctx context.Context, anchorID codeindex.AnchorID) (map[int]string, error) {
	row := s.db.QueryRowContext(ctx, `SELECT i.id FROM `+tableItems+` i JOIN intel_code_anchors a ON a.id = i.anchor_row_id WHERE a.anchor_id = ?`, string(anchorID))
	var rowID int64
	if err := row.Scan(&rowID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return map[int]string{}, nil
		}
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT chunk_index, content_hash
		FROM `+tableChunkEmbeddings+`
		WHERE item_row_id = ?
		ORDER BY chunk_index, created_at DESC, id DESC
	`, rowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hashes := make(map[int]string)
	for rows.Next() {
		var idx int
		var hash string
		if err := rows.Scan(&idx, &hash); err != nil {
			return nil, err
		}
		// Defensive: older data may contain multiple rows for the same chunk_index (different
		// granularities). Prefer the newest row (query ORDER BY) and keep the first seen.
		if _, ok := hashes[idx]; ok {
			continue
		}
		hashes[idx] = hash
	}
	return hashes, rows.Err()
}

// ItemFingerprint returns the stored fingerprint for an anchor, if present.
func (s *Store) ItemFingerprint(ctx context.Context, anchorID codeindex.AnchorID) (string, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT i.fingerprint FROM `+tableItems+` i JOIN intel_code_anchors a ON a.id = i.anchor_row_id WHERE a.anchor_id = ?`, string(anchorID))
	var fp string
	if err := row.Scan(&fp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return fp, true, nil
}

// ItemEmbeddingStates returns stored fingerprints, item-level hashes, and chunk hashes for anchors.
func (s *Store) ItemEmbeddingStates(ctx context.Context, anchorIDs []codeindex.AnchorID) (map[codeindex.AnchorID]codeindex.ItemEmbeddingState, error) {
	states := make(map[codeindex.AnchorID]codeindex.ItemEmbeddingState, len(anchorIDs))
	unique := dedupeAnchorIDs(anchorIDs)
	if len(unique) == 0 {
		return states, nil
	}

	for batch := range slices.Chunk(unique, 256) {
		holders := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, id := range batch {
			holders[i] = "?"
			args[i] = string(id)
		}
		inClause := strings.Join(holders, ",")

		// Fingerprints
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT a.anchor_id, i.fingerprint FROM %s i JOIN intel_code_anchors a ON a.id = i.anchor_row_id WHERE a.anchor_id IN (%s)`, tableItems, inClause), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var anchorID string
			var fp sql.NullString
			if err := rows.Scan(&anchorID, &fp); err != nil {
				_ = rows.Close()
				return nil, err
			}
			state := states[codeindex.AnchorID(anchorID)]
			state.Fingerprint = fp.String
			state.HasFingerprint = true
			states[codeindex.AnchorID(anchorID)] = state
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}

		// Item-level embeddings
		rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT a.anchor_id, e.content_hash
			FROM %s e
			JOIN %s i ON e.item_row_id = i.id
			JOIN intel_code_anchors a ON a.id = i.anchor_row_id
			WHERE a.anchor_id IN (%s)
		`, tableItemEmbeddings, tableItems, inClause), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var anchorID, hash string
			if err := rows.Scan(&anchorID, &hash); err != nil {
				_ = rows.Close()
				return nil, err
			}
			state := states[codeindex.AnchorID(anchorID)]
			state.ItemHash = hash
			state.HasItemHash = true
			states[codeindex.AnchorID(anchorID)] = state
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}

		// Chunk hashes (prefer newest per chunk index).
		rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT a.anchor_id, c.chunk_index, c.content_hash
			FROM %s c
			JOIN %s i ON c.item_row_id = i.id
			JOIN intel_code_anchors a ON a.id = i.anchor_row_id
			WHERE a.anchor_id IN (%s)
			ORDER BY c.chunk_index, c.created_at DESC, c.id DESC
		`, tableChunkEmbeddings, tableItems, inClause), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var anchorID, hash string
			var idx int
			if err := rows.Scan(&anchorID, &idx, &hash); err != nil {
				_ = rows.Close()
				return nil, err
			}
			state := states[codeindex.AnchorID(anchorID)]
			if state.ChunkHashes == nil {
				state.ChunkHashes = make(map[int]string)
			}
			if _, ok := state.ChunkHashes[idx]; ok {
				continue
			}
			state.ChunkHashes[idx] = hash
			states[codeindex.AnchorID(anchorID)] = state
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}

	return states, nil
}

// DeleteChunksNotIn removes chunk embeddings whose indices are not in the provided set for an anchor.
func (s *Store) DeleteChunksNotIn(ctx context.Context, anchorID codeindex.AnchorID, indices []int) error {
	ctx = indexingperf.WithOp(ctx, "codeemb.delete_chunks_not_in")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		row := db.QueryRowContext(ctx, `SELECT i.id FROM `+tableItems+` i JOIN intel_code_anchors a ON a.id = i.anchor_row_id WHERE a.anchor_id = ?`, string(anchorID))
		var rowID int64
		if err := row.Scan(&rowID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if len(indices) == 0 {
			if _, err := execWithRetry(ctx, db, `DELETE FROM `+tableChunkEmbeddings+` WHERE item_row_id = ?`, rowID); err != nil {
				return err
			}
			_, err := execWithRetry(ctx, db, `DELETE FROM `+tableChunkFTS+` WHERE anchor_id = ?`, string(anchorID))
			return err
		}
		holders := make([]string, len(indices))
		args := make([]any, 0, len(indices)+1)
		args = append(args, rowID)
		for i, idx := range indices {
			holders[i] = "?"
			args = append(args, idx)
		}
		stmt := fmt.Sprintf(`DELETE FROM %s WHERE item_row_id = ? AND chunk_index NOT IN (%s)`, tableChunkEmbeddings, strings.Join(holders, ","))
		if _, err := execWithRetry(ctx, db, stmt, args...); err != nil {
			return err
		}
		args[0] = string(anchorID)
		stmt = fmt.Sprintf(`DELETE FROM %s WHERE anchor_id = ? AND chunk_index NOT IN (%s)`, tableChunkFTS, strings.Join(holders, ","))
		_, err := execWithRetry(ctx, db, stmt, args...)
		return err
	})
}

// ItemChunks returns stored embeddings for all chunks of a code item.
func (s *Store) ItemChunks(ctx context.Context, anchorID codeindex.AnchorID) ([]codeindex.StoredChunk, error) {
	row := s.db.QueryRowContext(ctx, `SELECT i.id FROM `+tableItems+` i JOIN intel_code_anchors a ON a.id = i.anchor_row_id WHERE a.anchor_id = ?`, string(anchorID))
	var rowID int64
	if err := row.Scan(&rowID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
                SELECT chunk_index, granularity, breadcrumb, heading, embedding, dimensions
                FROM `+tableChunkEmbeddings+`
                WHERE item_row_id = ?
                ORDER BY chunk_index
        `, rowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []codeindex.StoredChunk
	expectedDims := int(s.runtime.Dimensions.Load())
	for rows.Next() {
		var idx, dims int
		var granularity, breadcrumb, heading string
		var blob []byte
		if err := rows.Scan(&idx, &granularity, &breadcrumb, &heading, &blob, &dims); err != nil {
			return nil, err
		}
		emb := bytesToEmbed(blob)
		if dims > 0 && len(emb) != dims {
			continue
		}
		if expectedDims > 0 && len(emb) != expectedDims {
			continue
		}
		chunks = append(chunks, codeindex.StoredChunk{
			Index:       idx,
			Granularity: granularity,
			Breadcrumb:  breadcrumb,
			Heading:     heading,
			Embedding:   emb,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return chunks, nil
}

// SearchChunksByVector performs cosine similarity search across code chunks.
func (s *Store) SearchChunksByVector(ctx context.Context, query embeddings.Embedding, k int) ([]codeindex.SimilarChunk, int, error) {
	return s.SearchChunksByVectorFiltered(ctx, query, k, codeindex.VectorSearchFilters{})
}

// SearchChunksByVectorFiltered performs cosine search with optional early filtering.
func (s *Store) SearchChunksByVectorFiltered(ctx context.Context, query embeddings.Embedding, k int, filters codeindex.VectorSearchFilters) ([]codeindex.SimilarChunk, int, error) {
	if len(query) == 0 {
		return nil, 0, errors.New("query embedding is empty")
	}
	if math.Sqrt(dotFloat64(query, query)) == 0 {
		return nil, 0, errors.New("query embedding has zero norm")
	}
	queryDims := len(query)
	if err := s.ensureVecMirrors(ctx, queryDims); err != nil {
		return nil, 0, err
	}
	queryBlob := embedToBytes(query)
	chunkVecTable := chunkVecTableName(queryDims)

	candidateWhere := `c.dimensions = ?`
	candidateArgs := []any{queryDims}
	var clauses []string
	if len(filters.PathPrefixes) > 0 {
		var parts []string
		for _, p := range filters.PathPrefixes {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			parts = append(parts, "a.path LIKE ?")
			candidateArgs = append(candidateArgs, p+"%")
		}
		if len(parts) > 0 {
			clauses = append(clauses, "("+strings.Join(parts, " OR ")+")")
		}
	}
	if len(filters.Kinds) > 0 {
		var parts []string
		for _, knd := range filters.Kinds {
			knd = strings.TrimSpace(strings.ToLower(knd))
			if knd == "" {
				continue
			}
			parts = append(parts, "?")
			candidateArgs = append(candidateArgs, knd)
		}
		if len(parts) > 0 {
			clauses = append(clauses, "lower(a.kind) IN ("+strings.Join(parts, ",")+")")
		}
	}
	if len(filters.Granularity) > 0 {
		var parts []string
		for _, g := range filters.Granularity {
			g = strings.TrimSpace(strings.ToLower(g))
			if g == "" {
				continue
			}
			parts = append(parts, "?")
			candidateArgs = append(candidateArgs, g)
		}
		if len(parts) > 0 {
			clauses = append(clauses, "lower(c.granularity) IN ("+strings.Join(parts, ",")+")")
		}
	}
	if gen, ok, err := s.currentVisibleGeneration(ctx); err != nil {
		return nil, 0, err
	} else if ok {
		clauses = append(clauses, "i.sync_generation = ?")
		candidateArgs = append(candidateArgs, gen)
	}
	if len(clauses) > 0 {
		candidateWhere += " AND " + strings.Join(clauses, " AND ")
	}
	base := `
		WITH candidates AS (
			SELECT c.id,
			       a.anchor_id AS anchor_id,
			       a.path AS path,
			       a.symbol AS symbol,
			       a.fqn AS fqn,
			       a.kind AS kind,
			       c.chunk_index, c.granularity, c.breadcrumb, c.heading
			FROM ` + tableChunkEmbeddings + ` c
			JOIN ` + tableItems + ` i ON c.item_row_id = i.id
			JOIN intel_code_anchors a ON a.id = i.anchor_row_id
			WHERE ` + candidateWhere + `
		)
		SELECT candidates.anchor_id, candidates.path, candidates.symbol, candidates.fqn, candidates.kind,
		       candidates.chunk_index, candidates.granularity, candidates.breadcrumb, candidates.heading,
		       1.0 - vec_distance_cosine(v.embedding, ?) AS score
		FROM candidates
		JOIN ` + chunkVecTable + ` v ON v.chunk_id = candidates.id`
	args := make([]any, 0, len(candidateArgs)+2)
	args = append(args, candidateArgs...)
	args = append(args, queryBlob)
	base += " ORDER BY score DESC"
	if k > 0 {
		base += " LIMIT ?"
		args = append(args, k)
	}

	rows, err := s.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	capHint := 0
	if k > 0 {
		capHint = k
	}
	out := make([]codeindex.SimilarChunk, 0, capHint)
	for rows.Next() {
		var sc codeindex.SimilarChunk
		if err := rows.Scan(&sc.AnchorID, &sc.Path, &sc.Symbol, &sc.FQN, &sc.Kind, &sc.ChunkIndex, &sc.Granularity, &sc.Breadcrumb, &sc.Heading, &sc.Score); err != nil {
			return nil, 0, err
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if k > 0 && len(out) > k {
		out = out[:k]
	}
	return out, 0, nil
}

// SearchItemsByVector performs cosine similarity search across item embeddings.
func (s *Store) SearchItemsByVector(ctx context.Context, query embeddings.Embedding, k int) ([]codeindex.SimilarItem, int, error) {
	if len(query) == 0 {
		return nil, 0, errors.New("query embedding is empty")
	}
	if math.Sqrt(dotFloat64(query, query)) == 0 {
		return nil, 0, errors.New("query embedding has zero norm")
	}
	queryDims := len(query)
	if err := s.ensureVecMirrors(ctx, queryDims); err != nil {
		return nil, 0, err
	}
	queryBlob := embedToBytes(query)
	itemVecTable := itemVecTableName(queryDims)

	base := `
                SELECT a.anchor_id, a.path, a.symbol, a.fqn, a.kind,
                       1.0 - vec_distance_cosine(v.embedding, ?) AS score
                FROM ` + itemVecTable + ` v
                JOIN ` + tableItemEmbeddings + ` e ON e.id = v.item_id
                JOIN ` + tableItems + ` i ON e.item_row_id = i.id
                JOIN intel_code_anchors a ON a.id = i.anchor_row_id
                WHERE e.dimensions = ?`
	args := []any{queryBlob, queryDims}
	if gen, ok, err := s.currentVisibleGeneration(ctx); err != nil {
		return nil, 0, err
	} else if ok {
		base += ` AND i.sync_generation = ?`
		args = append(args, gen)
	}
	base += ` ORDER BY score DESC`
	if k > 0 {
		base += " LIMIT ?"
		args = append(args, k)
	}
	rows, err := s.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	capHint := 0
	if k > 0 {
		capHint = k
	}
	out := make([]codeindex.SimilarItem, 0, capHint)
	for rows.Next() {
		var si codeindex.SimilarItem
		if err := rows.Scan(&si.AnchorID, &si.Path, &si.Symbol, &si.FQN, &si.Kind, &si.Score); err != nil {
			return nil, 0, err
		}
		out = append(out, si)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if k > 0 && len(out) > k {
		out = out[:k]
	}
	return out, 0, nil
}

// SearchChunksByText performs a lexical search using the semantic FTS index.
func (s *Store) SearchChunksByText(ctx context.Context, query string, k int) ([]codeindex.SimilarChunk, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if k <= 0 {
		k = 25
	}

	ftsQuery := sqliteutil.FTS5QueryFromText(query)
	if ftsQuery == "" {
		return nil, nil
	}

	stmt := `SELECT ` + tableChunkFTS + `.anchor_id, ` + tableChunkFTS + `.chunk_index, ` + tableChunkFTS + `.path, ` + tableChunkFTS + `.symbol, ` + tableChunkFTS + `.fqn, ` + tableChunkFTS + `.kind, ` + tableChunkFTS + `.granularity, ` + tableChunkFTS + `.breadcrumb, ` + tableChunkFTS + `.heading, bm25(` + tableChunkFTS + `) AS rank
                FROM ` + tableChunkFTS + `
                JOIN intel_code_anchors a ON a.anchor_id = ` + tableChunkFTS + `.anchor_id
                JOIN ` + tableItems + ` i ON i.anchor_row_id = a.id
                WHERE ` + tableChunkFTS + ` MATCH ?`
	args := []any{ftsQuery}
	if gen, ok, err := s.currentVisibleGeneration(ctx); err != nil {
		return nil, err
	} else if ok {
		stmt += ` AND i.sync_generation = ?`
		args = append(args, gen)
	}
	stmt += ` ORDER BY rank ASC LIMIT ?`
	args = append(args, k)
	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []codeindex.SimilarChunk
	for rows.Next() {
		var sc codeindex.SimilarChunk
		var rank float64
		if err := rows.Scan(&sc.AnchorID, &sc.ChunkIndex, &sc.Path, &sc.Symbol, &sc.FQN, &sc.Kind, &sc.Granularity, &sc.Breadcrumb, &sc.Heading, &rank); err != nil {
			return nil, err
		}
		sc.Score = 1 / (1 + rank)
		out = append(out, sc)
	}
	return out, rows.Err()
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

// VisibleGeneration returns the committed code-embedding generation used by
// search queries. It does not expose an in-progress indexing generation.
func (s *Store) VisibleGeneration(ctx context.Context) (int64, bool, error) {
	return s.currentVisibleGeneration(ctx)
}

// GetChunkBody retrieves the chunk body text from FTS for a given anchor and chunk index.
// Returns empty string if not found. This avoids file I/O and includes doc comments that were
// included when the chunk was indexed.
func (s *Store) GetChunkBody(ctx context.Context, anchorID codeindex.AnchorID, chunkIndex int) (string, error) {
	var body string
	err := s.db.QueryRowContext(ctx,
		`SELECT body FROM `+tableChunkFTS+` WHERE anchor_id = ? AND chunk_index = ?`,
		string(anchorID), chunkIndex).Scan(&body)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return body, nil
}

// Stats returns counts of items and chunks.
func (s *Store) Stats(ctx context.Context) (int, int, error) {
	var items, chunks int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableItems).Scan(&items); err != nil {
		return 0, 0, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableChunkEmbeddings).Scan(&chunks); err != nil {
		return 0, 0, err
	}
	return items, chunks, nil
}

func embedToBytes(emb embeddings.Embedding) []byte {
	if len(emb) == 0 {
		return nil
	}
	b := make([]byte, 4*len(emb))
	for i, f := range emb {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func bytesToEmbed(b []byte) embeddings.Embedding {
	if len(b)%4 != 0 {
		return nil
	}
	out := make(embeddings.Embedding, len(b)/4)
	for i := range out {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		out[i] = math.Float32frombits(bits)
	}
	return out
}

func (s *Store) embeddingFromBlob(blob []byte, dims int) (embeddings.Embedding, bool, error) {
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

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func dotFloat64(a, b embeddings.Embedding) float64 {
	if len(a) != len(b) {
		return 0
	}
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

func execWithRetry(ctx context.Context, db *sql.DB, stmt string, args ...any) (sql.Result, error) {
	const maxAttempts = 5
	const baseBackoff = 50 * time.Millisecond
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := db.ExecContext(ctx, stmt, args...)
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
