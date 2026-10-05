package indexing

import (
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const dependencyTaskPath = "tasks/work.md"

func TestRefreshValidationProjectionLinkDependenciesConverge(t *testing.T) {
	for _, scenario := range []string{"remove duplicate alias", "add duplicate alias", "delete duplicate owner", "change target type"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := t.Context()
			ambiguous := scenario == "remove duplicate alias" || scenario == "delete duplicate owner"
			request := dependencyProjectionFixture(t, ambiguous)
			executor := lane.New(lane.Options{LockPath: obsidian.IndexLockPath(request.VaultPath)})
			t.Cleanup(executor.Close)
			request.Lane = executor
			initial, err := RefreshValidationProjection(ctx, request)
			require.NoError(t, err)
			require.True(t, initial.Runtime.Ready)
			if ambiguous {
				requireValidationProjectionIssue(t, initial, dependencyTaskPath, "link_target_missing")
			} else {
				require.Empty(t, initial.Runtime.Issues)
			}
			control, err := ontology.SyncPublishedPaths(ctx, request.NoteMetadata, request.VaultDef, &obsidian.Note{}, initial.Runtime.Store, nil, []string{"people/independent.md"}, nil)
			require.NoError(t, err)
			require.Zero(t, control.Assessed, "an unchanged direct candidate without dependencies must still skip")
			require.Equal(t, 1, control.Skipped)
			taskBytes, err := os.ReadFile(filepath.Join(request.VaultPath, dependencyTaskPath))
			require.NoError(t, err)
			require.NoError(t, initial.Close())

			wantPaths := []string{"people/a.md", "people/b.md", "people/independent.md", dependencyTaskPath}
			wantTarget, wantIssue := "people/a.md", ""
			switch scenario {
			case "remove duplicate alias":
				writeDependencyProjectionNote(t, request.VaultPath, "people/b.md", dependencyPerson("B", "Person", false))
			case "add duplicate alias":
				writeDependencyProjectionNote(t, request.VaultPath, "people/b.md", dependencyPerson("B", "Person", true))
				wantTarget, wantIssue = "", "link_target_missing"
			case "delete duplicate owner":
				require.NoError(t, os.Rename(filepath.Join(request.VaultPath, "people/b.md"), filepath.Join(request.VaultPath, ".rhizome/retired.md")))
				wantPaths = []string{"people/a.md", "people/independent.md", dependencyTaskPath}
			case "change target type":
				writeDependencyProjectionNote(t, request.VaultPath, "people/a.md", dependencyPerson("A", "Organization", true))
				// The dependent's bytes are unchanged, but it is also an exact candidate.
				request.ExactPaths = &ValidationProjectionPaths{Changed: []paths.NotePath{"people/a.md", dependencyTaskPath}}
				wantTarget, wantIssue = "", "wrong_target_type"
			}
			refreshed, err := RefreshValidationProjection(ctx, request)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, refreshed.Close()) })
			require.Equal(t, 1, refreshed.Counters.MetadataBatches)
			require.Positive(t, refreshed.Counters.QueueFlushes)
			currentTaskBytes, err := os.ReadFile(filepath.Join(request.VaultPath, dependencyTaskPath))
			require.NoError(t, err)
			require.Equal(t, taskBytes, currentTaskBytes)
			requireDependencyProjectionConverged(t, request, refreshed.Runtime, wantPaths, wantTarget, wantIssue)
			if scenario == "delete duplicate owner" {
				deletedNodes, err := refreshed.Runtime.Store.OntologyNodesByPaths(ctx, []string{"people/b.md"})
				require.NoError(t, err)
				require.Empty(t, deletedNodes)
				deletedAssessments, err := refreshed.Runtime.Store.OntologyAssessmentsByPaths(ctx, []string{"people/b.md"})
				require.NoError(t, err)
				require.Empty(t, deletedAssessments)
			}
		})
	}
}

func dependencyProjectionFixture(t *testing.T, ambiguous bool) ValidationProjectionRequest {
	t.Helper()
	root := t.TempDir()
	writeDependencyProjectionNote(t, root, ".rhizome/ontology/schema.graphql", `
type Person @node(paths: ["people/*.md"]) { name: String! }
type Organization @node(paths: ["people/*.md"]) { name: String! }
type Task @node(paths: ["tasks/*.md"]) { name: String! assignee: Person @link }
`)
	writeDependencyProjectionNote(t, root, "people/a.md", dependencyPerson("A", "Person", true))
	writeDependencyProjectionNote(t, root, "people/b.md", dependencyPerson("B", "Person", ambiguous))
	writeDependencyProjectionNote(t, root, "people/independent.md", dependencyPerson("Independent", "Person", false))
	writeDependencyProjectionNote(t, root, dependencyTaskPath, "---\ntype: Task\nname: Work\nassignee: SHARED\n---\n")
	return ValidationProjectionRequest{
		VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root, Links: obsidian.LinkTypeBoth},
		NoteMetadata: testNoteMetadataIndexer(t), Target: ValidationProjectionLive,
	}
}

