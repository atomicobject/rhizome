package sqlite

import (
	"context"
	"fmt"
	"strings"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	embeddingstypes "github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func TestSearchFiltersPathPrefixesAreLiteralAndAppliedBeforeLimit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "search-filter-paths.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	const prefix = "pkg/special%_name"
	fixtures := []struct {
		path string
		id   string
	}{
		{path: prefix, id: "target-exact"},
		{path: prefix + "/nested.go", id: "target-descendant"},
		{path: prefix + "-sibling.go", id: "sibling-boundary"},
		{path: "pkg/specialX_name.go", id: "percent-underscore-wildcard"},
		{path: "pkg/Special%_name", id: "wrong-case-exact"},
		{path: "pkg/Special%_name/nested.go", id: "wrong-case-descendant"},
	}
	for i := range 34 {
		fixtures = append(fixtures, struct{ path, id string }{
			path: fmt.Sprintf("pkg/other/near-%02d.go", i), id: fmt.Sprintf("near-%02d", i),
		})
	}
	vectors := map[string]embeddingstypes.Embedding{}
	for _, fixture := range fixtures {
		anchorID := "anchor-" + fixture.id
		anchor := codeanchor.IntelAnchor{
			AnchorID:    anchorID,
			Lang:        codeanchor.LangGo,
			Kind:        "function",
			Path:        fixture.path,
			Symbol:      fixture.id,
			FQN:         "pkg." + fixture.id,
			Fingerprint: fixture.id,
		}
		body := "needle"
		if fixture.id == "sibling-boundary" {
			body = strings.Repeat("needle ", 8)
		}
		fts := []codeanchor.IntelFTSRow{{
			ItemType: "anchor",
			ItemID:   anchorID,
			Path:     fixture.path,
			Title:    fixture.id,
			Body:     body,
		}}
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, fixture.path, []codeanchor.IntelAnchor{anchor}, nil, fts))
		require.NoError(t, store.ReplaceIntelChunks(ctx, []string{anchorID}, []codeanchor.IntelChunk{{
			ChunkID:     "chunk-" + fixture.id,
			OwnerID:     anchorID,
			OwnerType:   "anchor",
			Granularity: "symbol",
			ContentHash: fixture.id,
		}}))
		vectors["chunk-"+fixture.id] = embeddingstypes.Embedding{1, 0, 0, 0}
	}
	vectors["chunk-target-exact"] = embeddingstypes.Embedding{0.9, 0.1, 0, 0}
	vectors["chunk-target-descendant"] = embeddingstypes.Embedding{0.8, 0.2, 0, 0}
	require.NoError(t, store.UpsertEmbeddings(ctx, vectors))
	unfiltered, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 1, EmbeddingSearchFilters{})
	require.NoError(t, err)
	require.NotContains(t, []string{"chunk-target-exact", "chunk-target-descendant"}, unfiltered[0].ChunkID)

	filters := EmbeddingSearchFilters{PathPrefixes: []string{prefix}}
	vectorResults, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 10, filters)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"chunk-target-exact", "chunk-target-descendant"}, chunkIDs(vectorResults))
	require.Len(t, vectorResults, 2, "the eligible rows must be selected before the nearest-neighbor limit")
	boundedVectors, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 1, filters)
	require.NoError(t, err)
	require.Equal(t, []string{"chunk-target-exact"}, chunkIDs(boundedVectors))

	scalarResults, _, err := store.searchEmbeddingsScalar(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 10, filters)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"chunk-target-exact", "chunk-target-descendant"}, chunkIDs(scalarResults))

	ftsResults, err := store.SearchIntelFTSFiltered(ctx, "needle", 10, IntelSearchFilters{PathPrefixes: []string{prefix}})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"anchor-target-exact", "anchor-target-descendant"}, intelSearchIDs(ftsResults))
	require.Len(t, ftsResults, 2, "the eligible FTS rows must be selected before the ranked limit")
	boundedFTS, err := store.SearchIntelFTSFiltered(ctx, "needle", 1, IntelSearchFilters{PathPrefixes: []string{prefix}})
	require.NoError(t, err)
	require.Equal(t, []string{"anchor-target-exact"}, intelSearchIDs(boundedFTS))
}

