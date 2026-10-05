package indexing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRefreshValidationProjectionScratchBootstrapsAndCleansUp(t *testing.T) {
	root := writeValidationProjectionVault(t)
	result, err := RefreshValidationProjection(context.Background(), ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionScratch,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Runtime)
	require.True(t, result.Runtime.Ready)
	require.Equal(t, ProjectionFresh, result.Freshness[ProjectionDomainMetadata].State)
	require.Equal(t, ProjectionFresh, result.Freshness[ProjectionDomainMarkdownTargets].State)
	require.Equal(t, ProjectionFresh, result.Freshness[ProjectionDomainOntology].State)
	require.Contains(t, result.Freshness[ProjectionDomainOntology].Hash, fmt.Sprintf(";materialization:%d", ontology.OntologyMaterializationVersion))
	require.Equal(t, ProjectionUnavailable, result.Freshness[ProjectionDomainCode].State)
	require.Equal(t, 1, result.Counters.NoteScans)
	require.Zero(t, result.Counters.ProjectCodeEnumerations)
	require.Zero(t, result.Counters.ProviderCalls)
	require.False(t, strings.HasPrefix(result.IndexPath, root), "scratch DB must not live in the checkout/vault")
	pathsBeforeClose, err := result.Runtime.Store.CurrentNoteMetadataPaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"notes/one.md"}, pathsBeforeClose)
	targets, err := result.Runtime.Store.CurrentNoteFragmentTargets(context.Background(), []string{"notes/one.md"}, "", "")
	require.NoError(t, err)
	require.Len(t, targets, 2)

	scratchDir := filepath.Dir(result.IndexPath)
	require.NoError(t, result.Close())
	require.NoError(t, result.Close(), "result cleanup is idempotent for one consumer-owned lifetime")
	_, err = os.Stat(scratchDir)
	require.True(t, os.IsNotExist(err), "scratch lifecycle must remove DB and WAL sidecars")
}

func TestRefreshValidationProjectionKeepsProjectableHTMLOutOfMarkdownOnlyDomains(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "reference.html"), []byte("<article>not Markdown</article>\n"), 0o644))

	result, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath: root,
		VaultDef: obsidian.VaultDefinition{
			Path:     root,
			Includes: []string{"notes/*.md", "docs/*.html"},
		},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionScratch,
	})
	require.NoError(t, err)
	defer func() { _ = result.Close() }()

	metadataPaths, err := result.Runtime.Store.CurrentNoteMetadataPaths(ctx)
	require.NoError(t, err)
	// Validation projects only the Markdown domains consumed by its checks.
	// Projectable HTML remains owned by unified indexing, but does not enter the
	// Markdown parser or ontology projection here.
	require.Equal(t, []string{"notes/one.md"}, metadataPaths)
	targets, err := result.Runtime.Store.CurrentNoteFragmentTargets(ctx, []string{"docs/reference.html"}, "", "")
	require.NoError(t, err)
	require.Empty(t, targets)
	nodes, err := result.Runtime.Store.OntologyNodesByPaths(ctx, []string{"docs/reference.html"})
	require.NoError(t, err)
	require.Empty(t, nodes)
}

func TestRefreshValidationProjectionRunsPreMutationCheckBeforeOpeningStore(t *testing.T) {
	t.Parallel()

	root := writeValidationProjectionVault(t)
	barrier := errors.New("pending repair journal")
	called := false
	_, err := RefreshValidationProjection(context.Background(), ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
		BeforeMutation: func(context.Context) error {
			called = true
			return barrier
		},
	})
	require.ErrorIs(t, err, barrier)
	require.True(t, called)
	_, statErr := os.Stat(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.True(t, os.IsNotExist(statErr), "pre-mutation barrier must run before opening the live store")
}

