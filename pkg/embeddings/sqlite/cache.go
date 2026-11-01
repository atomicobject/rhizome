package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func (s *engine) embeddingFromBlob(blob []byte, dims int) (embeddings.Embedding, bool, error) {
	emb := bytesToEmbed(blob)
	if dims > 0 && len(emb) != dims {
		return nil, false, nil
	}
	if expected := s.Dimensions(); expected > 0 && len(emb) != expected {
		return nil, false, nil
	}
	if s.Dimensions() == 0 && len(emb) > 0 {
		s.runtime.Dimensions.Store(int64(len(emb)))
	}
	return emb, true, nil
}
func (s *engine) EmbeddingByHash(ctx context.Context, hash string) (embeddings.Embedding, bool, error) {
	if strings.TrimSpace(hash) == "" {
		return nil, false, nil
	}
	var blob []byte
	var dims int
	if err := s.db.QueryRowContext(ctx, `SELECT embedding,dimensions FROM `+s.spec.cache+` WHERE content_hash=?`, hash).Scan(&blob, &dims); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return s.embeddingFromBlob(blob, dims)
}
func (s *engine) EmbeddingByHashes(ctx context.Context, hashes []string) (map[string]embeddings.Embedding, error) {
	unique := dedupeStrings(hashes)
	out := make(map[string]embeddings.Embedding, len(unique))
	for batch := range slices.Chunk(unique, hashLookupBatchSize) {
		args := make([]any, len(batch))
		for i, h := range batch {
			args[i] = h
		}
		rows, err := s.db.QueryContext(ctx, `SELECT content_hash,embedding,dimensions FROM `+s.spec.cache+` WHERE content_hash IN (`+makePlaceholders(len(batch))+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var h string
			var blob []byte
			var dims int
			if err := rows.Scan(&h, &blob, &dims); err != nil {
				rows.Close()
				return nil, err
			}
			v, ok, err := s.embeddingFromBlob(blob, dims)
			if err != nil {
				rows.Close()
				return nil, err
			}
			if ok {
				out[h] = v
			}
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	return out, nil
}
func (s *engine) CacheEmbedding(ctx context.Context, hash string, emb embeddings.Embedding) error {
	ctx = indexingperf.WithOp(ctx, s.spec.label+".cache_embedding")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if strings.TrimSpace(hash) == "" || len(emb) == 0 {
			return nil
		}
		_, err := execWithRetry(ctx, db, cacheInsertSQL(s.spec.cache)+` VALUES(?,?,?,?,?,?)`+cacheConflictSQL(s.spec.cache), hash, embedToBytes(emb), math.Sqrt(dotFloat64(emb, emb)), len(emb), time.Now().Unix(), cacheFormat(s.spec.cache))
		if err == nil && s.Dimensions() == 0 {
			s.runtime.Dimensions.Store(int64(len(emb)))
		}
		return err
	})
}
func (s *engine) ClearEmbeddingCache(ctx context.Context) error {
	ctx = indexingperf.WithOp(ctx, s.spec.label+".clear_embedding_cache")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := execWithRetry(ctx, db, `DELETE FROM `+s.spec.cache)
		return err
	})
}

// The persisted domains deliberately have different content-hash conflict
// policies: notes keep the first vector, code updates it. Both stamp the
// current text format.
func cacheInsertSQL(table string) string {
	return `INSERT INTO ` + table + ` (content_hash,embedding,norm,dimensions,created_at,format)`
}
func cacheConflictSQL(table string) string {
	if table != codeTableEmbeddingCache {
		return ` ON CONFLICT(content_hash) DO UPDATE SET format=excluded.format`
	}
	return ` ON CONFLICT(content_hash) DO UPDATE SET embedding=excluded.embedding,norm=excluded.norm,dimensions=excluded.dimensions,created_at=excluded.created_at,format=excluded.format`
}

// cacheFormat is the text format that produced a domain's current vectors.
func cacheFormat(table string) string {
	if table == codeTableEmbeddingCache {
		return embeddings.CodeEmbeddingTextFormat
	}
	return embeddings.NoteEmbeddingTextFormat
}
func cacheEmbeddingTx(ctx context.Context, tx *sql.Tx, table string, hash string, emb embeddings.Embedding, now int64) error {
	if strings.TrimSpace(hash) == "" || len(emb) == 0 {
		return nil
	}
	return bulkUpsertEmbeddingCacheTx(ctx, tx, table, []embeddingCacheRow{{hash: hash, vec: emb}}, now)
}
func bulkUpsertEmbeddingCacheTx(ctx context.Context, tx *sql.Tx, table string, rows []embeddingCacheRow, now int64) error {
	batchSize := embeddingCacheBatchSize
	switch table {
	case codeTableEmbeddingCache:
		batchSize = codeEmbeddingCacheBatchSize
	case tableEmbeddingCache:
	default:
		return fmt.Errorf("invalid embedding cache table: %s", table)
	}
	for batch := range slices.Chunk(rows, batchSize) {
		args := make([]any, 0, len(batch)*6)
		values := make([]string, 0, len(batch))
		for _, r := range batch {
			if strings.TrimSpace(r.hash) == "" || len(r.vec) == 0 {
				continue
			}
			values = append(values, "(?,?,?,?,?,?)")
			args = append(args, r.hash, embedToBytes(r.vec), math.Sqrt(dotFloat64(r.vec, r.vec)), len(r.vec), now, cacheFormat(table))
		}
		if len(values) == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, cacheInsertSQL(table)+` VALUES `+strings.Join(values, ",")+cacheConflictSQL(table), args...); err != nil {
			return err
		}
	}
	return nil
}

// PruneEmbeddingCache deletes cached vectors that an earlier text format
// produced and no current chunk or item uses. Vectors of the current format are
// kept even when unreferenced, so reverting an edit still reuses them. Every
// vector a current chunk or item uses first adopts the current format, so text
// that survived a format change keeps its revert reuse. Rows with an empty
// format (written before formats were recorded, or restored by recovery) are
// kept unless referenced, since nothing shows which format produced them.
// Callers run it only after a complete index with no pending derived work, so
// vectors awaiting republication are never removed.
func (s *engine) PruneEmbeddingCache(ctx context.Context) (int64, error) {
	ctx = indexingperf.WithOp(ctx, s.spec.label+".prune_embedding_cache")
	refs := `SELECT content_hash FROM ` + s.spec.chunks + ` WHERE content_hash IS NOT NULL AND content_hash!=''`
	if s.spec.itemVectors != "" {
		refs += ` UNION SELECT content_hash FROM ` + s.spec.itemVectors + ` WHERE content_hash IS NOT NULL AND content_hash!=''`
	}
	format := cacheFormat(s.spec.cache)
	var deleted int64
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE `+s.spec.cache+` SET format = ? WHERE format != ? AND content_hash IN (`+refs+`)`, format, format); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM `+s.spec.cache+` WHERE format NOT IN (?, '') AND content_hash NOT IN (`+refs+`)`, format)
		if err == nil {
			deleted, err = result.RowsAffected()
		}
		return err
	})
	return deleted, err
}