func TestSearchEmbeddingsTestEligibilityPrecedesKNNWindow(t *testing.T) {
	for _, testsOnly := range []bool{true, false} {
		t.Run(fmt.Sprintf("testsOnly=%t", testsOnly), func(t *testing.T) {
			ctx := context.Background()
			store, err := Open(currentSchemaTestDBPath(t, "test-window.db"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			vectors := map[string]embeddingstypes.Embedding{}
			for i := range 35 {
				path := fmt.Sprintf("pkg/near-%02d.go", i)
				if testsOnly && i == 34 || !testsOnly && i < 34 {
					path = fmt.Sprintf("pkg/near-%02d_test.go", i)
				}
				id := fmt.Sprintf("near-%02d", i)
				anchorID := "anchor-" + id
				require.NoError(t, store.ReplaceIntelCodeFile(ctx, path, []codeanchor.IntelAnchor{{
					AnchorID: anchorID, Lang: codeanchor.LangGo, Kind: "function", Path: path,
					Symbol: id, FQN: "pkg." + id, Fingerprint: id,
				}}, nil, nil))
				require.NoError(t, store.ReplaceIntelChunks(ctx, []string{anchorID}, []codeanchor.IntelChunk{{
					ChunkID: "chunk-" + id, OwnerID: anchorID, OwnerType: "anchor", Granularity: "symbol", ContentHash: id,
				}}))
				vectors["chunk-"+id] = embeddingstypes.Embedding{1, 0, 0, 0}
			}
			vectors["chunk-near-34"] = embeddingstypes.Embedding{0.8, 0.2, 0, 0}
			require.NoError(t, store.UpsertEmbeddings(ctx, vectors))
			unfiltered, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 1, EmbeddingSearchFilters{})
			require.NoError(t, err)
			require.NotEqual(t, "chunk-near-34", unfiltered[0].ChunkID)
			got, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 1, EmbeddingSearchFilters{TestsOnly: testsOnly, ExcludeTests: !testsOnly})
			require.NoError(t, err)
			require.Equal(t, []string{"chunk-near-34"}, chunkIDs(got))
		})
	}
}

