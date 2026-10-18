package store

import (
	"context"
	"fmt"
	"strings"
)

// IntelChunkEmbeddingStates returns chunk hashes for anchors that have embeddings, keyed by owner ID.
func (s *Store) IntelChunkEmbeddingStates(ctx context.Context, ownerIDs []string) (map[string]map[int]string, error) {
	out := make(map[string]map[int]string, len(ownerIDs))
	ids := normalizeNonEmptyStrings(ownerIDs)
	if len(ids) == 0 {
		return out, nil
	}
	const batchSize = 400
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[start:end]
		holders := strings.Repeat("?,", len(batch))
		holders = strings.TrimSuffix(holders, ",")
		query := fmt.Sprintf(`
			SELECT c.owner_id, c.ord, c.content_hash
			FROM intel_chunks c
			JOIN intel_embeddings e ON e.chunk_row_id = c.id
			WHERE c.owner_type = 'anchor' AND c.owner_id IN (%s)
			ORDER BY c.owner_id, c.ord
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
			var ownerID, hash string
			var ord int
			if err := rows.Scan(&ownerID, &ord, &hash); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if out[ownerID] == nil {
				out[ownerID] = make(map[int]string)
			}
			if _, ok := out[ownerID][ord]; ok {
				continue
			}
			out[ownerID][ord] = hash
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

// EmbeddingStats returns the total embedding count and distinct dimensions.
func (s *Store) EmbeddingStats(ctx context.Context) (int, []int, error) {
	if s == nil {
		return 0, nil, nil
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intel_embeddings`).Scan(&count); err != nil {
		return 0, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT dimensions FROM intel_embeddings ORDER BY dimensions`)
	if err != nil {
		return count, nil, err
	}
	defer rows.Close()
	var dims []int
	for rows.Next() {
		var d int
		if err := rows.Scan(&d); err != nil {
			return count, dims, err
		}
		dims = append(dims, d)
	}
	if err := rows.Err(); err != nil {
		return count, dims, err
	}
	return count, dims, nil
}
