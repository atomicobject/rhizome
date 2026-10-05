package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

const sqliteVecMaxK = 4096

// Canonical metadata cascades with chunk deletion and records the current
// dimension. Retained vec rows alone cannot authorize a reused chunk row ID.
const currentEmbeddingRowIDsSQL = `SELECT chunk_row_id FROM intel_embeddings WHERE dimensions = ?`

// SearchEmbeddings performs exact cosine nearest-neighbor search over the
// dimension-specific sqlite-vec table. Filters are converted into an eligible
// row-id set before KNN selection so filtered queries cannot lose a valid hit
// merely because unrelated rows are globally nearer.
func (s *Store) SearchEmbeddings(ctx context.Context, query embeddings.Embedding, k int, filters EmbeddingSearchFilters) ([]ScoredChunk, int, error) {
	if len(query) == 0 {
		return nil, 0, errors.New("query embedding is empty")
	}
	if k <= 0 {
		k = 25
	}
	if math.Sqrt(dotFloat64(query, query)) == 0 {
		return nil, 0, errors.New("query embedding has zero norm")
	}
	if err := s.requireIntelVecPrimary(ctx); err != nil {
		return nil, 0, err
	}
	if err := s.ensureIntelVecMirror(ctx, len(query)); err != nil {
		return nil, 0, err
	}

	// sqlite-vec v0.1.6 caps KNN queries at 4096. At the cap there is also no
	// room to inspect a possible equal-score boundary, so retain the exact
	// scalar path for these uncommon large requests.
	if k >= sqliteVecMaxK {
		indexingperf.AddCount(ctx, indexingperf.SemanticQueryOpVectorScalarFallbacks, 1)
		return s.searchEmbeddingsScalar(ctx, query, k, filters)
	}

	probeK := k + 32
	if doubled := k * 2; doubled > probeK {
		probeK = doubled
	}
	if probeK > sqliteVecMaxK {
		probeK = sqliteVecMaxK
	}

	for {
		indexingperf.AddCount(ctx, indexingperf.SemanticQueryOpVectorKNNQueries, 1)
		out, err := s.searchEmbeddingsKNN(ctx, query, probeK, filters)
		if err != nil {
			// Do not conceal vec/schema/database failures behind a slower query.
			return nil, 0, err
		}
		if len(out) <= k {
			return out, 0, nil
		}

		// The query orders equal scores by chunk id. If sqlite-vec returned all
		// eligible rows, or the requested boundary is not tied, its first k rows
		// are the same deterministic result as the scalar reference query.
		if len(out) < probeK || out[k-1].Score != out[k].Score {
			return out[:k], 0, nil
		}

		if probeK == sqliteVecMaxK {
			indexingperf.AddCount(ctx, indexingperf.SemanticQueryOpVectorScalarFallbacks, 1)
			return s.searchEmbeddingsScalar(ctx, query, k, filters)
		}
		indexingperf.AddCount(ctx, indexingperf.SemanticQueryOpVectorTieRetries, 1)
		probeK *= 2
		if probeK > sqliteVecMaxK {
			probeK = sqliteVecMaxK
		}
	}
}

func (s *Store) searchEmbeddingsKNN(ctx context.Context, query embeddings.Embedding, k int, filters EmbeddingSearchFilters) ([]ScoredChunk, error) {
	statement, args := buildEmbeddingKNNQuery(intelVecTableName(len(query)), embedToBytes(query), k, filters)
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanScoredChunks(rows, k)
}

