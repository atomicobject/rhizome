package store

import (
	"context"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"slices"
)

// IntelChunksByOwners returns chunks for the given owner IDs, ordered by owner_id then ord.
func (s *Store) IntelChunksByOwners(ctx context.Context, ownerIDs []string) ([]codeanchor.IntelChunk, error) {
	if len(ownerIDs) == 0 {
		return nil, nil
	}
	var out []codeanchor.IntelChunk
	for batch := range slices.Chunk(ownerIDs, 100) {
		holders, args := placeholders(batch)
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT chunk_id, owner_id, owner_type, chunk_family, ord, granularity, breadcrumb, heading, content_hash, start_byte, end_byte, updated_at
			FROM intel_chunks
			WHERE owner_id IN (%s)
			ORDER BY owner_id, ord
		`, holders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c codeanchor.IntelChunk
			if err := rows.Scan(&c.ChunkID, &c.OwnerID, &c.OwnerType, &c.ChunkFamily, &c.Ord, &c.Granularity, &c.Breadcrumb, &c.Heading, &c.ContentHash, &c.StartByte, &c.EndByte, &c.UpdatedAt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out = append(out, c)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

// IntelChunks returns all chunks in the store, ordered by owner_id then ord.
func (s *Store) IntelChunks(ctx context.Context) ([]codeanchor.IntelChunk, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT chunk_id, owner_id, owner_type, chunk_family, ord, granularity, breadcrumb, heading, content_hash, start_byte, end_byte, updated_at
		FROM intel_chunks
		ORDER BY owner_id, ord
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []codeanchor.IntelChunk
	for rows.Next() {
		var c codeanchor.IntelChunk
		if err := rows.Scan(&c.ChunkID, &c.OwnerID, &c.OwnerType, &c.ChunkFamily, &c.Ord, &c.Granularity, &c.Breadcrumb, &c.Heading, &c.ContentHash, &c.StartByte, &c.EndByte, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
