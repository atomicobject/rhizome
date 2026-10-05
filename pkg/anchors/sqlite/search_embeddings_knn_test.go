package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	embeddingstypes "github.com/atomicobject/rhizome/pkg/search/embeddings"
)

func TestSearchEmbeddingsKNNReportsQueryDiagnostics(t *testing.T) {
	collector := indexingperf.NewSemanticQueryCollector()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	store := newEmbeddingParityStore(t, ctx)

	_, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 2, EmbeddingSearchFilters{})
	require.NoError(t, err)

	operations := map[string]int64{}
	for _, operation := range collector.SemanticQueryDiagnostics().Operations {
		operations[operation.Label] = operation.Count
	}
	require.Equal(t, int64(1), operations[indexingperf.SemanticQueryOpVectorKNNQueries])
	require.Zero(t, operations[indexingperf.SemanticQueryOpVectorTieRetries])
	require.Zero(t, operations[indexingperf.SemanticQueryOpVectorScalarFallbacks])
}

func TestSearchEmbeddingsKNNMatchesScalarAcrossFilters(t *testing.T) {
	ctx := context.Background()
	store := newEmbeddingParityStore(t, ctx)
	query := embeddingstypes.Embedding{1, 0, 0, 0}

	tests := []struct {
		name    string
		k       int
		filters EmbeddingSearchFilters
		ids     []string
	}{
		{name: "unfiltered", k: 4, ids: []string{"chunk-a", "chunk-b", "chunk-node", "chunk-doc"}},
		{name: "path", k: 4, filters: EmbeddingSearchFilters{PathPrefixes: []string{"pkg"}}, ids: []string{"chunk-a"}},
		{name: "kind", k: 4, filters: EmbeddingSearchFilters{Kinds: []string{"function"}}, ids: []string{"chunk-a"}},
		{name: "granularity", k: 4, filters: EmbeddingSearchFilters{Granularity: []string{"section"}}, ids: []string{"chunk-doc"}},
		{name: "exclude granularity", k: 4, filters: EmbeddingSearchFilters{ExcludeGranularity: []string{"symbol"}}, ids: []string{"chunk-node", "chunk-doc"}},
		{name: "owner type", k: 4, filters: EmbeddingSearchFilters{OwnerTypes: []string{"doc_section"}}, ids: []string{"chunk-doc"}},
		{name: "ontology type", k: 4, filters: EmbeddingSearchFilters{OntologyTypeNames: []string{"TechnicalSpec"}}, ids: []string{"chunk-node"}},
		{name: "combined", k: 2, filters: EmbeddingSearchFilters{PathPrefixes: []string{"docs"}, OwnerTypes: []string{"doc_section", "ontology_node"}}, ids: []string{"chunk-node", "chunk-doc"}},
		{name: "limit below eligible", k: 2, ids: []string{"chunk-a", "chunk-b"}},
		{name: "limit above eligible", k: 20, ids: []string{"chunk-a", "chunk-b", "chunk-node", "chunk-doc"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := scalarEmbeddingSearchReference(t, ctx, store, query, tt.k, tt.filters)
			got, skipped, err := store.SearchEmbeddings(ctx, query, tt.k, tt.filters)
			require.NoError(t, err)
			require.Zero(t, skipped)
			requireScoredChunkParity(t, want, got)
			require.Equal(t, tt.ids, scoredChunkIDs(got))
		})
	}
}

func TestSearchEmbeddingsKNNExcludesGranularityBeforeLimit(t *testing.T) {
	ctx := context.Background()
	store := newEmbeddingParityStore(t, ctx)

	got, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 1, EmbeddingSearchFilters{
		ExcludeGranularity: []string{"symbol", "node_body"},
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "chunk-doc", got[0].ChunkID)
}

func TestSearchEmbeddingsKNNPrefiltersBeforeLimit(t *testing.T) {
	ctx := context.Background()
	store := newEmbeddingParityStore(t, ctx)

	// The three globally nearest chunks are not doc sections. A post-filtered
	// top-k query would return no result; an exact prefiltered KNN must find it.
	got, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 1, EmbeddingSearchFilters{
		OwnerTypes: []string{"doc_section"},
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "chunk-doc", got[0].ChunkID)
}