func TestRefreshValidationProjectionRequiresNoteMetadataBeforeOpeningStore(t *testing.T) {
	root := writeValidationProjectionVault(t)

	_, err := RefreshValidationProjection(context.Background(), ValidationProjectionRequest{
		VaultPath: root,
		VaultDef:  obsidian.VaultDefinition{Path: root},
		Target:    ValidationProjectionLive,
	})
	require.ErrorContains(t, err, "note metadata indexer")
	_, statErr := os.Stat(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.True(t, os.IsNotExist(statErr), "missing composition dependency must fail before opening the store")
}

func TestRefreshValidationProjectionProductionPathRendersWritebackMetrics(t *testing.T) {
	root := writeValidationProjectionVault(t)
	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	result, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionScratch,
	})
	require.NoError(t, err)
	require.NoError(t, result.Close())

	summary := collector.RenderSummary()
	require.Contains(t, summary, "validation_projection_source")
	require.Contains(t, summary, "writeback_validation_metadata_flushes=1")
	require.Contains(t, summary, "writeback_validation_metadata_batch_rows_max=")
	require.Contains(t, summary, "writeback_validation_metadata_queue_depth_peak=")
	require.Contains(t, summary, "validation_projection_ontology")
}

func TestRefreshValidationProjectionLeavesIndependentDomainTablesUnchanged(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}
	first, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	db := first.Runtime.Store.DB()
	seedStatements := []string{
		`INSERT INTO files(path, lang, hash, indexer_version, parse_status, call_edges_stale, mtime) VALUES ('code/example.go', 'go', 'hash', 'v1', 'ok', 0, 1)`,
		`INSERT INTO intel_code_anchors(id, anchor_id, lang, kind, path, symbol, fqn, start_byte, end_byte, start_line, end_line, fingerprint, updated_at) VALUES (41, 'anchor-41', 'go', 'function', 'code/example.go', 'Example', 'example.Example', 0, 7, 1, 1, 'fp', 1)`,
		`INSERT INTO intel_chunks(id, chunk_id, owner_id, owner_row_id, owner_type, chunk_family, ord, granularity, content_hash, start_byte, end_byte, updated_at) VALUES (42, 'chunk-42', 'anchor-41', 41, 'anchor', 'default', 0, 'symbol', 'content', 0, 7, 1)`,
		`INSERT INTO intel_embeddings(chunk_row_id, chunk_id, norm, dimensions, created_at) VALUES (42, 'chunk-42', 1.0, 3, 1)`,
		`INSERT INTO graph_doc_scores(doc_path, doc_type, hub, authority, community, inbound, outbound, updated_at) VALUES ('code/example.go', 'code', 1, 2, 'seed', 3, 4, 1)`,
		`INSERT INTO graph_anchor_scores(anchor_id, pagerank, updated_at) VALUES ('anchor-41', 0.5, 1)`,
	}
	for _, statement := range seedStatements {
		_, err := db.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	tables := []string{"files", "intel_code_anchors", "intel_chunks", "intel_embeddings", "graph_doc_scores", "graph_anchor_scores"}
	before := snapshotValidationProjectionTables(t, first, tables)
	require.NoError(t, first.Close())

	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "one.md"), []byte("---\ntype: ExampleNote\nname: Changed\n---\n# Changed\n^ChangedBlock\n"), 0o644))
	request.ExactPaths = &ValidationProjectionPaths{Changed: []paths.NotePath{"notes/one.md"}}
	second, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	defer func() { _ = second.Close() }()
	after := snapshotValidationProjectionTables(t, second, tables)
	require.Equal(t, before, after)
	require.Zero(t, second.Counters.ProjectCodeEnumerations)
	require.Zero(t, second.Counters.ProviderCalls)
	require.Equal(t, ProjectionFreshness{State: ProjectionUntouched, Hash: "count:1;indexer:", Generation: 1}, second.Freshness[ProjectionDomainCode])
	anchorFreshness := second.Freshness[ProjectionDomainCodeAnchors]
	require.Equal(t, ProjectionUntouched, anchorFreshness.State)
	require.Equal(t, int64(1), anchorFreshness.Generation)
	require.Contains(t, anchorFreshness.Hash, "matches:1;generation:1;selectors:")
	require.Equal(t, ProjectionFreshness{State: ProjectionUntouched, Hash: "count:1", Generation: 1}, second.Freshness[ProjectionDomainChunks])
	require.Equal(t, ProjectionFreshness{State: ProjectionUntouched, Hash: "count:1;model:", Generation: 1}, second.Freshness[ProjectionDomainEmbeddings])
	require.Equal(t, ProjectionFreshness{State: ProjectionUntouched, Hash: "docs:1;anchors:1", Generation: 1}, second.Freshness[ProjectionDomainGraphScores])
}

