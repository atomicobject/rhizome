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
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// UpsertNoteChunks upserts embeddings for provided chunk indices (skips unchanged if caller omits).
func (s *NoteStore) UpsertNoteChunks(ctx context.Context, id embeddings.NoteID, chunks []embeddings.ChunkInput, vecs []embeddings.Embedding) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "noteemb.upsert_note_chunks")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		if len(chunks) != len(vecs) {
			return fmt.Errorf("chunks/embeddings length mismatch: %d vs %d", len(chunks), len(vecs))
		}
		if len(chunks) == 0 {
			return nil
		}

		err := sqliteutil.ExecTxWithRetry(ctx, db, func(tx *sql.Tx) error {
			var rowID int64
			if err := tx.QueryRowContext(ctx, `SELECT id FROM `+tableNotes+` WHERE note_id = ?`, string(id)).Scan(&rowID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("note metadata missing for %s", id)
				}
				return err
			}

			now := time.Now().Unix()

			cacheRows, chunkRows, err := prepareNoteChunkRows(chunks, vecs, s.runtime.Dimensions.Load())
			if err != nil {
				return err
			}

			if err := bulkUpsertEmbeddingCacheTx(ctx, tx, tableEmbeddingCache, cacheRows, now); err != nil {
				return err
			}
			if err := bulkUpsertNoteChunkRowsTx(ctx, tx, rowID, chunkRows, now); err != nil {
				return err
			}
			return nil
		})
		if err == nil && s.runtime.Dimensions.Load() == 0 && len(vecs) > 0 {
			s.runtime.Dimensions.Store(int64(len(vecs[0])))
		}
		return err
	})
}

