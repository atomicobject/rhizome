package sqlite

import (
	"context"
	"errors"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/embeddings"
)

// SearchChunksByVector performs cosine similarity search across chunks using sqlite-vec.
func (s *NoteStore) SearchChunksByVector(ctx context.Context, query embeddings.Embedding, k int) ([]embeddings.SimilarChunk, int, error) {
	return s.searchChunksByVector(ctx, query, k, nil)
}

func (s *NoteStore) searchChunksByVector(ctx context.Context, query embeddings.Embedding, k int, allow map[string]struct{}) ([]embeddings.SimilarChunk, int, error) {
	if len(query) == 0 {
		return nil, 0, errors.New("query embedding is empty")
	}
	if math.Sqrt(dotFloat64(query, query)) == 0 {
		return nil, 0, errors.New("query embedding has zero norm")
	}
	queryDims := len(query)
	queryBlob := embedToBytes(query)
	err := s.ensureMirrors(ctx, queryDims)
	vecTable := chunkVecTableName(queryDims)
	if err != nil {
		return nil, 0, err
	}

	allowedRowIDs, err := s.allowedNoteRowIDs(ctx, allow)
	if err != nil {
		return nil, 0, err
	}
	if allow != nil && len(allowedRowIDs) == 0 {
		return nil, 0, nil
	}

	querySQL := `
		SELECT c.id, c.note_row_id, c.chunk_index, 1.0 - vec_distance_cosine(v.embedding, ?) AS score
		FROM ` + vecTable + ` v
		JOIN ` + tableChunkEmbeddings + ` c ON c.id = v.chunk_id
		JOIN ` + tableNotes + ` n ON n.id = c.note_row_id
		WHERE c.dimensions = ?`
	args := make([]any, 0, 2+len(allowedRowIDs))
	args = append(args, queryBlob, queryDims)
	if gen, ok, err := s.currentVisibleGeneration(ctx); err != nil {
		return nil, 0, err
	} else if ok {
		querySQL += ` AND n.sync_generation = ?`
		args = append(args, gen)
	}
	if len(allowedRowIDs) > 0 {
		querySQL += ` AND c.note_row_id IN (` + makePlaceholders(len(allowedRowIDs)) + `)`
		for _, id := range allowedRowIDs {
			args = append(args, id)
		}
	}
	querySQL += ` ORDER BY score DESC`
	if k > 0 {
		querySQL += ` LIMIT ?`
		args = append(args, k)
	}

	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	capHint := 0
	if k > 0 {
		capHint = k
	}
	scored := make([]scoredChunk, 0, capHint)
	for rows.Next() {
		var sc scoredChunk
		if err := rows.Scan(&sc.chunkID, &sc.noteRowID, &sc.index, &sc.score); err != nil {
			return nil, 0, err
		}
		scored = append(scored, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if k > 0 && len(scored) > k {
		scored = scored[:k]
	}

	chunkMeta, err := s.chunkMetadata(ctx, scored)
	if err != nil {
		return nil, 0, err
	}

	out := make([]embeddings.SimilarChunk, 0, len(scored))
	for _, sc := range scored {
		meta, ok := chunkMeta[sc.chunkID]
		if !ok {
			continue
		}
		out = append(out, embeddings.SimilarChunk{
			NoteID:     embeddings.NoteID(meta.noteID),
			Title:      meta.title,
			ChunkIndex: meta.index,
			Breadcrumb: meta.breadcrumb,
			Heading:    meta.heading,
			Score:      sc.score,
		})
	}
	return out, 0, nil
}

// SearchNotesByText performs a lightweight lexical search over note titles.
func (s *NoteStore) SearchNotesByText(ctx context.Context, query string, k int) ([]embeddings.SimilarNote, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if k <= 0 {
		k = 25
	}
	pat := "%" + strings.ToLower(q) + "%"

	querySQL := `
		SELECT note_id, title
		FROM ` + tableNotes + `
		WHERE lower(title) LIKE ?`
	args := []any{pat}
	if gen, ok, err := s.currentVisibleGeneration(ctx); err != nil {
		return nil, err
	} else if ok {
		querySQL += ` AND sync_generation = ?`
		args = append(args, gen)
	}
	querySQL += ` LIMIT ?`
	args = append(args, k*2)
	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []embeddings.SimilarNote
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		score := lexicalScore(title, q)
		out = append(out, embeddings.SimilarNote{
			ID:    embeddings.NoteID(id),
			Title: title,
			Score: score,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

func (s *NoteStore) allowedNoteRowIDs(ctx context.Context, allow map[string]struct{}) ([]int64, error) {
	if allow == nil {
		return nil, nil
	}
	if len(allow) == 0 {
		return []int64{}, nil
	}
	ids := make([]string, 0, len(allow))
	for id := range allow {
		ids = append(ids, id)
	}
	const batchSize = 500
	var rowIDs []int64
	for batch := range slices.Chunk(ids, batchSize) {
		args := make([]any, 0, len(batch))
		for _, id := range batch {
			args = append(args, id)
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT id
			FROM `+tableNotes+`
			WHERE note_id IN (`+makePlaceholders(len(batch))+`)
		`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var rowID int64
			if err := rows.Scan(&rowID); err != nil {
				_ = rows.Close()
				return nil, err
			}
			rowIDs = append(rowIDs, rowID)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return rowIDs, nil
}

func lexicalScore(title, query string) float64 {
	titleLower := strings.ToLower(strings.TrimSpace(title))
	queryLower := strings.ToLower(strings.TrimSpace(query))
	if queryLower == "" || titleLower == "" {
		return 0
	}
	if titleLower == queryLower {
		return 1.0
	}
	if strings.HasPrefix(titleLower, queryLower) {
		return 0.8
	}
	if strings.Contains(titleLower, queryLower) {
		return 0.6
	}
	return 0
}
