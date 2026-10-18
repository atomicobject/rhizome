package validationproduct

import (
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"testing"

	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestEnsureFreshRuntimeRepairsOldDependencyProjectionOnce(t *testing.T) {
	ctx := t.Context()
	request := dependencyProjectionFixture(t, true)
	initial, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	noteState, err := initial.Runtime.Store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	old, err := ontology.BuildIndexWithStore(ctx, request.NoteMetadata, request.VaultDef, &obsidian.Note{}, initial.Runtime.Store, initial.Runtime.Schema, noteState.NotesHash)
	require.NoError(t, err)
	require.Len(t, old.ValidationIssues, 1)
	require.Equal(t, "link_target_missing", old.ValidationIssues[0].Code)
	require.NoError(t, initial.Close())

	writeDependencyProjectionNote(t, request.VaultPath, "people/b.md", dependencyPerson("B", "Person", false))
	request.ExactPaths = &ValidationProjectionPaths{Changed: []paths.NotePath{"people/b.md"}}
	current, err := RefreshValidationProjection(ctx, request)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, current.Close()) })
	state, err := current.Runtime.Store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	metadataBefore, err := current.Runtime.Store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	// Restore the unchanged source's real ambiguous projection, as v12 retained it
	// after metadata expanded the candidate set. Other notes remain current.
	state.MaterializationVersion = 12
	issues, err := json.Marshal(old.ValidationIssues)
	require.NoError(t, err)
	state.ErrorJSON = string(issues)
	stale := oldDependencyTaskDelta(old)
	stale.SchemaState = &state
	require.NoError(t, current.Runtime.Store.ApplyOntologyDelta(ctx, stale))
	before, err := ontology.PublishedRuntimeWithStore(ctx, request.NoteMetadata, request.VaultDef, current.Runtime.Store)
	require.NoError(t, err)
	require.Len(t, before.Issues, 1)
	t.Logf("v12 published ready=%v; task still has %s", before.Ready, before.Issues[0].Code)
	beforeEdges, err := current.Runtime.Store.OntologyEdgesForPaths(ctx, []string{dependencyTaskPath}, true, "", 0)
	require.NoError(t, err)
	require.Empty(t, beforeEdges)

	repaired, err := ontologytest.Publish(ctx, request.NoteMetadata, request.VaultDef, &obsidian.Note{}, current.Runtime.Store)
	require.NoError(t, err)
	requireDependencyProjectionConverged(t, request, repaired,
		[]string{"people/a.md", "people/b.md", "people/independent.md", dependencyTaskPath}, "people/a.md", "")
	metadataAfter, err := current.Runtime.Store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.Equal(t, metadataBefore, metadataAfter, "migration repairs derived ontology without changing source freshness")
	repairedState, err := current.Runtime.Store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Equal(t, ontology.OntologyMaterializationVersion, repairedState.MaterializationVersion)
	again, err := ontologytest.Sync(ctx, request.NoteMetadata, request.VaultDef, &obsidian.Note{}, current.Runtime.Store, nil, nil)
	require.NoError(t, err)
	require.False(t, again.Dirty, "the old-version rebuild happens once")
	afterAgain, err := current.Runtime.Store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Equal(t, repairedState, afterAgain)
	t.Logf("ordinary refresh repaired v12 -> v%d without source changes; repeat is clean", repairedState.MaterializationVersion)
}

func oldDependencyTaskDelta(old *ontology.BuildResult) semdb.OntologyDelta {
	model := readmodel.NodeReadModel{NotePaths: []string{dependencyTaskPath}}
	delta := semdb.OntologyDelta{ReplacePaths: []string{dependencyTaskPath}, EdgeSources: []string{dependencyTaskPath}}
	for _, row := range old.AssessmentRows {
		if row.NotePath == dependencyTaskPath {
			delta.Assessments = append(delta.Assessments, row)
		}
	}
	for _, row := range old.NoteStates {
		if row.NotePath == dependencyTaskPath {
			delta.NoteStates = append(delta.NoteStates, row)
		}
	}
	for _, row := range old.NoteTypes {
		if row.NotePath == dependencyTaskPath {
			delta.NoteTypes = append(delta.NoteTypes, row)
		}
	}
	for _, row := range old.Edges {
		if row.SrcPath == dependencyTaskPath {
			delta.Edges = append(delta.Edges, row)
		}
	}
	for _, row := range old.Nodes {
		if row.NotePath == dependencyTaskPath {
			model.Nodes = append(model.Nodes, row)
		}
	}
	for _, row := range old.NodeFieldValues {
		if row.NotePath == dependencyTaskPath {
			model.FieldValues = append(model.FieldValues, row)
		}
	}
	for _, row := range old.NodeLinkDeps {
		if row.SourceNotePath == dependencyTaskPath {
			model.LinkDependencies = append(model.LinkDependencies, row)
		}
	}
	delta.ReadModels = []readmodel.NodeReadModel{model}
	return delta
}
