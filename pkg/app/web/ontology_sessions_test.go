package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestOntologyEditOperationTargetKeepsNarrativeIdentityStableAcrossRangeChanges(t *testing.T) {
	first := OntologyEditOp{
		Kind: "setNarrative", Path: "specs/100-demo/spec.md#story", NodeID: "story",
		RangeStart: 10, RangeEnd: 20,
	}
	second := first
	second.RangeStart = 30
	second.RangeEnd = 50

	require.Equal(t, ontologyEditOperationTarget(first), ontologyEditOperationTarget(second))

	field := first
	field.Kind = "setField"
	field.Field = "summary"
	changedField := field
	changedField.RangeStart = 30
	require.NotEqual(t, ontologyEditOperationTarget(field), ontologyEditOperationTarget(changedField))
}

func TestReplayOntologyEditOpsPreservesTypedRootMetadataListWithoutWitness(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	defs, err := srv.ontologyDefinitions()
	require.NoError(t, err)

	session, err := srv.replayOntologyEditOps(defs, []OntologyEditOp{{
		Kind:       "setRootMetadataList",
		Path:       "specs/100-demo/plan.md",
		Field:      "tags",
		FieldValue: &OntologyEditFieldValue{Kind: "list", Items: []string{"alpha", "beta"}},
	}})
	require.NoError(t, err)
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)
	require.Contains(t, plan.Files[0].UpdatedContentPreview, "tags: [alpha, beta]")

	emptySession, err := srv.replayOntologyEditOps(defs, []OntologyEditOp{{
		Kind: "setRootMetadataList", Path: "specs/100-demo/plan.md", Field: "tags",
	}})
	require.NoError(t, err)
	emptyPlan, err := emptySession.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, emptyPlan.Files, 1)
	require.Contains(t, emptyPlan.Files[0].UpdatedContentPreview, "tags: []")
}

func TestStageOntologyEditSessionRejectsRefMissingFromPreview(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	ctx := context.Background()
	created, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{
		{Kind: "setField", Path: "specs/100-demo/plan.md", Field: "summary", Value: "Updated summary"},
	}})
	require.NoError(t, err)

	_, err = srv.stageOntologyEditSessionResponse(ctx, created.SessionID, OntologyEditSessionStageRequest{Ops: []OntologyEditOp{
		{Kind: "setField", Path: "specs/100-demo/plan.md#missing", Field: "status", Value: "blocked"},
	}})
	require.ErrorContains(t, err, "section missing not found in specs/100-demo/plan.md")

	preview, err := srv.previewOntologyEditSessionResponse(ctx, created.SessionID, OntologyEditSessionPreviewRequest{})
	require.NoError(t, err)
	require.Equal(t, created.Revision, preview.Revision, "a rejected stage must not advance the session")
	require.Len(t, preview.Ops, 1)
	require.Equal(t, "summary", preview.Ops[0].Field)
	require.Equal(t, "Updated summary", preview.Ops[0].Value)
	require.NotNil(t, preview.Plan)
	require.Len(t, preview.Plan.Files, 1)
	require.Contains(t, preview.Plan.Files[0].UpdatedContentPreview, "summary: Updated summary")
	require.NotContains(t, preview.Plan.Files[0].UpdatedContentPreview, "blocked")
}

