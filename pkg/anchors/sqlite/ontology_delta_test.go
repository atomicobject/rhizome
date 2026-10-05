package sqlite

import (
	"context"
	"fmt"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestOntologyDeltaRowCountIncludesSchemaState(t *testing.T) {
	delta := OntologyDelta{
		DeletePaths: []string{"notes/a.md"},
		SchemaState: &OntologySchemaState{SchemaHash: "schema"},
	}

	require.Equal(t, 2, delta.RowCount())
}

func TestApplyOntologyDelta_ReplacesSourcesAndDeletesTouchedPaths(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		Assessments: []OntologyNoteAssessmentRow{
			{NotePath: "notes/a.md", DeclaredType: "Project", ResolvedType: "Project", AssessmentJSON: `{"notePath":"notes/a.md"}`, SchemaHash: "old", UpdatedAt: 1},
			{NotePath: "notes/b.md", DeclaredType: "Decision", ResolvedType: "Decision", AssessmentJSON: `{"notePath":"notes/b.md"}`, SchemaHash: "old", UpdatedAt: 1},
		},
		NoteTypes: []OntologyNoteTypeRow{
			{NotePath: "notes/a.md", TypeName: "Project", SchemaHash: "old", UpdatedAt: 1},
			{NotePath: "notes/b.md", TypeName: "Decision", SchemaHash: "old", UpdatedAt: 1},
		},
		Edges: []OntologyEdgeRow{
			{SrcPath: "notes/a.md", RelationName: "decision", DstPath: "notes/b.md", DstType: "Decision", Provenance: "field", Structural: true, SchemaHash: "old", UpdatedAt: 1},
			{SrcPath: "notes/c.md", RelationName: "related", DstPath: "notes/b.md", DstType: "Decision", Provenance: "backlink", Structural: false, SchemaHash: "old", UpdatedAt: 1},
		},
		SchemaState: OntologySchemaState{SchemaHash: "old", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, store.UpsertOntologyNoteStates(ctx, []OntologyNoteStateRow{
		{NotePath: "notes/a.md", InputFingerprint: "fp-a-old", SchemaHash: "old", ResolvedType: "Project", UpdatedAt: 1},
		{NotePath: "notes/b.md", InputFingerprint: "fp-b-old", SchemaHash: "old", ResolvedType: "Decision", UpdatedAt: 1},
	}))

	err = store.ApplyOntologyDelta(ctx, OntologyDelta{
		DeletePaths:  []string{"notes/b.md"},
		ReplacePaths: []string{"notes/a.md"},
		EdgeSources:  []string{"notes/a.md"},
		Assessments: []OntologyNoteAssessmentRow{
			{NotePath: "notes/a.md", DeclaredType: "Project", ResolvedType: "Project", AssessmentJSON: `{"notePath":"notes/a.md","resolvedType":"Project"}`, SchemaHash: "new", UpdatedAt: 2},
		},
		NoteStates: []OntologyNoteStateRow{
			{NotePath: "notes/a.md", InputFingerprint: "fp-a-new", SchemaHash: "new", ResolvedType: "Project", UpdatedAt: 2},
		},
		NoteTypes: []OntologyNoteTypeRow{
			{NotePath: "notes/a.md", TypeName: "Project", SchemaHash: "new", UpdatedAt: 2},
		},
		Edges: []OntologyEdgeRow{
			{SrcPath: "notes/a.md", RelationName: "decision", DstPath: "notes/d.md", DstType: "Decision", Provenance: "field", Structural: true, SchemaHash: "new", UpdatedAt: 2},
		},
		SchemaState: &OntologySchemaState{SchemaHash: "new", NotesHash: "n2", MaterializationVersion: 7, LoadedAt: 2, Ready: true},
	})
	require.NoError(t, err)

	state, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Equal(t, "new", state.SchemaHash)
	require.Equal(t, "n2", state.NotesHash)
	require.Equal(t, 7, state.MaterializationVersion)

	row, ok, err := store.GetOntologyTypeByPath(ctx, "notes/a.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "Project", row.TypeName)

	_, ok, err = store.GetOntologyTypeByPath(ctx, "notes/b.md")
	require.NoError(t, err)
	require.False(t, ok)

	edges, err := store.OntologyEdgesForPaths(ctx, []string{"notes/a.md", "notes/b.md", "notes/c.md"}, true, "", 0)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	require.Equal(t, "notes/a.md", edges[0].SrcPath)
	require.Equal(t, "notes/d.md", edges[0].DstPath)

	noteStates, err := store.OntologyNoteStatesByPaths(ctx, []string{"notes/a.md", "notes/b.md"})
	require.NoError(t, err)
	require.Contains(t, noteStates, "notes/a.md")
	require.NotContains(t, noteStates, "notes/b.md")
	require.Equal(t, "fp-a-new", noteStates["notes/a.md"].InputFingerprint)
}

func TestValidationState_SurvivesOntologySnapshotReplace(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "validation-state.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	gen, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	updated, err := store.SetValidationResult(ctx, gen, `{"ok":false,"issueCount":2}`, 37)
	require.NoError(t, err)
	require.True(t, updated)

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		SchemaState: OntologySchemaState{SchemaHash: "schema", NotesHash: "notes", LoadedAt: 1, Ready: true},
	}))

	state, err := store.GetValidationState(ctx)
	require.NoError(t, err)
	require.Equal(t, ValidationStatusOK, state.Status)
	require.Equal(t, gen, state.Generation)
	require.Equal(t, `{"ok":false,"issueCount":2}`, state.ResultJSON)
	require.Equal(t, int64(37), state.DurationMs)
}

