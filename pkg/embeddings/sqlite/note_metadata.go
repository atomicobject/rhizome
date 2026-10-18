package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/atomicobject/rhizome/pkg/embeddings"
)

// validateLegacyMetadataVectors accepts metadata that is absent or wholly
// unknown only when compatible persisted vectors prove that the legacy index
// is usable. It is deliberately read-only for query-only runtime callers.
func validateLegacyMetadataVectors(ctx context.Context, db *sql.DB, expected embeddings.IndexMetadata) error {
	if expected.Dimensions > 0 {
		table := fmt.Sprintf("intel_embeddings_vec_d%d", expected.Dimensions)
		var exists bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM sqlite_master
				WHERE type = 'table' AND name = ?
			)
		`, table).Scan(&exists)
		if err != nil {
			return embeddings.MetadataError{Err: err}
		}
		if exists {
			err = db.QueryRowContext(ctx, fmt.Sprintf(`
				SELECT EXISTS(
					SELECT 1
					FROM intel_embeddings e
					JOIN intel_chunks c ON c.id = e.chunk_row_id
					JOIN %s v ON v.chunk_id = e.chunk_row_id
					WHERE e.dimensions = ?
				)
			`, table), expected.Dimensions).Scan(&exists)
			if err != nil {
				return embeddings.MetadataError{Err: err}
			}
		}
		if exists {
			return nil
		}
	}
	if expected.Provider != "" {
		return embeddings.MetadataError{Err: fmt.Errorf("provider mismatch: have , expected %s", expected.Provider)}
	}
	if expected.Model != "" {
		return embeddings.MetadataError{Err: fmt.Errorf("model mismatch: have , expected %s", expected.Model)}
	}
	return embeddings.MetadataError{Err: fmt.Errorf("dimensions mismatch: no compatible vectors for expected %d", expected.Dimensions)}
}

type chunkMetadataRow struct {
	noteID     string
	title      string
	index      int
	breadcrumb string
	heading    string
}

func (s *NoteStore) chunkMetadata(ctx context.Context, scored []scoredChunk) (map[int64]chunkMetadataRow, error) {
	if len(scored) == 0 {
		return map[int64]chunkMetadataRow{}, nil
	}
	ids := make([]int64, 0, len(scored))
	for _, c := range scored {
		ids = append(ids, c.chunkID)
	}

	meta := make(map[int64]chunkMetadataRow, len(ids))
	const batchSize = 500
	for batch := range slices.Chunk(ids, batchSize) {
		args := make([]any, 0, len(batch))
		for _, id := range batch {
			args = append(args, id)
		}
		query := `
			SELECT c.id, n.note_id, n.title, c.chunk_index, c.breadcrumb, c.heading
			FROM ` + tableChunkEmbeddings + ` c
			JOIN ` + tableNotes + ` n ON c.note_row_id = n.id
			WHERE c.id IN (` + makePlaceholders(len(batch)) + `)
		`
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var (
				chunkID    int64
				noteID     string
				title      string
				idx        int
				breadcrumb string
				heading    string
			)
			if err := rows.Scan(&chunkID, &noteID, &title, &idx, &breadcrumb, &heading); err != nil {
				_ = rows.Close()
				return nil, err
			}
			meta[chunkID] = chunkMetadataRow{
				noteID:     noteID,
				title:      title,
				index:      idx,
				breadcrumb: breadcrumb,
				heading:    heading,
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return meta, nil
}
