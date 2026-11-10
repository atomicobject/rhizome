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

// EmbeddingCacheEntry is one provider vector keyed by the hash of the
// exact ontology or plain-note chunk text that produced it.
type EmbeddingCacheEntry struct {
	Provider string
	Model    string
	TextHash string
	Vector   embeddings.Embedding
	// Format names the chunk-text format that produced the text, so a later
	// format change can prune the vectors only the old format referenced.
	Format string
}

// embeddingCacheColumns is the cache table's validated shape.
var embeddingCacheColumns = []string{"provider", "model", "dimensions", "text_hash", "vector", "format"}

// legacyEmbeddingCacheTable is the name interim v78 builds gave the cache
// while it held only ontology vectors.
const legacyEmbeddingCacheTable = "ontology_embedding_cache"

// Text hashes that current chunks use: ontology embedding states and the
// authored-section chunks plain notes publish.
const (
	ontologyCacheRefs = `SELECT chunk_text_hash FROM ontology_node_embedding_state`
	noteCacheRefs     = `SELECT content_hash FROM intel_chunks WHERE owner_type = 'doc_section' AND content_hash != ''`
)

// embeddingCacheIntact reports whether a domain reset may keep the
// cache: it is a real table with exactly the expected columns, since an extra
// NOT NULL column (an interim v78 build had last_used_at) makes every upsert
// fail. Paid vectors survive resets the same way the note and code embedding
// caches do; a malformed table is dropped and recreated.
func embeddingCacheIntact(ctx context.Context, q queryRower) (bool, error) {
	exists, err := tableExistsQuery(ctx, q, "intel_embedding_cache")
	if err != nil || !exists {
		return false, err
	}
	var columns string
	if err := q.QueryRowContext(ctx, `
		SELECT group_concat(name, ',') FROM (
			SELECT name FROM pragma_table_info('intel_embedding_cache') ORDER BY name
		)`).Scan(&columns); err != nil {
		return false, err
	}
	return columns == strings.Join(slices.Sorted(slices.Values(embeddingCacheColumns)), ","), nil
}

// repairEmbeddingCache converges a current-version database from an interim
// build without resetting the rest of Intel: it rebuilds a cache table whose
// columns are wrong, moves the rows of the interim ontology-only table, and
// backfills ontology vectors from stored vectors.
func (s *Store) repairEmbeddingCache(ctx context.Context) error {
	version, dirty, exists, err := s.intelSchemaState(ctx)
	if err != nil || !exists || dirty || version != currentSchemaVersion {
		return err
	}
	// Any other missing table is drift that the domain reset repairs.
	for _, table := range []string{"intel_embeddings", "intel_chunks", "ontology_node_embedding_state"} {
		if exists, err := tableExistsQuery(ctx, s.db, table); err != nil || !exists {
			return err
		}
	}
	intact, err := embeddingCacheIntact(ctx, s.db)
	if err != nil {
		return err
	}
	legacy, err := tableExistsQuery(ctx, s.db, legacyEmbeddingCacheTable)
	if err != nil || (intact && !legacy) {
		return err
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if !intact {
			if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS intel_embedding_cache`); err != nil {
				return err
			}
		}
		if err := createEmbeddingCacheSchema(ctx, tx); err != nil {
			return err
		}
		if legacy {
			if err := moveLegacyEmbeddingCache(ctx, tx); err != nil {
				return err
			}
		}
		return backfillEmbeddingCacheSchema(ctx, tx)
	})
}

// moveLegacyEmbeddingCache copies the interim table's vectors, including
// unreferenced history that the stored-vector backfill cannot recover, then
// drops it. A table missing the key or vector columns has nothing usable.
func moveLegacyEmbeddingCache(ctx context.Context, tx *sql.Tx) error {
	usable := true
	for _, column := range []string{"provider", "model", "dimensions", "text_hash", "vector"} {
		has, err := tableHasColumnQuery(ctx, tx, legacyEmbeddingCacheTable, column)
		if err != nil {
			return err
		}
		usable = usable && has
	}
	if usable {
		format := "''"
		if has, err := tableHasColumnQuery(ctx, tx, legacyEmbeddingCacheTable, "format"); err != nil {
			return err
		} else if has {
			format = "format"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO intel_embedding_cache (provider, model, dimensions, text_hash, vector, format)
			SELECT provider, model, dimensions, text_hash, vector, `+format+` FROM `+legacyEmbeddingCacheTable+`
			WHERE provider != '' AND model != '' AND dimensions > 0 AND text_hash != '' AND length(vector) = dimensions * 4`); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `DROP TABLE `+legacyEmbeddingCacheTable)
	return err
}

