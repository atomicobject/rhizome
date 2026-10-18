package store

import (
	"context"
	"strings"
)

func intelBaselineStep16(ctx context.Context, db execer) error { // v16 -> v17
	// Intel Spine: add intel_chunks table for explicit chunk ordering + provenance.
	// Also add pack metadata entries for deterministic builds.
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS intel_chunks (
				chunk_id TEXT PRIMARY KEY CHECK (chunk_id != ''),
				owner_id TEXT NOT NULL CHECK (owner_id != ''),
				owner_type TEXT NOT NULL CHECK (owner_type IN ('anchor', 'doc_section')),
				ord INTEGER NOT NULL CHECK (ord >= 0),
				granularity TEXT NOT NULL CHECK (granularity != ''),
				breadcrumb TEXT,
				heading TEXT,
				content_hash TEXT NOT NULL CHECK (content_hash != ''),
				start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
				end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
				updated_at INTEGER NOT NULL CHECK (updated_at >= 0),
				UNIQUE(owner_id, ord, granularity)
			) WITHOUT ROWID, STRICT;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_chunks_owner ON intel_chunks(owner_id);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func intelBaselineStep44(ctx context.Context, db execer) error { // v44 -> v45
	stmts := []string{
		`ALTER TABLE intel_chunks ADD COLUMN chunk_family TEXT NOT NULL DEFAULT 'default' CHECK (chunk_family != '');`,
		`CREATE INDEX IF NOT EXISTS idx_intel_chunks_family_owner ON intel_chunks(chunk_family, owner_type, owner_id, ord);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}
