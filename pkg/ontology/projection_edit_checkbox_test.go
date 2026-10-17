package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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
