package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOntologyOperationAdapterPreviewDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Spec @node(paths: ["notes/*.md"], label: "Spec") {
  id: ID! @field
}
`,
		"notes/one.md": "---\ntype: Spec\nid: SPEC-0001\n---\n# One\n",
	}))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	notePath := filepath.Join(root, "notes/one.md")
	before, err := os.ReadFile(notePath)
	require.NoError(t, err)
	beforeInfo, err := os.Stat(notePath)
	require.NoError(t, err)

	runCtx := RunContext{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
	}
	result, err := NewOntologyOperationAdapter().Preview(
		context.Background(),
		runCtx,
		schema,
		OntologyPreviewRequest{
			OperationID: "op-a",
			ActionIDs:   []string{"action-a"},
			IssueKeys:   []string{"issue-a"},
			Edits: []OntologyEdit{{
				Kind:  OntologyEditSetScalar,
				Ref:   ontology.NodeRef{NotePath: "notes/one.md", Kind: ontology.NodeKindNote},
				Field: "id",
				Value: "SPEC-0002",
			}},
		},
	)
	require.NoError(t, err)
	require.Len(t, result.Operations, 1)
	assert.Contains(t, string(result.Operations[0].Content), "id: SPEC-0002")

	after, err := os.ReadFile(notePath)
	require.NoError(t, err)
	afterInfo, err := os.Stat(notePath)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.Equal(t, beforeInfo.Mode(), afterInfo.Mode())
	assert.Equal(t, beforeInfo.ModTime(), afterInfo.ModTime())
}

func TestOntologyOperationAdapterPreservesPluralMembershipInOneFinalizedWrite(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Spec @node(paths: ["notes/*.md"], label: "Spec") {
  id: ID! @field
  aliases: [String!] @field
}
`,
		"notes/one.md": "---\ntype: Spec\nid: SPEC-0001\naliases: [SPEC-0001]\n---\n# One\n",
	}))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)

	result, err := NewOntologyOperationAdapter().Preview(
		context.Background(),
		RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}},
		schema,
		OntologyPreviewRequest{
			OperationID: "op-shared",
			ActionIDs:   []string{"action-b", "action-a", "action-b"},
			IssueKeys:   []string{"issue-b", "issue-a", "issue-b"},
			Edits: []OntologyEdit{
				{Kind: OntologyEditSetScalar, Ref: ontology.NodeRef{NotePath: "notes/one.md", Kind: ontology.NodeKindNote}, Field: "id", Value: "SPEC-0002"},
				{Kind: OntologyEditSetScalarList, Ref: ontology.NodeRef{NotePath: "notes/one.md", Kind: ontology.NodeKindNote}, Field: "aliases", Values: []string{"SPEC-0001", "SPEC-0002"}},
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, result.Operations, 1)
	operation := result.Operations[0]
	assert.Equal(t, "action-a", operation.ActionID)
	assert.Equal(t, []string{"action-a", "action-b"}, operation.ActionIDs)
	assert.Equal(t, "issue-a", operation.IssueKey)
	assert.Equal(t, []string{"issue-a", "issue-b"}, operation.IssueKeys)
}

func TestOntologyOperationAdapterRejectsMissingPluralAuthority(t *testing.T) {
	tests := []struct {
		name    string
		request OntologyPreviewRequest
		wantErr string
	}{
		{name: "action ids", request: OntologyPreviewRequest{OperationID: "op-a", IssueKeys: []string{"issue-a"}}, wantErr: "action ids are required"},
		{name: "issue keys", request: OntologyPreviewRequest{OperationID: "op-a", ActionIDs: []string{"action-a"}}, wantErr: "issue keys are required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeOntologyPreviewSession{}
			adapter := &OntologyOperationAdapter{newSession: func(RunContext, *ontology.Schema, bool) ontologyPreviewSession { return fake }}
			result, err := adapter.Preview(context.Background(), RunContext{}, &ontology.Schema{}, tt.request)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, result.Operations)
			assert.False(t, fake.previewCurrentCalled)
		})
	}
}

