package sqlite

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// SearchChunksByVector performs cosine similarity search across code chunks.
func (s *CodeStore) SearchChunksByVector(ctx context.Context, query embeddings.Embedding, k int) ([]codeindex.SimilarChunk, int, error) {
	return s.SearchChunksByVectorFiltered(ctx, query, k, codeindex.VectorSearchFilters{})
}

// SearchChunksByVectorFiltered performs cosine search with optional early filtering.
func (s *CodeStore) SearchChunksByVectorFiltered(ctx context.Context, query embeddings.Embedding, k int, filters codeindex.VectorSearchFilters) ([]codeindex.SimilarChunk, int, error) {
	if len(query) == 0 {
		return nil, 0, errors.New("query embedding is empty")
	}
	if math.Sqrt(dotFloat64(query, query)) == 0 {
		return nil, 0, errors.New("query embedding has zero norm")
	}
	queryDims := len(query)
	if err := s.ensureMirrors(ctx, queryDims); err != nil {
		return nil, 0, err
	}
	queryBlob := embedToBytes(query)
	chunkVecTable := codeChunkVecTableName(queryDims)

	candidateWhere := `c.dimensions = ?`
	candidateArgs := []any{queryDims}
	var clauses []string
	if len(filters.PathPrefixes) > 0 {
		var parts []string
		for _, p := range filters.PathPrefixes {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			parts = append(parts, "a.path LIKE ?")
			candidateArgs = append(candidateArgs, p+"%")
		}
		if len(parts) > 0 {
			clauses = append(clauses, "("+strings.Join(parts, " OR ")+")")
		}
	}
	if len(filters.Kinds) > 0 {
		var parts []string
		for _, knd := range filters.Kinds {
			knd = strings.TrimSpace(strings.ToLower(knd))
			if knd == "" {
				continue
			}
			parts = append(parts, "?")
			candidateArgs = append(candidateArgs, knd)
		}
		if len(parts) > 0 {
			clauses = append(clauses, "lower(a.kind) IN ("+strings.Join(parts, ",")+")")
		}
	}
	if len(filters.Granularity) > 0 {
		var parts []string
		for _, g := range filters.Granularity {
			g = strings.TrimSpace(strings.ToLower(g))
			if g == "" {
				continue
			}
			parts = append(parts, "?")
			candidateArgs = append(candidateArgs, g)
		}
		if len(parts) > 0 {
			clauses = append(clauses, "lower(c.granularity) IN ("+strings.Join(parts, ",")+")")
		}
	}
	if gen, ok, err := s.currentVisibleGeneration(ctx); err != nil {
		return nil, 0, err
	} else if ok {
		clauses = append(clauses, "i.sync_generation = ?")
		candidateArgs = append(candidateArgs, gen)
	}
	if len(clauses) > 0 {
		candidateWhere += " AND " + strings.Join(clauses, " AND ")
	}
	base := `
		WITH candidates AS (
			SELECT c.id,
			       a.anchor_id AS anchor_id,
			       a.path AS path,
			       a.symbol AS symbol,
			       a.fqn AS fqn,
			       a.kind AS kind,
			       c.chunk_index, c.granularity, c.breadcrumb, c.heading
			FROM ` + codeTableChunkEmbeddings + ` c
			JOIN ` + tableItems + ` i ON c.item_row_id = i.id
			JOIN intel_code_anchors a ON a.id = i.anchor_row_id
			WHERE ` + candidateWhere + `
		)
		SELECT candidates.anchor_id, candidates.path, candidates.symbol, candidates.fqn, candidates.kind,
		       candidates.chunk_index, candidates.granularity, candidates.breadcrumb, candidates.heading,
		       1.0 - vec_distance_cosine(v.embedding, ?) AS score
		FROM candidates
		JOIN ` + chunkVecTable + ` v ON v.chunk_id = candidates.id`
	args := make([]any, 0, len(candidateArgs)+2)
	args = append(args, candidateArgs...)
	args = append(args, queryBlob)
	base += " ORDER BY score DESC"
	if k > 0 {
		base += " LIMIT ?"
		args = append(args, k)
	}

	rows, err := s.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	capHint := 0
	if k > 0 {
		capHint = k
	}
	out := make([]codeindex.SimilarChunk, 0, capHint)
	for rows.Next() {
		var sc codeindex.SimilarChunk
		if err := rows.Scan(&sc.AnchorID, &sc.Path, &sc.Symbol, &sc.FQN, &sc.Kind, &sc.ChunkIndex, &sc.Granularity, &sc.Breadcrumb, &sc.Heading, &sc.Score); err != nil {
			return nil, 0, err
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if k > 0 && len(out) > k {
		out = out[:k]
	}
	return out, 0, nil
}

// SearchItemsByVector performs cosine similarity search across item embeddings.
func (s *CodeStore) SearchItemsByVector(ctx context.Context, query embeddings.Embedding, k int) ([]codeindex.SimilarItem, int, error) {
	if len(query) == 0 {
		return nil, 0, errors.New("query embedding is empty")
	}
	if math.Sqrt(dotFloat64(query, query)) == 0 {
		return nil, 0, errors.New("query embedding has zero norm")
	}
	queryDims := len(query)
	if err := s.ensureMirrors(ctx, queryDims); err != nil {
		return nil, 0, err
	}
	queryBlob := embedToBytes(query)
	itemVecTable := itemVecTableName(queryDims)

	base := `
                SELECT a.anchor_id, a.path, a.symbol, a.fqn, a.kind,
                       1.0 - vec_distance_cosine(v.embedding, ?) AS score
                FROM ` + itemVecTable + ` v
                JOIN ` + tableItemEmbeddings + ` e ON e.id = v.item_id
                JOIN ` + tableItems + ` i ON e.item_row_id = i.id
                JOIN intel_code_anchors a ON a.id = i.anchor_row_id
                WHERE e.dimensions = ?`
	args := []any{queryBlob, queryDims}
	if gen, ok, err := s.currentVisibleGeneration(ctx); err != nil {
		return nil, 0, err
	} else if ok {
		base += ` AND i.sync_generation = ?`
		args = append(args, gen)
	}
	base += ` ORDER BY score DESC`
	if k > 0 {
		base += " LIMIT ?"
		args = append(args, k)
	}
	rows, err := s.db.QueryContext(ctx, base, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	capHint := 0
	if k > 0 {
		capHint = k
	}
	out := make([]codeindex.SimilarItem, 0, capHint)
	for rows.Next() {
		var si codeindex.SimilarItem
		if err := rows.Scan(&si.AnchorID, &si.Path, &si.Symbol, &si.FQN, &si.Kind, &si.Score); err != nil {
			return nil, 0, err
		}
		out = append(out, si)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if k > 0 && len(out) > k {
		out = out[:k]
	}
	return out, 0, nil
}

// SearchChunksByText performs a lexical search using the semantic FTS index.
func (s *CodeStore) SearchChunksByText(ctx context.Context, query string, k int) ([]codeindex.SimilarChunk, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if k <= 0 {
		k = 25
	}

	ftsQuery := sqliteutil.FTS5QueryFromText(query)
	if ftsQuery == "" {
		return nil, nil
	}

	stmt := `SELECT ` + tableChunkFTS + `.anchor_id, ` + tableChunkFTS + `.chunk_index, ` + tableChunkFTS + `.path, ` + tableChunkFTS + `.symbol, ` + tableChunkFTS + `.fqn, ` + tableChunkFTS + `.kind, ` + tableChunkFTS + `.granularity, ` + tableChunkFTS + `.breadcrumb, ` + tableChunkFTS + `.heading, bm25(` + tableChunkFTS + `) AS rank
                FROM ` + tableChunkFTS + `
                JOIN intel_code_anchors a ON a.anchor_id = ` + tableChunkFTS + `.anchor_id
                JOIN ` + tableItems + ` i ON i.anchor_row_id = a.id
                WHERE ` + tableChunkFTS + ` MATCH ?`
	args := []any{ftsQuery}
	if gen, ok, err := s.currentVisibleGeneration(ctx); err != nil {
		return nil, err
	} else if ok {
		stmt += ` AND i.sync_generation = ?`
		args = append(args, gen)
	}
	stmt += ` ORDER BY rank ASC LIMIT ?`
	args = append(args, k)
	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []codeindex.SimilarChunk
	for rows.Next() {
		var sc codeindex.SimilarChunk
		var rank float64
		if err := rows.Scan(&sc.AnchorID, &sc.ChunkIndex, &sc.Path, &sc.Symbol, &sc.FQN, &sc.Kind, &sc.Granularity, &sc.Breadcrumb, &sc.Heading, &rank); err != nil {
			return nil, err
		}
		sc.Score = 1 / (1 + rank)
		out = append(out, sc)
	}
	return out, rows.Err()
}