// UpsertNoteChunksBatch upserts embeddings for multiple notes in one transaction.
func (s *NoteStore) UpsertNoteChunksBatch(ctx context.Context, items []embeddings.NoteChunksUpsert) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "noteemb.upsert_note_chunks")
	if len(items) == 0 {
		return nil
	}
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return sqliteutil.ExecTxWithRetry(ctx, db, func(tx *sql.Tx) error {
			now := time.Now().Unix()
			for _, item := range items {
				if len(item.Chunks) != len(item.Embeddings) {
					return fmt.Errorf("chunks/embeddings length mismatch: %d vs %d", len(item.Chunks), len(item.Embeddings))
				}
				if len(item.Chunks) == 0 {
					continue
				}
				var rowID int64
				if err := tx.QueryRowContext(ctx, `SELECT id FROM `+tableNotes+` WHERE note_id = ?`, string(item.NoteID)).Scan(&rowID); err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						return fmt.Errorf("note metadata missing for %s", item.NoteID)
					}
					return err
				}
				cacheRows, chunkRows, err := prepareNoteChunkRows(item.Chunks, item.Embeddings, s.runtime.Dimensions.Load())
				if err != nil {
					return err
				}
				if err := bulkUpsertEmbeddingCacheTx(ctx, tx, tableEmbeddingCache, cacheRows, now); err != nil {
					return err
				}
				if err := bulkUpsertNoteChunkRowsTx(ctx, tx, rowID, chunkRows, now); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// SyncNoteChunksBatch upserts note chunks and removes stale chunk rows for multiple notes in one transaction.
func (s *NoteStore) SyncNoteChunksBatch(ctx context.Context, items []embeddings.NoteChunkSync) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "noteemb.sync_note_chunks")
	if len(items) == 0 {
		return nil
	}
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		return sqliteutil.ExecTxWithRetry(ctx, db, func(tx *sql.Tx) error {
			now := time.Now().Unix()
			for _, item := range items {
				rowLookupStarted := time.Now()
				var rowID int64
				if err := tx.QueryRowContext(ctx, `SELECT id FROM `+tableNotes+` WHERE note_id = ?`, string(item.NoteID)).Scan(&rowID); err != nil {
					indexingperf.ObserveLatency(ctx, indexingperf.NoteembSyncNoteChunksLookupNoteRow, time.Since(rowLookupStarted))
					if errors.Is(err, sql.ErrNoRows) {
						return fmt.Errorf("note metadata missing for %s", item.NoteID)
					}
					return err
				}
				indexingperf.ObserveLatency(ctx, indexingperf.NoteembSyncNoteChunksLookupNoteRow, time.Since(rowLookupStarted))
				if len(item.Chunks) != len(item.Embeddings) {
					return fmt.Errorf("chunks/embeddings length mismatch: %d vs %d", len(item.Chunks), len(item.Embeddings))
				}

				cacheRows, chunkRows, err := prepareNoteChunkRows(item.Chunks, item.Embeddings, s.runtime.Dimensions.Load())
				if err != nil {
					return err
				}

				upsertCacheStarted := time.Now()
				if err := bulkUpsertEmbeddingCacheTx(ctx, tx, tableEmbeddingCache, cacheRows, now); err != nil {
					indexingperf.ObserveLatency(ctx, indexingperf.NoteembSyncNoteChunksUpsertCache, time.Since(upsertCacheStarted))
					return err
				}
				indexingperf.ObserveLatency(ctx, indexingperf.NoteembSyncNoteChunksUpsertCache, time.Since(upsertCacheStarted))

				upsertRowsStarted := time.Now()
				if err := bulkUpsertNoteChunkRowsTx(ctx, tx, rowID, chunkRows, now); err != nil {
					indexingperf.ObserveLatency(ctx, indexingperf.NoteembSyncNoteChunksUpsertRows, time.Since(upsertRowsStarted))
					return err
				}
				indexingperf.ObserveLatency(ctx, indexingperf.NoteembSyncNoteChunksUpsertRows, time.Since(upsertRowsStarted))

				deleteStarted := time.Now()
				if err := deleteNoteChunkRowsNotInTx(ctx, tx, rowID, item.KeepIndices); err != nil {
					indexingperf.ObserveLatency(ctx, indexingperf.NoteembSyncNoteChunksDeleteStale, time.Since(deleteStarted))
					return err
				}
				indexingperf.ObserveLatency(ctx, indexingperf.NoteembSyncNoteChunksDeleteStale, time.Since(deleteStarted))
			}
			if s.runtime.Dimensions.Load() == 0 {
				for _, item := range items {
					for _, vec := range item.Embeddings {
						if len(vec) > 0 {
							s.runtime.Dimensions.Store(int64(len(vec)))
							return nil
						}
					}
				}
			}
			return nil
		})
	})
}

func prepareNoteChunkRows(chunks []embeddings.ChunkInput, vecs []embeddings.Embedding, dims int64) ([]embeddingCacheRow, []noteChunkRow, error) {
	cacheRows := make([]embeddingCacheRow, 0, len(chunks))
	chunkRows := make([]noteChunkRow, 0, len(chunks))
	for i, chunk := range chunks {
		vec := vecs[i]
		if len(vec) == 0 {
			continue
		}
		if dims > 0 && len(vec) != int(dims) {
			return nil, nil, fmt.Errorf("chunk dimension mismatch: have %d want %d", len(vec), dims)
		}
		norm := math.Sqrt(dotFloat64(vec, vec))
		cacheRows = append(cacheRows, embeddingCacheRow{hash: chunk.Hash, vec: vec})
		chunkRows = append(chunkRows, noteChunkRow{
			index:      chunk.Index,
			breadcrumb: chunk.Breadcrumb,
			heading:    chunk.Heading,
			hash:       chunk.Hash,
			vec:        vec,
			norm:       norm,
		})
	}
	return cacheRows, chunkRows, nil
}

type noteChunkRow struct {
	index      int
	breadcrumb string
	heading    string
	hash       string
	vec        embeddings.Embedding
	norm       float64
}

const (
	noteChunkBatchSize      = 100
	embeddingCacheBatchSize = 200
)

