package store

import (
	"context"
	"database/sql"
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

// OntologyEmbeddingCacheVectors returns cached vectors keyed by text hash.
// dims <= 0 means the provider's default size and accepts any stored size.
func (s *Store) OntologyEmbeddingCacheVectors(ctx context.Context, provider, model string, dims int, hashes []string) (map[string]embeddings.Embedding, error) {
	hashes = normalizeNonEmptyStrings(hashes)
	out := make(map[string]embeddings.Embedding, len(hashes))
	for batch := range slices.Chunk(hashes, 400) {
		holders, args := placeholders(batch)
		args = append([]any{provider, model, dims, dims}, args...)
		rows, err := s.db.QueryContext(ctx, `
			SELECT text_hash, vector FROM ontology_embedding_cache
			WHERE provider = ? AND model = ? AND (? <= 0 OR dimensions = ?) AND text_hash IN (`+holders+`)
			ORDER BY last_used_at`, args...)
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
			out[hash] = bytesToEmbed(blob) // most recently used size wins
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// UpsertOntologyEmbeddingCache records vectors and refreshes last_used_at for
// reused ones.
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

// PruneOntologyEmbeddingCache deletes vectors unused since cutoff whose text no
// current ontology chunk still has.
func (s *Store) PruneOntologyEmbeddingCache(ctx context.Context, cutoff time.Time) (int64, error) {
	ctx = indexingperf.WithOp(ctx, "intel.prune_ontology_embedding_cache")
	var deleted int64
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			DELETE FROM ontology_embedding_cache
			WHERE last_used_at < ?
				AND text_hash NOT IN (SELECT chunk_text_hash FROM ontology_node_embedding_state)`, cutoff.Unix())
		if err != nil {
			return err
		}
		deleted, err = result.RowsAffected()
		return err
	})
	return deleted, err
}