func TestRefreshValidationProjectionInvalidSchemaPreservesOntologySnapshotAndSemanticRows(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}
	first, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	nodes, err := first.Runtime.Store.OntologyNodesByPaths(ctx, []string{"notes/one.md"})
	require.NoError(t, err)
	require.NotEmpty(t, nodes)
	node := nodes[0]
	require.NoError(t, first.Runtime.Store.ReplaceIntelChunks(ctx, []string{node.NodeID}, []codeanchor.IntelChunk{{
		ChunkID:     "validation-schema-invalid-chunk",
		OwnerID:     node.NodeID,
		OwnerType:   "ontology_node",
		Ord:         0,
		Granularity: "node_body",
		ContentHash: "validation-schema-invalid-content",
		StartByte:   0,
		EndByte:     8,
		UpdatedAt:   11,
	}}))
	require.NoError(t, first.Runtime.Store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{
		"validation-schema-invalid-chunk": {1, 0, 0},
	}))
	require.NoError(t, first.Runtime.Store.UpsertOntologyNodeEmbeddingStates(ctx, []codeanchor.IntelOntologyNodeEmbeddingState{{
		ChunkID:                  "validation-schema-invalid-chunk",
		NodeID:                   node.NodeID,
		NotePath:                 node.NotePath,
		TypeName:                 node.TypeName,
		NodeKind:                 node.NodeKind,
		EmbeddingSchemaSignature: "validation-schema-signature",
		NodeStructureFingerprint: node.StructuralFingerprint,
		SourceContentHash:        "validation-source-hash",
		ChunkTextHash:            "validation-schema-invalid-content",
		ChunkGranularity:         "node_body",
		Provider:                 "test",
		Model:                    "deterministic",
		UpdatedAt:                12,
	}}))
	tables := []string{
		"ontology_note_assessments",
		"ontology_note_state",
		"ontology_note_types",
		"ontology_edges",
		"ontology_type_policies",
		"ontology_nodes",
		"ontology_node_field_values",
		"intel_chunks",
		"intel_embeddings",
		"ontology_node_embedding_state",
	}
	before := snapshotValidationProjectionTables(t, first, tables)
	require.NoError(t, first.Close())

	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Invalid @node(paths: ["notes/*.md"]) {
`), 0o644))
	request.ExactPaths = &ValidationProjectionPaths{Changed: []paths.NotePath{"notes/one.md"}}
	second, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	defer func() { _ = second.Close() }()

	require.NotNil(t, second.Runtime)
	require.False(t, second.Runtime.Ready)
	require.Nil(t, second.Runtime.Schema)
	require.NotEmpty(t, second.Runtime.Issues)
	require.Equal(t, "schema_invalid", second.Runtime.Issues[0].Code)
	require.Equal(t, before, snapshotValidationProjectionTables(t, second, tables))
	state, err := second.Runtime.Store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.False(t, state.Ready)
	require.Contains(t, state.ErrorJSON, "schema_invalid")
}

func TestRefreshValidationProjectionScratchAndExactRefreshConverge(t *testing.T) {
	ctx := context.Background()
	fullRoot := writeValidationProjectionVault(t)
	incrementalRoot := writeValidationProjectionVault(t)
	finalContent := "---\ntype: ExampleNote\nname: Final\ntags: [projection]\n---\n# Final\nParagraph ^FinalBlock\n"
	require.NoError(t, os.WriteFile(filepath.Join(fullRoot, "notes", "one.md"), []byte(finalContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(incrementalRoot, "notes", "one.md"), []byte("---\ntype: ExampleNote\nname: Initial\n---\n# Initial\n^InitialBlock\n"), 0o644))

	initial, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath:    incrementalRoot,
		VaultDef:     obsidian.VaultDefinition{Path: incrementalRoot, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	})
	require.NoError(t, err)
	require.NoError(t, initial.Close())
	require.NoError(t, os.WriteFile(filepath.Join(incrementalRoot, "notes", "one.md"), []byte(finalContent), 0o644))

	full, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath:    fullRoot,
		VaultDef:     obsidian.VaultDefinition{Path: fullRoot, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionScratch,
	})
	require.NoError(t, err)
	defer func() { _ = full.Close() }()
	incremental, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath:    incrementalRoot,
		VaultDef:     obsidian.VaultDefinition{Path: incrementalRoot, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
		ExactPaths: &ValidationProjectionPaths{
			Changed: []paths.NotePath{"notes/one.md"},
		},
	})
	require.NoError(t, err)
	defer func() { _ = incremental.Close() }()

	require.Equal(t, snapshotValidationProjectionRows(t, full), snapshotValidationProjectionRows(t, incremental))
}

func TestRefreshValidationProjectionContextDocFullAndIncrementalConvergeOutsideNoteIncludes(t *testing.T) {
	ctx := context.Background()
	fullRoot := writeValidationProjectionVault(t)
	incrementalRoot := writeValidationProjectionVault(t)
	initialContext := "# Service\n\nInitial guidance.\n"
	finalContext := "# Service\n\nFinal guidance.\n"
	for _, root := range []string{fullRoot, incrementalRoot} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "src", "service"), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(fullRoot, "src", "service", "CONTEXT.md"), []byte(finalContext), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(incrementalRoot, "src", "service", "CONTEXT.md"), []byte(initialContext), 0o644))

	vaultDef := func(root string) obsidian.VaultDefinition {
		return obsidian.VaultDefinition{
			Root:     root,
			Includes: []string{"notes/**/*.md"},
			Links:    obsidian.LinkTypeBoth,
		}
	}
	initial, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath:    incrementalRoot,
		VaultDef:     vaultDef(incrementalRoot),
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	})
	require.NoError(t, err)
	initialPaths, err := initial.Runtime.Store.CurrentNoteMetadataPaths(ctx)
	require.NoError(t, err)
	require.Contains(t, initialPaths, "src/service/CONTEXT.md")
	require.NoError(t, initial.Close())

	require.NoError(t, os.WriteFile(filepath.Join(incrementalRoot, "src", "service", "CONTEXT.md"), []byte(finalContext), 0o644))
	incremental, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath:    incrementalRoot,
		VaultDef:     vaultDef(incrementalRoot),
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
		ExactPaths: &ValidationProjectionPaths{
			Changed: []paths.NotePath{"src/service/CONTEXT.md"},
		},
	})
	require.NoError(t, err)
	defer func() { _ = incremental.Close() }()

	full, err := RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath:    fullRoot,
		VaultDef:     vaultDef(fullRoot),
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionScratch,
	})
	require.NoError(t, err)
	defer func() { _ = full.Close() }()

	require.Equal(t, snapshotValidationProjectionRows(t, full), snapshotValidationProjectionRows(t, incremental))
}

func TestRefreshValidationProjectionUnchangedFastPath(t *testing.T) {
	root := writeValidationProjectionVault(t)
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}
	first, err := RefreshValidationProjection(context.Background(), request)
	require.NoError(t, err)
	require.NoError(t, first.Close())

	second, err := RefreshValidationProjection(context.Background(), request)
	require.NoError(t, err)
	defer func() { _ = second.Close() }()
	require.Empty(t, second.ChangedPaths)
	require.Empty(t, second.DeletedPaths)
	require.Equal(t, 1, second.Counters.NoteScans)
	require.Zero(t, second.Counters.MetadataBatches)
	require.Zero(t, second.Counters.ProjectCodeEnumerations)
	require.Zero(t, second.Counters.ProviderCalls)
}

func TestRefreshValidationProjectionRebuildsMissingOntologyAssessments(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "one.md"), []byte("---\ntype: ExampleNote\n---\n# One\n"), 0o644))
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}

	initial, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	require.NotEmpty(t, initial.Runtime.Issues)
	_, err = initial.Runtime.Store.DB().ExecContext(ctx, `DELETE FROM ontology_note_assessments`)
	require.NoError(t, err)
	_, err = initial.Runtime.Store.DB().ExecContext(ctx, `UPDATE ontology_schema_state SET error_json = '[]'`)
	require.NoError(t, err)
	require.NoError(t, initial.Close())

	refreshed, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	defer func() { _ = refreshed.Close() }()
	require.Empty(t, refreshed.ChangedPaths, "source hashes remain current in the stale live projection")
	require.NotEmpty(t, refreshed.Runtime.Issues, "validation must rebuild missing assessment evidence without agent start")
	require.Positive(t, refreshed.Counters.OntologyNotesConsidered)
}

func TestRefreshValidationProjectionRebuildsStaleOntologyIssueAggregate(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "one.md"), []byte("---\ntype: ExampleNote\n---\n# One\n"), 0o644))
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}

	initial, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	requireValidationProjectionIssue(t, initial, "notes/one.md", "missing_required_field")
	_, err = initial.Runtime.Store.DB().ExecContext(ctx, `UPDATE ontology_schema_state SET error_json = '[]'`)
	require.NoError(t, err)
	require.NoError(t, initial.Close())

	refreshed, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	defer func() { _ = refreshed.Close() }()
	require.Empty(t, refreshed.ChangedPaths)
	requireValidationProjectionIssue(t, refreshed, "notes/one.md", "missing_required_field")
	require.Positive(t, refreshed.Counters.OntologyNotesConsidered)
}

func TestRefreshValidationProjectionRebuildsStaleConsistentOntologyMaterialization(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "one.md"), []byte("---\ntype: NotAType\nname: One\n---\n# One\n"), 0o644))
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}

	initial, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	requireValidationProjectionIssue(t, initial, "notes/one.md", "unknown_declared_type")
	_, err = initial.Runtime.Store.DB().ExecContext(ctx, `
		UPDATE ontology_note_assessments
		SET assessment_json = '{"notePath":"notes/one.md","declaredType":"NotAType"}'
		WHERE note_path = 'notes/one.md';
		UPDATE ontology_schema_state
		SET error_json = '[]', materialization_version = ?
	`, ontology.OntologyMaterializationVersion-1)
	require.NoError(t, err)
	require.NoError(t, initial.Close())

	refreshed, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	require.Empty(t, refreshed.ChangedPaths, "source and schema hashes remain current across the binary upgrade")
	requireValidationProjectionIssue(t, refreshed, "notes/one.md", "unknown_declared_type")
	require.Positive(t, refreshed.Counters.OntologyNotesConsidered)
	state, err := refreshed.Runtime.Store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Equal(t, ontology.OntologyMaterializationVersion, state.MaterializationVersion)
	require.NoError(t, refreshed.Close())

	stable, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	defer func() { _ = stable.Close() }()
	require.Zero(t, stable.Counters.OntologyNotesConsidered, "the version mismatch must force exactly one rebuild")
	requireValidationProjectionIssue(t, stable, "notes/one.md", "unknown_declared_type")
}

func TestRefreshValidationProjectionRetainsUntouchedOntologyIssuesAfterIncrementalSync(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "one.md"), []byte("---\ntype: ExampleNote\n---\n# One\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "two.md"), []byte("---\ntype: ExampleNote\nname: Two\n---\n# Two\n"), 0o644))
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}

	initial, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	requireValidationProjectionIssue(t, initial, "notes/one.md", "missing_required_field")
	_, err = initial.Runtime.Store.DB().ExecContext(ctx, `UPDATE ontology_schema_state SET error_json = '[]'`)
	require.NoError(t, err)
	require.NoError(t, initial.Close())

	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "two.md"), []byte("---\ntype: ExampleNote\nname: Updated\n---\n# Two\n"), 0o644))
	refreshed, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	defer func() { _ = refreshed.Close() }()
	require.Equal(t, []paths.NotePath{"notes/two.md"}, refreshed.ChangedPaths)
	requireValidationProjectionIssue(t, refreshed, "notes/one.md", "missing_required_field")
}

func TestRefreshValidationProjectionExactPathsSkipsDiscovery(t *testing.T) {
	root := writeValidationProjectionVault(t)
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}
	first, err := RefreshValidationProjection(context.Background(), request)
	require.NoError(t, err)
	require.NoError(t, first.Close())
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "one.md"), []byte("---\ntype: ExampleNote\nname: Changed\n---\n"), 0o644))

	request.ExactPaths = &ValidationProjectionPaths{Changed: []paths.NotePath{"notes/one.md"}}
	second, err := RefreshValidationProjection(context.Background(), request)
	require.NoError(t, err)
	defer func() { _ = second.Close() }()
	require.Equal(t, []paths.NotePath{"notes/one.md"}, second.ChangedPaths)
	require.Zero(t, second.Counters.NoteScans)
	require.Equal(t, 1, second.Counters.ExactPathsRead)
	require.Equal(t, 1, second.Counters.MetadataBatches)
}

func TestValidationProjectionPathsPreservesMixedCaseExactPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	counters := &ValidationProjectionCounters{}
	changed, deleted, err := validationProjectionPaths(
		context.Background(),
		ValidationProjectionRequest{
			VaultPath: root,
			ExactPaths: &ValidationProjectionPaths{
				Changed: []paths.NotePath{"notes/Decision.MD"},
				Deleted: []paths.NotePath{"notes/Archived.mD"},
			},
		},
		nil,
		nil,
		counters,
		map[string]time.Duration{},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/Decision.MD"}, changed)
	require.Equal(t, []string{"notes/Archived.mD"}, deleted)
	require.Equal(t, []paths.NotePath{"notes/Decision.MD"}, stringPathsToNotePaths(changed))
	require.Equal(t, []paths.NotePath{"notes/Archived.mD"}, stringPathsToNotePaths(deleted))
	require.Equal(t, 2, counters.ExactPathsRead)
}

func TestRefreshValidationProjectionEmptyExactPathsIsNoOp(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}
	first, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	before, err := first.Runtime.Store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/one.md"})
	require.NoError(t, err)
	require.NoError(t, first.Close())

	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "one.md"), []byte("---\ntype: ExampleNote\nname: Changed\n---\n# Changed\n"), 0o644))
	request.ExactPaths = &ValidationProjectionPaths{}
	second, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	defer func() { _ = second.Close() }()
	after, err := second.Runtime.Store.CurrentNoteMetadataRowsByPaths(ctx, []string{"notes/one.md"})
	require.NoError(t, err)

	require.Equal(t, before["notes/one.md"].ContentHash, after["notes/one.md"].ContentHash)
	require.Empty(t, second.ChangedPaths)
	require.Empty(t, second.DeletedPaths)
	require.Zero(t, second.Counters.MetadataBatches)
	require.Zero(t, second.Counters.QueueFlushes)
}

func TestRefreshValidationProjectionRejectsMismatchedVaultRoots(t *testing.T) {
	firstRoot := writeValidationProjectionVault(t)
	secondRoot := writeValidationProjectionVault(t)

	_, err := RefreshValidationProjection(context.Background(), ValidationProjectionRequest{
		VaultPath:    firstRoot,
		VaultDef:     obsidian.VaultDefinition{Path: secondRoot},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	})
	require.ErrorContains(t, err, "does not match vault definition root")
}

func TestRefreshValidationProjectionRejectsExactPathOutsideVault(t *testing.T) {
	root := writeValidationProjectionVault(t)
	outside := filepath.Join(t.TempDir(), "outside.md")

	_, err := RefreshValidationProjection(context.Background(), ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
		ExactPaths: &ValidationProjectionPaths{
			Changed: []paths.NotePath{paths.NotePath(outside)},
		},
	})
	require.ErrorIs(t, err, paths.ErrOutsideVault)
}

func TestRefreshValidationProjectionHeldLockCoreDoesNotReacquireOrRelease(t *testing.T) {
	root := writeValidationProjectionVault(t)
	lockPath := obsidian.IndexLockPath(root)
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)

	result, err := refreshValidationProjectionWithHeldIndexLock(context.Background(), normalizeValidationProjectionRequest(ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}))
	require.NoError(t, err)
	require.NoError(t, result.Close())
	otherRelease, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.False(t, acquired, "projection must not release a consumer-owned lock")
	require.Nil(t, otherRelease)
	require.NoError(t, release())
}

func TestValidationProjectionFailureWaitsForQueuedWritesBeforeStoreCleanup(t *testing.T) {
	writeStarted := make(chan struct{})
	releaseWrite := make(chan struct{})
	storeClosed := make(chan struct{})
	cfg := indexwriter.DefaultConfig()
	cfg.DefaultPolicy.Rows = 1
	q := indexwriter.NewWithConfig(context.Background(), indexwriter.Handlers{
		ApplyOntologyDelta: func(context.Context, semdb.OntologyDelta) error {
			close(writeStarted)
			<-releaseWrite
			return nil
		},
	}, cfg)
	require.NoError(t, q.SubmitOntologyDelta(context.Background(), semdb.OntologyDelta{DeletePaths: []string{"queued.md"}}))
	<-writeStarted
	result := &ValidationProjectionResult{closeFn: func() error {
		close(storeClosed)
		return nil
	}}

	cleanupDone := make(chan error, 1)
	go func() {
		cleanupDone <- closeValidationProjectionAfterError(q, result, context.Canceled)
	}()
	select {
	case <-storeClosed:
		t.Fatal("store closed before queued ontology write stopped")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseWrite)
	require.ErrorIs(t, <-cleanupDone, context.Canceled)
	select {
	case <-storeClosed:
	default:
		t.Fatal("store cleanup did not run after writer shutdown")
	}
}

func TestRefreshValidationProjectionOwnedLockHonorsContention(t *testing.T) {
	root := writeValidationProjectionVault(t)
	release, acquired, err := indexlock.TryAcquire(obsidian.IndexLockPath(root))
	require.NoError(t, err)
	require.True(t, acquired)
	defer func() { _ = release() }()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err = RefreshValidationProjection(ctx, ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testNoteMetadataIndexer(t),
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRefreshValidationProjectionExactRenameTombstonesOldPath(t *testing.T) {
	root := writeValidationProjectionVault(t)
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}
	first, err := RefreshValidationProjection(context.Background(), request)
	require.NoError(t, err)
	require.NoError(t, first.Close())
	require.NoError(t, os.Rename(filepath.Join(root, "notes", "one.md"), filepath.Join(root, "notes", "two.md")))

	request.ExactPaths = &ValidationProjectionPaths{
		Changed: []paths.NotePath{"notes/two.md"},
		Deleted: []paths.NotePath{"notes/one.md"},
	}
	second, err := RefreshValidationProjection(context.Background(), request)
	require.NoError(t, err)
	defer func() { _ = second.Close() }()
	require.Equal(t, 2, second.Counters.ExactPathsRead)
	metadataPaths, err := second.Runtime.Store.CurrentNoteMetadataPaths(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"notes/two.md"}, metadataPaths)
	nodes, err := second.Runtime.Store.OntologyNodesByPaths(context.Background(), []string{"notes/one.md", "notes/two.md"})
	require.NoError(t, err)
	require.NotEmpty(t, nodes)
	for _, node := range nodes {
		require.Equal(t, "notes/two.md", node.NotePath)
	}
}

func TestRefreshValidationProjectionPublishesExpandedBootstrapPathsToOntology(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}
	initial, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	require.NoError(t, initial.Close())

	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "two.md"), []byte("---\ntype: ExampleNote\nname: Two\n---\n# Two\n"), 0o644))
	request.ExactPaths = &ValidationProjectionPaths{Changed: []paths.NotePath{"notes/two.md"}}
	refreshed, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	defer func() { _ = refreshed.Close() }()

	require.Equal(t, 2, refreshed.Counters.OntologyNotesConsidered, "ontology must consume every path expanded by the metadata bootstrap")
	nodes, err := refreshed.Runtime.Store.OntologyNodesByPaths(ctx, []string{"notes/one.md", "notes/two.md"})
	require.NoError(t, err)
	notePaths := make([]string, 0, len(nodes))
	for _, node := range nodes {
		notePaths = append(notePaths, node.NotePath)
	}
	require.ElementsMatch(t, []string{"notes/one.md", "notes/two.md"}, notePaths)
}

func TestRefreshValidationProjectionStaleMetadataBootstrapConvergesOntologyBeyondExactRequest(t *testing.T) {
	ctx := context.Background()
	root := writeValidationProjectionVault(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "two.md"), []byte("---\ntype: ExampleNote\nname: Two\n---\n# Two\n"), 0o644))
	request := ValidationProjectionRequest{
		VaultPath:    root,
		VaultDef:     obsidian.VaultDefinition{Path: root},
		NoteMetadata: testNoteMetadataIndexer(t),
		Target:       ValidationProjectionLive,
	}
	initial, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	state, err := initial.Runtime.Store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.NoError(t, initial.Runtime.Store.ApplyNoteMetadataDelta(ctx, semdb.NoteMetadataDelta{State: semdb.NoteMetadataState{
		NotesHash:    "stale-derivation",
		RawNotesHash: state.RawNotesHash,
		LoadedAt:     state.LoadedAt + 1,
		Ready:        true,
	}}))
	require.NoError(t, initial.Close())

	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "two.md"), []byte("---\ntype: ExampleNote\nname: Updated\n---\n# Two\n"), 0o644))
	request.ExactPaths = &ValidationProjectionPaths{Changed: []paths.NotePath{"notes/one.md"}}
	refreshed, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	defer func() { _ = refreshed.Close() }()

	require.Equal(t, 2, refreshed.Counters.OntologyNotesConsidered)
	nodes, err := refreshed.Runtime.Store.OntologyNodesByPaths(ctx, []string{"notes/two.md"})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	fields, err := refreshed.Runtime.Store.OntologyNodeFieldValuesByNodeIDs(ctx, []string{nodes[0].NodeID}, []string{"name"})
	require.NoError(t, err)
	require.Len(t, fields, 1)
	require.Equal(t, "Updated", fields[0].ValueText)
}

func writeValidationProjectionVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type ExampleNote @node(paths: ["notes/*.md"]) {
  name: String!
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "notes", "one.md"), []byte("---\ntype: ExampleNote\nname: One\n---\n# One\n^OneBlock\n"), 0o644))
	return root
}

func requireValidationProjectionIssue(t *testing.T, result *ValidationProjectionResult, notePath, code string) {
	t.Helper()
	require.NotNil(t, result)
	require.NotNil(t, result.Runtime)
	for _, issue := range result.Runtime.Issues {
		if issue.NotePath == notePath && issue.Code == code {
			return
		}
	}
	require.Failf(t, "missing validation projection issue", "path=%s code=%s issues=%v", notePath, code, result.Runtime.Issues)
}

func snapshotValidationProjectionTables(t *testing.T, result *ValidationProjectionResult, tables []string) map[string][]string {
	t.Helper()
	out := make(map[string][]string, len(tables))
	for _, table := range tables {
		rows, err := result.Runtime.Store.DB().QueryContext(context.Background(), "SELECT * FROM "+table)
		require.NoError(t, err)
		columns, err := rows.Columns()
		require.NoError(t, err)
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			require.NoError(t, rows.Scan(dest...))
			for i, value := range values {
				if bytes, ok := value.([]byte); ok {
					values[i] = string(bytes)
				}
			}
			out[table] = append(out[table], fmt.Sprint(values...))
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		sort.Strings(out[table])
	}
	return out
}

func snapshotValidationProjectionRows(t *testing.T, result *ValidationProjectionResult) map[string][]string {
	t.Helper()
	queries := map[string]string{
		"notes": `SELECT path, title, content_hash, size FROM notes WHERE indexed_at > 0 ORDER BY path`,
		"properties": `SELECT n.path, k.property_name, v.source, v.value_text, v.value_norm, v.value_kind, v.is_list, v.list_ordinal
			FROM note_property_values v JOIN notes n ON n.id = v.note_id JOIN property_keys k ON k.property_id = v.property_id
			ORDER BY n.path, k.property_name, v.source, v.list_ordinal`,
		"tags": `SELECT n.path, t.tag_norm FROM note_tags t JOIN notes n ON n.id = t.note_id ORDER BY n.path, t.tag_norm`,
		"targets": `SELECT n.path, t.target_kind, t.target_text, t.target_norm, t.ordinal
			FROM note_fragment_targets t JOIN notes n ON n.id = t.note_id
			ORDER BY n.path, t.target_kind, t.target_norm, t.ordinal`,
		"links": `SELECT src_path, dst_path, kind FROM graph_doc_edges
			WHERE kind = 'wikilink' OR kind = 'mdlink' OR kind GLOB 'note_link:*'
			ORDER BY src_path, dst_path, kind`,
		"ontology": `SELECT node_id, note_path, node_ref_json, node_kind, type_name, parent_node_id, parent_type_name,
			title, source_locator, fragment, block_id, display_label, locator_status, start_byte, end_byte,
			structural_fingerprint, schema_hash FROM ontology_nodes ORDER BY node_id`,
	}
	out := make(map[string][]string, len(queries))
	for name, query := range queries {
		rows, err := result.Runtime.Store.DB().QueryContext(context.Background(), query)
		require.NoError(t, err)
		columns, err := rows.Columns()
		require.NoError(t, err)
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			require.NoError(t, rows.Scan(dest...))
			for i, value := range values {
				if bytes, ok := value.([]byte); ok {
					values[i] = string(bytes)
				}
			}
			out[name] = append(out[name], fmt.Sprint(values...))
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
	}
	return out
}
