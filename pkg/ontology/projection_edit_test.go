package ontology

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
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
	schema, err := LoadSchema(root)
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
	schema, err := LoadSchema(root)
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

func TestProjectNode_EmbeddedNodeByFragment(t *testing.T) {
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

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	projection, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "^story-a",
		Kind:     NodeKindEmbedded,
	})
	require.NoError(t, err)
	require.Equal(t, "UserStory", projection.ResolvedType)
	require.Equal(t, NodeKindEmbedded, projection.Ref.Kind)
	require.Equal(t, []string{"TODO"}, projection.Fields["status"].Values)
}

func TestProjectNode_EmbeddedNodeLinkFieldsUseInlineValues(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  storiesSection: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  status: String @field
  spec: Spec @link
}
`)
	writeOntologyNote(t, root, "specs/search-rewrite.md", `---
type: Spec
summary: Search rewrite
---

# Search Rewrite

## Stories

### Stable typed retrieval
status:: ready
spec:: [[search-rewrite]]
^story-a
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	projection, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, NodeRef{
		NotePath: "specs/search-rewrite.md",
		Fragment: "^story-a",
		Kind:     NodeKindEmbedded,
	})
	require.NoError(t, err)
	require.Equal(t, "UserStory", projection.ResolvedType)
	require.Equal(t, []string{"ready"}, projection.Fields["status"].Values)
	require.Equal(t, []string{"[[search-rewrite]]"}, projection.Fields["spec"].Values)
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

	schema, err := LoadSchema(root)
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

func TestEditSession_PreviewCurrent_SeparatesBaseCurrentAndUpdatedFingerprints(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Kind:     NodeKindNote,
	}, "summary", "Better test"))
	_, err = session.Preview(context.Background())
	require.NoError(t, err)

	path := filepath.Join(root, "specs/001-test/spec.md")
	baseContent, err := os.ReadFile(path)
	require.NoError(t, err)
	currentContent := strings.Replace(string(baseContent), "# Spec", "# External Spec", 1)
	require.NoError(t, os.WriteFile(path, []byte(currentContent), 0o644))

	plan, conflicts, err := session.PreviewCurrent(context.Background())
	require.NoError(t, err)
	require.Empty(t, conflicts)
	require.Len(t, plan.Files, 1)
	file := plan.Files[0]

	require.Equal(t, hashText(string(baseContent)), file.BaseFingerprint)
	require.Equal(t, hashText(currentContent), file.CurrentFingerprint)
	require.Equal(t, hashText(file.UpdatedContentPreview), file.UpdatedFingerprint)
	require.NotEqual(t, file.BaseFingerprint, file.CurrentFingerprint)
	require.NotEqual(t, file.CurrentFingerprint, file.UpdatedFingerprint)
	require.True(t, file.Rebased)
	require.True(t, file.HasMaterialChange)
	require.Contains(t, file.UpdatedContentPreview, "summary: Better test")
	require.Contains(t, file.UpdatedContentPreview, "# External Spec")
}

func TestEditSession_SetInlineField_AutoAddsBlockID(t *testing.T) {
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
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	containerRef := note.Fields["userStoriesSection"].SectionNodes[0]
	container, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, containerRef)
	require.NoError(t, err)
	storyRef := container.Fields["stories"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	err = session.SetInlineField(storyRef, "status", "DONE")
	require.NoError(t, err)

	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)
	updated := plan.Files[0].UpdatedContentPreview
	require.Contains(t, updated, "status:: DONE")
	require.Contains(t, updated, "^userstory-story-a-")
}

func TestEditSession_SetCheckboxField_TogglesItemTokenOnly(t *testing.T) {
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
- [ ] Leave this alone #action-item
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "meetings/sync.md")
	require.NoError(t, err)
	itemRef := note.Fields["actionItems"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(itemRef, "done", "true"))
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)

	updated := plan.Files[0].UpdatedContentPreview
	require.Contains(t, updated, "- [x] Update docs #action-item")
	require.Contains(t, updated, "- [ ] Leave this alone #action-item")
	require.NotContains(t, updated, "^actionitem-")
}

