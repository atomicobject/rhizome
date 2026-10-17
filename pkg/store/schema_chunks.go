package store

import (
	"context"
)

func recreateIntelChunksWithOntologyNode(ctx context.Context, db execer) error {
	stmts := []string{
		`DROP TABLE IF EXISTS intel_chunks_new;`,
		`DROP TABLE IF EXISTS intel_embeddings_v44_backup;`,
		`CREATE TEMP TABLE intel_embeddings_v44_backup AS
			SELECT chunk_row_id, chunk_id, norm, dimensions, created_at
			FROM intel_embeddings;`,
		`CREATE TABLE intel_chunks_new (
			id INTEGER PRIMARY KEY,
			chunk_id TEXT NOT NULL UNIQUE CHECK (chunk_id != ''),
			owner_id TEXT NOT NULL CHECK (owner_id != ''),
			owner_row_id INTEGER,
			owner_type TEXT NOT NULL CHECK (owner_type IN ('anchor', 'doc_section', 'ontology_node')),
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
		`INSERT OR IGNORE INTO intel_chunks_new (
			id, chunk_id, owner_id, owner_row_id, owner_type, chunk_family, ord, granularity,
			breadcrumb, heading, content_hash, start_byte, end_byte, updated_at
		)
		SELECT id, chunk_id, owner_id, owner_row_id, owner_type, chunk_family, ord, granularity,
			breadcrumb, heading, content_hash, start_byte, end_byte, updated_at
		FROM intel_chunks;`,
		`DROP TABLE intel_chunks;`,
		`ALTER TABLE intel_chunks_new RENAME TO intel_chunks;`,
		`INSERT OR IGNORE INTO intel_embeddings (chunk_row_id, chunk_id, norm, dimensions, created_at)
		SELECT b.chunk_row_id, b.chunk_id, b.norm, b.dimensions, b.created_at
		FROM intel_embeddings_v44_backup b
		JOIN intel_chunks c ON c.id = b.chunk_row_id AND c.chunk_id = b.chunk_id;`,
		`DROP TABLE IF EXISTS intel_embeddings_v44_backup;`,
		`CREATE INDEX IF NOT EXISTS idx_intel_chunks_owner ON intel_chunks(owner_type, owner_id);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_chunks_owner_id ON intel_chunks(owner_id);`,
		`CREATE INDEX IF NOT EXISTS idx_intel_chunks_family_owner ON intel_chunks(chunk_family, owner_type, owner_id, ord);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
