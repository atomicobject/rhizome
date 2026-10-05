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
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
)

// UpsertItemChunks inserts or updates chunk embeddings for a code item.
func (s *Store) UpsertItemChunks(ctx context.Context, anchorID codeindex.AnchorID, chunks []codeindex.ChunkInput, texts []string, vecs []embeddings.Embedding) error {
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
func (s *Store) UpsertItemChunksBatch(ctx context.Context, items []codeindex.ItemChunksUpsert) error {
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

	if err := bulkUpsertEmbeddingCacheTx(ctx, tx, tableEmbeddingCache, cacheRows, now); err != nil {
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