func TestValidationState_RejectsStaleGenerationWrites(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "validation-generation.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	oldGen, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	newGen, err := store.SetValidationRunning(ctx)
	require.NoError(t, err)
	require.Greater(t, newGen, oldGen)

	updated, err := store.SetValidationResult(ctx, oldGen, `{"ok":true}`, 10)
	require.NoError(t, err)
	require.False(t, updated)

	updated, err = store.SetValidationError(ctx, newGen, "boom", 12)
	require.NoError(t, err)
	require.True(t, updated)

	state, err := store.GetValidationState(ctx)
	require.NoError(t, err)
	require.Equal(t, ValidationStatusError, state.Status)
	require.Equal(t, newGen, state.Generation)
	require.Equal(t, "boom", state.Error)
	require.Empty(t, state.ResultJSON)
	require.Equal(t, int64(12), state.DurationMs)
}

func TestMergeOntologyDelta_PreservesFullRebuild(t *testing.T) {
	var merged OntologyDelta
	MergeOntologyDelta(&merged, OntologyDelta{
		FullRebuild: true,
		SchemaState: &OntologySchemaState{SchemaHash: "schema-a", Ready: true},
	})
	MergeOntologyDelta(&merged, OntologyDelta{
		ReplacePaths: []string{"notes/a.md"},
		Assessments:  []OntologyNoteAssessmentRow{{NotePath: "notes/a.md", AssessmentJSON: "{}"}},
	})

	require.True(t, merged.FullRebuild)
	require.Equal(t, []string{"notes/a.md"}, merged.ReplacePaths)
	require.NotNil(t, merged.SchemaState)
	require.Equal(t, "schema-a", merged.SchemaState.SchemaHash)
}

func TestApplyOntologyDelta_FullRebuildClearsOntologyNodesAndFieldValues(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology-full-rebuild-nodes.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	node := codeanchor.IntelOntologyNode{
		NodeID:      "node:a",
		NotePath:    "docs/a.md",
		NodeRefJSON: "{}",
		NodeKind:    "NOTE",
		TypeName:    "Spec",
		UpdatedAt:   1,
	}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{node.NotePath},
		Nodes:     []codeanchor.IntelOntologyNode{node},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{{
			NodeID:    node.NodeID,
			NotePath:  node.NotePath,
			TypeName:  "Spec",
			FieldName: "status",
			ValueKind: "string",
			ValueText: "Active",
			ValueNorm: "active",
			UpdatedAt: 1,
		}},
		LinkDependencies: []codeanchor.IntelOntologyNodeLinkDependency{{
			SourceNotePath: node.NotePath, NodeID: node.NodeID, TypeName: node.TypeName,
			FieldName: "owner", TargetInput: "Alice", TargetInputNorm: "alice",
			ResolvedTargetNotePath: "people/alice.md", UpdatedAt: 1,
		}},
	}))

	var (
		nodeCount, fieldCount, dependencyCount int
	)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_nodes`).Scan(&nodeCount))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_node_field_values`).Scan(&fieldCount))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_node_field_value_dependencies`).Scan(&dependencyCount))
	require.Equal(t, 1, nodeCount)
	require.Equal(t, 1, fieldCount)
	require.Equal(t, 1, dependencyCount)

	require.NoError(t, store.ApplyOntologyDelta(ctx, OntologyDelta{FullRebuild: true}))

	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_nodes`).Scan(&nodeCount))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_node_field_values`).Scan(&fieldCount))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ontology_node_field_value_dependencies`).Scan(&dependencyCount))
	require.Zero(t, nodeCount, "ontology_nodes should be truncated on full rebuild")
	require.Zero(t, fieldCount, "ontology_node_field_values should be truncated on full rebuild")
	require.Zero(t, dependencyCount, "ontology_node_field_value_dependencies should be truncated on full rebuild")
}