func dependencyPerson(name, typeName string, alias bool) string {
	content := "---\ntype: " + typeName + "\nname: " + name + "\n"
	if alias {
		content += "aliases: [SHARED]\n"
	}
	return content + "---\n"
}

func writeDependencyProjectionNote(t *testing.T, root, notePath, content string) {
	t.Helper()
	abs := filepath.Join(root, notePath)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
}

func dependencyTaskFieldRows(t *testing.T, store *semdb.Store) []codeanchor.IntelOntologyNodeFieldValue {
	t.Helper()
	nodes, err := store.OntologyNodesByPaths(t.Context(), []string{dependencyTaskPath})
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	rows, err := store.OntologyNodeFieldValuesByNodeIDs(t.Context(), []string{nodes[0].NodeID}, []string{"assignee"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	for i := range rows {
		rows[i].UpdatedAt = 0
	}
	return rows
}

func requireDependencyProjectionConverged(t *testing.T, request ValidationProjectionRequest, runtime *ontology.Runtime, wantPaths []string, wantTarget, wantIssue string) {
	t.Helper()
	ctx := t.Context()
	store := runtime.Store
	require.True(t, runtime.Ready)
	metadataPaths, err := store.CurrentNoteMetadataPaths(ctx)
	require.NoError(t, err)
	require.Equal(t, wantPaths, metadataPaths)
	published, err := ontology.PublishedRuntimeWithStore(ctx, request.NoteMetadata, request.VaultDef, store)
	require.NoError(t, err)
	require.True(t, published.Ready)
	fresh, err := semdb.Open(filepath.Join(t.TempDir(), "fresh.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, fresh.Close()) })
	want, err := ontology.EnsureFreshRuntimeWithStore(ctx, request.NoteMetadata, request.VaultDef, &obsidian.Note{}, fresh)
	require.NoError(t, err)
	require.True(t, want.Ready)
	gotEdges, err := store.OntologyEdgesForPaths(ctx, []string{dependencyTaskPath}, true, "", 0)
	require.NoError(t, err)
	wantEdges, err := fresh.OntologyEdgesForPaths(ctx, []string{dependencyTaskPath}, true, "", 0)
	require.NoError(t, err)
	for i := range gotEdges {
		gotEdges[i].UpdatedAt = 0
	}
	for i := range wantEdges {
		wantEdges[i].UpdatedAt = 0
	}
	require.Equal(t, wantEdges, gotEdges, "persisted relations must agree with a fresh build")
	if wantTarget == "" {
		require.Empty(t, gotEdges)
	} else {
		require.Len(t, gotEdges, 1)
		require.Equal(t, wantTarget, gotEdges[0].DstPath)
		require.Equal(t, "Person", gotEdges[0].DstType)
	}
	require.Equal(t, dependencyTaskFieldRows(t, fresh), dependencyTaskFieldRows(t, store), "persisted field targets must agree with a fresh build")
	gotRow, ok, err := store.GetOntologyAssessmentByPath(ctx, dependencyTaskPath)
	require.NoError(t, err)
	require.True(t, ok)
	wantRow, ok, err := fresh.GetOntologyAssessmentByPath(ctx, dependencyTaskPath)
	require.NoError(t, err)
	require.True(t, ok)
	gotAssessment, err := ontology.AssessmentFromJSON(gotRow.AssessmentJSON)
	require.NoError(t, err)
	wantAssessment, err := ontology.AssessmentFromJSON(wantRow.AssessmentJSON)
	require.NoError(t, err)
	require.Equal(t, wantAssessment, gotAssessment)
	require.Equal(t, wantRow.HasIssues, gotRow.HasIssues)
	wantIssues := append([]ontology.ValidationIssue{}, want.Issues...)
	require.Equal(t, wantIssues, append([]ontology.ValidationIssue{}, runtime.Issues...))
	require.Equal(t, wantIssues, append([]ontology.ValidationIssue{}, published.Issues...))
	state, err := store.GetOntologySchemaState(ctx)
	require.NoError(t, err)
	require.Equal(t, wantIssues, append([]ontology.ValidationIssue{}, ontology.ValidationIssuesFromJSON(state.ErrorJSON)...))
	if wantIssue != "" {
		require.Len(t, wantIssues, 1)
		require.Equal(t, wantIssue, wantIssues[0].Code)
		require.Equal(t, dependencyTaskPath, wantIssues[0].NotePath)
		require.Equal(t, "assignee", wantIssues[0].FieldName)
	} else {
		require.Empty(t, wantIssues)
	}
}
