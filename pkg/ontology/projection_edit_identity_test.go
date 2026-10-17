package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditSessionSelectorRecoveryRequiresIdentifierRepairAuthority(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type EffortNote @node(paths: ["efforts/*.md"]) {
  id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
`)
	writeOntologyNote(t, root, "efforts/2026-08-06-12-20-example.md", `---
id: EFF-0001
---

# Example
`)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	ref := NodeRef{NotePath: "efforts/2026-08-06-12-20-example.md", Kind: NodeKindNote, TypeName: "EffortNote"}
	vaultDef := obsidian.VaultDefinition{Path: root}

	ordinary, err := NewEditSession(vaultDef, &obsidian.Note{}, schema).LoadNode(context.Background(), ref)
	require.NoError(t, err)
	require.Nil(t, ordinary.Type, "ordinary edits must retain identifier-gated resolution")

	recovered, err := NewIdentifierRepairEditSession(vaultDef, &obsidian.Note{}, schema).LoadNode(context.Background(), ref)
	require.NoError(t, err)
	require.NotNil(t, recovered.Type)
	require.Equal(t, "EffortNote", recovered.Type.Name)
}

func TestPreviewRefLineageRejectsDeletedDuplicateFromPositionalReplay(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Criterion implements Section @node(locator: EMBEDDED) {
  summary: String! @field(sourceKind: ITEM_SUMMARY)
  verification: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  criteria: [Criterion!] @contains(shape: LIST_ITEM, marker: "#criterion")
}
`)
	writeOntologyNote(t, root, "specs/example.md", `---
type: Spec
---

# Example

- Identical criterion #criterion
- Identical criterion #criterion
`)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := LoadDocumentSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "specs/example.md")
	require.NoError(t, err)
	nodes, err := ProjectDocumentNodesFromSnapshot(snapshot, schema)
	require.NoError(t, err)
	criteria := make([]NodeRef, 0, 2)
	for _, node := range nodes {
		if node.Ref.TypeName == "Criterion" {
			criteria = append(criteria, node.Ref)
		}
	}
	require.Len(t, criteria, 2)
	require.Equal(t, criteria[0].Structural, criteria[1].Structural)
	require.NotEqual(t, criteria[0].NodeID, criteria[1].NodeID)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	// Direct or legacy EditSession callers may use a positional-only delete. A current
	// canonical ref remains ambiguous here and fails before the delete applies.
	positionalDelete := criteria[0]
	positionalDelete.Structural = ""
	require.NoError(t, session.DeleteNode(positionalDelete))
	require.NoError(t, session.SetScalarField(criteria[0], "verification", "changed"))
	_, lineage, err := session.PreviewWithRefLineage(context.Background())
	require.NoError(t, err)
	require.Empty(t, lineage, "the surviving duplicate must not inherit the deleted item's base ref")
	_, currentLineage, conflicts, err := session.PreviewCurrentWithRefLineage(context.Background())
	require.NoError(t, err)
	require.Empty(t, conflicts)
	require.Empty(t, currentLineage, "commit replay must not retarget the deleted duplicate")
}

func TestValidateResolvedProjectionRefAddressesNoteRootsByPath(t *testing.T) {
	err := validateResolvedProjectionRef(
		NodeRef{NotePath: "meetings/demo.md", Kind: NodeKindNote, Structural: "before-earlier-edit"},
		NodeRef{NotePath: "meetings/demo.md", Kind: NodeKindNote, Structural: "after-earlier-edit"},
	)

	require.NoError(t, err)
}

func TestValidateResolvedProjectionRefRejectsStaleItemWithoutBlockIDReplay(t *testing.T) {
	err := validateResolvedProjectionRef(
		NodeRef{
			NotePath:   "meetings/demo.md",
			Fragment:   "item-12",
			Kind:       NodeKindEmbedded,
			Structural: "before",
		},
		NodeRef{
			NotePath:   "meetings/demo.md",
			Fragment:   "item-12",
			Kind:       NodeKindEmbedded,
			Structural: "after",
			StartByte:  12,
		},
	)

	require.ErrorIs(t, err, errMissingNode)
}