const (
	tableIndexMeta       = "emb_index_meta"
	tableNotes           = "emb_notes"
	tableChunkEmbeddings = "emb_chunk_embeddings"
	tableEmbeddingCache  = "emb_embedding_cache"
	vecChunkTablePrefix  = "emb_chunk_embeddings_vec_d"

	// DefaultStaleNoteThreshold is the fraction of stale notes (not seen in current sync)
	// above which we prune them. Below this threshold, notes are kept to avoid re-embedding
	// costs when switching branches. 0.3 means we keep stale notes if they're <30% of total.
	DefaultStaleNoteThreshold = 0.3
)

// DeleteChunksNotIn removes chunk embeddings whose indices are not in the provided set.
func (s *NoteStore) DeleteChunksNotIn(ctx context.Context, id embeddings.NoteID, indices []int) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	ctx = indexingperf.WithOp(ctx, "noteemb.delete_chunks_not_in")
	return s.withWrite(ctx, func(ctx context.Context, db *sql.DB) error {
		row := db.QueryRowContext(ctx, `SELECT id FROM `+tableNotes+` WHERE note_id = ?`, string(id))
		var rowID int64
		if err := row.Scan(&rowID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		return deleteNoteChunkRowsNotInDB(ctx, db, rowID, indices)
	})
}

func deleteNoteChunkRowsNotInDB(ctx context.Context, db *sql.DB, rowID int64, indices []int) error {
	if len(indices) == 0 {
		_, err := execWithRetry(ctx, db, `DELETE FROM `+tableChunkEmbeddings+` WHERE note_row_id = ?`, rowID)
		return err
	}
	holders := make([]string, len(indices))
	args := make([]any, 0, len(indices)+1)
	args = append(args, rowID)
	for i, idx := range indices {
		holders[i] = "?"
		args = append(args, idx)
	}
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE note_row_id = ? AND chunk_index NOT IN (%s)`, tableChunkEmbeddings, strings.Join(holders, ","))
	_, err := execWithRetry(ctx, db, stmt, args...)
	return err
}

func deleteNoteChunkRowsNotInTx(ctx context.Context, tx *sql.Tx, rowID int64, indices []int) error {
	if len(indices) == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM `+tableChunkEmbeddings+` WHERE note_row_id = ?`, rowID)
		return err
	}
	holders := make([]string, len(indices))
	args := make([]any, 0, len(indices)+1)
	args = append(args, rowID)
	for i, idx := range indices {
		holders[i] = "?"
		args = append(args, idx)
	}
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE note_row_id = ? AND chunk_index NOT IN (%s)`, tableChunkEmbeddings, strings.Join(holders, ","))
	_, err := tx.ExecContext(ctx, stmt, args...)
	return err
}

// SyncNoteChunks upserts note chunks and removes stale chunk rows in one write path.
func (s *NoteStore) SyncNoteChunks(ctx context.Context, item embeddings.NoteChunkSync) error {
	if err := s.requireWrite(); err != nil {
		return err
	}

	return s.SyncNoteChunksBatch(ctx, []embeddings.NoteChunkSync{item})
}

// NoteChunks returns stored embeddings for all chunks of a note.
func (s *NoteStore) NoteChunks(ctx context.Context, id embeddings.NoteID) ([]embeddings.StoredChunk, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id FROM `+tableNotes+` WHERE note_id = ?`, string(id))
	var rowID int64
	if err := row.Scan(&rowID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT chunk_index, breadcrumb, heading, embedding, dimensions
		FROM `+tableChunkEmbeddings+`
		WHERE note_row_id = ?
		ORDER BY chunk_index
	`, rowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []embeddings.StoredChunk
	expectedDims := int(s.runtime.Dimensions.Load())
	for rows.Next() {
		var idx, dims int
		var breadcrumb, heading string
		var blob []byte
		if err := rows.Scan(&idx, &breadcrumb, &heading, &blob, &dims); err != nil {
			return nil, err
		}
		emb := bytesToEmbed(blob)
		if dims > 0 && len(emb) != dims {
			continue
		}
		if expectedDims > 0 && len(emb) != expectedDims {
			continue
		}
		chunks = append(chunks, embeddings.StoredChunk{
			Index:      idx,
			Breadcrumb: breadcrumb,
			Heading:    heading,
			Embedding:  emb,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return chunks, nil
}

// ChunkHashes returns existing chunk hashes for a note keyed by chunk index.
func (s *NoteStore) ChunkHashes(ctx context.Context, id embeddings.NoteID) (map[int]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.chunk_index, c.content_hash
		FROM `+tableChunkEmbeddings+` c
		JOIN `+tableNotes+` n ON c.note_row_id = n.id
		WHERE n.note_id = ?
	`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[int]string)
	for rows.Next() {
		var idx int
		var hash string
		if err := rows.Scan(&idx, &hash); err != nil {
			return nil, err
		}
		res[idx] = hash
	}
	return res, rows.Err()
}

