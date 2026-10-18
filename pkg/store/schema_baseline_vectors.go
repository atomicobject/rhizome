package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func intelBaselineStep17(ctx context.Context, db execer) error { // v17 -> v18
	// Intel Spine: add intel_embeddings table to unify note and code vector storage.
	// Vectors are keyed by chunk_id from intel_chunks, enabling unified search.
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS intel_embeddings (
				chunk_id TEXT PRIMARY KEY CHECK (chunk_id != ''),
				embedding BLOB NOT NULL,
				norm REAL NOT NULL DEFAULT 0,
				dimensions INTEGER NOT NULL CHECK (dimensions > 0),
				created_at INTEGER NOT NULL CHECK (created_at >= 0),
				FOREIGN KEY(chunk_id) REFERENCES intel_chunks(chunk_id) ON DELETE CASCADE
			) WITHOUT ROWID, STRICT;`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep18(ctx context.Context, db execer) error { // v18 -> v19
	// Migrate legacy embeddings into intel_embeddings where matching intel_chunks exist.
	// This is a best-effort migration; embeddings without matching chunks are skipped.
	// After this migration, legacy tables can eventually be dropped.
	hasEmbeddingBlob := true
	hasCodeItemsAnchorRowID := false
	hasCodeItemsAnchorID := false
	hasIntelCodeAnchorsID := false
	if q, ok := db.(queryRower); ok {
		has, err := tableHasColumnQuery(ctx, q, "intel_embeddings", "embedding")
		if err != nil {
			return err
		}
		hasEmbeddingBlob = has
		hasCodeItemsAnchorRowID, err = tableHasColumnQuery(ctx, q, "code_items", "anchor_row_id")
		if err != nil && !strings.Contains(err.Error(), "no such table") {
			return err
		}
		hasCodeItemsAnchorID, err = tableHasColumnQuery(ctx, q, "code_items", "anchor_id")
		if err != nil && !strings.Contains(err.Error(), "no such table") {
			return err
		}
		hasIntelCodeAnchorsID, err = tableHasColumnQuery(ctx, q, "intel_code_anchors", "id")
		if err != nil && !strings.Contains(err.Error(), "no such table") {
			return err
		}
	}

	// Migrate code embeddings: code_chunk_embeddings -> intel_embeddings
	// Match current schema via code_items.anchor_row_id -> intel_code_anchors.anchor_id.
	// Fall back to the legacy code_items.anchor_id column for older DBs.
	codeMigration := ""
	switch {
	case hasCodeItemsAnchorRowID && hasIntelCodeAnchorsID:
		codeMigration = `
				INSERT OR IGNORE INTO intel_embeddings (chunk_id, embedding, norm, dimensions, created_at)
				SELECT c.chunk_id, ce.embedding, ce.norm, ce.dimensions, CAST(strftime('%s', 'now') AS INTEGER)
				FROM code_chunk_embeddings ce
				JOIN code_items ci ON ci.id = ce.item_row_id
				JOIN intel_code_anchors a ON a.id = ci.anchor_row_id
				JOIN intel_chunks c ON c.owner_id = a.anchor_id AND c.ord = ce.chunk_index AND c.owner_type = 'anchor'
				WHERE NOT EXISTS (SELECT 1 FROM intel_embeddings WHERE chunk_id = c.chunk_id)
			`
	case hasCodeItemsAnchorRowID:
		codeMigration = `
				INSERT OR IGNORE INTO intel_embeddings (chunk_id, embedding, norm, dimensions, created_at)
				SELECT c.chunk_id, ce.embedding, ce.norm, ce.dimensions, CAST(strftime('%s', 'now') AS INTEGER)
				FROM code_chunk_embeddings ce
				JOIN code_items ci ON ci.id = ce.item_row_id
				JOIN intel_code_anchors a ON a.rowid = ci.anchor_row_id
				JOIN intel_chunks c ON c.owner_id = a.anchor_id AND c.ord = ce.chunk_index AND c.owner_type = 'anchor'
				WHERE NOT EXISTS (SELECT 1 FROM intel_embeddings WHERE chunk_id = c.chunk_id)
			`
	case hasCodeItemsAnchorID:
		codeMigration = `
				INSERT OR IGNORE INTO intel_embeddings (chunk_id, embedding, norm, dimensions, created_at)
				SELECT c.chunk_id, ce.embedding, ce.norm, ce.dimensions, CAST(strftime('%s', 'now') AS INTEGER)
				FROM code_chunk_embeddings ce
				JOIN code_items ci ON ci.id = ce.item_row_id
				JOIN intel_chunks c ON c.owner_id = ci.anchor_id AND c.ord = ce.chunk_index AND c.owner_type = 'anchor'
				WHERE NOT EXISTS (SELECT 1 FROM intel_embeddings WHERE chunk_id = c.chunk_id)
			`
	}
	if hasEmbeddingBlob && codeMigration != "" {
		if _, err := db.ExecContext(ctx, codeMigration); err != nil {
			// Table might not exist; that's fine
			if !strings.Contains(err.Error(), "no such table") && !strings.Contains(err.Error(), "no such column: a.rowid") {
				return fmt.Errorf("migrate code embeddings: %w", err)
			}
		}
	}

	// Migrate note embeddings: emb_chunk_embeddings -> intel_embeddings
	// Match via: emb_notes.note_id + intel_doc_sections matching by path
	// Note: This is approximate since note_id is the title but intel uses section_id by path.
	// We match by note path and first section (ord=0), which covers single-section notes.
	noteMigration := `
			INSERT OR IGNORE INTO intel_embeddings (chunk_id, embedding, norm, dimensions, created_at)
			SELECT c.chunk_id, ne.embedding, ne.norm, ne.dimensions, CAST(strftime('%s', 'now') AS INTEGER)
			FROM emb_chunk_embeddings ne
			JOIN emb_notes n ON n.id = ne.note_row_id
			JOIN intel_doc_sections s ON s.path = n.path
			JOIN intel_chunks c ON c.owner_id = s.section_id AND c.ord = ne.chunk_index AND c.owner_type = 'doc_section'
			WHERE NOT EXISTS (SELECT 1 FROM intel_embeddings WHERE chunk_id = c.chunk_id)
		`
	if hasEmbeddingBlob {
		if _, err := db.ExecContext(ctx, noteMigration); err != nil {
			if !strings.Contains(err.Error(), "no such table") {
				return fmt.Errorf("migrate note embeddings: %w", err)
			}
		}
	}

	// Clean up orphaned doc_links entries where source files no longer exist.
	// This can happen when files are excluded via .gitignore after being indexed.
	orphanCleanup := `
			DELETE FROM doc_links
			WHERE src_type = 'code' AND src_path NOT IN (SELECT path FROM files)
		`
	if _, err := db.ExecContext(ctx, orphanCleanup); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("cleanup orphan doc_links: %w", err)
		}
	}

	return nil
}

func intelBaselineStep20(ctx context.Context, db execer) error { // v20 -> v21
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS intent_embeddings (
				intent TEXT NOT NULL,
				exemplar TEXT NOT NULL,
				embedding BLOB NOT NULL,
				norm REAL NOT NULL DEFAULT 0,
				dimensions INTEGER NOT NULL CHECK (dimensions > 0),
				updated_at INTEGER NOT NULL CHECK (updated_at >= 0),
				PRIMARY KEY (intent, exemplar)
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_intent_embeddings_intent ON intent_embeddings(intent);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep25(ctx context.Context, db execer) error { // v25 -> v26
	tx, ok := db.(*sql.Tx)
	if !ok {
		return errors.New("intel vec migration requires sql transaction")
	}
	_, err := migrateIntelEmbeddingsToVecPrimaryTx(ctx, tx)
	return err
}

func intelBaselineStep36(ctx context.Context, db execer) error { // v36 -> v37
	tx, ok := db.(*sql.Tx)
	if !ok {
		return errors.New("intel schema migration v37 requires sql transaction")
	}
	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_files_call_edges_stale ON files(call_edges_stale, path);`,
		`DROP INDEX IF EXISTS idx_intel_doc_sections_level;`,
		`DROP INDEX IF EXISTS idx_intel_chunks_owner_type;`,
		`DROP INDEX IF EXISTS idx_intel_embeddings_dimensions;`,
		`DROP TABLE IF EXISTS file_symbols;`,
		`CREATE TABLE IF NOT EXISTS intel_symbol_ref_targets (
				target_id INTEGER PRIMARY KEY,
				dst_lang  TEXT NOT NULL,
				dst_pkg   TEXT NOT NULL,
				dst_name  TEXT NOT NULL,
				dst_fqn   TEXT NOT NULL,
				dst_member INTEGER NOT NULL DEFAULT 0 CHECK (dst_member IN (0, 1)),
				UNIQUE (dst_lang, dst_pkg, dst_name, dst_fqn, dst_member)
			) STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_symbol_ref_targets_name ON intel_symbol_ref_targets(dst_lang, dst_name);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_symbol_ref_targets_fqn ON intel_symbol_ref_targets(dst_lang, dst_fqn);`,
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	hasRefsTable, err := tableExistsQuery(ctx, tx, "intel_symbol_refs")
	if err != nil {
		return err
	}
	hasSrcPath := false
	if hasRefsTable {
		hasSrcPath, err = tableHasColumnQuery(ctx, tx, "intel_symbol_refs", "src_path")
		if err != nil {
			return err
		}
	}
	if hasSrcPath {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE intel_symbol_refs RENAME TO intel_symbol_refs_legacy`); err != nil {
			return err
		}
	} else if hasRefsTable {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS intel_symbol_refs`); err != nil {
			return err
		}
	}

	createRefs := []string{
		`CREATE TABLE IF NOT EXISTS intel_symbol_refs (
				src_file_id INTEGER NOT NULL,
				owner_symbol_id INTEGER,
				owner_fqn TEXT NOT NULL,
				ref_kind TEXT NOT NULL,
				dst_target_id INTEGER NOT NULL,
				PRIMARY KEY (src_file_id, owner_fqn, ref_kind, dst_target_id),
				FOREIGN KEY(dst_target_id) REFERENCES intel_symbol_ref_targets(target_id) ON DELETE CASCADE
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_symbol_refs_dst_target ON intel_symbol_refs(dst_target_id, src_file_id, owner_symbol_id);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_symbol_refs_owner ON intel_symbol_refs(owner_symbol_id, src_file_id, dst_target_id) WHERE owner_symbol_id IS NOT NULL;`,
	}
	for _, stmt := range createRefs {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	exists, err := tableExistsQuery(ctx, tx, "intel_symbol_refs_legacy")
	if err != nil {
		return err
	}
	if exists {
		if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO intel_symbol_ref_files(path)
				SELECT DISTINCT src_path
				FROM intel_symbol_refs_legacy
			`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO intel_symbol_ref_targets(dst_lang, dst_pkg, dst_name, dst_fqn)
				SELECT DISTINCT dst_lang, dst_pkg, dst_name, dst_fqn
				FROM intel_symbol_refs_legacy
			`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO intel_symbol_refs(src_file_id, owner_symbol_id, owner_fqn, ref_kind, dst_target_id)
				SELECT sf.file_id, s.id, l.owner_fqn, l.ref_kind, t.target_id
				FROM intel_symbol_refs_legacy l
				JOIN intel_symbol_ref_files sf ON sf.path = l.src_path
				LEFT JOIN symbols s ON s.fqn = l.owner_fqn
				JOIN intel_symbol_ref_targets t
				  ON t.dst_lang = l.dst_lang
				 AND t.dst_pkg = l.dst_pkg
				 AND t.dst_name = l.dst_name
				 AND t.dst_fqn = l.dst_fqn
			`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE intel_symbol_refs_legacy`); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep37(ctx context.Context, db execer) error { // v37 -> v38
	tx, ok := db.(*sql.Tx)
	if ok {
		if err := dropVecTablesByPrefixTx(ctx, tx, intelVecTablePrefix); err != nil {
			return err
		}
	}
	stmts := []string{
		`DROP TABLE IF EXISTS intel_fts_rowid;`,
		`DROP TABLE IF EXISTS intel_fts;`,
		`DROP TABLE IF EXISTS intel_embeddings;`,
		`DROP TABLE IF EXISTS intel_chunks;`,
		`DROP TABLE IF EXISTS intel_edges;`,
		`DROP TABLE IF EXISTS intel_doc_sections;`,
		`DROP TABLE IF EXISTS intel_code_anchors;`,
		`CREATE TABLE IF NOT EXISTS intel_code_anchors (
					id INTEGER PRIMARY KEY,
					anchor_id TEXT NOT NULL UNIQUE CHECK (anchor_id != ''),
					lang TEXT NOT NULL CHECK (lang != ''),
					kind TEXT NOT NULL CHECK (kind != ''),
					path TEXT NOT NULL CHECK (path != ''),
					symbol TEXT NOT NULL CHECK (symbol != ''),
					fqn TEXT,
					signature TEXT,
					doc_comment TEXT,
					start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
					end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
					start_line INTEGER NOT NULL CHECK (start_line >= 0),
					end_line INTEGER NOT NULL CHECK (end_line >= start_line),
					fingerprint TEXT NOT NULL CHECK (fingerprint != ''),
					updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
				) STRICT;`,
		`CREATE TABLE IF NOT EXISTS intel_doc_sections (
					id INTEGER PRIMARY KEY,
					section_id TEXT NOT NULL UNIQUE CHECK (section_id != ''),
					path TEXT NOT NULL CHECK (path != ''),
					title TEXT NOT NULL,
					level INTEGER NOT NULL CHECK (level >= 1 AND level <= 6),
					start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
					end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
					content TEXT NOT NULL,
					fingerprint TEXT NOT NULL CHECK (fingerprint != ''),
					updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
				) STRICT;`,
		`CREATE TABLE IF NOT EXISTS intel_edges (
					src_type TEXT NOT NULL DEFAULT 'anchor' CHECK (src_type IN ('anchor', 'doc_section')),
					src_row_id INTEGER,
					dst_type TEXT NOT NULL DEFAULT 'anchor' CHECK (dst_type IN ('anchor', 'doc_section')),
					dst_row_id INTEGER,
					kind TEXT NOT NULL CHECK (kind != ''),
					meta_json TEXT,
					src_id TEXT NOT NULL CHECK (src_id != ''),
					dst_id TEXT NOT NULL CHECK (dst_id != ''),
					PRIMARY KEY (src_id, dst_id, kind)
				) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS intel_chunks (
					id INTEGER PRIMARY KEY,
					chunk_id TEXT NOT NULL UNIQUE CHECK (chunk_id != ''),
					owner_id TEXT NOT NULL CHECK (owner_id != ''),
					owner_row_id INTEGER,
					owner_type TEXT NOT NULL CHECK (owner_type IN ('anchor', 'doc_section')),
					chunk_family TEXT NOT NULL DEFAULT 'default' CHECK (chunk_family != ''),
					ord INTEGER NOT NULL CHECK (ord >= 0),
					granularity TEXT NOT NULL CHECK (granularity != ''),
					breadcrumb TEXT,
					heading TEXT,
					content_hash TEXT NOT NULL CHECK (content_hash != ''),
					start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
					end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
					updated_at INTEGER NOT NULL CHECK (updated_at >= 0),
					UNIQUE(owner_type, owner_id, ord, granularity)
				) STRICT;`,
		`CREATE TABLE IF NOT EXISTS intel_embeddings (
					chunk_row_id INTEGER PRIMARY KEY,
					chunk_id TEXT NOT NULL UNIQUE,
					norm REAL NOT NULL DEFAULT 0,
					dimensions INTEGER NOT NULL CHECK (dimensions > 0),
					created_at INTEGER NOT NULL CHECK (created_at >= 0),
					FOREIGN KEY(chunk_row_id) REFERENCES intel_chunks(id) ON DELETE CASCADE
				) STRICT;`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS intel_fts USING fts5(
					item_type,
					item_id,
					path,
					title,
					body,
					tokenize='porter'
				);`,
		`CREATE TABLE IF NOT EXISTS intel_fts_rowid (
					item_type TEXT NOT NULL CHECK (item_type != ''),
					item_id TEXT NOT NULL CHECK (item_id != ''),
					fts_rowid INTEGER NOT NULL CHECK (fts_rowid > 0),
					PRIMARY KEY (item_type, item_id)
				) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_path ON intel_code_anchors(path);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_symbol ON intel_code_anchors(symbol);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_path_symbol ON intel_code_anchors(path, symbol);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_lang_fqn ON intel_code_anchors(lang, fqn) WHERE fqn IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_doc_sections_path ON intel_doc_sections(path);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_edges_kind_dst ON intel_edges(kind, dst_type, dst_row_id);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_edges_kind_src ON intel_edges(kind, src_type, src_row_id);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_edges_mentions_dst ON intel_edges(dst_row_id) WHERE kind = 'mentions' AND dst_type = 'anchor' AND dst_row_id IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_chunks_owner ON intel_chunks(owner_type, owner_id, ord);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_chunks_owner_id ON intel_chunks(owner_id);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_chunks_family_owner ON intel_chunks(chunk_family, owner_type, owner_id, ord);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_fts_rowid_item ON intel_fts_rowid(item_type, item_id);`,
		`UPDATE files SET hash = NULL;`,
		`UPDATE notes SET mtime = 0, content_hash = '', indexer_version = '';`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
