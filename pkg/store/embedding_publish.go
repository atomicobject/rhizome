package store

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
)

// UpsertEmbeddings atomically upserts embeddings for the given chunk IDs.
// The embeddings map keys are chunk_id values from intel_chunks.
func (s *Store) UpsertEmbeddings(ctx context.Context, embeddings map[string]embeddings.Embedding) error {
	ctx = indexingperf.WithOp(ctx, "intel.upsert_embeddings")
	if len(embeddings) == 0 {
		return nil
	}
	return s.withWriteTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		const batchSize = 50
		chunkIDs := make([]string, 0, len(embeddings))
		for chunkID := range embeddings {
			chunkIDs = append(chunkIDs, chunkID)
		}
		sort.Strings(chunkIDs)
		dimSet := make(map[int]struct{})
		for _, chunkID := range chunkIDs {
			dims := len(embeddings[chunkID])
			if dims <= 0 {
				continue
			}
			dimSet[dims] = struct{}{}
		}
		dimsList := make([]int, 0, len(dimSet))
		for dims := range dimSet {
			dimsList = append(dimsList, dims)
		}
		sort.Ints(dimsList)
		for _, dims := range dimsList {
			if err := createIntelVecTable(ctx, tx, dims); err != nil {
				return err
			}
		}

		for batch := range slices.Chunk(chunkIDs, batchSize) {
			chunkRowIDs, err := lookupIntelChunkRowIDsTx(ctx, tx, batch)
			if err != nil {
				return fmt.Errorf("lookup intel chunk row ids: %w", err)
			}
			type embedRow struct {
				chunkID    string
				chunkRowID int64
				blob       []byte
				norm       float64
				dims       int
			}
			byDims := make(map[int][]embedRow)
			for _, chunkID := range batch {
				emb := embeddings[chunkID]
				if len(emb) == 0 {
					continue
				}
				chunkRowID, exists := chunkRowIDs[chunkID]
				if !exists {
					continue
				}
				dims := len(emb)
				byDims[dims] = append(byDims[dims], embedRow{
					chunkID:    chunkID,
					chunkRowID: chunkRowID,
					blob:       embedToBytes(emb),
					norm:       math.Sqrt(dotFloat64(emb, emb)),
					dims:       dims,
				})
			}

			for dims, rows := range byDims {
				metaValueSQL := make([]string, 0, len(rows))
				metaArgs := make([]any, 0, len(rows)*5)
				vecValueSQL := make([]string, 0, len(rows))
				vecArgs := make([]any, 0, len(rows)*2)
				vecIDValueSQL := make([]string, 0, len(rows))
				vecIDArgs := make([]any, 0, len(rows))
				for _, row := range rows {
					metaValueSQL = append(metaValueSQL, "(?, ?, ?, ?, ?)")
					metaArgs = append(metaArgs, row.chunkRowID, row.chunkID, row.norm, row.dims, now)
					vecValueSQL = append(vecValueSQL, "(?, ?)")
					vecArgs = append(vecArgs, row.chunkRowID, row.blob)
					vecIDValueSQL = append(vecIDValueSQL, "(?)")
					vecIDArgs = append(vecIDArgs, row.chunkRowID)
				}
				if len(metaValueSQL) == 0 {
					continue
				}

				metaStmt := fmt.Sprintf(`
					WITH input(chunk_row_id, chunk_id, norm, dimensions, created_at) AS (VALUES %s)
					INSERT INTO intel_embeddings (chunk_row_id, chunk_id, norm, dimensions, created_at)
					SELECT i.chunk_row_id, i.chunk_id, i.norm, i.dimensions, i.created_at
					FROM input i
					JOIN intel_chunks c ON c.id = i.chunk_row_id
					ON CONFLICT(chunk_row_id) DO UPDATE SET
						chunk_id = excluded.chunk_id,
						norm = excluded.norm,
						dimensions = excluded.dimensions,
						created_at = excluded.created_at
				`, strings.Join(metaValueSQL, ","))
				if _, err := tx.ExecContext(ctx, metaStmt, metaArgs...); err != nil {
					return fmt.Errorf("upsert intel embedding meta dims=%d rows=%d vars=%d: %w", dims, len(rows), len(metaArgs), err)
				}

				vecDeleteStmt := fmt.Sprintf(`
					WITH input(chunk_row_id) AS (VALUES %s)
					DELETE FROM %s
					WHERE chunk_id IN (
						SELECT i.chunk_row_id
						FROM input i
						JOIN intel_chunks c ON c.id = i.chunk_row_id
					)
				`, strings.Join(vecIDValueSQL, ","), intelVecTableName(dims))
				if _, err := tx.ExecContext(ctx, vecDeleteStmt, vecIDArgs...); err != nil {
					return fmt.Errorf("delete intel vec dims=%d rows=%d vars=%d: %w", dims, len(rows), len(vecIDArgs), err)
				}

				vecStmt := fmt.Sprintf(`
					WITH input(chunk_row_id, embedding) AS (VALUES %s)
					INSERT INTO %s(chunk_id, owner_type, embedding)
					SELECT i.chunk_row_id, c.owner_type, i.embedding
					FROM input i
					JOIN intel_chunks c ON c.id = i.chunk_row_id
				`, strings.Join(vecValueSQL, ","), intelVecTableName(dims))
				if _, err := tx.ExecContext(ctx, vecStmt, vecArgs...); err != nil {
					return fmt.Errorf("insert intel vec dims=%d rows=%d vars=%d: %w", dims, len(rows), len(vecArgs), err)
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO index_metadata(key, value) VALUES (?, '1')
			ON CONFLICT(key) DO UPDATE SET value = CAST(value AS INTEGER) + 1
		`, searchEmbeddingGenerationKey); err != nil {
			return fmt.Errorf("advance search embedding generation: %w", err)
		}
		return nil
	})
}
