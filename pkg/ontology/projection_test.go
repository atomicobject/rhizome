package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
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

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	_, err = ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "User Stories",
		Kind:     NodeKindSection,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous")
}