func scanScoredChunks(rows *sql.Rows, capacity int) ([]ScoredChunk, error) {
	out := make([]ScoredChunk, 0, capacity)
	for rows.Next() {
		var item ScoredChunk
		if err := rows.Scan(&item.ChunkID, &item.OwnerID, &item.OwnerType, &item.Ord, &item.Granularity, &item.Breadcrumb, &item.Heading, &item.Path, &item.Score); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func buildEmbeddingKNNQuery(vecTable string, queryBlob []byte, k int, filters EmbeddingSearchFilters) (string, []any) {
	ownerTypes := normalizeEmbeddingOwnerTypes(filters.OwnerTypes)
	filters.OwnerTypes = nil
	filterSQL, filterArgs := buildEmbeddingFilterSQL(filters, "c", "a", "s", "n")
	ownerSQL, ownerArgs := embeddingOwnerPartitionSQL("v.owner_type", ownerTypes)
	eligibleOwnerSQL, eligibleOwnerArgs := embeddingOwnerPartitionSQL("c.owner_type", ownerTypes)
	var statement string
	var args []any
	queryDims := len(queryBlob) / 4 // embedToBytes encodes each float32 in four bytes.
	if filterSQL == "" {
		statement = `
			WITH nearest(chunk_id, distance) AS MATERIALIZED (
				SELECT v.chunk_id, v.distance
				FROM ` + vecTable + ` v
				WHERE v.embedding MATCH ? AND v.k = ?
				  ` + ownerSQL + `
				  AND v.chunk_id IN (` + currentEmbeddingRowIDsSQL + `)
			)
		`
		args = append([]any{queryBlob, k}, ownerArgs...)
		args = append(args, queryDims)
	} else {
		statement = `
			WITH eligible(id) AS MATERIALIZED (
				SELECT c.id
				FROM intel_chunks c
				JOIN (` + currentEmbeddingRowIDsSQL + `) e ON e.chunk_row_id = c.id
				LEFT JOIN intel_code_anchors a ON a.id = c.owner_row_id AND c.owner_type = 'anchor'
				LEFT JOIN intel_doc_sections s ON s.id = c.owner_row_id AND c.owner_type = 'doc_section'
				LEFT JOIN ontology_nodes n ON n.node_id = c.owner_id AND c.owner_type = 'ontology_node'
				WHERE 1 = 1` + filterSQL + `
				  ` + eligibleOwnerSQL + `
			),
			nearest(chunk_id, distance) AS MATERIALIZED (
				SELECT v.chunk_id, v.distance
				FROM ` + vecTable + ` v
				WHERE v.embedding MATCH ? AND v.k = ?
				  ` + ownerSQL + `
				  AND v.chunk_id IN (SELECT id FROM eligible)
			)
		`
		args = append([]any{queryDims}, filterArgs...)
		args = append(args, eligibleOwnerArgs...)
		args = append(args, queryBlob, k)
		args = append(args, ownerArgs...)
	}

	statement += `
		SELECT c.chunk_id, c.owner_id, c.owner_type, c.ord, c.granularity, c.breadcrumb, c.heading,
		       COALESCE(a.path, s.path, n.note_path, '') AS path,
		       1.0 - nearest.distance AS score
		FROM nearest
		JOIN intel_chunks c ON c.id = nearest.chunk_id
		LEFT JOIN intel_code_anchors a ON a.id = c.owner_row_id AND c.owner_type = 'anchor'
		LEFT JOIN intel_doc_sections s ON s.id = c.owner_row_id AND c.owner_type = 'doc_section'
		LEFT JOIN ontology_nodes n ON n.node_id = c.owner_id AND c.owner_type = 'ontology_node'
		ORDER BY score DESC, c.chunk_id ASC
	`
	return statement, args
}

func normalizeEmbeddingOwnerTypes(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func embeddingOwnerPartitionSQL(column string, ownerTypes []string) (string, []any) {
	if len(ownerTypes) == 0 {
		return "", nil
	}
	placeholders := make([]string, len(ownerTypes))
	args := make([]any, len(ownerTypes))
	for i, ownerType := range ownerTypes {
		placeholders[i] = "?"
		args[i] = ownerType
	}
	return "AND " + column + " IN (" + strings.Join(placeholders, ",") + ")", args
}

func buildEmbeddingFilterSQL(filters EmbeddingSearchFilters, chunkAlias, anchorAlias, sectionAlias, nodeAlias string) (string, []any) {
	var clauses []string
	var args []any

	if predicate, symbolArgs := exactSymbolFilterSQL(filters.ExactSymbols, anchorAlias+".symbol", anchorAlias+".fqn"); predicate != "" {
		clauses = append(clauses, predicate)
		args = append(args, symbolArgs...)
	}

	if len(filters.PathPrefixes) > 0 {
		var parts []string
		for _, prefix := range filters.PathPrefixes {
			prefix = strings.TrimSpace(prefix)
			if prefix == "" {
				continue
			}
			prefix = filepath.ToSlash(filepath.Clean(prefix))
			prefix = strings.TrimSuffix(prefix, "/")
			if prefix == "." || prefix == "/" {
				continue
			}
			parts = append(parts, fmt.Sprintf("(%s OR %s OR %s)",
				caseSensitivePathPrefixSQL(anchorAlias+".path"),
				caseSensitivePathPrefixSQL(sectionAlias+".path"),
				caseSensitivePathPrefixSQL(nodeAlias+".note_path"),
			))
			args = append(args, prefix, prefix, prefix, prefix, prefix, prefix, prefix, prefix, prefix)
		}
		if len(parts) > 0 {
			clauses = append(clauses, "("+strings.Join(parts, " OR ")+")")
		}
	}

	appendIn := func(values []string, expression string, lower bool) {
		var placeholders []string
		for _, value := range values {
			value = strings.TrimSpace(value)
			if lower {
				value = strings.ToLower(value)
			}
			if value == "" {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, value)
		}
		if len(placeholders) > 0 {
			clauses = append(clauses, expression+" IN ("+strings.Join(placeholders, ",")+")")
		}
	}
	appendIn(filters.Kinds, "lower("+anchorAlias+".kind)", true)
	appendIn(filters.Granularity, "lower("+chunkAlias+".granularity)", true)
	appendNotIn := func(values []string, expression string) {
		var placeholders []string
		for _, value := range values {
			value = strings.ToLower(strings.TrimSpace(value))
			if value == "" {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, value)
		}
		if len(placeholders) > 0 {
			clauses = append(clauses, expression+" NOT IN ("+strings.Join(placeholders, ",")+")")
		}
	}
	appendNotIn(filters.ExcludeGranularity, "lower("+chunkAlias+".granularity)")
	appendIn(filters.OwnerTypes, "lower("+chunkAlias+".owner_type)", true)
	appendIn(filters.OntologyTypeNames, nodeAlias+".type_name", false)
	if len(filters.NoteTypes) > 0 {
		var placeholders []string
		for _, typeName := range filters.NoteTypes {
			typeName = strings.TrimSpace(typeName)
			if typeName == "" {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, typeName)
		}
		if len(placeholders) > 0 {
			// Filter by the resolved type of the owning note. This applies to
			// doc sections and embedded nodes as well as note-root chunks.
			pathExpr := fmt.Sprintf("COALESCE(%s.path, %s.path, %s.note_path, '')", anchorAlias, sectionAlias, nodeAlias)
			clauses = append(clauses, "EXISTS (SELECT 1 FROM ontology_note_types nt WHERE nt.note_path = "+pathExpr+" AND nt.type_name IN ("+strings.Join(placeholders, ",")+"))")
		}
	}
	pathExpr := fmt.Sprintf("COALESCE(%s.path, %s.path, %s.note_path, '')", anchorAlias, sectionAlias, nodeAlias)
	if filters.TestsOnly {
		clauses = append(clauses, testPathSQL(pathExpr))
	}
	if filters.ExcludeTests {
		clauses = append(clauses, "NOT "+testPathSQL(pathExpr))
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " AND " + strings.Join(clauses, " AND "), args
}

// testPathSQL mirrors codeanchor.IsTestPath for normalized stored file paths.
func testPathSQL(expression string) string {
	lower := "lower(trim(" + expression + "))"
	// Trimming every non-slash character from the right leaves the directory
	// prefix, so filename conventions cannot accidentally match directory names.
	base := "substr(" + lower + ", length(rtrim(" + lower + ", replace(" + lower + ", '/', ''))) + 1)"
	clauses := make([]string, 0, 8)
	for _, directory := range []string{"test", "tests", "__tests__", "testdata"} {
		clauses = append(clauses, "instr('/' || "+lower+", '/"+directory+"/') > 0")
	}
	clauses = append(clauses, "substr("+base+", 1, 5) = 'test_'")
	for _, marker := range []string{"_test.", ".test.", ".spec."} {
		clauses = append(clauses, "instr("+base+", '"+marker+"') > 0")
	}
	return "(" + strings.Join(clauses, " OR ") + ")"
}

func caseSensitivePathPrefixSQL(column string) string {
	return fmt.Sprintf("(%s = ? COLLATE BINARY OR substr(%s, 1, length(?) + 1) = (? || '/') COLLATE BINARY)", column, column)
}