// The cache is content-addressed: ownership transitions, node deletion and
// ontology rebuilds delete chunk and state rows but never cache rows, so text
// that was embedded once is not sent to the provider again.
func createEmbeddingCacheSchema(ctx context.Context, db execer) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS intel_embedding_cache (
			provider TEXT NOT NULL,
			model TEXT NOT NULL,
			dimensions INTEGER NOT NULL CHECK (dimensions > 0),
			text_hash TEXT NOT NULL CHECK (text_hash != ''),
			vector BLOB NOT NULL,
			format TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (provider, model, dimensions, text_hash)
		) STRICT`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// EmbeddingCacheVectors returns cached vectors of exactly dims values
// keyed by text hash. Default provider configurations learn their size only
// from a first provider response, so an unknown size (dims <= 0) uses the one
// size cached for this provider and model; with none or several it matches
// nothing rather than guessing.
func (s *Store) EmbeddingCacheVectors(ctx context.Context, provider, model string, dims int, hashes []string) (map[string]embeddings.Embedding, error) {
	hashes = normalizeNonEmptyStrings(hashes)
	out := make(map[string]embeddings.Embedding, len(hashes))
	if dims <= 0 && len(hashes) > 0 {
		var smallest, largest sql.NullInt64
		if err := s.db.QueryRowContext(ctx, `
			SELECT
				(SELECT MIN(dimensions) FROM intel_embedding_cache WHERE provider = ? AND model = ?),
				(SELECT MAX(dimensions) FROM intel_embedding_cache WHERE provider = ? AND model = ?)`,
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
			SELECT text_hash, vector FROM intel_embedding_cache
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

// UpsertEmbeddingCache records provider vectors. Text embedded once is
// not sent to the provider again; only PruneEmbeddingCache removes rows.
func (s *Store) UpsertEmbeddingCache(ctx context.Context, entries []EmbeddingCacheEntry) error {
	if len(entries) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "intel.upsert_embedding_cache")
	return s.withEmbeddingWriteTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO intel_embedding_cache (provider, model, dimensions, text_hash, vector, format)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(provider, model, dimensions, text_hash) DO UPDATE SET
				vector = excluded.vector,
				format = excluded.format`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, entry := range entries {
			if strings.TrimSpace(entry.TextHash) == "" || len(entry.Vector) == 0 {
				continue
			}
			if _, err := stmt.ExecContext(ctx, entry.Provider, entry.Model, len(entry.Vector), entry.TextHash, embedToBytes(entry.Vector), entry.Format); err != nil {
				return err
			}
		}
		return nil
	})
}

