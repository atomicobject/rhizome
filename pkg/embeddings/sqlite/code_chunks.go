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

// UpsertItemChunks inserts or updates chunk embeddings for a code item.
func (s *CodeStore) UpsertItemChunks(ctx context.Context, anchorID codeindex.AnchorID, chunks []codeindex.ChunkInput, texts []string, vecs []embeddings.Embedding) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "codeemb.upsert_item_chunks")
	if len(chunks) != len(vecs) || len(chunks) != len(texts) {
		return errors.New("chunks, texts, and embeddings length mismatch")
	}
	item := codeindex.ItemChunksUpsert{AnchorID: anchorID, Chunks: chunks, Texts: texts, Embeddings: vecs}
	err := s.withWriteTx(ctx, func(tx *sql.Tx) error {
		return upsertItemChunksTx(ctx, tx, item, time.Now().Unix())
	})
	if err == nil && s.runtime.Dimensions.Load() == 0 && len(vecs) > 0 {
		s.runtime.Dimensions.Store(int64(len(vecs[0])))
	}
	return err
}

// UpsertItemChunksBatch inserts or updates chunk embeddings for multiple code items in one transaction.
func (s *CodeStore) UpsertItemChunksBatch(ctx context.Context, items []codeindex.ItemChunksUpsert) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "codeemb.upsert_item_chunks")
	if len(items) == 0 {
		return nil
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		for _, item := range items {
			if len(item.Chunks) != len(item.Embeddings) || len(item.Chunks) != len(item.Texts) {
				return errors.New("chunks, texts, and embeddings length mismatch")
			}
			if err := upsertItemChunksTx(ctx, tx, item, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func upsertItemChunksTx(ctx context.Context, tx *sql.Tx, item codeindex.ItemChunksUpsert, now int64) error {
	var rowID int64
	var path, symbol, fqn, kind string
	if err := tx.QueryRowContext(ctx, `SELECT i.id, a.path, a.symbol, a.fqn, a.kind FROM `+tableItems+` i JOIN intel_code_anchors a ON a.id = i.anchor_row_id WHERE a.anchor_id = ?`, string(item.AnchorID)).Scan(&rowID, &path, &symbol, &fqn, &kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	cacheRows := make([]embeddingCacheRow, 0, len(item.Chunks))
	chunkRows := make([]codeChunkRow, 0, len(item.Chunks))
	ftsRows := make([]codeFTSRow, 0, len(item.Chunks))
	indicesByGranularity := make(map[string][]int)
	var chunkIndices []int

	for i, chunk := range item.Chunks {
		vec := item.Embeddings[i]
		if len(vec) == 0 {
			continue
		}
		norm := math.Sqrt(dotFloat64(vec, vec))
		cacheRows = append(cacheRows, embeddingCacheRow{hash: chunk.Hash, vec: vec})
		chunkRows = append(chunkRows, codeChunkRow{
			index:       chunk.Index,
			granularity: chunk.Granularity,
			breadcrumb:  chunk.Breadcrumb,
			heading:     chunk.Heading,
			startByte:   chunk.StartByte,
			endByte:     chunk.EndByte,
			startLine:   chunk.StartLine,
			endLine:     chunk.EndLine,
			hash:        chunk.Hash,
			vec:         vec,
			norm:        norm,
		})
		ftsRows = append(ftsRows, codeFTSRow{
			anchorID:    string(item.AnchorID),
			chunkIndex:  chunk.Index,
			path:        path,
			symbol:      symbol,
			fqn:         fqn,
			kind:        kind,
			granularity: chunk.Granularity,
			breadcrumb:  chunk.Breadcrumb,
			heading:     chunk.Heading,
			body:        item.Texts[i],
		})
		indicesByGranularity[chunk.Granularity] = append(indicesByGranularity[chunk.Granularity], chunk.Index)
		chunkIndices = append(chunkIndices, chunk.Index)
	}

	if err := bulkUpsertEmbeddingCacheTx(ctx, tx, codeTableEmbeddingCache, cacheRows, now); err != nil {
		return fmt.Errorf("upsert chunk embedding cache rows=%d anchor=%s: %w", len(cacheRows), item.AnchorID, err)
	}
	for granularity, indices := range indicesByGranularity {
		if err := deleteChunkEmbeddingsForOtherGranularity(ctx, tx, rowID, granularity, indices); err != nil {
			return fmt.Errorf("delete conflicting chunk embeddings anchor=%s granularity=%s indices=%d: %w", item.AnchorID, granularity, len(indices), err)
		}
	}
	if err := bulkUpsertCodeChunkRowsTx(ctx, tx, rowID, chunkRows, now); err != nil {
		return fmt.Errorf("upsert code chunk rows anchor=%s rows=%d: %w", item.AnchorID, len(chunkRows), err)
	}
	if err := deleteChunkFTSForIndices(ctx, tx, string(item.AnchorID), chunkIndices); err != nil {
		return fmt.Errorf("delete chunk fts anchor=%s indices=%d: %w", item.AnchorID, len(chunkIndices), err)
	}
	if err := bulkInsertChunkFTSTx(ctx, tx, ftsRows); err != nil {
		return fmt.Errorf("insert chunk fts anchor=%s rows=%d: %w", item.AnchorID, len(ftsRows), err)
	}
	return nil
}

type codeChunkRow struct {
	index       int
	granularity string
	breadcrumb  string
	heading     string
	startByte   int
	endByte     int
	startLine   int
	endLine     int
	hash        string
	vec         embeddings.Embedding
	norm        float64
}

const (
	codeSchemaVersion        = 12
	codeTableIndexMeta       = "code_index_meta"
	tableItems               = "code_items"
	tableItemEmbeddings      = "code_item_embeddings"
	codeTableChunkEmbeddings = "code_chunk_embeddings"
	tableChunkFTS            = "code_chunk_fts"
	codeTableEmbeddingCache  = "code_embedding_cache"
	codeVecChunkTablePrefix  = "code_chunk_embeddings_vec_d"
	vecItemTablePrefix       = "code_item_embeddings_vec_d"

	// DefaultStaleItemThreshold is the fraction of stale items (not seen in current sync)
	// above which we prune them. Below this threshold, items are kept to avoid re-embedding
	// costs when switching branches. 0.3 means we keep stale items if they're <30% of total.
	DefaultStaleItemThreshold = 0.3

	codeChunkBatchSize          = 20
	codeEmbeddingCacheBatchSize = 50
	hashLookupBatchSize         = 100
)

func codeChunkVecTableName(dims int) string {
	return fmt.Sprintf("%s%d", codeVecChunkTablePrefix, dims)
}

func bulkUpsertCodeChunkRowsTx(ctx context.Context, tx *sql.Tx, rowID int64, rows []codeChunkRow, now int64) error {
	if len(rows) == 0 {
		return nil
	}
	for batch := range slices.Chunk(rows, codeChunkBatchSize) {
		args := make([]any, 0, len(batch)*14)
		valueSQL := make([]string, 0, len(batch))
		for _, r := range batch {
			if len(r.vec) == 0 {
				continue
			}
			valueSQL = append(valueSQL, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, rowID, r.index, r.granularity, r.breadcrumb, r.heading, r.startByte, r.endByte, r.startLine, r.endLine, r.hash, embedToBytes(r.vec), r.norm, len(r.vec), now)
		}
		if len(valueSQL) == 0 {
			continue
		}
		stmt := fmt.Sprintf(`
			INSERT INTO %s (item_row_id, chunk_index, granularity, breadcrumb, heading, start_byte, end_byte, start_line, end_line, content_hash, embedding, norm, dimensions, created_at)
			VALUES %s
			ON CONFLICT(item_row_id, granularity, chunk_index) DO UPDATE SET
				breadcrumb = excluded.breadcrumb,
				heading = excluded.heading,
				start_byte = excluded.start_byte,
				end_byte = excluded.end_byte,
				start_line = excluded.start_line,
				end_line = excluded.end_line,
				content_hash = excluded.content_hash,
				embedding = excluded.embedding,
				norm = excluded.norm,
				dimensions = excluded.dimensions,
				created_at = excluded.created_at
		`, codeTableChunkEmbeddings, strings.Join(valueSQL, ","))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

func bulkInsertChunkFTSTx(ctx context.Context, tx *sql.Tx, rows []codeFTSRow) error {
	if len(rows) == 0 {
		return nil
	}
	for batch := range slices.Chunk(rows, codeChunkBatchSize) {
		args := make([]any, 0, len(batch)*10)
		valueSQL := make([]string, 0, len(batch))
		for _, r := range batch {
			valueSQL = append(valueSQL, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, r.anchorID, r.chunkIndex, r.path, r.symbol, r.fqn, r.kind, r.granularity, r.breadcrumb, r.heading, r.body)
		}
		stmt := fmt.Sprintf(`
			INSERT INTO %s (anchor_id, chunk_index, path, symbol, fqn, kind, granularity, breadcrumb, heading, body)
			VALUES %s
		`, tableChunkFTS, strings.Join(valueSQL, ","))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

func deleteChunkFTSForIndices(ctx context.Context, tx *sql.Tx, anchorID string, indices []int) error {
	if len(indices) == 0 {
		return nil
	}
	if len(indices) <= 900 {
		args := make([]any, 0, len(indices)+1)
		holders := make([]string, 0, len(indices))
		args = append(args, anchorID)
		for _, idx := range indices {
			holders = append(holders, "?")
			args = append(args, idx)
		}
		stmt := fmt.Sprintf(`DELETE FROM %s WHERE anchor_id = ? AND chunk_index IN (%s)`, tableChunkFTS, strings.Join(holders, ","))
		_, err := tx.ExecContext(ctx, stmt, args...)
		return err
	}

	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_chunk_fts_idx`)
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_delete_chunk_fts_idx (chunk_index INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_delete_chunk_fts_idx(chunk_index) VALUES (?)`)
	if err != nil {
		return err
	}
	for _, idx := range indices {
		if _, err := stmt.ExecContext(ctx, idx); err != nil {
			_ = stmt.Close()
			return err
		}
	}
	_ = stmt.Close()
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM %s
		WHERE anchor_id = ?
		  AND chunk_index IN (SELECT chunk_index FROM temp_delete_chunk_fts_idx)
	`, tableChunkFTS), anchorID)
	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_chunk_fts_idx`)
	return err
}

func deleteChunkEmbeddingsForOtherGranularity(ctx context.Context, tx *sql.Tx, rowID int64, granularity string, indices []int) error {
	if len(indices) == 0 {
		return nil
	}
	if len(indices) <= 900 {
		args := make([]any, 0, len(indices)+2)
		holders := make([]string, 0, len(indices))
		args = append(args, rowID)
		for _, idx := range indices {
			holders = append(holders, "?")
			args = append(args, idx)
		}
		args = append(args, granularity)
		stmt := fmt.Sprintf(`DELETE FROM %s WHERE item_row_id = ? AND chunk_index IN (%s) AND granularity != ?`, codeTableChunkEmbeddings, strings.Join(holders, ","))
		_, err := tx.ExecContext(ctx, stmt, args...)
		return err
	}

	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_chunk_embeddings_idx`)
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE temp_delete_chunk_embeddings_idx (chunk_index INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO temp_delete_chunk_embeddings_idx(chunk_index) VALUES (?)`)
	if err != nil {
		return err
	}
	for _, idx := range indices {
		if _, err := stmt.ExecContext(ctx, idx); err != nil {
			_ = stmt.Close()
			return err
		}
	}
	_ = stmt.Close()
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM %s
		WHERE item_row_id = ?
		  AND chunk_index IN (SELECT chunk_index FROM temp_delete_chunk_embeddings_idx)
		  AND granularity != ?
	`, codeTableChunkEmbeddings), rowID, granularity)
	_, _ = tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp_delete_chunk_embeddings_idx`)
	return err
}

// ChunkHashes returns content hashes for stored chunks of a code item.
func (s *CodeStore) ChunkHashes(ctx context.Context, anchorID codeindex.AnchorID) (map[int]string, error) {
	row := s.db.QueryRowContext(ctx, `SELECT i.id FROM `+tableItems+` i JOIN intel_code_anchors a ON a.id = i.anchor_row_id WHERE a.anchor_id = ?`, string(anchorID))
	var rowID int64
	if err := row.Scan(&rowID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return map[int]string{}, nil
		}
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT chunk_index, content_hash
		FROM `+codeTableChunkEmbeddings+`
		WHERE item_row_id = ?
		ORDER BY chunk_index, created_at DESC, id DESC
	`, rowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hashes := make(map[int]string)
	for rows.Next() {
		var idx int
		var hash string
		if err := rows.Scan(&idx, &hash); err != nil {
			return nil, err
		}
		// Defensive: older data may contain multiple rows for the same chunk_index (different
		// granularities). Prefer the newest row (query ORDER BY) and keep the first seen.
		if _, ok := hashes[idx]; ok {
			continue
		}
		hashes[idx] = hash
	}
	return hashes, rows.Err()
}

// DeleteChunksNotIn removes chunk embeddings whose indices are not in the provided set for an anchor.
func (s *CodeStore) DeleteChunksNotIn(ctx context.Context, anchorID codeindex.AnchorID, indices []int) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "codeemb.delete_chunks_not_in")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		row := db.QueryRowContext(ctx, `SELECT i.id FROM `+tableItems+` i JOIN intel_code_anchors a ON a.id = i.anchor_row_id WHERE a.anchor_id = ?`, string(anchorID))
		var rowID int64
		if err := row.Scan(&rowID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if len(indices) == 0 {
			if _, err := execWithRetry(ctx, db, `DELETE FROM `+codeTableChunkEmbeddings+` WHERE item_row_id = ?`, rowID); err != nil {
				return err
			}
			_, err := execWithRetry(ctx, db, `DELETE FROM `+tableChunkFTS+` WHERE anchor_id = ?`, string(anchorID))
			return err
		}
		holders := make([]string, len(indices))
		args := make([]any, 0, len(indices)+1)
		args = append(args, rowID)
		for i, idx := range indices {
			holders[i] = "?"
			args = append(args, idx)
		}
		stmt := fmt.Sprintf(`DELETE FROM %s WHERE item_row_id = ? AND chunk_index NOT IN (%s)`, codeTableChunkEmbeddings, strings.Join(holders, ","))
		if _, err := execWithRetry(ctx, db, stmt, args...); err != nil {
			return err
		}
		args[0] = string(anchorID)
		stmt = fmt.Sprintf(`DELETE FROM %s WHERE anchor_id = ? AND chunk_index NOT IN (%s)`, tableChunkFTS, strings.Join(holders, ","))
		_, err := execWithRetry(ctx, db, stmt, args...)
		return err
	})
}

// ItemChunks returns stored embeddings for all chunks of a code item.
func (s *CodeStore) ItemChunks(ctx context.Context, anchorID codeindex.AnchorID) ([]codeindex.StoredChunk, error) {
	row := s.db.QueryRowContext(ctx, `SELECT i.id FROM `+tableItems+` i JOIN intel_code_anchors a ON a.id = i.anchor_row_id WHERE a.anchor_id = ?`, string(anchorID))
	var rowID int64
	if err := row.Scan(&rowID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
                SELECT chunk_index, granularity, breadcrumb, heading, embedding, dimensions
                FROM `+codeTableChunkEmbeddings+`
                WHERE item_row_id = ?
                ORDER BY chunk_index
        `, rowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []codeindex.StoredChunk
	expectedDims := int(s.runtime.Dimensions.Load())
	for rows.Next() {
		var idx, dims int
		var granularity, breadcrumb, heading string
		var blob []byte
		if err := rows.Scan(&idx, &granularity, &breadcrumb, &heading, &blob, &dims); err != nil {
			return nil, err
		}
		emb := bytesToEmbed(blob)
		if dims > 0 && len(emb) != dims {
			continue
		}
		if expectedDims > 0 && len(emb) != expectedDims {
			continue
		}
		chunks = append(chunks, codeindex.StoredChunk{
			Index:       idx,
			Granularity: granularity,
			Breadcrumb:  breadcrumb,
			Heading:     heading,
			Embedding:   emb,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return chunks, nil
}

func (s *CodeStore) GetChunkBody(ctx context.Context, anchorID codeindex.AnchorID, chunkIndex int) (string, error) {
	var body string
	err := s.db.QueryRowContext(ctx,
		`SELECT body FROM `+tableChunkFTS+` WHERE anchor_id = ? AND chunk_index = ?`,
		string(anchorID), chunkIndex).Scan(&body)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return body, nil
}
