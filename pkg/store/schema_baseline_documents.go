package store

import (
	"context"
	"strings"
)

func intelBaselineStep04(ctx context.Context, db execer) error { // v4 -> v5
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS intel_code_anchors (
				anchor_id TEXT PRIMARY KEY,
				lang TEXT NOT NULL,
				kind TEXT NOT NULL,
				path TEXT NOT NULL,
				symbol TEXT NOT NULL,
				fqn TEXT,
				signature TEXT,
				doc_comment TEXT,
				start_byte INTEGER NOT NULL,
				end_byte INTEGER NOT NULL,
				start_line INTEGER NOT NULL,
				end_line INTEGER NOT NULL,
				fingerprint TEXT NOT NULL,
				updated_at INTEGER NOT NULL
			);`,
		`CREATE TABLE IF NOT EXISTS intel_doc_sections (
				section_id TEXT PRIMARY KEY,
				path TEXT NOT NULL,
				title TEXT NOT NULL,
				level INTEGER NOT NULL,
				start_byte INTEGER NOT NULL,
				end_byte INTEGER NOT NULL,
				content TEXT NOT NULL,
				fingerprint TEXT NOT NULL,
				updated_at INTEGER NOT NULL
			);`,
		`CREATE TABLE IF NOT EXISTS intel_edges (
				src_id TEXT NOT NULL,
				dst_id TEXT NOT NULL,
				kind TEXT NOT NULL,
				meta_json TEXT,
				PRIMARY KEY (src_id, dst_id, kind)
			);`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS intel_fts USING fts5(
				item_type,
				item_id,
				path,
				title,
				body,
				tokenize='porter'
			);`,
		`CREATE TABLE IF NOT EXISTS intel_fts_rowid (
				item_type TEXT NOT NULL,
				item_id TEXT NOT NULL,
				fts_rowid INTEGER NOT NULL,
				PRIMARY KEY (item_type, item_id)
			);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_path ON intel_code_anchors(path);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_fqn ON intel_code_anchors(fqn);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_edges_dst_kind ON intel_edges(dst_id, kind);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_edges_src_kind ON intel_edges(src_id, kind);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_doc_sections_path ON intel_doc_sections(path);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_fts_rowid_item ON intel_fts_rowid(item_type, item_id);`,
		// Force a one-time reindex after the intel schema is introduced.
		// Existing hashes would otherwise cause IndexCodeFile to skip unchanged files,
		// leaving intel tables empty/stale until a rebuild.
		`UPDATE files SET hash = NULL;`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep07(ctx context.Context, db execer) error { // v7 -> v8
	// Intel schema upgrade:
	// - Use modern SQLite table features (STRICT + WITHOUT ROWID + basic invariants).
	// - Add traversal-oriented indexes (kind-first + hot partial index for mentions).
	// - Force a one-time reindex so intel/FTS is rebuilt after the structural change.
	//
	// The intel tables are derived and safe to drop/rebuild; correctness is ensured by reindex.
	stmts := []string{
		// Drop (derived) intel tables. This intentionally clears intel + FTS data.
		`DROP TABLE IF EXISTS intel_fts_rowid;`,
		`DROP TABLE IF EXISTS intel_fts;`,
		`DROP TABLE IF EXISTS intel_edges;`,
		`DROP TABLE IF EXISTS intel_doc_sections;`,
		`DROP TABLE IF EXISTS intel_code_anchors;`,

		`CREATE TABLE IF NOT EXISTS intel_code_anchors (
					anchor_id TEXT PRIMARY KEY CHECK (anchor_id != ''),
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
				) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS intel_doc_sections (
					section_id TEXT PRIMARY KEY CHECK (section_id != ''),
					path TEXT NOT NULL CHECK (path != ''),
					title TEXT NOT NULL,
					level INTEGER NOT NULL CHECK (level >= 1 AND level <= 6),
					start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
					end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
					content TEXT NOT NULL,
					fingerprint TEXT NOT NULL CHECK (fingerprint != ''),
					updated_at INTEGER NOT NULL CHECK (updated_at >= 0)
				) WITHOUT ROWID, STRICT;`,
		`CREATE TABLE IF NOT EXISTS intel_edges (
					src_id TEXT NOT NULL CHECK (src_id != ''),
					dst_id TEXT NOT NULL CHECK (dst_id != ''),
					kind TEXT NOT NULL CHECK (kind != ''),
					meta_json TEXT,
					PRIMARY KEY (src_id, dst_id, kind)
				) WITHOUT ROWID, STRICT;`,
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

		// Indexes tuned for common lookups + traversal.
		`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_path ON intel_code_anchors(path);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_code_anchors_lang_fqn ON intel_code_anchors(lang, fqn) WHERE fqn IS NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_doc_sections_path ON intel_doc_sections(path);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_edges_kind_dst ON intel_edges(kind, dst_id);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_edges_kind_src ON intel_edges(kind, src_id);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_edges_mentions_dst ON intel_edges(dst_id) WHERE kind = 'mentions';`,

		// Force a one-time reindex after the intel schema changes.
		`UPDATE files SET hash = NULL;`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep24(ctx context.Context, db execer) error { // v24 -> v25
	stmts := []string{
		`ALTER TABLE notes ADD COLUMN content_hash TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE notes ADD COLUMN indexer_version TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE notes ADD COLUMN mtime INTEGER NOT NULL DEFAULT 0;`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `
			UPDATE notes
			SET mtime = COALESCE(
				(SELECT MAX(updated_at) FROM intel_doc_sections WHERE path = notes.path),
				mtime,
				0
			)
			WHERE COALESCE(mtime, 0) = 0
		`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return err
		}
	}
	return nil
}