func TestSearchFiltersTestEligibilityIsAppliedBeforeLimit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "search-filter-tests.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	fixtures := []struct {
		path string
		id   string
	}{
		{path: "pkg/worker.go", id: "production"},
		{path: "pkg/worker_test.go", id: "suffix-test"},
		{path: "pkg/tests/helper.go", id: "tests-directory"},
		{path: "test/helper.go", id: "root-test"},
		{path: "tests/helper.go", id: "root-tests"},
		{path: "__tests__/helper.ts", id: "root-jest"},
		{path: "src/__tests__/helper.ts", id: "nested-jest"},
		{path: "testdata/input.go", id: "root-testdata"},
		{path: "pkg/testdata/input.go", id: "nested-testdata"},
		{path: "test_worker.py", id: "python-test"},
		{path: "src/test_worker.py", id: "nested-python-test"},
		{path: "src/worker.test.ts", id: "javascript-test"},
		{path: "src/worker.spec.ts", id: "javascript-spec"},
		{path: "SRC/WORKER.SPEC.TS", id: "uppercase-spec"},
		{path: "src/testdata_loader.go", id: "production-testdata-name"},
		{path: "test_helpers/worker.go", id: "production-directory-prefix"},
		{path: "worker.test.ts/worker.go", id: "production-directory-marker"},
		{path: "test_helpers/test_worker.py", id: "python-test-under-prefixed-directory"},
	}
	vectors := make(map[string]embeddingstypes.Embedding)
	var testChunks, productionChunks, testAnchors, productionAnchors []string
	for _, fixture := range fixtures {
		vectors["chunk-"+fixture.id] = embeddingstypes.Embedding{0.8, 0.2, 0, 0}
		body := "needle"
		if fixture.id == "production" {
			vectors["chunk-"+fixture.id] = embeddingstypes.Embedding{1, 0, 0, 0}
			body = strings.Repeat("needle ", 8)
		}
		if codeanchor.IsTestPath(fixture.path) {
			testChunks = append(testChunks, "chunk-"+fixture.id)
			testAnchors = append(testAnchors, "anchor-"+fixture.id)
		} else {
			productionChunks = append(productionChunks, "chunk-"+fixture.id)
			productionAnchors = append(productionAnchors, "anchor-"+fixture.id)
		}
		anchorID := "anchor-" + fixture.id
		require.NoError(t, store.ReplaceIntelCodeFile(ctx, fixture.path, []codeanchor.IntelAnchor{{
			AnchorID: anchorID, Lang: codeanchor.LangGo, Kind: "function", Path: fixture.path,
			Symbol: fixture.id, FQN: "pkg." + fixture.id, Fingerprint: fixture.id,
		}}, nil, []codeanchor.IntelFTSRow{{ItemType: "anchor", ItemID: anchorID, Path: fixture.path, Title: fixture.id, Body: body}}))
		require.NoError(t, store.ReplaceIntelChunks(ctx, []string{anchorID}, []codeanchor.IntelChunk{{
			ChunkID: "chunk-" + fixture.id, OwnerID: anchorID, OwnerType: "anchor", Granularity: "symbol", ContentHash: fixture.id,
		}}))
	}
	require.NoError(t, store.UpsertEmbeddings(ctx, vectors))
	require.Equal(t, []string{
		"chunk-suffix-test", "chunk-tests-directory", "chunk-root-test", "chunk-root-tests",
		"chunk-root-jest", "chunk-nested-jest", "chunk-root-testdata", "chunk-nested-testdata",
		"chunk-python-test", "chunk-nested-python-test", "chunk-javascript-test",
		"chunk-javascript-spec", "chunk-uppercase-spec", "chunk-python-test-under-prefixed-directory",
	}, testChunks)
	require.Equal(t, []string{
		"chunk-production", "chunk-production-testdata-name", "chunk-production-directory-prefix", "chunk-production-directory-marker",
	}, productionChunks)

	for _, testCase := range []struct {
		name        string
		embedding   EmbeddingSearchFilters
		fts         IntelSearchFilters
		expected    []string
		expectedFTS []string
	}{
		{name: "tests only", embedding: EmbeddingSearchFilters{TestsOnly: true}, fts: IntelSearchFilters{TestsOnly: true}, expected: testChunks, expectedFTS: testAnchors},
		{name: "exclude tests", embedding: EmbeddingSearchFilters{ExcludeTests: true}, fts: IntelSearchFilters{ExcludeTests: true}, expected: productionChunks, expectedFTS: productionAnchors},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			vectorResults, _, searchErr := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, len(fixtures), testCase.embedding)
			require.NoError(t, searchErr)
			require.ElementsMatch(t, testCase.expected, chunkIDs(vectorResults))
			scalarResults, _, searchErr := store.searchEmbeddingsScalar(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, len(fixtures), testCase.embedding)
			require.NoError(t, searchErr)
			require.ElementsMatch(t, testCase.expected, chunkIDs(scalarResults))
			ftsResults, searchErr := store.SearchIntelFTSFiltered(ctx, "needle", len(fixtures), testCase.fts)
			require.NoError(t, searchErr)
			require.ElementsMatch(t, testCase.expectedFTS, intelSearchIDs(ftsResults))
		})
	}
	boundedTests, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 1, EmbeddingSearchFilters{TestsOnly: true})
	require.NoError(t, err)
	require.Len(t, boundedTests, 1)
	require.Contains(t, testChunks, boundedTests[0].ChunkID)
	boundedFTS, err := store.SearchIntelFTSFiltered(ctx, "needle", 1, IntelSearchFilters{TestsOnly: true})
	require.NoError(t, err)
	require.Len(t, boundedFTS, 1)
	require.Contains(t, testAnchors, boundedFTS[0].ID)
}