func TestValidateResolvedProjectionRefAllowsInSessionBlockIDReplay(t *testing.T) {
	err := validateResolvedProjectionRef(
		NodeRef{
			NotePath:   "meetings/demo.md",
			Fragment:   "item-12",
			Kind:       NodeKindEmbedded,
			Structural: "before",
		},
		NodeRef{
			NotePath:   "meetings/demo.md",
			Fragment:   "^ai-1",
			NodeID:     "ai-1",
			Kind:       NodeKindEmbedded,
			TypeName:   "ActionItem",
			Structural: "after",
			StartByte:  12,
		},
	)

	require.NoError(t, err)
}

func TestEditSession_NoMaterialRebaseDoesNotOverwriteCurrentDisk(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Kind:     NodeKindNote,
	}, "summary", "Test"))

	_, err = session.Preview(context.Background())
	require.NoError(t, err)

	path := filepath.Join(root, "specs/001-test/spec.md")
	current, err := os.ReadFile(path)
	require.NoError(t, err)
	externallyEdited := strings.Replace(string(current), "summary: Test", "summary: External", 1)
	require.NoError(t, os.WriteFile(path, []byte(externallyEdited), 0o644))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.True(t, result.Rebased)
	require.False(t, planHasMaterialChange(result.Plan))

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(updated), "summary: External")
	require.NotContains(t, string(updated), "summary: Test")
}

func TestEditSession_RebindsUnanchoredItemDuringSequentialFieldReplay(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) {
  title: String!
  done: Boolean! @field(sourceKind: CHECKBOX)
  status: String @field
  assignedTo: Person @link
}

type Conversation @node(paths: ["meetings/*.md"]) {
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}

type Person @node(paths: ["people/*.md"]) {
  title: String!
}
`)
	writeOntologyNote(t, root, "meetings/sync.md", `---
type: Conversation
---

## Action Items

- [ ] Update docs #action-item
  status:: open
  assigned-to:: [[people/alice]]
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "meetings/sync.md")
	require.NoError(t, err)
	itemRef := note.Fields["actionItems"].SectionNodes[0]
	itemRef.TypeName = ""

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(itemRef, "status", "blocked"))
	require.NoError(t, session.SetScalarField(itemRef, "done", "true"))
	require.NoError(t, session.SetLinkField(itemRef, "assignedTo", []string{"[[people/bob]]"}))

	plan, lineage, err := session.PreviewWithRefLineage(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)
	require.Contains(t, plan.Files[0].UpdatedContentPreview, "- [x] Update docs #action-item")
	require.Contains(t, plan.Files[0].UpdatedContentPreview, "status:: blocked")
	require.Contains(t, plan.Files[0].UpdatedContentPreview, "assigned-to:: [[people/bob]]")
	require.Len(t, lineage, 1)
	require.Equal(t, itemRef, lineage[0].Original)
	require.NotEqual(t, itemRef.Structural, lineage[0].Preview.Structural)

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.Equal(t, CommitOutcomeCommitted, result.Outcome)
	updated, err := os.ReadFile(filepath.Join(root, "meetings/sync.md"))
	require.NoError(t, err)
	require.Contains(t, string(updated), "- [x] Update docs #action-item")
	require.Contains(t, string(updated), "status:: blocked")
	require.Contains(t, string(updated), "assigned-to:: [[people/bob]]")
}

func TestEditSession_ItemRefSurvivesBlockIDInsertionReplay(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}

type Conversation @node(paths: ["meetings/*.md"]) {
  title: String!
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}
`)
	writeOntologyNote(t, root, "meetings/sync.md", `---
type: Conversation
title: Sync
---

## Action Items

- [ ] Update docs #action-item
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "meetings/sync.md")
	require.NoError(t, err)
	itemRef := note.Fields["actionItems"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.EnsureBlockID(itemRef, "ai-1"))
	require.NoError(t, session.SetScalarField(itemRef, "done", "true"))
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)
	require.Contains(t, plan.Files[0].UpdatedContentPreview, "- [x] Update docs #action-item ^ai-1")
}

func TestEditSession_PathDerivedSectionRefSurvivesUnrelatedInsertions(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Story A
status:: TODO
^story-a
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	containerRef := note.Fields["userStoriesSection"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.AddEmbeddedNode(containerRef, "stories", "Story B", "status:: TODO", "story-b"))
	_, err = session.Preview(context.Background())
	require.NoError(t, err)

	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

Intro before spec.

# Spec

## User Stories

### Story A
status:: TODO
^story-a
`)

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)

	data, err := os.ReadFile(filepath.Join(root, "specs/001-test/spec.md"))
	require.NoError(t, err)
	require.Contains(t, string(data), "^story-b")
}
