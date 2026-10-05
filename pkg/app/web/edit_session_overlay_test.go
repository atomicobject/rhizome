package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// Staged reads must follow the session: a repeated read serves the same staged
// state, and a restage, an external disk change, or a newly published schema
// must each surface in the next read of an already-warmed session.
func TestReadOverlayForEditSessionRefreshesStagedReads(t *testing.T) {
	planPath, tasksPath := "specs/100-demo/plan.md", "specs/100-demo/tasks.md"
	tests := []struct {
		name   string
		change func(t *testing.T, fixture fixtureVault, srv *Server, sessionID string, revision uint64)
		check  func(t *testing.T, overlay *ontologyquery.ReadOverlay, plan, tasks *ontology.NodeProjection, planRecord noderead.NodeRecord, scope *noderead.Scope)
	}{
		{
			name: "restage",
			change: func(t *testing.T, _ fixtureVault, srv *Server, sessionID string, revision uint64) {
				_, err := srv.stageOntologyEditSessionResponse(context.Background(), sessionID, OntologyEditSessionStageRequest{
					RequestID: "restage", ExpectedRevision: revision,
					Ops: []OntologyEditOp{{ID: "field:plan-summary", Kind: "setField", Path: planPath, Field: "summary", Value: "Restaged plan summary"}},
				})
				require.NoError(t, err)
			},
			check: func(t *testing.T, overlay *ontologyquery.ReadOverlay, plan, _ *ontology.NodeProjection, _ noderead.NodeRecord, _ *noderead.Scope) {
				require.Equal(t, []string{"Restaged plan summary"}, plan.Fields["summary"].Values)
				require.Equal(t, string(OntologyEditSessionStatusDirty), overlay.Status)
			},
		},
		{
			name: "external nonoverlapping change",
			change: func(t *testing.T, fixture fixtureVault, _ *Server, _ string, _ uint64) {
				planFile := filepath.Join(fixture.root, filepath.FromSlash(planPath))
				original, err := os.ReadFile(planFile)
				require.NoError(t, err)
				changed := strings.Replace(string(original), "1. Build the browser.", "1. Build the browser externally.", 1)
				require.NotEqual(t, string(original), changed)
				require.NoError(t, os.WriteFile(planFile, []byte(changed), 0o644))
			},
			check: func(t *testing.T, overlay *ontologyquery.ReadOverlay, plan, _ *ontology.NodeProjection, _ noderead.NodeRecord, _ *noderead.Scope) {
				require.Equal(t, []string{"Staged plan summary"}, plan.Fields["summary"].Values)
				staged, ok := overlay.StagedContent(planPath)
				require.True(t, ok)
				require.Contains(t, staged, "1. Build the browser externally.")
				require.Contains(t, staged, "summary: Staged plan summary")
				require.Equal(t, string(OntologyEditSessionStatusRebased), overlay.Status)
				require.False(t, overlay.Conflicted)
			},
		},
		{
			name: "published schema change",
			change: func(t *testing.T, fixture fixtureVault, _ *Server, _ string, _ uint64) {
				schemaPath := filepath.Join(fixture.root, ".rhizome/ontology/schema.graphql")
				schema, err := os.ReadFile(schemaPath)
				require.NoError(t, err)
				updated := strings.NewReplacer("type Plan\n", "type UpdatedPlan\n", "[Plan!]", "[UpdatedPlan!]", "plan: Plan!", "plan: UpdatedPlan!").Replace(string(schema))
				require.NotEqual(t, string(schema), updated)
				require.NoError(t, os.WriteFile(schemaPath, []byte(updated), 0o644))
				_, err = ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
				require.NoError(t, err)
			},
			check: func(t *testing.T, _ *ontologyquery.ReadOverlay, plan, _ *ontology.NodeProjection, planRecord noderead.NodeRecord, scope *noderead.Scope) {
				require.Equal(t, "UpdatedPlan", plan.ResolvedType)
				require.Equal(t, "UpdatedPlan", planRecord.TypeName)
				instances, err := scope.TypeInstances(context.Background(), noderead.TypeInstancesRequest{TypeName: "UpdatedPlan", Limit: 10})
				require.NoError(t, err)
				paths := []string{}
				for _, item := range instances.Items {
					paths = append(paths, item.NotePath)
				}
				require.Equal(t, []string{planPath}, paths, "staged note must list under the newly published type")
				require.Equal(t, []string{"Staged plan summary"}, plan.Fields["summary"].Values)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareOntologyFixtureVault(t)
			srv := newFixtureServer(t, fixture, nil)
			ctx := context.Background()
			created, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{
				{ID: "field:plan-summary", Kind: "setField", Path: planPath, Field: "summary", Value: "Staged plan summary"},
				{ID: "field:tasks-summary", Kind: "setField", Path: tasksPath, Field: "summary", Value: "Staged tasks summary"},
			}})
			require.NoError(t, err)
			read := func() (*ontologyquery.ReadOverlay, *ontology.NodeProjection, *ontology.NodeProjection, noderead.NodeRecord, *noderead.Scope) {
				t.Helper()
				overlay, err := srv.readOverlayForEditSession(ctx, &EditSessionReadRequest{SessionID: created.SessionID})
				require.NoError(t, err)
				require.NotNil(t, overlay)
				defs, err := srv.ontologyDefinitions()
				require.NoError(t, err)
				scope := srv.nodeReadScopeWithOverlay(ctx, defs, overlay)
				plan, err := scope.Projection(ctx, ontology.NodeRef{NotePath: planPath, Kind: ontology.NodeKindNote})
				require.NoError(t, err)
				tasks, err := scope.Projection(ctx, ontology.NodeRef{NotePath: tasksPath, Kind: ontology.NodeKindNote})
				require.NoError(t, err)
				records, err := scope.Hydrate(ctx, []ontology.NodeRef{{NotePath: planPath, Kind: ontology.NodeKindNote}}, noderead.HydrateOptions{Profile: noderead.HydrateSummary})
				require.NoError(t, err)
				require.Len(t, records, 1)
				return overlay, plan, tasks, records[0], scope
			}

			for range 2 {
				overlay, plan, tasks, planRecord, _ := read()
				require.Equal(t, string(OntologyEditSessionStatusDirty), overlay.Status)
				require.False(t, overlay.Conflicted)
				require.False(t, overlay.Rebased)
				require.Equal(t, "Plan", plan.ResolvedType)
				require.Equal(t, "Plan", planRecord.TypeName)
				require.Equal(t, []string{"Staged plan summary"}, plan.Fields["summary"].Values)
				require.Equal(t, []string{"Staged tasks summary"}, tasks.Fields["summary"].Values)
			}

			test.change(t, fixture, srv, created.SessionID, created.Revision)
			overlay, plan, tasks, planRecord, scope := read()
			test.check(t, overlay, plan, tasks, planRecord, scope)
			require.Equal(t, []string{"Staged tasks summary"}, tasks.Fields["summary"].Values, "unchanged staged note must survive the refresh")
		})
	}
}

func TestMarkdownReadOverlayFromCommitPlanExcludesFutureProjectableFormat(t *testing.T) {
	root := t.TempDir()
	const rel = "notes/Decision.future"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte("future source"), 0o644))

	descriptor := futureFileViewDescriptor()
	projector := &fileViewProjector{descriptor: descriptor}
	registry, err := noteformat.NewRegistry(markdown.New(), projector)
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, projector)
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	vaultDef := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.future"}}
	catalog, err := NewFileCatalog(vaultDef, nil, indexer)
	require.NoError(t, err)
	srv := &Server{cfg: Config{VaultPath: root, VaultDef: vaultDef, NoteMetadata: indexer}, catalog: catalog}

	overlay := srv.markdownReadOverlayFromCommitPlan(ontology.CommitPlan{Files: []ontology.FileCommitPlan{{
		NotePath:              rel,
		HasMaterialChange:     true,
		UpdatedContentPreview: "---\ntitle: Markdown-looking\n---\n[[raw-only]]\n",
	}}}, OntologyEditSessionStatusDirty, false)

	require.Nil(t, overlay, "non-Markdown providers must not enter the Markdown-only overlay index")
}