func TestSearchEmbeddingsKNNDeterministicAtTieBoundary(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ties.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchors := make([]codeanchor.IntelAnchor, 0, 48)
	chunks := make([]codeanchor.IntelChunk, 0, 48)
	embeddings := make(map[string]embeddingstypes.Embedding, 48)
	owners := make([]string, 0, 48)
	for i := 47; i >= 0; i-- { // reverse lexical/insertion order deliberately
		id := fmt.Sprintf("anchor-%02d", i)
		chunkID := fmt.Sprintf("chunk-%02d", i)
		anchors = append(anchors, codeanchor.IntelAnchor{AnchorID: id, Lang: codeanchor.LangGo, Kind: "function", Path: fmt.Sprintf("pkg/%02d.go", i), Symbol: id, FQN: id, Fingerprint: id})
		chunks = append(chunks, codeanchor.IntelChunk{ChunkID: chunkID, OwnerID: id, OwnerType: "anchor", Granularity: "symbol", ContentHash: id})
		embeddings[chunkID] = embeddingstypes.Embedding{1, 0, 0, 0}
		owners = append(owners, id)
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/ties.go", anchors, nil, nil))
	require.NoError(t, store.ReplaceIntelChunks(ctx, owners, chunks))
	require.NoError(t, store.UpsertEmbeddings(ctx, embeddings))

	want := scalarEmbeddingSearchReference(t, ctx, store, embeddingstypes.Embedding{1, 0, 0, 0}, 5, EmbeddingSearchFilters{})
	got, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 5, EmbeddingSearchFilters{})
	require.NoError(t, err)
	requireScoredChunkParity(t, want, got)
}