func TestEditSession_BatchesSameNoteFieldEdits(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) {
  title: String!
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: String @field
  assignee: String @field
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

- [ ] Update docs #action-item assignee:: Alice
- [ ] Buy snacks #action-item
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "meetings/sync.md")
	require.NoError(t, err)
	firstRef := note.Fields["actionItems"].SectionNodes[0]
	secondRef := note.Fields["actionItems"].SectionNodes[1]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(firstRef, "due", "2026-05-12"))
	require.NoError(t, session.SetScalarField(firstRef, "done", "true"))
	require.NoError(t, session.SetScalarField(firstRef, "title", "Update docs today"))
	require.NoError(t, session.SetScalarField(secondRef, "assignee", "Bob"))

	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)
	updated := plan.Files[0].UpdatedContentPreview
	require.Contains(t, updated, "- [x] Update docs today #action-item assignee:: Alice\ndue:: 2026-05-12")
	require.Contains(t, updated, "- [ ] Buy snacks #action-item\nassignee:: Bob")
}

func TestEditSession_BatchValidatesTypedValuesAndHonorsUnset(t *testing.T) {
	for _, tc := range []struct {
		name, due string
		labels    []string
		wantField string
	}{
		{"invalid date", "not-a-date", []string{"OPEN"}, "due"},
		{"invalid enum list", "2026-09-12", []string{"UNKNOWN"}, "labels"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologyTestConfig(t, root)
			writeOntologySchema(t, root, `
enum Status { OPEN CLOSED }

type ActionItem implements Section @node(locator: EMBEDDED) {
  title: String!
  status: Status @field
  due: Date @field
  labels: [Status!] @field
}

type ActionItemsSection implements Section {
  actionItems: [ActionItem!] @contains(level: H3)
}

type Conversation @node(paths: ["meetings/*.md"]) {
  actionItemsSection: ActionItemsSection @contains(level: H2, heading: "Action Items")
}
`)
			content := `---
type: Conversation
---
# Conversation

## Action Items

### Update docs
status:: OPEN
due:: 2026-09-11
labels:: OPEN, CLOSED
^action-item-1
`
			writeOntologyNote(t, root, "meetings/sync.md", content)
			schema, err := LoadSchema(root)
			require.NoError(t, err)
			note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "meetings/sync.md")
			require.NoError(t, err)
			require.Len(t, note.Fields["actionItemsSection"].SectionNodes, 1)
			section, err := ProjectNodeFromSnapshot(
				note.Snapshot,
				schema,
				note.Fields["actionItemsSection"].SectionNodes[0],
			)
			require.NoError(t, err)
			require.Len(t, section.Fields["actionItems"].SectionNodes, 1)
			ref := section.Fields["actionItems"].SectionNodes[0]
			session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
			require.NoError(t, session.SetScalarField(ref, "status", "CLOSED"))
			require.NoError(t, session.SetScalarField(ref, "due", tc.due))
			require.NoError(t, session.SetScalarListField(ref, "labels", tc.labels))

			plan, err := session.Preview(context.Background())
			require.Error(t, err)
			require.ErrorContains(t, err, "field "+tc.wantField)
			require.Len(t, plan.Files, 1)
			require.Equal(t, content, plan.Files[0].UpdatedContentPreview)
			stored, readErr := os.ReadFile(filepath.Join(root, "meetings", "sync.md"))
			require.NoError(t, readErr)
			require.Equal(t, content, string(stored))
		})
	}

	t.Run("falls back to sequential edits when one operation unsets a field", func(t *testing.T) {
		root := t.TempDir()
		writeOntologyTestConfig(t, root)
		writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/*.md"]) {
  count: Int
  summary: String
}
`)
		content := `---
type: Spec
count: 1
summary: Remove me
---
# Spec
`
		writeOntologyNote(t, root, "notes/spec.md", content)
		schema, err := LoadSchema(root)
		require.NoError(t, err)
		ref := NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote}
		session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
		require.NoError(t, session.SetScalarField(ref, "count", "2"))
		require.NoError(t, session.UnsetField(ref, "summary"))

		plan, err := session.Preview(context.Background())
		require.NoError(t, err)
		require.Len(t, plan.Files, 1)
		updated := plan.Files[0].UpdatedContentPreview
		require.Contains(t, updated, "count: 2")
		require.NotContains(t, updated, "summary:")
	})
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

	schema, err := LoadSchema(root)
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

func TestEditSession_BarrierSplitsFieldBatchesAndPreservesOrder(t *testing.T) {
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

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "meetings/sync.md")
	require.NoError(t, err)
	itemRef := note.Fields["actionItems"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(itemRef, "done", "true"))
	require.NoError(t, session.EnsureBlockID(itemRef, "ai-1"))
	require.NoError(t, session.SetScalarField(itemRef, "done", "false"))

	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)
	require.Contains(t, plan.Files[0].UpdatedContentPreview, "- [ ] Update docs #action-item ^ai-1")
	require.NotContains(t, plan.Files[0].UpdatedContentPreview, "- [x] Update docs")
}

func TestEditSession_SetCheckboxField_TogglesGlobalSourceItemByViewRef(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`)
	writeOntologyNote(t, root, "notes/tasks.md", `# Tasks

- [ ] Buy flour #action-item
- [ ] Leave this alone #action-item
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(root, "notes", "tasks.md"))
	require.NoError(t, err)
	snapshot, err := BuildDocumentSnapshot("notes/tasks.md", string(content), time.Time{})
	require.NoError(t, err)
	refs, err := GlobalSourceNodeRefsFromSnapshot(snapshot, schema)
	require.NoError(t, err)
	require.Len(t, refs, 2)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(refs[0], "done", "true"))
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)

	updated := plan.Files[0].UpdatedContentPreview
	require.Contains(t, updated, "- [x] Buy flour #action-item")
	require.Contains(t, updated, "- [ ] Leave this alone #action-item")

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)
	written, err := os.ReadFile(filepath.Join(root, "notes", "tasks.md"))
	require.NoError(t, err)
	require.Contains(t, string(written), "- [x] Buy flour #action-item")
	require.Contains(t, string(written), "- [ ] Leave this alone #action-item")
}

func TestEditSession_SetTitleField_RenamesCheckboxItemTextOnly(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
  due: Date @field
}

type Person @node(paths: ["people/*.md"]) {
  displayName: String @field(source: "display-name")
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

- [ ] Update docs #action-item assignee:: [[Alice]] due:: 2026-05-10 ^todo-1
- [ ] Leave this alone #action-item
`)
	writeOntologyNote(t, root, "people/alice.md", `---
display-name: Alice
---

# Alice
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "meetings/sync.md")
	require.NoError(t, err)
	itemRef := note.Fields["actionItems"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(itemRef, "title", "Refresh onboarding docs"))
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)

	updated := plan.Files[0].UpdatedContentPreview
	require.Contains(t, updated, "- [ ] Refresh onboarding docs #action-item assignee:: [[Alice]] due:: 2026-05-10 ^todo-1")
	require.Contains(t, updated, "- [ ] Leave this alone #action-item")
	require.NotContains(t, updated, "Update docs #action-item")
}

func TestEditSession_SetTitleField_RenamesSectionHeading(t *testing.T) {
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

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	containerRef := note.Fields["userStoriesSection"].SectionNodes[0]
	container, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, containerRef)
	require.NoError(t, err)
	storyRef := container.Fields["stories"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(storyRef, "title", "Story B"))
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)

	updated := plan.Files[0].UpdatedContentPreview
	require.Contains(t, updated, "### Story B\nstatus:: TODO")
	require.NotContains(t, updated, "### Story A")
}

func TestEditSession_SetTitleField_RenamesNoteFrontmatterTitle(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
}
`)
	writeOntologyNote(t, root, "specs/spec.md", `---
type: Spec
title: Old Title
summary: Test
---

# Old Title
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "specs/spec.md", Kind: NodeKindNote}, "title", "New Title"))
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)

	updated := plan.Files[0].UpdatedContentPreview
	require.Contains(t, updated, "title: New Title")
	require.Contains(t, updated, "# Old Title")
}

func TestEditSession_SetInlineFieldPreservesPackedSiblings(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  summary: String @field
  status: String @field
}

type Stories implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: Stories @contains(level: H2, heading: "Stories")
}
`)
	writeOntologyNote(t, root, "specs/spec.md", `---
type: Spec
summary: Test
---

# Spec

## Stories

### Story A
id:: ^SPEC-001-US1 summary:: Ship packed fields safely. status:: ready
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := LoadDocumentSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "specs/spec.md")
	require.NoError(t, err)
	storyRef := mustFindEmbeddedNodeByTitle(t, snapshot, schema, "Story A")

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(storyRef, "status", "done"))
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)

	updated := plan.Files[0].UpdatedContentPreview
	require.Contains(t, updated, "id:: ^SPEC-001-US1 summary:: Ship packed fields safely. status:: done")
}

func TestEditSession_SetTitleField_RenamesSingleH1WhenNoFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["specs/*.md"]) {
  summary: String
}
`)
	writeOntologyNote(t, root, "specs/spec.md", `# Old Title

summary:: Test
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "specs/spec.md", Kind: NodeKindNote}, "title", "New Title"))
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)

	updated := plan.Files[0].UpdatedContentPreview
	require.Contains(t, updated, "# New Title")
	require.NotContains(t, updated, "# Old Title")
}

func TestEditSession_SetItemInlineFields_PreservesItemLine(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
enum ActionStatus {
  open
  blocked
}

type Person @node(paths: ["people/*.md"]) {
  name: String @field(source: "name")
}

type ActionItem implements Section @node(locator: EMBEDDED) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignedTo: Person @link
  status: ActionStatus @field
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

- [ ] Follow up assigned-to:: people/alice status:: open #action-item
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "meetings/sync.md")
	require.NoError(t, err)
	itemRef := note.Fields["actionItems"].SectionNodes[0]
	item, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, itemRef)
	require.NoError(t, err)
	require.Equal(t, []string{"people/alice"}, item.Fields["assignedTo"].Values)
	require.Equal(t, []string{"open"}, item.Fields["status"].Values)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetLinkField(itemRef, "assignedTo", []string{"people/bob.md"}))
	firstPlan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Contains(t, firstPlan.Files[0].UpdatedContentPreview, "assigned-to:: people/bob.md")
	require.NoError(t, session.SetScalarField(itemRef, "status", "blocked"))
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)

	updated := plan.Files[0].UpdatedContentPreview
	require.Contains(t, updated, "- [ ] Follow up assigned-to:: people/bob.md status:: blocked #action-item")
	require.NotContains(t, updated, "status:: open")
	require.NotContains(t, updated, "assigned-to:: people/alice")
}

func TestEditSession_EnsureBlockID_AddsLocatorToItemLine(t *testing.T) {
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

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "meetings/sync.md")
	require.NoError(t, err)
	itemRef := note.Fields["actionItems"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.EnsureBlockID(itemRef, "ai-1"))
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)
	require.Contains(t, plan.Files[0].UpdatedContentPreview, "- [ ] Update docs #action-item ^ai-1")
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

	schema, err := LoadSchema(root)
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

func TestEditSession_AddReorderDeleteEmbeddedNodes(t *testing.T) {
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

### Story B
status:: TODO
^story-b
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	containerRef := note.Fields["userStoriesSection"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.AddEmbeddedNode(containerRef, "stories", "Story C", "status:: TODO", "story-c"))
	require.NoError(t, session.ReorderCollection(containerRef, "stories", []string{"^story-b", "^story-a", "^story-c"}))
	require.NoError(t, session.DeleteNode(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "^story-a",
		Kind:     NodeKindEmbedded,
	}))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)

	data, err := os.ReadFile(filepath.Join(root, "specs/001-test/spec.md"))
	require.NoError(t, err)
	text := string(data)
	require.NotContains(t, text, "^story-a")
	for _, marker := range []string{"^story-b", "^story-c"} {
		require.Contains(t, text, marker)
	}
	require.Less(t, strings.Index(text, "^story-b"), strings.Index(text, "^story-c"))
}

func TestEditSession_PathDerivedRefsAddReorderDeleteEmbeddedNodes(t *testing.T) {
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

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	containerRef := NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "User Stories",
		Kind:     NodeKindSection,
	}
	require.NoError(t, session.AddEmbeddedNode(containerRef, "stories", "Story B", "status:: TODO", "story-b"))
	require.NoError(t, session.AddEmbeddedNode(containerRef, "stories", "Story C", "status:: TODO", "story-c"))
	require.NoError(t, session.ReorderCollection(containerRef, "stories", []string{"^story-c", "^story-a", "^story-b"}))
	require.NoError(t, session.DeleteNode(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "^story-a",
		Kind:     NodeKindEmbedded,
	}))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	if !result.Applied {
		t.Fatalf("conflicts: %+v", result.Conflicts)
	}

	data, err := os.ReadFile(filepath.Join(root, "specs/001-test/spec.md"))
	require.NoError(t, err)
	text := string(data)
	require.NotContains(t, text, "^story-a")
	require.Contains(t, text, "^story-b")
	require.Contains(t, text, "^story-c")
	require.True(t, strings.Index(text, "^story-c") < strings.Index(text, "^story-b"))
}

func TestEditSession_Commit_AutoRebasesSimpleExternalChange(t *testing.T) {
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

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Kind:     NodeKindNote,
	}, "summary", "Updated summary"))

	_, err = session.Preview(context.Background())
	require.NoError(t, err)

	path := filepath.Join(root, "specs/001-test/spec.md")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(original, []byte("\nExternal tail.\n")...), 0o644))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.Rebased)

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(updated), "summary: Updated summary")
	require.Contains(t, string(updated), "External tail.")
}

func TestEditSession_SetNarrative_CommitRebasesByMatchingNarrativeBlock(t *testing.T) {
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
This story starts with narrative text.

status:: TODO
^story-a
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	storyRef := NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "^story-a",
		Kind:     NodeKindEmbedded,
	}
	projection, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, storyRef)
	require.NoError(t, err)
	blocks := BuildNodeBody(projection, schema)
	require.NotEmpty(t, blocks)
	require.Equal(t, NodeBodyBlockKindNarrative, blocks[0].Kind)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetNarrative(
		storyRef,
		blocks[0].Range.Start,
		blocks[0].Range.End,
		blocks[0].Markdown,
		"Updated narrative text for Story A.",
	))
	_, err = session.Preview(context.Background())
	require.NoError(t, err)

	path := filepath.Join(root, "specs/001-test/spec.md")
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append([]byte("Intro line added before the note.\n\n"), original...), 0o644))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.True(t, result.Rebased)

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(updated), "Updated narrative text for Story A.")
	require.Contains(t, string(updated), "status:: TODO")
	require.Contains(t, string(updated), "Intro line added before the note.")
}

func TestEditSession_SetBlockID_ReplacesExistingEmbeddedLocator(t *testing.T) {
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

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	storyRef := NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "^story-a",
		Kind:     NodeKindEmbedded,
	}

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetBlockID(storyRef, "story-b"))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)

	updated, err := os.ReadFile(filepath.Join(root, "specs/001-test/spec.md"))
	require.NoError(t, err)
	require.Contains(t, string(updated), "^story-b")
	require.NotContains(t, string(updated), "^story-a")
}

func TestProjectNote_CapturesFrontmatterAndSectionBindings(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, projectionBindingSchema)
	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
summary: Spec summary
decisions:
  - notes/decisions/alpha.md
  - notes/decisions/beta.md
---

Intro paragraph.

## User Stories

### Story A
status:: TODO
^story-a

### Story B
status:: IN_PROGRESS
^story-b
`)
	writeOntologyNote(t, root, "notes/decisions/alpha.md", `---
type: Decision
summary: Alpha
---
`)
	writeOntologyNote(t, root, "notes/decisions/beta.md", `---
type: Decision
summary: Beta
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes/specs/spec.md")
	require.NoError(t, err)
	require.Equal(t, "Spec", note.ResolvedType)
	require.Equal(t, []string{"Spec summary"}, note.Fields["summary"].Values)
	require.Equal(t, []string{"notes/decisions/alpha.md", "notes/decisions/beta.md"}, note.Fields["decisions"].Values)
	require.Len(t, note.Fields["userStories"].SectionNodes, 1)
	require.True(t, note.Fields["summary"].Range.Valid(len(note.Snapshot.Content)))

	containerRef := note.Fields["userStories"].SectionNodes[0]
	container, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, containerRef)
	require.NoError(t, err)
	require.Len(t, container.Collections["stories"].Items, 2)
	require.NotEmpty(t, container.Collections["stories"].OrderFingerprint)
	require.True(t, container.Collections["stories"].Range.Valid(len(container.Snapshot.Content)))

	story, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, container.Collections["stories"].Items[0].Ref)
	require.NoError(t, err)
	require.Equal(t, []string{"TODO"}, story.Fields["status"].Values)
	require.True(t, story.Fields["status"].Range.Valid(len(story.Snapshot.Content)))
}

func TestEditSession_SetScalarAndLinkFields_CommitPreservesBody(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, projectionBindingSchema)
	writeOntologyNote(t, root, "notes/specs/spec.md", `---
type: Spec
summary: Spec summary
decisions:
  - notes/decisions/alpha.md
---

Intro paragraph.

## User Stories

### Story A
status:: TODO
^story-a

Closing paragraph.
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{
		NotePath: "notes/specs/spec.md",
		Kind:     NodeKindNote,
	}, "summary", "Updated summary"))
	require.NoError(t, session.SetLinkField(NodeRef{
		NotePath: "notes/specs/spec.md",
		Kind:     NodeKindNote,
	}, "decisions", []string{"notes/decisions/beta.md", "notes/decisions/gamma.md"}))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)

	data, err := os.ReadFile(filepath.Join(root, "notes/specs/spec.md"))
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "summary: Updated summary")
	require.Contains(t, text, "- notes/decisions/beta.md")
	require.Contains(t, text, "- notes/decisions/gamma.md")
	require.Contains(t, text, "Intro paragraph.\n\n## User Stories")
	require.Contains(t, text, "Closing paragraph.")
}

func TestEditSession_SetInlineField_PreservesEmbeddedFormatting(t *testing.T) {
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

<!-- keep-comment -->
- preserve bullet one

Paragraph after bullet.

### Story B
status:: TODO
^story-b
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetInlineField(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "^story-a",
		Kind:     NodeKindEmbedded,
	}, "status", "DONE"))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)

	data, err := os.ReadFile(filepath.Join(root, "specs/001-test/spec.md"))
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "status:: DONE\n^story-a\n\n<!-- keep-comment -->\n- preserve bullet one\n\nParagraph after bullet.")
	require.Contains(t, text, "### Story B\nstatus:: TODO\n^story-b")
}

func TestEditSession_SetInlineField_RemovesTrailingDuplicateInlineSpansSafely(t *testing.T) {
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
status:: OLD
^story-a

Paragraph after duplicates.

### Story B
status:: TODO
^story-b
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetInlineField(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "^story-a",
		Kind:     NodeKindEmbedded,
	}, "status", "DONE-LONGER"))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)

	data, err := os.ReadFile(filepath.Join(root, "specs/001-test/spec.md"))
	require.NoError(t, err)
	text := string(data)
	require.Contains(t, text, "### Story A\nstatus:: DONE-LONGER\n^story-a")
	require.NotContains(t, text, "status:: OLD")
	require.Contains(t, text, "Paragraph after duplicates.")
	require.Contains(t, text, "### Story B\nstatus:: TODO\n^story-b")
}

func TestEditSession_Preview_IncludesDiffAndFingerprints(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Kind:     NodeKindNote,
	}, "summary", "Better test"))

	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)
	file := plan.Files[0]
	require.NotEmpty(t, file.BaseFingerprint)
	require.NotEmpty(t, file.UpdatedFingerprint)
	require.NotEqual(t, file.BaseFingerprint, file.UpdatedFingerprint)
	require.True(t, file.HasMaterialChange)
	require.Contains(t, file.UpdatedContentPreview, "summary: Better test")
	require.Contains(t, file.Diff, "Better test")
}

func TestEditSession_Commit_ReportsCollectionDriftConflict(t *testing.T) {
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

### Story B
status:: TODO
^story-b
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	containerRef := note.Fields["userStoriesSection"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.ReorderCollection(containerRef, "stories", []string{"^story-b", "^story-a"}))
	_, err = session.Preview(context.Background())
	require.NoError(t, err)

	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Story A
status:: TODO
^story-a

### Story B
status:: TODO
^story-b

### Story C
status:: TODO
^story-c
`)

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.Len(t, result.Conflicts, 1)
	require.Equal(t, ConflictKindCollectionDrift, result.Conflicts[0].Kind)
	require.Equal(t, "stories", result.Conflicts[0].Field)
}

func TestEditSession_Commit_ReportsMissingNodeConflict(t *testing.T) {
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

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetInlineField(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "^story-a",
		Kind:     NodeKindEmbedded,
	}, "status", "DONE"))
	_, err = session.Preview(context.Background())
	require.NoError(t, err)

	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Story Renamed
status:: TODO
^story-renamed
`)

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.Len(t, result.Conflicts, 1)
	require.Equal(t, ConflictKindMissingNode, result.Conflicts[0].Kind)
	require.Equal(t, "specs/001-test/spec.md#^story-a", result.Conflicts[0].NodeRef)
}

func TestEditSession_Commit_UpdatesMultipleFiles(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
}
`)
	writeOntologyNote(t, root, "notes/specs/one.md", `---
type: Spec
summary: One
---
`)
	writeOntologyNote(t, root, "notes/specs/two.md", `---
type: Spec
summary: Two
---
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/specs/one.md", Kind: NodeKindNote}, "summary", "One updated"))
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/specs/two.md", Kind: NodeKindNote}, "summary", "Two updated"))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Len(t, result.Plan.Files, 2)

	one, err := os.ReadFile(filepath.Join(root, "notes/specs/one.md"))
	require.NoError(t, err)
	two, err := os.ReadFile(filepath.Join(root, "notes/specs/two.md"))
	require.NoError(t, err)
	require.Contains(t, string(one), "summary: One updated")
	require.Contains(t, string(two), "summary: Two updated")
}

func TestEditSession_PreviewOrdersFilesDeterministically(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
}
`)
	for _, name := range []string{"a", "b", "c"} {
		writeOntologyNote(t, root, "notes/specs/"+name+".md", `---
type: Spec
summary: `+strings.ToUpper(name)+`
---
`)
	}

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/specs/c.md", Kind: NodeKindNote}, "summary", "C updated"))
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/specs/a.md", Kind: NodeKindNote}, "summary", "A updated"))
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/specs/b.md", Kind: NodeKindNote}, "summary", "B updated"))

	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{
		"notes/specs/a.md",
		"notes/specs/b.md",
		"notes/specs/c.md",
	}, []string{
		plan.Files[0].NotePath,
		plan.Files[1].NotePath,
		plan.Files[2].NotePath,
	})
}

func TestEditSession_PreviewPlansFilesConcurrently(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
}
`)
	paths := make([]string, 0, maxConcurrentFilePlans+3)
	for i := range maxConcurrentFilePlans + 3 {
		path := fmt.Sprintf("notes/specs/%02d.md", i)
		paths = append(paths, path)
		writeOntologyNote(t, root, path, `---
type: Spec
summary: Test
---
`)
	}
	schema, err := LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	var active int32
	var maxActive int32
	for _, path := range paths {
		path := path
		require.NoError(t, session.StageFileTransform(path, func(content string) (string, error) {
			now := atomic.AddInt32(&active, 1)
			for {
				max := atomic.LoadInt32(&maxActive)
				if now <= max || atomic.CompareAndSwapInt32(&maxActive, max, now) {
					break
				}
			}
			time.Sleep(25 * time.Millisecond)
			atomic.AddInt32(&active, -1)
			return strings.Replace(content, "summary: Test", "summary: "+path, 1), nil
		}))
	}

	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, len(paths))
	require.Greater(t, atomic.LoadInt32(&maxActive), int32(1))
	require.LessOrEqual(t, atomic.LoadInt32(&maxActive), int32(maxConcurrentFilePlans))
}

func TestEditSession_ReorderCollection_PreservesInterstitialContent(t *testing.T) {
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

<!-- keep-with-a -->

### Story B
status:: TODO
^story-b

Paragraph between stories.

### Story C
status:: TODO
^story-c
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	containerRef := note.Fields["userStoriesSection"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.ReorderCollection(containerRef, "stories", []string{"^story-c", "^story-a", "^story-b"}))

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.True(t, result.Applied)

	data, err := os.ReadFile(filepath.Join(root, "specs/001-test/spec.md"))
	require.NoError(t, err)
	text := string(data)
	ordered := []string{"### Story C", "### Story A", "<!-- keep-with-a -->", "### Story B", "Paragraph between stories."}
	previous := -1
	for _, marker := range ordered {
		index := strings.Index(text, marker)
		require.GreaterOrEqual(t, index, 0, "missing %q", marker)
		require.Greater(t, index, previous, "out of order: %q", marker)
		previous = index
	}
}

func TestEditSession_Commit_ReportsCollectionOrderDriftConflict(t *testing.T) {
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

### Story B
status:: TODO
^story-b
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	containerRef := note.Fields["userStoriesSection"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.ReorderCollection(containerRef, "stories", []string{"^story-b", "^story-a"}))
	_, err = session.Preview(context.Background())
	require.NoError(t, err)

	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Story B
status:: TODO
^story-b

### Story A
status:: TODO
^story-a
`)

	result, err := session.Commit(context.Background())
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.Len(t, result.Conflicts, 1)
	require.Equal(t, ConflictKindCollectionDrift, result.Conflicts[0].Kind)
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

	schema, err := LoadSchema(root)
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

func TestProjectNode_RejectsAmbiguousHeadingFragment(t *testing.T) {
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

## User Stories

### Story B
status:: TODO
^story-b
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	_, err = ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "User Stories",
		Kind:     NodeKindSection,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous")
}

func TestEditSession_CommitWithOptionsCanReturnSlimPlan(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{
		NotePath: "specs/001-test/spec.md",
		Kind:     NodeKindNote,
	}, "summary", "Saved"))

	result, err := session.CommitWithOptions(context.Background(), CommitOptions{})
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Len(t, result.Plan.Files, 1)
	require.Empty(t, result.Plan.Files[0].Diff)
	require.Empty(t, result.Plan.Files[0].UpdatedContentPreview)
	require.NotEmpty(t, result.Plan.Files[0].UpdatedFingerprint)

	data, err := os.ReadFile(filepath.Join(root, "specs/001-test/spec.md"))
	require.NoError(t, err)
	require.Contains(t, string(data), "summary: Saved")
}

const embeddedProjectionSchema = `
type UserStory implements Section @node(locator: EMBEDDED) {
  status: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*/spec.md"]) {
  summary: String!
  userStoriesSection: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`

const projectionBindingSchema = `
type Decision @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type UserStory implements Section @node(locator: EMBEDDED) {
  status: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  decisions: [Decision!] @link
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`
