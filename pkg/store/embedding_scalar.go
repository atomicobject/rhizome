package store

import (
	"context"
	"errors"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"math"
)

// searchEmbeddingsScalar is the exact fallback for requests outside sqlite-vec's
// bounded KNN contract and unresolved equal-score boundaries.
func (s *Store) searchEmbeddingsScalar(ctx context.Context, query embeddings.Embedding, k int, filters EmbeddingSearchFilters) ([]ScoredChunk, int, error) {
	if len(query) == 0 {
		return nil, 0, errors.New("query embedding is empty")
	}
	if k <= 0 {
		k = 25
	}

	if math.Sqrt(dotFloat64(query, query)) == 0 {
		return nil, 0, errors.New("query embedding has zero norm")
	}
	queryDims := len(query)
	if err := s.requireIntelVecPrimary(ctx); err != nil {
		return nil, 0, err
	}
	if err := s.ensureIntelVecMirror(ctx, queryDims); err != nil {
		return nil, 0, err
	}
	queryBlob := embedToBytes(query)
	vecTable := intelVecTableName(queryDims)

	filterSQL, filterArgs := buildEmbeddingFilterSQL(filters, "c", "a", "s", "n")
	args := append([]any{queryBlob, queryDims}, filterArgs...)

	// Join vec-scored chunk ids with intel_chunks and owner tables for path/kind.
	baseQuery := `
		SELECT c.chunk_id, c.owner_id, c.owner_type, c.ord, c.granularity, c.breadcrumb, c.heading,
		       COALESCE(a.path, s.path, n.note_path, '') as path,
		       1.0 - vec_distance_cosine(v.embedding, ?) AS score
		FROM ` + vecTable + ` v
		JOIN intel_chunks c ON c.id = v.chunk_id
		JOIN (` + currentEmbeddingRowIDsSQL + `) e ON e.chunk_row_id = c.id
		LEFT JOIN intel_code_anchors a ON a.id = c.owner_row_id AND c.owner_type = 'anchor'
		LEFT JOIN intel_doc_sections s ON s.id = c.owner_row_id AND c.owner_type = 'doc_section'
		LEFT JOIN ontology_nodes n ON n.node_id = c.owner_id AND c.owner_type = 'ontology_node'
		WHERE 1 = 1
	`
	baseQuery += filterSQL
	baseQuery += " ORDER BY score DESC, c.chunk_id ASC"
	if k > 0 {
		baseQuery += " LIMIT ?"
		args = append(args, k)
	}

	rows, err := s.db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]ScoredChunk, 0, k)
	for rows.Next() {
		var sc ScoredChunk
		if err := rows.Scan(&sc.ChunkID, &sc.OwnerID, &sc.OwnerType, &sc.Ord, &sc.Granularity, &sc.Breadcrumb, &sc.Heading, &sc.Path, &sc.Score); err != nil {
			return nil, 0, err
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(out) > k {
		out = out[:k]
	}
	return out, 0, nil
}
