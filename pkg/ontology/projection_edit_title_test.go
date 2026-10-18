package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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
