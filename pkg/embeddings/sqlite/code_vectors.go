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
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

// GetItemEmbedding returns the embedding for a code item if present.
func (s *CodeStore) GetItemEmbedding(ctx context.Context, anchorID codeindex.AnchorID) (embeddings.Embedding, string, bool, error) {
	row := s.db.QueryRowContext(ctx, `
                SELECT e.embedding, e.content_hash, e.dimensions
                FROM `+tableItemEmbeddings+` e
                JOIN `+tableItems+` i ON e.item_row_id = i.id
                JOIN intel_code_anchors a ON a.id = i.anchor_row_id
                WHERE a.anchor_id = ?
        `, string(anchorID))
	var blob []byte
	var contentHash string
	var dims int
	if err := row.Scan(&blob, &contentHash, &dims); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", false, nil
		}
		return nil, "", false, err
	}
	emb := bytesToEmbed(blob)
	if dims > 0 && len(emb) != dims {
		return nil, "", false, nil
	}
	if s.runtime.Dimensions.Load() == 0 && len(emb) > 0 {
		s.runtime.Dimensions.Store(int64(len(emb)))
	}
	return emb, contentHash, true, nil
}

// UpsertItemEmbedding stores the embedding for a code item.
func (s *CodeStore) UpsertItemEmbedding(ctx context.Context, anchorID codeindex.AnchorID, contentHash string, emb embeddings.Embedding) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	if len(emb) == 0 {
		return nil
	}
	ctx = indexingperf.WithOp(ctx, "codeemb.upsert_item_embedding")
	norm := math.Sqrt(dotFloat64(emb, emb))
	now := time.Now().Unix()
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		if err := cacheEmbeddingTx(ctx, tx, codeTableEmbeddingCache, contentHash, emb, now); err != nil {
			return err
		}
		var rowID int64
		if err := tx.QueryRowContext(ctx, `SELECT i.id FROM `+tableItems+` i JOIN intel_code_anchors a ON a.id = i.anchor_row_id WHERE a.anchor_id = ?`, string(anchorID)).Scan(&rowID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO `+tableItemEmbeddings+` (item_row_id, content_hash, embedding, norm, dimensions, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(item_row_id) DO UPDATE SET
				content_hash = excluded.content_hash,
				embedding = excluded.embedding,
				norm = excluded.norm,
				dimensions = excluded.dimensions,
				created_at = excluded.created_at
		`, rowID, contentHash, embedToBytes(emb), norm, len(emb), now)
		return err
	})
	if err == nil && s.runtime.Dimensions.Load() == 0 && len(emb) > 0 {
		s.runtime.Dimensions.Store(int64(len(emb)))
	}
	return err
}

// UpsertItemEmbeddingBatch stores embeddings for multiple code items in one transaction.
func (s *CodeStore) UpsertItemEmbeddingBatch(ctx context.Context, items []codeindex.ItemEmbeddingUpsert) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "codeemb.upsert_item_embeddings")
	if len(items) == 0 {
		return nil
	}
	var dims int
	for _, item := range items {
		if len(item.Embedding) == 0 {
			continue
		}
		dims = len(item.Embedding)
		break
	}
	if dims == 0 {
		return nil
	}
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		cacheRows := make([]embeddingCacheRow, 0, len(items))
		anchorIDs := make([]codeindex.AnchorID, 0, len(items))
		for _, item := range items {
			if len(item.Embedding) == 0 {
				continue
			}
			cacheRows = append(cacheRows, embeddingCacheRow{hash: item.Hash, vec: item.Embedding})
			anchorIDs = append(anchorIDs, item.AnchorID)
		}
		cacheStarted := time.Now()
		if err := bulkUpsertEmbeddingCacheTx(ctx, tx, codeTableEmbeddingCache, cacheRows, now); err != nil {
			return fmt.Errorf("upsert item embedding cache rows=%d: %w", len(cacheRows), err)
		}
		indexingperf.ObserveLatency(ctx, indexingperf.CodeembEmbedCacheUpsert, time.Since(cacheStarted))

		insertStmt, err := tx.PrepareContext(ctx, `
			INSERT INTO `+tableItemEmbeddings+` (item_row_id, content_hash, embedding, norm, dimensions, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(item_row_id) DO UPDATE SET
				content_hash = excluded.content_hash,
				embedding = excluded.embedding,
				norm = excluded.norm,
				dimensions = excluded.dimensions,
				created_at = excluded.created_at
		`)
		if err != nil {
			return err
		}
		defer insertStmt.Close()

		rowResolveStarted := time.Now()
		rowIDs, err := selectCodeItemRowIDsByAnchorIDs(ctx, tx, anchorIDs)
		if err != nil {
			return fmt.Errorf("select item row ids anchors=%d: %w", len(anchorIDs), err)
		}
		indexingperf.AddCount(ctx, indexingperf.CodeembEmbedItemRowidLookupCount, int64(len(anchorIDs)))
		indexingperf.ObserveLatency(ctx, indexingperf.CodeembEmbedItemRowidResolve, time.Since(rowResolveStarted))

		insertStarted := time.Now()
		for _, item := range items {
			vec := item.Embedding
			if len(vec) == 0 {
				continue
			}
			dims := s.runtime.Dimensions.Load()
			if dims > 0 && len(vec) != int(dims) {
				return fmt.Errorf("embedding dimension mismatch: have %d want %d", len(vec), dims)
			}
			rowID := rowIDs[string(item.AnchorID)]
			if rowID <= 0 {
				continue
			}
			norm := math.Sqrt(dotFloat64(vec, vec))
			if _, err := insertStmt.ExecContext(ctx, rowID, item.Hash, embedToBytes(vec), norm, len(vec), now); err != nil {
				return fmt.Errorf("upsert item embedding anchor=%s: %w", item.AnchorID, err)
			}
		}
		indexingperf.ObserveLatency(ctx, indexingperf.CodeembEmbedInsert, time.Since(insertStarted))
		return nil
	})
	if err == nil && s.runtime.Dimensions.Load() == 0 && dims > 0 {
		s.runtime.Dimensions.Store(int64(dims))
	}
	return err
}

// ItemFingerprint returns the stored fingerprint for an anchor, if present.
func (s *CodeStore) ItemFingerprint(ctx context.Context, anchorID codeindex.AnchorID) (string, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT i.fingerprint FROM `+tableItems+` i JOIN intel_code_anchors a ON a.id = i.anchor_row_id WHERE a.anchor_id = ?`, string(anchorID))
	var fp string
	if err := row.Scan(&fp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return fp, true, nil
}

// ItemEmbeddingStates returns stored fingerprints, item-level hashes, and chunk hashes for anchors.
func (s *CodeStore) ItemEmbeddingStates(ctx context.Context, anchorIDs []codeindex.AnchorID) (map[codeindex.AnchorID]codeindex.ItemEmbeddingState, error) {
	states := make(map[codeindex.AnchorID]codeindex.ItemEmbeddingState, len(anchorIDs))
	unique := dedupeAnchorIDs(anchorIDs)
	if len(unique) == 0 {
		return states, nil
	}

	for batch := range slices.Chunk(unique, 256) {
		holders := make([]string, len(batch))
		args := make([]any, len(batch))
		for i, id := range batch {
			holders[i] = "?"
			args[i] = string(id)
		}
		inClause := strings.Join(holders, ",")

		// Fingerprints
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT a.anchor_id, i.fingerprint FROM %s i JOIN intel_code_anchors a ON a.id = i.anchor_row_id WHERE a.anchor_id IN (%s)`, tableItems, inClause), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var anchorID string
			var fp sql.NullString
			if err := rows.Scan(&anchorID, &fp); err != nil {
				_ = rows.Close()
				return nil, err
			}
			state := states[codeindex.AnchorID(anchorID)]
			state.Fingerprint = fp.String
			state.HasFingerprint = true
			states[codeindex.AnchorID(anchorID)] = state
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}

		// Item-level embeddings
		rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT a.anchor_id, e.content_hash
			FROM %s e
			JOIN %s i ON e.item_row_id = i.id
			JOIN intel_code_anchors a ON a.id = i.anchor_row_id
			WHERE a.anchor_id IN (%s)
		`, tableItemEmbeddings, tableItems, inClause), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var anchorID, hash string
			if err := rows.Scan(&anchorID, &hash); err != nil {
				_ = rows.Close()
				return nil, err
			}
			state := states[codeindex.AnchorID(anchorID)]
			state.ItemHash = hash
			state.HasItemHash = true
			states[codeindex.AnchorID(anchorID)] = state
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}

		// Chunk hashes (prefer newest per chunk index).
		rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT a.anchor_id, c.chunk_index, c.content_hash
			FROM %s c
			JOIN %s i ON c.item_row_id = i.id
			JOIN intel_code_anchors a ON a.id = i.anchor_row_id
			WHERE a.anchor_id IN (%s)
			ORDER BY c.chunk_index, c.created_at DESC, c.id DESC
		`, codeTableChunkEmbeddings, tableItems, inClause), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var anchorID, hash string
			var idx int
			if err := rows.Scan(&anchorID, &idx, &hash); err != nil {
				_ = rows.Close()
				return nil, err
			}
			state := states[codeindex.AnchorID(anchorID)]
			if state.ChunkHashes == nil {
				state.ChunkHashes = make(map[int]string)
			}
			if _, ok := state.ChunkHashes[idx]; ok {
				continue
			}
			state.ChunkHashes[idx] = hash
			states[codeindex.AnchorID(anchorID)] = state
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}

	return states, nil
}

// Stats returns counts of items and chunks.
func (s *CodeStore) Stats(ctx context.Context) (int, int, error) {
	var items, chunks int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tableItems).Scan(&items); err != nil {
		return 0, 0, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+codeTableChunkEmbeddings).Scan(&chunks); err != nil {
		return 0, 0, err
	}
	return items, chunks, nil
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