func TestSearchEmbeddingsNoteTypeUsesOwningNoteForSectionsAndEmbeddedNodes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "search-filter-note-type.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	sectionPath := "notes/project.md"
	section := codeanchor.IntelDocSection{
		SectionID:   "section-project",
		Path:        sectionPath,
		Title:       "Project",
		Level:       1,
		Content:     "needle project section",
		Fingerprint: "project-section",
	}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, sectionPath, []codeanchor.IntelDocSection{section}, nil, []codeanchor.IntelFTSRow{{
		ItemType: "doc_section",
		ItemID:   section.SectionID,
		Path:     sectionPath,
		Title:    section.Title,
		Body:     section.Content,
	}}))

	node := codeanchor.IntelOntologyNode{
		NodeID:                "node-project-task",
		NotePath:              sectionPath,
		NodeRefJSON:           `{"notePath":"notes/project.md","nodeId":"task","typeName":"EmbeddedTask","kind":"EMBEDDED"}`,
		NodeKind:              "EMBEDDED",
		TypeName:              "EmbeddedTask",
		Title:                 "Task",
		StructuralFingerprint: "project-task",
		SchemaHash:            "schema",
	}
	require.NoError(t, store.ReplaceOntologyNodes(ctx, []string{sectionPath}, []codeanchor.IntelOntologyNode{node}))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{section.SectionID, node.NodeID}, []codeanchor.IntelChunk{
		{ChunkID: "chunk-project-section", OwnerID: section.SectionID, OwnerType: "doc_section", Granularity: "section", ContentHash: "section"},
		{ChunkID: "chunk-project-embedded", OwnerID: node.NodeID, OwnerType: "ontology_node", Granularity: "node_body", ContentHash: "embedded"},
	}))

	otherPath := "notes/reference.md"
	otherSection := codeanchor.IntelDocSection{
		SectionID:   "section-reference",
		Path:        otherPath,
		Title:       "Reference",
		Level:       1,
		Content:     "needle reference section",
		Fingerprint: "reference-section",
	}
	require.NoError(t, store.ReplaceIntelDocSections(ctx, otherPath, []codeanchor.IntelDocSection{otherSection}, nil, []codeanchor.IntelFTSRow{{
		ItemType: "doc_section",
		ItemID:   otherSection.SectionID,
		Path:     otherPath,
		Title:    otherSection.Title,
		Body:     otherSection.Content,
	}}))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{otherSection.SectionID}, []codeanchor.IntelChunk{{
		ChunkID: "chunk-reference-section", OwnerID: otherSection.SectionID, OwnerType: "doc_section", Granularity: "section", ContentHash: "reference",
	}}))

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO ontology_note_types(note_path, type_name, schema_hash, updated_at) VALUES
			(?, 'Project', 'schema', 1),
			(?, 'Reference', 'schema', 1)
	`, sectionPath, otherPath)
	require.NoError(t, err)
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddingstypes.Embedding{
		"chunk-project-section":   {1, 0, 0, 0},
		"chunk-project-embedded":  {1, 0, 0, 0},
		"chunk-reference-section": {1, 0, 0, 0},
	}))

	results, _, err := store.SearchEmbeddings(ctx, embeddingstypes.Embedding{1, 0, 0, 0}, 10, EmbeddingSearchFilters{NoteTypes: []string{"Project"}})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"chunk-project-section", "chunk-project-embedded"}, chunkIDs(results))

	ftsResults, err := store.SearchIntelFTSFiltered(ctx, "needle", 10, IntelSearchFilters{NoteTypes: []string{"Project"}})
	require.NoError(t, err)
	require.Equal(t, []string{"section-project"}, intelSearchIDs(ftsResults))
}

func chunkIDs(results []ScoredChunk) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.ChunkID)
	}
	return ids
}

func intelSearchIDs(results []IntelSearchRow) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.ID)
	}
	return ids
}