func TestSearchEmbeddingsKNNIgnoresOrphanedVectorRows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "orphaned-vectors.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	const orphanCount = 40
	anchors := make([]codeanchor.IntelAnchor, 0, orphanCount+1)
	chunks := make([]codeanchor.IntelChunk, 0, orphanCount+1)
	embeddings := make(map[string]embeddingstypes.Embedding, orphanCount+1)
	owners := make([]string, 0, orphanCount+1)
	orphanOwners := make([]string, 0, orphanCount)
	for i := 0; i < orphanCount; i++ {
		ownerID := fmt.Sprintf("orphan-owner-%02d", i)
		chunkID := fmt.Sprintf("orphan-chunk-%02d", i)
		anchors = append(anchors, codeanchor.IntelAnchor{AnchorID: ownerID, Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/orphans.go", Symbol: ownerID, FQN: ownerID, Fingerprint: ownerID})
		chunks = append(chunks, codeanchor.IntelChunk{ChunkID: chunkID, OwnerID: ownerID, OwnerType: "anchor", Granularity: "symbol", ContentHash: chunkID})
		embeddings[chunkID] = embeddingstypes.Embedding{1, 0, 0, 0}
		owners = append(owners, ownerID)
		orphanOwners = append(orphanOwners, ownerID)
	}
	anchors = append(anchors, codeanchor.IntelAnchor{AnchorID: "live-owner", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/orphans.go", Symbol: "Live", FQN: "Live", Fingerprint: "live"})
	chunks = append(chunks, codeanchor.IntelChunk{ChunkID: "live-chunk", OwnerID: "live-owner", OwnerType: "anchor", Granularity: "symbol", ContentHash: "live"})
	embeddings["live-chunk"] = embeddingstypes.Embedding{0.8, 0.2, 0, 0}
	owners = append(owners, "live-owner")

	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/orphans.go", anchors, nil, nil))
	require.NoError(t, store.ReplaceIntelChunks(ctx, owners, chunks))
	require.NoError(t, store.UpsertEmbeddings(ctx, embeddings))

	// Populate the vec mirror, then remove the relational chunks through the
	// normal replacement path. Vec rows are deliberately retained until later
	// cleanup and must not consume the nearest-neighbor window.
	_, _, err = store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 1, EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.NoError(t, store.ReplaceIntelChunks(ctx, orphanOwners, nil))

	want := scalarEmbeddingSearchReference(t, ctx, store, embeddingstypes.Embedding{1, 0, 0, 0}, 1, EmbeddingSearchFilters{})
	got, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 1, EmbeddingSearchFilters{})
	require.NoError(t, err)
	requireScoredChunkParity(t, want, got)
	require.Equal(t, "live-chunk", got[0].ChunkID)
}

func TestEmbeddingKNNQueryUsesVecMatchPlan(t *testing.T) {
	ctx := context.Background()
	store := newEmbeddingParityStore(t, ctx)
	for name, filters := range map[string]EmbeddingSearchFilters{
		"unfiltered":     {},
		"owner only":     {OwnerTypes: []string{"doc_section", "ontology_node"}},
		"owner and path": {OwnerTypes: []string{" ontology_node ", "ANCHOR", "anchor", ""}, PathPrefixes: []string{"pkg"}},
	} {
		t.Run(name, func(t *testing.T) {
			query, args := buildEmbeddingKNNQuery(intelVecTableName(4), []byte{0, 0, 128, 63, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 4, filters)
			rows, err := store.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
			require.NoError(t, err)
			defer rows.Close()
			var details []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
				details = append(details, detail)
			}
			require.NoError(t, rows.Err())
			plan := strings.Join(details, "\n")
			t.Log(plan)
			require.Contains(t, plan, "VIRTUAL TABLE INDEX")
			require.Contains(t, plan, ":3{")
			require.Contains(t, plan, "intel_embeddings")
			// sqlite-vec encodes an equality constraint on the first partition as ]Aa_.
			if len(filters.OwnerTypes) > 0 {
				require.Contains(t, plan, "]Aa_")
			}
			if name != "owner and path" {
				require.NotContains(t, plan, "MATERIALIZE eligible")
				require.NotContains(t, plan, "SCAN eligible")
			}
			require.NotContains(t, query, "vec_distance_cosine")
		})
	}
}

func TestSearchEmbeddingsKNNOwnerPartitionUnionAndUnknownValue(t *testing.T) {
	ctx := context.Background()
	store := newEmbeddingParityStore(t, ctx)
	query := embeddingstypes.Embedding{1, 0, 0, 0}

	got, skipped, err := store.SearchEmbeddings(ctx, query, 10, EmbeddingSearchFilters{
		OwnerTypes: []string{" DOC_SECTION ", "anchor", "anchor"},
	})
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Equal(t, []string{"chunk-a", "chunk-b", "chunk-doc"}, scoredChunkIDs(got))
	got, skipped, err = store.SearchEmbeddings(ctx, query, 10, EmbeddingSearchFilters{
		OwnerTypes: []string{" ontology_node ", "ANCHOR", "anchor", ""}, PathPrefixes: []string{"pkg"},
	})
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Equal(t, []string{"chunk-a"}, scoredChunkIDs(got))

	got, skipped, err = store.SearchEmbeddings(ctx, query, 10, EmbeddingSearchFilters{
		OwnerTypes: []string{"unknown-owner-type"},
	})
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Empty(t, got, "an unknown non-empty partition must remain restrictive")
}

func TestReplaceIntelChunksMovesExistingEmbeddingToChangedOwnerPartition(t *testing.T) {
	ctx := context.Background()
	store := newEmbeddingParityStore(t, ctx)
	query := embeddingstypes.Embedding{1, 0, 0, 0}

	before := readVecLogicalRows(t, ctx, store.db, intelVecTableName(4))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"anchor-a", "section-doc"}, []codeanchor.IntelChunk{
		{ChunkID: "chunk-a", OwnerID: "section-doc", OwnerType: "doc_section", Ord: 1, Granularity: "section", ContentHash: "a"},
		{ChunkID: "chunk-doc", OwnerID: "section-doc", OwnerType: "doc_section", Granularity: "section", ContentHash: "doc"},
	}))
	require.Equal(t, before, readVecLogicalRows(t, ctx, store.db, intelVecTableName(4)), "repartitioning must preserve vector IDs and bytes")
	var vecOwnerType, chunkOwnerType string
	var chunkRowID int64
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT id, owner_type FROM intel_chunks WHERE chunk_id = 'chunk-a'`).Scan(&chunkRowID, &chunkOwnerType))
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT v.owner_type, c.owner_type
		FROM `+intelVecTableName(4)+` v JOIN intel_chunks c ON c.id = v.chunk_id
		WHERE c.chunk_id = 'chunk-a'
	`).Scan(&vecOwnerType, &chunkOwnerType))
	require.Equal(t, "doc_section", chunkOwnerType)
	require.Equal(t, chunkOwnerType, vecOwnerType)

	anchorResults, _, err := store.SearchEmbeddings(ctx, query, 10, EmbeddingSearchFilters{OwnerTypes: []string{"anchor"}})
	require.NoError(t, err)
	require.NotContains(t, scoredChunkIDs(anchorResults), "chunk-a")
	docResults, _, err := store.SearchEmbeddings(ctx, query, 10, EmbeddingSearchFilters{OwnerTypes: []string{"DOC_SECTION"}})
	require.NoError(t, err)
	require.Contains(t, scoredChunkIDs(docResults), "chunk-a")
}

func scoredChunkIDs(chunks []ScoredChunk) []string {
	ids := make([]string, len(chunks))
	for i := range chunks {
		ids[i] = chunks[i].ChunkID
	}
	return ids
}

func newEmbeddingParityStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	store, err := Open(currentSchemaTestDBPath(t, "parity.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	anchors := []codeanchor.IntelAnchor{
		{AnchorID: "anchor-a", Lang: codeanchor.LangGo, Kind: "function", Path: "pkg/a.go", Symbol: "A", FQN: "pkg.A", Fingerprint: "a"},
		{AnchorID: "anchor-b", Lang: codeanchor.LangGo, Kind: "class", Path: "lib/b.go", Symbol: "B", FQN: "lib.B", Fingerprint: "b"},
	}
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "pkg/a.go", anchors[:1], nil, nil))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "lib/b.go", anchors[1:], nil, nil))
	section := codeanchor.IntelDocSection{SectionID: "section-doc", Path: "docs/guide.md", Title: "Guide", Level: 1, Content: "guide", Fingerprint: "doc"}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, section.Path, []codeanchor.IntelDocSection{section}, nil, nil))
	node := codeanchor.IntelOntologyNode{NodeID: "node-spec", NotePath: "docs/spec.md", NodeRefJSON: `{"notePath":"docs/spec.md","nodeId":"spec","typeName":"TechnicalSpec","kind":"NOTE"}`, NodeKind: "NOTE", TypeName: "TechnicalSpec", Title: "Spec", StructuralFingerprint: "node", SchemaHash: "schema"}
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{node.NotePath}, []codeanchor.IntelOntologyNode{node}))

	chunks := []codeanchor.IntelChunk{
		{ChunkID: "chunk-a", OwnerID: "anchor-a", OwnerType: "anchor", Granularity: "symbol", ContentHash: "a"},
		{ChunkID: "chunk-b", OwnerID: "anchor-b", OwnerType: "anchor", Granularity: "symbol", ContentHash: "b"},
		{ChunkID: "chunk-doc", OwnerID: section.SectionID, OwnerType: "doc_section", Granularity: "section", ContentHash: "doc"},
		{ChunkID: "chunk-node", OwnerID: node.NodeID, OwnerType: "ontology_node", Granularity: "node_body", ContentHash: "node"},
	}
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"anchor-a", "anchor-b", section.SectionID, node.NodeID}, chunks))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddingstypes.Embedding{
		"chunk-a":    {1, 0, 0, 0},
		"chunk-b":    {0.99, 0.01, 0, 0},
		"chunk-node": {0.98, 0.02, 0, 0},
		"chunk-doc":  {0.8, 0.2, 0, 0},
	}))
	return store
}

func scalarEmbeddingSearchReference(t *testing.T, ctx context.Context, store *Store, query embeddingstypes.Embedding, k int, filters EmbeddingSearchFilters) []ScoredChunk {
	t.Helper()
	where, filterArgs := buildEmbeddingFilterSQL(filters, "c", "a", "s", "n")
	args := []any{embedToBytes(query)}
	args = append(args, filterArgs...)
	args = append(args, k)
	statement := `
		SELECT c.chunk_id, c.owner_id, c.owner_type, c.ord, c.granularity, c.breadcrumb, c.heading,
		       COALESCE(a.path, s.path, n.note_path, '') AS path,
		       1.0 - vec_distance_cosine(v.embedding, ?) AS score
		FROM ` + intelVecTableName(len(query)) + ` v
		JOIN intel_chunks c ON c.id = v.chunk_id
		LEFT JOIN intel_code_anchors a ON a.id = c.owner_row_id AND c.owner_type = 'anchor'
		LEFT JOIN intel_doc_sections s ON s.id = c.owner_row_id AND c.owner_type = 'doc_section'
		LEFT JOIN ontology_nodes n ON n.node_id = c.owner_id AND c.owner_type = 'ontology_node'
		WHERE 1 = 1` + where + `
		ORDER BY score DESC, c.chunk_id ASC
		LIMIT ?`
	rows, err := store.db.QueryContext(ctx, statement, args...)
	require.NoError(t, err)
	defer rows.Close()
	return scanScoredChunksForTest(t, rows)
}

func scanScoredChunksForTest(t *testing.T, rows *sql.Rows) []ScoredChunk {
	t.Helper()
	var out []ScoredChunk
	for rows.Next() {
		var item ScoredChunk
		require.NoError(t, rows.Scan(&item.ChunkID, &item.OwnerID, &item.OwnerType, &item.Ord, &item.Granularity, &item.Breadcrumb, &item.Heading, &item.Path, &item.Score))
		out = append(out, item)
	}
	require.NoError(t, rows.Err())
	return out
}

func requireScoredChunkParity(t *testing.T, want, got []ScoredChunk) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		require.Equal(t, want[i].ChunkID, got[i].ChunkID)
		require.Equal(t, want[i].OwnerID, got[i].OwnerID)
		require.Equal(t, want[i].OwnerType, got[i].OwnerType)
		require.Equal(t, want[i].Path, got[i].Path)
		require.True(t, math.Abs(want[i].Score-got[i].Score) < 1e-6, "score[%d]: want %.9f got %.9f", i, want[i].Score, got[i].Score)
	}
}