func TestOntologyOperationAdapterSetScalarListPreservesAliasListStyleAndDoesNotWrite(t *testing.T) {
	tests := []struct {
		name        string
		aliases     string
		wantAliases string
	}{
		{name: "flow", aliases: "aliases: [OLD, KEEP]", wantAliases: "aliases: [KEEP, NEW]"},
		{name: "block", aliases: "aliases:\n  - OLD\n  - KEEP", wantAliases: "aliases:\n  - KEEP\n  - NEW"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, writeFixtureFiles(root, map[string]string{
				".rhizome/ontology/schema.graphql": `
type Spec @node(paths: ["notes/*.md"]) {
  aliases: [String!] @field
}
`,
				"notes/one.md": "---\ntype: Spec\n" + tt.aliases + "\n---\n# One\n",
			}))
			schema, err := ontology.LoadSchema(root)
			require.NoError(t, err)
			notePath := filepath.Join(root, "notes/one.md")
			before, err := os.ReadFile(notePath)
			require.NoError(t, err)

			result, err := NewOntologyOperationAdapter().Preview(
				context.Background(),
				RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}},
				schema,
				OntologyPreviewRequest{
					OperationID: "op-list", ActionIDs: []string{"action-list"}, IssueKeys: []string{"issue-list"},
					Edits: []OntologyEdit{{
						Kind:  OntologyEditSetScalarList,
						Ref:   ontology.NodeRef{NotePath: "notes/one.md", Kind: ontology.NodeKindNote},
						Field: "aliases", Values: []string{"KEEP", "NEW"},
					}},
				},
			)
			require.NoError(t, err)
			require.Len(t, result.Operations, 1)
			assert.Contains(t, string(result.Operations[0].Content), tt.wantAliases)
			after, err := os.ReadFile(notePath)
			require.NoError(t, err)
			assert.Equal(t, before, after)
		})
	}
}

