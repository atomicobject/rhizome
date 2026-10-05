package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// UpsertNoteChunks upserts embeddings for provided chunk indices (skips unchanged if caller omits).
func (s *Store) UpsertNoteChunks(ctx context.Context, id embeddings.NoteID, chunks []embeddings.ChunkInput, vecs []embeddings.Embedding) error {
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
func (s *Store) UpsertNoteChunksBatch(ctx context.Context, items []embeddings.NoteChunksUpsert) error {
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
func (s *Store) SyncNoteChunksBatch(ctx context.Context, items []embeddings.NoteChunkSync) error {
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
					indexingperf.ObserveLatency(ctx, "noteemb.sync_note_chunks.lookup_note_row", time.Since(rowLookupStarted))
					if errors.Is(err, sql.ErrNoRows) {
						return fmt.Errorf("note metadata missing for %s", item.NoteID)
					}
					return err
				}
				indexingperf.ObserveLatency(ctx, "noteemb.sync_note_chunks.lookup_note_row", time.Since(rowLookupStarted))
				if len(item.Chunks) != len(item.Embeddings) {
					return fmt.Errorf("chunks/embeddings length mismatch: %d vs %d", len(item.Chunks), len(item.Embeddings))
				}

				cacheRows, chunkRows, err := prepareNoteChunkRows(item.Chunks, item.Embeddings, s.runtime.Dimensions.Load())
				if err != nil {
					return err
				}

				upsertCacheStarted := time.Now()
				if err := bulkUpsertEmbeddingCacheTx(ctx, tx, tableEmbeddingCache, cacheRows, now); err != nil {
					indexingperf.ObserveLatency(ctx, "noteemb.sync_note_chunks.upsert_cache", time.Since(upsertCacheStarted))
					return err
				}
				indexingperf.ObserveLatency(ctx, "noteemb.sync_note_chunks.upsert_cache", time.Since(upsertCacheStarted))

				upsertRowsStarted := time.Now()
				if err := bulkUpsertNoteChunkRowsTx(ctx, tx, rowID, chunkRows, now); err != nil {
					indexingperf.ObserveLatency(ctx, "noteemb.sync_note_chunks.upsert_rows", time.Since(upsertRowsStarted))
					return err
				}
				indexingperf.ObserveLatency(ctx, "noteemb.sync_note_chunks.upsert_rows", time.Since(upsertRowsStarted))

				deleteStarted := time.Now()
				if err := deleteNoteChunkRowsNotInTx(ctx, tx, rowID, item.KeepIndices); err != nil {
					indexingperf.ObserveLatency(ctx, "noteemb.sync_note_chunks.delete_stale", time.Since(deleteStarted))
					return err
				}
				indexingperf.ObserveLatency(ctx, "noteemb.sync_note_chunks.delete_stale", time.Since(deleteStarted))
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