// ChunkHashesBatch returns existing chunk hashes for multiple notes keyed by note ID and chunk index.
func (s *NoteStore) ChunkHashesBatch(ctx context.Context, ids []embeddings.NoteID) (map[embeddings.NoteID]map[int]string, error) {
	out := make(map[embeddings.NoteID]map[int]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	const maxVars = 900
	for batch := range slices.Chunk(ids, maxVars) {
		holders := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch))
		for _, id := range batch {
			holders = append(holders, "?")
			args = append(args, string(id))
		}
		query := fmt.Sprintf(`
			SELECT n.note_id, c.chunk_index, c.content_hash
			FROM %s c
			JOIN %s n ON c.note_row_id = n.id
			WHERE n.note_id IN (%s)
		`, tableChunkEmbeddings, tableNotes, strings.Join(holders, ","))
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var noteID string
			var idx int
			var hash sql.NullString
			if err := rows.Scan(&noteID, &idx, &hash); err != nil {
				rows.Close()
				return nil, err
			}
			id := embeddings.NoteID(noteID)
			if _, ok := out[id]; !ok {
				out[id] = make(map[int]string)
			}
			if hash.Valid {
				out[id][idx] = hash.String
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func bulkUpsertNoteChunkRowsTx(ctx context.Context, tx *sql.Tx, rowID int64, rows []noteChunkRow, now int64) error {
	if len(rows) == 0 {
		return nil
	}
	for batch := range slices.Chunk(rows, noteChunkBatchSize) {
		args := make([]any, 0, len(batch)*9)
		valueSQL := make([]string, 0, len(batch))
		for _, r := range batch {
			if len(r.vec) == 0 {
				continue
			}
			valueSQL = append(valueSQL, "(?, ?, ?, ?, ?, ?, ?, ?, ?)")
			args = append(args, rowID, r.index, r.breadcrumb, r.heading, r.hash, embedToBytes(r.vec), r.norm, len(r.vec), now)
		}
		if len(valueSQL) == 0 {
			continue
		}
		stmt := fmt.Sprintf(`
			INSERT INTO %s (note_row_id, chunk_index, breadcrumb, heading, content_hash, embedding, norm, dimensions, created_at)
			VALUES %s
			ON CONFLICT(note_row_id, chunk_index) DO UPDATE SET
				breadcrumb = excluded.breadcrumb,
				heading = excluded.heading,
				content_hash = excluded.content_hash,
				embedding = excluded.embedding,
				norm = excluded.norm,
				dimensions = excluded.dimensions,
				created_at = excluded.created_at
		`, tableChunkEmbeddings, strings.Join(valueSQL, ","))
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return err
		}
	}
	return nil
}

type scoredChunk struct {
	chunkID   int64
	noteRowID int64
	index     int
	score     float64
}