func TestOntologyOperationAdapterSetsUndeclaredRawFrontmatterListWithoutWriting(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Spec @node(paths: ["notes/*.md"]) {
  id: String @field
}
`,
		"notes/one.md": "---\ntype: Spec\nid: SPEC-0001\nlegacy_ids: [OLD, KEEP]\n---\n# One\n",
	}))
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	notePath := filepath.Join(root, "notes/one.md")
	before, err := os.ReadFile(notePath)
	require.NoError(t, err)

	result, err := NewOntologyOperationAdapter().Preview(
		context.Background(),
		RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}},
		schema,
		OntologyPreviewRequest{
			OperationID: "op-raw-list", ActionIDs: []string{"action-raw-list"}, IssueKeys: []string{"issue-raw-list"},
			Edits: []OntologyEdit{{
				Kind:  OntologyEditSetRawFrontmatterList,
				Ref:   ontology.NodeRef{NotePath: "notes/one.md", Kind: ontology.NodeKindNote},
				Field: "legacy_ids", Values: []string{"KEEP", "NEW"},
			}},
		},
	)
	require.NoError(t, err)
	require.Len(t, result.Operations, 1)
	assert.Contains(t, string(result.Operations[0].Content), "legacy_ids: [KEEP, NEW]")
	after, err := os.ReadFile(notePath)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestOntologyOperationAdapterConflictReturnsNoWrites(t *testing.T) {
	fake := &fakeOntologyPreviewSession{
		conflicts: []ontology.ConflictReport{
			{Kind: ontology.ConflictKindMissingNode, NotePath: "notes/one.md", NodeRef: "notes/one.md#^task-1", Message: "missing"},
			{Kind: ontology.ConflictKindMissingField, NotePath: "notes/one.md", NodeRef: "notes/one.md", Field: "id", Message: "field"},
			{Kind: ontology.ConflictKindCollectionDrift, NotePath: "notes/two.md", NodeRef: "notes/two.md#^tasks", Field: "tasks", Message: "drift"},
			{Kind: ontology.ConflictKindUnsupportedTarget, NotePath: "notes/three.md", Field: "status", Message: "unsupported"},
			{
				Kind: ontology.ConflictKindUnsupportedTarget, NotePath: "notes/one.md",
				Field: "legacy_ids", Message: "raw frontmatter list requires a note root",
			},
		},
	}
	adapter := &OntologyOperationAdapter{
		newSession: func(RunContext, *ontology.Schema, bool) ontologyPreviewSession { return fake },
	}
	result, err := adapter.Preview(
		context.Background(),
		RunContext{},
		&ontology.Schema{},
		OntologyPreviewRequest{
			OperationID: "op-a",
			ActionIDs:   []string{"action-a"},
			IssueKeys:   []string{"issue-a"},
			Edits: []OntologyEdit{
				{
					Kind:  OntologyEditSetScalar,
					Ref:   ontology.NodeRef{NotePath: "notes/one.md", Kind: ontology.NodeKindNote},
					Field: "id",
					Value: "SPEC-0002",
				},
				{
					Kind:  OntologyEditSetRawFrontmatterList,
					Ref:   ontology.NodeRef{NotePath: "notes/one.md", Kind: ontology.NodeKindNote},
					Field: "legacy_ids", Values: []string{"NEW"},
				},
			},
		},
	)
	require.NoError(t, err)
	assert.True(t, fake.previewCurrentCalled)
	assert.Empty(t, result.Operations)
	assert.Equal(t, []OntologyPreviewConflict{
		{Kind: ontology.ConflictKindMissingNode, NotePath: "notes/one.md", NodeRef: "notes/one.md#^task-1", Message: "missing"},
		{Kind: ontology.ConflictKindMissingField, NotePath: "notes/one.md", NodeRef: "notes/one.md", Field: "id", Message: "field"},
		{Kind: ontology.ConflictKindCollectionDrift, NotePath: "notes/two.md", NodeRef: "notes/two.md#^tasks", Field: "tasks", Message: "drift"},
		{Kind: ontology.ConflictKindUnsupportedTarget, NotePath: "notes/three.md", Field: "status", Message: "unsupported"},
		{
			Kind: ontology.ConflictKindUnsupportedTarget, NotePath: "notes/one.md",
			Field: "legacy_ids", Message: "raw frontmatter list requires a note root",
		},
	}, result.Conflicts)
}

func TestOntologyOperationAdapterRejectsDriftBetweenPreviewAndSourceRead(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"notes/one.md": "# Current\n",
	}))
	fake := &fakeOntologyPreviewSession{
		plan: ontology.CommitPlan{Files: []ontology.FileCommitPlan{{
			NotePath:              "notes/one.md",
			CurrentFingerprint:    "stale-fingerprint",
			HasMaterialChange:     true,
			UpdatedContentPreview: "# Updated\n",
		}}},
	}
	adapter := &OntologyOperationAdapter{
		newSession: func(RunContext, *ontology.Schema, bool) ontologyPreviewSession { return fake },
	}
	_, err := adapter.Preview(
		context.Background(),
		RunContext{
			VaultDef:   obsidian.VaultDefinition{Path: root},
			VaultPath:  root,
			NoteReader: &obsidian.Note{},
		},
		&ontology.Schema{},
		OntologyPreviewRequest{
			OperationID: "op-a",
			ActionIDs:   []string{"action-a"},
			IssueKeys:   []string{"issue-a"},
			Edits: []OntologyEdit{{
				Kind:  OntologyEditSetScalar,
				Ref:   ontology.NodeRef{NotePath: "notes/one.md", Kind: ontology.NodeKindNote},
				Field: "id",
				Value: "SPEC-0002",
			}},
		},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "became stale")
}

type fakeOntologyPreviewSession struct {
	conflicts            []ontology.ConflictReport
	plan                 ontology.CommitPlan
	previewCurrentCalled bool
}

func (f *fakeOntologyPreviewSession) RestoreBaseFingerprints(context.Context, map[string]string) error {
	return nil
}

func (f *fakeOntologyPreviewSession) LoadNode(_ context.Context, ref ontology.NodeRef) (*ontology.NodeProjection, error) {
	return &ontology.NodeProjection{Ref: ref}, nil
}

func (f *fakeOntologyPreviewSession) SetScalarField(ontology.NodeRef, string, string) error {
	return nil
}

func (f *fakeOntologyPreviewSession) SetScalarListField(ontology.NodeRef, string, []string) error {
	return nil
}

func (f *fakeOntologyPreviewSession) SetRawFrontmatterList(ontology.NodeRef, string, []string) error {
	return nil
}

func (f *fakeOntologyPreviewSession) SetInlineField(ontology.NodeRef, string, string) error {
	return nil
}

func (f *fakeOntologyPreviewSession) SetLinkField(ontology.NodeRef, string, []string) error {
	return nil
}

func (f *fakeOntologyPreviewSession) AddEmbeddedNode(ontology.NodeRef, string, string, string, string) error {
	return nil
}

func (f *fakeOntologyPreviewSession) DeleteNode(ontology.NodeRef) error { return nil }

func (f *fakeOntologyPreviewSession) ReorderCollection(ontology.NodeRef, string, []string) error {
	return nil
}

func (f *fakeOntologyPreviewSession) SetNarrative(ontology.NodeRef, int, int, string, string) error {
	return nil
}

func (f *fakeOntologyPreviewSession) EnsureBlockID(ontology.NodeRef, string) error { return nil }
func (f *fakeOntologyPreviewSession) SetBlockID(ontology.NodeRef, string) error    { return nil }
func (f *fakeOntologyPreviewSession) RemoveBlockID(ontology.NodeRef) error         { return nil }
func (f *fakeOntologyPreviewSession) AddSectionField(ontology.NodeRef, string) error {
	return nil
}

func (f *fakeOntologyPreviewSession) PreviewCurrent(context.Context) (ontology.CommitPlan, []ontology.ConflictReport, error) {
	f.previewCurrentCalled = true
	return f.plan, f.conflicts, nil
}