func TestOntologyAssessmentsByPaths_BatchesAndFiltersRequestedPaths(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		Assessments: []OntologyNoteAssessmentRow{
			{NotePath: "notes/a.md", DeclaredType: "Project", ResolvedType: "Project", AssessmentJSON: `{"notePath":"notes/a.md","resolvedType":"Project"}`, SchemaHash: "schema", UpdatedAt: 1},
			{NotePath: "notes/b.md", DeclaredType: "Decision", ResolvedType: "Decision", AssessmentJSON: `{"notePath":"notes/b.md","resolvedType":"Decision"}`, SchemaHash: "schema", UpdatedAt: 1},
		},
		SchemaState: OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))

	rows, err := store.OntologyAssessmentsByPaths(ctx, []string{"notes/b.md", "notes/missing.md"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Contains(t, rows, "notes/b.md")
	require.Equal(t, "Decision", rows["notes/b.md"].ResolvedType)
}

func TestOntologyNoteStatesByPaths_ChunksLargePathSets(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	const total = sqliteValuesBatchMaxParams*40 + 37
	stateRows := make([]OntologyNoteStateRow, 0, total)
	paths := make([]string, 0, total+1)
	for i := range total {
		path := fmt.Sprintf("notes/%04d.md", i)
		stateRows = append(stateRows, OntologyNoteStateRow{
			NotePath:         path,
			InputFingerprint: fmt.Sprintf("fingerprint-%d", i),
			SchemaHash:       "schema",
			ResolvedType:     "Project",
			UpdatedAt:        int64(i + 1),
		})
		paths = append(paths, path)
	}
	paths = append(paths, "notes/missing.md")
	require.NoError(t, store.UpsertOntologyNoteStates(ctx, stateRows))

	rows, err := store.OntologyNoteStatesByPaths(ctx, paths)
	require.NoError(t, err)
	require.Len(t, rows, total)
	require.Equal(t, "fingerprint-0", rows["notes/0000.md"].InputFingerprint)
	require.Equal(t, fmt.Sprintf("fingerprint-%d", total-1), rows[fmt.Sprintf("notes/%04d.md", total-1)].InputFingerprint)
}

func TestOntologyTypesByPaths_ChunksLargePathSets(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	const total = sqliteValuesBatchMaxParams + 37
	typeRows := make([]OntologyNoteTypeRow, 0, total)
	paths := make([]string, 0, total)
	for i := 0; i < total; i++ {
		path := fmt.Sprintf("notes/%04d.md", i)
		paths = append(paths, path)
		typeRows = append(typeRows, OntologyNoteTypeRow{
			NotePath:   path,
			TypeName:   "Project",
			SchemaHash: "schema",
			UpdatedAt:  1,
		})
	}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		NoteTypes:   typeRows,
		SchemaState: OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))

	rows, err := store.OntologyTypesByPaths(ctx, paths)
	require.NoError(t, err)
	require.Len(t, rows, total)
	require.Equal(t, "Project", rows["notes/0000.md"].TypeName)
	require.Equal(t, "Project", rows[fmt.Sprintf("notes/%04d.md", total-1)].TypeName)
}

func TestOntologyAmbientEdgesByTargets_PreservesGlobalLimitOrderAcrossBatches(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	total := sqliteValuesBatchMaxParams + 1
	paths := make([]string, 0, total)
	edges := make([]OntologyEdgeRow, 0, total)
	for i := 0; i < total; i++ {
		target := fmt.Sprintf("targets/%04d.md", i)
		paths = append(paths, target)
		src := "zz/source.md"
		if i == total-1 {
			src = "aa/source.md"
		}
		edges = append(edges, OntologyEdgeRow{
			SrcPath:      src,
			RelationName: "mentions",
			DstPath:      target,
			DstType:      "Note",
			Provenance:   "body_link",
			Structural:   false,
			SchemaHash:   "schema",
			UpdatedAt:    1,
		})
	}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		Edges:       edges,
		SchemaState: OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))

	rows, err := store.OntologyAmbientEdgesByTargets(ctx, paths, "", 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "aa/source.md", rows[0].SrcPath)
	require.Equal(t, fmt.Sprintf("targets/%04d.md", total-1), rows[0].DstPath)
}

