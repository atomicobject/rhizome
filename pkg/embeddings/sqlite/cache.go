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
		_, err := execWithRetry(ctx, db, cacheInsertSQL(s.spec.cache)+` VALUES(?,?,?,?,?)`+cacheConflictSQL(s.spec.cache), hash, embedToBytes(emb), math.Sqrt(dotFloat64(emb, emb)), len(emb), time.Now().Unix())
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

// The persisted domains deliberately have different content-hash conflict policies.
func cacheInsertSQL(table string) string {
	verb := "INSERT INTO "
	if table == tableEmbeddingCache {
		verb = "INSERT OR IGNORE INTO "
	}
	return verb + table + ` (content_hash,embedding,norm,dimensions,created_at)`
}
func cacheConflictSQL(table string) string {
	if table != codeTableEmbeddingCache {
		return ""
	}
	return ` ON CONFLICT(content_hash) DO UPDATE SET embedding=excluded.embedding,norm=excluded.norm,dimensions=excluded.dimensions,created_at=excluded.created_at`
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
		args := make([]any, 0, len(batch)*5)
		values := make([]string, 0, len(batch))
		for _, r := range batch {
			if strings.TrimSpace(r.hash) == "" || len(r.vec) == 0 {
				continue
			}
			values = append(values, "(?,?,?,?,?)")
			args = append(args, r.hash, embedToBytes(r.vec), math.Sqrt(dotFloat64(r.vec, r.vec)), len(r.vec), now)
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
func (s *engine) PruneStaleCache(ctx context.Context, threshold float64) (int, error) {
	if err := s.requireWrite(); err != nil {
		return 0, err
	}
	ctx = indexingperf.WithOp(ctx, s.spec.label+".prune_stale_cache")
	refs := `SELECT content_hash FROM ` + s.spec.chunks + ` WHERE content_hash IS NOT NULL AND content_hash!=''`
	if s.spec.itemVectors != "" {
		refs += ` UNION SELECT content_hash FROM ` + s.spec.itemVectors + ` WHERE content_hash IS NOT NULL AND content_hash!=''`
	}
	predicate := ` WHERE content_hash NOT IN (` + refs + `)`
	var total, orphaned int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+s.spec.cache).Scan(&total); err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+s.spec.cache+predicate).Scan(&orphaned); err != nil {
		return 0, err
	}
	if orphaned == 0 || (threshold > 0 && float64(orphaned)/float64(total) < threshold) {
		return 0, nil
	}
	var count int64
	err := s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		result, err := execWithRetry(ctx, db, `DELETE FROM `+s.spec.cache+predicate)
		if err == nil {
			count, err = result.RowsAffected()
		}
		return err
	})
	return int(count), err
}