// backfillEmbeddingCacheSchema copies ontology vectors embedded before the
// cache existed, so the first ownership transition or projection-version bump
// after an upgrade reuses them instead of calling the provider. Ontology state
// hashes are the hashes of the exact embedded text. Plain-note vectors are not
// copied here: their stored hashes depend on the configured section limit,
// which only indexing knows (BackfillNoteEmbeddingCache). A vector table that
// cannot be read is skipped; its text is embedded once later.
func backfillEmbeddingCacheSchema(ctx context.Context, tx *sql.Tx) error { // v77 -> v78
	if err := createEmbeddingCacheSchema(ctx, tx); err != nil {
		return err
	}
	return forEachIntelVectorTable(ctx, tx, func(dims int, vecTable string) error {
		_, err := tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT OR IGNORE INTO intel_embedding_cache (provider, model, dimensions, text_hash, vector)
			SELECT s.provider, s.model, length(v.embedding) / 4, s.chunk_text_hash, v.embedding
			FROM ontology_node_embedding_state s
			JOIN intel_embeddings e ON e.chunk_id = s.chunk_id AND e.dimensions = ?
			JOIN %s v ON v.chunk_id = e.chunk_row_id
			WHERE COALESCE(s.provider, '') != '' AND COALESCE(s.model, '') != '' AND s.chunk_text_hash != ''
				AND length(v.embedding) = ? * 4`, vecTable), dims, dims)
		return err
	})
}

// forEachIntelVectorTable calls fn for each stored Intel vector size whose
// vector table exists. An fn error stops the walk only when ctx is done.
func forEachIntelVectorTable(ctx context.Context, tx *sql.Tx, fn func(dims int, vecTable string) error) error {
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
		if err := fn(dims, intelVecTableName(dims)); err != nil && ctx.Err() != nil {
			return err
		}
	}
	return nil
}

// noteEmbeddingCacheBackfillKey records in index_metadata that plain-note
// vectors stored before the cache existed have been considered.
const noteEmbeddingCacheBackfillKey = "intel_embedding_cache_note_backfill"

// NoteEmbeddingCacheBackfillPending reports whether BackfillNoteEmbeddingCache
// has not yet run on this database.
func (s *Store) NoteEmbeddingCacheBackfillPending(ctx context.Context) (bool, error) {
	_, ok, err := s.GetMetadata(ctx, noteEmbeddingCacheBackfillKey)
	return !ok, err
}

// BackfillNoteEmbeddingCache runs once per database: it copies published
// plain-note vectors into the cache under their stored chunk hashes, taking
// provider and model from the note embedding index metadata, and records that
// it ran. Builds before the cache hashed authored sections at the default
// section limit whatever limit embedded them, so the stored hash names the
// embedded text only when the effective limit is the default; any other
// caller passes copyVectors false and only records the marker. A one-time
// cache miss is acceptable; a vector served for differently truncated text is
// not.
func (s *Store) BackfillNoteEmbeddingCache(ctx context.Context, copyVectors bool) error {
	ctx = indexingperf.WithOp(ctx, "intel.backfill_note_embedding_cache")
	return s.withEmbeddingWriteTx(ctx, func(tx *sql.Tx) error {
		noteMeta, err := tableExistsQuery(ctx, tx, "emb_index_meta")
		if err != nil {
			return err
		}
		if copyVectors && noteMeta {
			if err := forEachIntelVectorTable(ctx, tx, func(dims int, vecTable string) error {
				_, err := tx.ExecContext(ctx, fmt.Sprintf(`
					INSERT OR IGNORE INTO intel_embedding_cache (provider, model, dimensions, text_hash, vector)
					SELECT m.provider, m.model, length(v.embedding) / 4, c.content_hash, v.embedding
					FROM emb_index_meta m
					JOIN intel_chunks c ON c.owner_type = 'doc_section' AND c.content_hash != ''
					JOIN intel_embeddings e ON e.chunk_id = c.chunk_id AND e.dimensions = ?
					JOIN %s v ON v.chunk_id = e.chunk_row_id
					WHERE m.id = 1 AND COALESCE(m.provider, '') != '' AND COALESCE(m.model, '') != '' AND m.dimensions = ?
						AND length(v.embedding) = ? * 4`, vecTable), dims, dims, dims)
				return err
			}); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO index_metadata(key, value) VALUES (?, '1')
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, noteEmbeddingCacheBackfillKey)
		return err
	})
}

// PruneEmbeddingCache deletes cached vectors that an earlier chunk-text
// format produced and no current ontology embedding state or plain-note chunk
// uses. Vectors of a current format are kept even when unreferenced, so
// reverting an edit still reuses them; a format change leaves the old
// generation unreferenced, and this removes it. Every vector a current chunk
// uses adopts its kind's current format here, so text that survived a format
// change keeps its revert reuse after a later edit. Unreferenced rows
// backfilled from existing vectors carry no format and are kept, since nothing
// shows which format produced them. Callers run it only after a complete index
// with no pending derived work, so vectors awaiting republication are never
// removed.
func (s *Store) PruneEmbeddingCache(ctx context.Context, ontologyFormat, noteFormat string) (int64, error) {
	ctx = indexingperf.WithOp(ctx, "intel.prune_embedding_cache")
	var deleted int64
	err := s.withEmbeddingWriteTx(ctx, func(tx *sql.Tx) error {
		for _, promote := range [][2]string{{ontologyFormat, ontologyCacheRefs}, {noteFormat, noteCacheRefs}} {
			if _, err := tx.ExecContext(ctx, `UPDATE intel_embedding_cache SET format = ?
				WHERE format NOT IN (?, ?) AND text_hash IN (`+promote[1]+`)`, promote[0], ontologyFormat, noteFormat); err != nil {
				return err
			}
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM intel_embedding_cache
			WHERE format NOT IN (?, ?, '') AND text_hash NOT IN (`+ontologyCacheRefs+`) AND text_hash NOT IN (`+noteCacheRefs+`)`, ontologyFormat, noteFormat)
		if err == nil {
			deleted, err = result.RowsAffected()
		}
		return err
	})
	return deleted, err
}