func TestOntologyEdgesForPaths_ChunksDoublePathParameters(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "ontology.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	total := sqliteValuesBatchMaxParams/2 + 3
	paths := make([]string, 0, total)
	edges := make([]OntologyEdgeRow, 0, total)
	for i := 0; i < total; i++ {
		src := fmt.Sprintf("sources/%04d.md", i)
		dst := fmt.Sprintf("targets/%04d.md", i)
		paths = append(paths, src)
		edges = append(edges, OntologyEdgeRow{
			SrcPath:      src,
			RelationName: "mentions",
			DstPath:      dst,
			DstType:      "Note",
			Provenance:   "body_link",
			Structural:   i%2 == 0,
			SchemaHash:   "schema",
			UpdatedAt:    1,
		})
	}
	require.NoError(t, store.ReplaceOntologySnapshot(ctx, OntologySnapshot{
		Edges:       edges,
		SchemaState: OntologySchemaState{SchemaHash: "schema", NotesHash: "n1", LoadedAt: 1, Ready: true},
	}))

	rows, err := store.OntologyEdgesForPaths(ctx, paths, true, "", 0)
	require.NoError(t, err)
	require.Len(t, rows, total)
}

func TestOntologyPublicationFailurePreservesPreviousCatalog(t *testing.T) {
	ctx := context.Background()
	store, err := Open(currentSchemaTestDBPath(t, "publication.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	original := codeanchor.IntelOntologyNode{NodeID: "node:old", NotePath: "old.md", NodeRefJSON: "{}", NodeKind: "NOTE", Title: "Old"}
	require.NoError(t, store.ReplaceOntologyNodeReadModel(ctx, codeanchor.IntelOntologyNodeReadModel{
		NotePaths: []string{"old.md"}, Nodes: []codeanchor.IntelOntologyNode{original},
	}))
	// The replacement fails after the rebuild has cleared the old rows. Its
	// transaction must roll back the clear as well as the failed field write.
	delta := OntologyDelta{FullRebuild: true, ReadModels: []codeanchor.IntelOntologyNodeReadModel{{
		NotePaths:   []string{"new.md"},
		FieldValues: []codeanchor.IntelOntologyNodeFieldValue{{NodeID: "missing-parent", NotePath: "new.md", TypeName: "Spec", FieldName: "status", ValueKind: "string"}},
	}}}
	require.Error(t, store.ApplyOntologyDelta(ctx, delta))
	nodes, err := store.OntologyNodesByPaths(ctx, []string{"old.md"})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, original.NodeID, nodes[0].NodeID)
	replacement := original
	replacement.NodeID, replacement.NotePath, replacement.Title = "node:new", "new.md", "New"
	delta.ReadModels[0].FieldValues = nil
	delta.ReadModels[0].Nodes = []codeanchor.IntelOntologyNode{replacement}
	require.NoError(t, store.ApplyOntologyDelta(ctx, delta))
	nodes, err = store.OntologyNodesByPaths(ctx, []string{"old.md", "new.md"})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	require.Equal(t, "New", nodes[0].Title)
}

func TestMergeOntologyDeltaLaterRebuildSupersedesBufferedCatalog(t *testing.T) {
	delta := OntologyDelta{ReplacePaths: []string{"old.md"}, ReadModels: []codeanchor.IntelOntologyNodeReadModel{{NotePaths: []string{"old.md"}}}}
	MergeOntologyDelta(&delta, OntologyDelta{FullRebuild: true, ReadModels: []codeanchor.IntelOntologyNodeReadModel{{NotePaths: []string{"new.md"}}}})
	require.True(t, delta.FullRebuild)
	require.Empty(t, delta.ReplacePaths)
	require.Len(t, delta.ReadModels, 1)
	require.Equal(t, []string{"new.md"}, delta.ReadModels[0].NotePaths)
	MergeOntologyDelta(&delta, OntologyDelta{FullRebuild: true})
	require.Empty(t, delta.ReadModels, "removing the schema must not republish an earlier catalog")
}
