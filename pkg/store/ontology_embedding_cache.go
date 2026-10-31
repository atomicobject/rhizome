package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// OntologyEmbeddingCacheEntry is one provider vector keyed by the hash of the
// exact ontology chunk text that produced it.
type OntologyEmbeddingCacheEntry struct {
	Provider string
	Model    string
	TextHash string
	Vector   embeddings.Embedding
}

// ontologyEmbeddingCacheColumns is the cache table's validated shape.
var ontologyEmbeddingCacheColumns = []string{"provider", "model", "dimensions", "text_hash", "vector", "last_used_at"}

// ontologyEmbeddingCacheIntact reports whether a domain reset may keep the
// cache: it is a real table with every expected column. Paid vectors survive
// resets the same way the note and code embedding caches do; a malformed
// table is dropped and recreated empty.
func ontologyEmbeddingCacheIntact(ctx context.Context, q queryRower) (bool, error) {
	exists, err := tableExistsQuery(ctx, q, "ontology_embedding_cache")
	if err != nil || !exists {
		return false, err
	}
	for _, column := range ontologyEmbeddingCacheColumns {
		ok, err := tableHasColumnQuery(ctx, q, "ontology_embedding_cache", column)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// The cache is content-addressed: ownership transitions, node deletion and
// ontology rebuilds delete chunk and state rows but never cache rows, so text
// that was embedded once is not sent to the provider again.
func createOntologyEmbeddingCacheSchema(ctx context.Context, db execer) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS ontology_embedding_cache (
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			dimensions INTEGER NOT NULL CHECK (dimensions > 0),
			text_hash TEXT NOT NULL CHECK (text_hash != ''),
			vector BLOB NOT NULL,
			last_used_at INTEGER NOT NULL,
			PRIMARY KEY (provider, model, dimensions, text_hash)
		) STRICT`,
		`CREATE INDEX IF NOT EXISTS idx_ontology_embedding_cache_last_used ON ontology_embedding_cache(last_used_at)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// OntologyEmbeddingCacheVectors returns cached vectors of exactly dims values
// keyed by text hash. An unknown size (dims <= 0) matches nothing.
func (s *Store) OntologyEmbeddingCacheVectors(ctx context.Context, provider, model string, dims int, hashes []string) (map[string]embeddings.Embedding, error) {
	hashes = normalizeNonEmptyStrings(hashes)
	out := make(map[string]embeddings.Embedding, len(hashes))
	if dims <= 0 {
		return out, nil
	}
	for batch := range slices.Chunk(hashes, 400) {
		holders, args := placeholders(batch)
		args = append([]any{provider, model, dims}, args...)
		rows, err := s.db.QueryContext(ctx, `
			SELECT text_hash, vector FROM ontology_embedding_cache
			WHERE provider = ? AND model = ? AND dimensions = ? AND text_hash IN (`+holders+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var hash string
			var blob []byte
			if err := rows.Scan(&hash, &blob); err != nil {
				rows.Close()
				return nil, err
			}
			out[hash] = bytesToEmbed(blob)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// UpsertOntologyEmbeddingCache records provider vectors. Rows are never pruned:
// text embedded once is not sent to the provider again.
func (s *Store) UpsertOntologyEmbeddingCache(ctx context.Context, entries []OntologyEmbeddingCacheEntry) error {
	if len(entries) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.upsert_ontology_embedding_cache")
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO ontology_embedding_cache (provider, model, dimensions, text_hash, vector, last_used_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(provider, model, dimensions, text_hash) DO UPDATE SET
				vector = excluded.vector,
				last_used_at = excluded.last_used_at`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		now := time.Now().Unix()
		for _, entry := range entries {
			if strings.TrimSpace(entry.TextHash) == "" || len(entry.Vector) == 0 {
				continue
			}
			if _, err := stmt.ExecContext(ctx, entry.Provider, entry.Model, len(entry.Vector), entry.TextHash, embedToBytes(entry.Vector), now); err != nil {
				return err
			}
		}
		return nil
	})
}

// backfillOntologyEmbeddingCacheSchema copies ontology vectors embedded before
// the cache existed (v78 created it empty), so the first ownership transition
// or projection-version bump after an upgrade reuses them instead of calling
// the provider. A vector table that cannot be read is skipped; its text is
// embedded once later.
func backfillOntologyEmbeddingCacheSchema(ctx context.Context, tx *sql.Tx) error { // v78 -> v79
	if err := createOntologyEmbeddingCacheSchema(ctx, tx); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT dimensions FROM intel_embeddings WHERE dimensions > 0`)
	if err != nil {
		return err
	}
	var dimensions []int
	for rows.Next() {
		var dims int
		if err := rows.Scan(&dims); err != nil {
			_ = rows.Close()
			return err
		}
		dimensions = append(dimensions, dims)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, dims := range dimensions {
		exists, err := tableExistsQuery(ctx, tx, intelVecTableName(dims))
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		_, err = tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT OR IGNORE INTO ontology_embedding_cache (provider, model, dimensions, text_hash, vector, last_used_at)
			SELECT s.provider, s.model, length(v.embedding) / 4, s.chunk_text_hash, v.embedding, ?
			FROM ontology_node_embedding_state s
			JOIN intel_embeddings e ON e.chunk_id = s.chunk_id AND e.dimensions = ?
			JOIN %s v ON v.chunk_id = e.chunk_row_id
			WHERE COALESCE(s.provider, '') != '' AND COALESCE(s.model, '') != '' AND s.chunk_text_hash != ''
				AND length(v.embedding) = ? * 4`, intelVecTableName(dims)), time.Now().Unix(), dims, dims)
		if err != nil && ctx.Err() != nil {
			return err
		}
	}
	return nil
}
