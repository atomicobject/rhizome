package store

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"sort"
	"strings"
)

// EmbeddingsByChunkIDs returns embeddings keyed by chunk ID.
func (s *Store) EmbeddingsByChunkIDs(ctx context.Context, chunkIDs []string) (map[string]embeddings.Embedding, error) {
	ids := normalizeNonEmptyStrings(chunkIDs)
	if len(ids) == 0 {
		return map[string]embeddings.Embedding{}, nil
	}
	if err := s.requireIntelVecPrimary(ctx); err != nil {
		return nil, err
	}

	const batchSize = 400
	out := make(map[string]embeddings.Embedding, len(ids))

	type chunkMeta struct {
		rowID int64
		dims  int
	}
	metaByID := make(map[string]chunkMeta, len(ids))
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		holders := strings.Repeat("?,", len(batch))
		holders = strings.TrimSuffix(holders, ",")
		query := fmt.Sprintf(`
			SELECT chunk_id, chunk_row_id, dimensions
			FROM intel_embeddings
			WHERE chunk_id IN (%s)
		`, holders)
		args := make([]any, 0, len(batch))
		for _, id := range batch {
			args = append(args, id)
		}
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var rowID int64
			var dims int
			if err := rows.Scan(&id, &rowID, &dims); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if dims <= 0 {
				continue
			}
			metaByID[id] = chunkMeta{rowID: rowID, dims: dims}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}

	grouped := make(map[int]map[int64]string)
	for id, meta := range metaByID {
		if grouped[meta.dims] == nil {
			grouped[meta.dims] = make(map[int64]string)
		}
		grouped[meta.dims][meta.rowID] = id
	}

	for dims, groupedIDs := range grouped {
		table := intelVecTableName(dims)
		exists, err := s.tableExists(ctx, table)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		rowIDs := make([]int64, 0, len(groupedIDs))
		for rowID := range groupedIDs {
			rowIDs = append(rowIDs, rowID)
		}
		sort.Slice(rowIDs, func(i, j int) bool { return rowIDs[i] < rowIDs[j] })
		for start := 0; start < len(rowIDs); start += batchSize {
			end := start + batchSize
			if end > len(rowIDs) {
				end = len(rowIDs)
			}
			batch := rowIDs[start:end]
			holders := strings.Repeat("?,", len(batch))
			holders = strings.TrimSuffix(holders, ",")
			query := fmt.Sprintf(`
				SELECT chunk_id, embedding
				FROM %s
				WHERE chunk_id IN (%s)
			`, table, holders)
			args := make([]any, 0, len(batch))
			for _, rowID := range batch {
				args = append(args, rowID)
			}
			rows, err := s.db.QueryContext(ctx, query, args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var rowID int64
				var blob []byte
				if err := rows.Scan(&rowID, &blob); err != nil {
					_ = rows.Close()
					return nil, err
				}
				if len(blob) == 0 {
					continue
				}
				if id, ok := groupedIDs[rowID]; ok {
					out[id] = bytesToEmbed(blob)
				}
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return nil, err
			}
			_ = rows.Close()
		}
	}
	return out, nil
}
