package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

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
var ontologyEmbeddingCacheColumns = []string{"provider", "model", "dimensions", "text_hash", "vector"}

// ontologyEmbeddingCacheIntact reports whether a domain reset may keep the
// cache: it is a real table with exactly the expected columns, since an extra
// NOT NULL column (an interim v78 build had last_used_at) makes every upsert
// fail. Paid vectors survive resets the same way the note and code embedding
// caches do; a malformed table is dropped and recreated.
func ontologyEmbeddingCacheIntact(ctx context.Context, q queryRower) (bool, error) {
	exists, err := tableExistsQuery(ctx, q, "ontology_embedding_cache")
	if err != nil || !exists {
		return false, err
	}
	var columns string
	if err := q.QueryRowContext(ctx, `
		SELECT group_concat(name, ',') FROM (
			SELECT name FROM pragma_table_info('ontology_embedding_cache') ORDER BY name
		)`).Scan(&columns); err != nil {
		return false, err
	}
	return columns == strings.Join(slices.Sorted(slices.Values(ontologyEmbeddingCacheColumns)), ","), nil
}

// repairOntologyEmbeddingCache rebuilds a current-version cache table whose
// columns are wrong and backfills it from stored vectors, so an interim build's
// table is replaced without resetting the rest of Intel.
func (s *Store) repairOntologyEmbeddingCache(ctx context.Context) error {
	version, dirty, exists, err := s.intelSchemaState(ctx)
	if err != nil || !exists || dirty || version != currentSchemaVersion {
		return err
	}
	// Any other missing table is drift that the domain reset repairs.
	for _, table := range []string{"ontology_embedding_cache", "intel_embeddings", "ontology_node_embedding_state"} {
		if exists, err := tableExistsQuery(ctx, s.db, table); err != nil || !exists {
			return err
		}
	}
	intact, err := ontologyEmbeddingCacheIntact(ctx, s.db)
	if err != nil || intact {
		return err
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS ontology_embedding_cache`); err != nil {
			return err
		}
		return backfillOntologyEmbeddingCacheSchema(ctx, tx)
	})
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
			PRIMARY KEY (provider, model, dimensions, text_hash)
		) STRICT`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// OntologyEmbeddingCacheVectors returns cached vectors of exactly dims values
// keyed by text hash. Default provider configurations learn their size only
// from a first provider response, so an unknown size (dims <= 0) uses the one
// size cached for this provider and model; with none or several it matches
// nothing rather than guessing.
func (s *Store) OntologyEmbeddingCacheVectors(ctx context.Context, provider, model string, dims int, hashes []string) (map[string]embeddings.Embedding, error) {
	hashes = normalizeNonEmptyStrings(hashes)
	out := make(map[string]embeddings.Embedding, len(hashes))
	if dims <= 0 && len(hashes) > 0 {
		var smallest, largest sql.NullInt64
		if err := s.db.QueryRowContext(ctx, `
			SELECT
				(SELECT MIN(dimensions) FROM ontology_embedding_cache WHERE provider = ? AND model = ?),
				(SELECT MAX(dimensions) FROM ontology_embedding_cache WHERE provider = ? AND model = ?)`,
			provider, model, provider, model).Scan(&smallest, &largest); err != nil {
			return nil, err
		}
		if smallest.Valid && smallest.Int64 == largest.Int64 {
			dims = int(smallest.Int64)
		}
	}
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
			INSERT INTO ontology_embedding_cache (provider, model, dimensions, text_hash, vector)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(provider, model, dimensions, text_hash) DO UPDATE SET
				vector = excluded.vector`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, entry := range entries {
			if strings.TrimSpace(entry.TextHash) == "" || len(entry.Vector) == 0 {
				continue
			}
			if _, err := stmt.ExecContext(ctx, entry.Provider, entry.Model, len(entry.Vector), entry.TextHash, embedToBytes(entry.Vector)); err != nil {
				return err
			}
		}
		return nil
	})
}

// backfillOntologyEmbeddingCacheSchema copies ontology vectors embedded before
// the cache existed, so the first ownership transition
// or projection-version bump after an upgrade reuses them instead of calling
// the provider. A vector table that cannot be read is skipped; its text is
// embedded once later.
func backfillOntologyEmbeddingCacheSchema(ctx context.Context, tx *sql.Tx) error { // v77 -> v78
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
			INSERT OR IGNORE INTO ontology_embedding_cache (provider, model, dimensions, text_hash, vector)
			SELECT s.provider, s.model, length(v.embedding) / 4, s.chunk_text_hash, v.embedding
			FROM ontology_node_embedding_state s
			JOIN intel_embeddings e ON e.chunk_id = s.chunk_id AND e.dimensions = ?
			JOIN %s v ON v.chunk_id = e.chunk_row_id
			WHERE COALESCE(s.provider, '') != '' AND COALESCE(s.model, '') != '' AND s.chunk_text_hash != ''
				AND length(v.embedding) = ? * 4`, intelVecTableName(dims)), dims, dims)
		if err != nil && ctx.Err() != nil {
			return err
		}
	}
	return nil
}