func TestStageOntologyEditSessionMapsChangedPreviewIdentityToOriginalRef(t *testing.T) {
	ctx := context.Background()
	fixture := prepareOntologyFixtureVault(t)
	schemaPath := filepath.Join(fixture.root, ".rhizome", "ontology", "schema.graphql")
	schemaData, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	schemaData = []byte(strings.Replace(string(schemaData),
		"  summary: String! @field(sourceKind: ITEM_SUMMARY)\n",
		"  summary: String! @field(sourceKind: ITEM_SUMMARY)\n  verification: String @field\n", 1))
	require.NoError(t, os.WriteFile(schemaPath, schemaData, 0o644))
	notePath := filepath.Join(fixture.root, "specs", "100-demo", "spec.md")
	noteData, err := os.ReadFile(notePath)
	require.NoError(t, err)
	noteData = []byte(strings.Replace(string(noteData),
		"- Follow [[plan]] while validating.\n",
		"- Follow [[plan]] while validating.\n  verification:: initial\n", 1))
	require.NoError(t, os.WriteFile(notePath, noteData, 0o644))
	_, err = ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), fixture.vaultDef, &obsidian.Note{}, fixture.intelStore)
	require.NoError(t, err)

	srv := newFixtureServer(t, fixture, nil)
	defs, err := srv.ontologyDefinitions()
	require.NoError(t, err)

	listed, err := srv.nodeReadScope(ctx, defs).TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: "Criterion"})
	require.NoError(t, err)
	require.Len(t, listed.Items, 2)
	var original, preceding ontology.NodeRef
	for _, item := range listed.Items {
		if item.Title == "Follow [[plan]] while validating." {
			original = item.Ref
		} else {
			preceding = item.Ref
		}
	}
	require.NotEmpty(t, original.NodeID)
	require.NotEmpty(t, original.Structural)
	require.NotEmpty(t, preceding.NodeID)
	stories, err := srv.nodeReadScope(ctx, defs).TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: "Story"})
	require.NoError(t, err)
	require.NotEmpty(t, stories.Items)
	story := stories.Items[0].Ref
	require.NotEmpty(t, story.NodeID)

	created, err := srv.createOntologyEditSessionResponse(ctx, OntologyEditSessionCreateRequest{Ops: []OntologyEditOp{
		{
			Kind:       "deleteNode",
			Path:       preceding.String(),
			NodeID:     preceding.NodeID,
			Structural: preceding.Structural,
		},
		{
			Kind:       "setField",
			Path:       story.String(),
			NodeID:     story.NodeID,
			Structural: story.Structural,
			Field:      "status",
			Value:      "READY FOR A LENGTH-CHANGING REVIEW",
		},
		{
			Kind:       "setField",
			Path:       original.String(),
			NodeID:     original.NodeID,
			Structural: original.Structural,
			Field:      "verification",
			Value:      "first revision",
		},
	}})
	require.NoError(t, err)

	session, ok := srv.ontologyEditSession(created.SessionID)
	require.True(t, ok)
	session.mu.Lock()
	plan, err := session.Editor.Preview(ctx)
	session.mu.Unlock()
	require.NoError(t, err)
	overlay := srv.markdownReadOverlayFromCommitPlan(plan, OntologyEditSessionStatusDirty, false)
	require.NotNil(t, overlay)
	previewItems, err := srv.nodeReadScopeWithOverlay(ctx, defs, overlay).TypeInstances(ctx, noderead.TypeInstancesRequest{TypeName: "Criterion"})
	require.NoError(t, err)
	require.Len(t, previewItems.Items, 1)
	var preview ontology.NodeRef
	for _, item := range previewItems.Items {
		if item.Title == "Follow [[plan]] while validating." {
			preview = item.Ref
			break
		}
	}
	require.NotEqual(t, original.NodeID, preview.NodeID)
	require.NotEqual(t, original.Structural, preview.Structural)

	second := OntologyEditOp{
		Kind:       "setField",
		Path:       preview.String(),
		NodeID:     preview.NodeID,
		Structural: preview.Structural,
		Field:      "verification",
		Value:      "second revision",
	}
	_, err = srv.canonicalizeOntologyEditOps(ctx, defs, []OntologyEditOp{second})
	require.Error(t, err, "the preview identity must not resolve against disk")

	staged, err := srv.stageOntologyEditSessionResponse(ctx, created.SessionID, OntologyEditSessionStageRequest{Ops: []OntologyEditOp{second}})
	require.NoError(t, err)
	require.Len(t, staged.Ops, 3)
	var stagedField OntologyEditOp
	for _, op := range staged.Ops {
		if op.Kind == "setField" && op.Field == "verification" {
			stagedField = op
		}
	}
	require.Equal(t, "second revision", stagedField.Value)
	require.Equal(t, original.NodeID, stagedField.NodeID)
	require.Equal(t, original.Structural, stagedField.Structural)
	previewResponse, err := srv.previewOntologyEditSessionResponse(ctx, created.SessionID, OntologyEditSessionPreviewRequest{})
	require.NoError(t, err)
	require.Contains(t, previewResponse.Plan.Files[0].UpdatedContentPreview, "verification:: second revision")

	recreated := ontology.NewEditSession(fixture.vaultDef, &obsidian.Note{}, defs.schema)
	require.NoError(t, recreated.DeleteNode(original))
	originalMarkdown := string(noteData[original.StartByte:original.EndByte])
	require.NoError(t, recreated.StageFileTransform(original.NotePath, func(content string) (string, error) {
		return strings.TrimRight(content, "\n") + "\n" + originalMarkdown, nil
	}))
	require.NoError(t, recreated.SetScalarField(original, "verification", "replacement revision"))
	_, lineage, err := recreated.PreviewWithRefLineage(ctx)
	require.NoError(t, err)
	require.Empty(t, lineage, "a deleted and recreated item must not inherit the deleted item's base ref")
}
