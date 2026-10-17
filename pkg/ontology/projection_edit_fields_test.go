package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestEditSession_SetScalarField_AutoAddsBlockID(t *testing.T) {
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	containerRef := note.Fields["userStoriesSection"].SectionNodes[0]
	container, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, containerRef)
	require.NoError(t, err)
	storyRef := container.Fields["stories"].SectionNodes[0]

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	err = session.SetScalarField(storyRef, "status", "DONE")
	require.NoError(t, err)

	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)
	updated := plan.Files[0].UpdatedContentPreview
	require.Contains(t, updated, "status:: DONE")
	require.Contains(t, updated, "^userstory-story-a-")
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

	schema, err := ontoschema.LoadSchema(root)
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
			schema, err := ontoschema.LoadSchema(root)
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
		schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

func TestEditSession_SetScalarField_PreservesEmbeddedFormatting(t *testing.T) {
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{
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

func TestEditSession_SetScalarField_RemovesTrailingDuplicateInlineSpansSafely(t *testing.T) {
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.SetScalarField(NodeRef{
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
