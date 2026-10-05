package ontology

import (
	"context"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestSectionBlockIDRangeRequiresExactLine(t *testing.T) {
	content := "### Story\nid:: SPEC-1.US1\n^spec-1-us1-ac1\n"
	node := &SectionNode{
		StartByte: 0,
		EndByte:   len(content),
		Content:   content,
		BlockID:   "spec-1-us1",
	}

	_, ok := sectionBlockIDRange(content, node)

	require.False(t, ok, "parent block id must not match a child anchor by prefix")

	node.BlockID = "spec-1-us1-ac1"
	blockRange, ok := sectionBlockIDRange(content, node)
	require.True(t, ok)
	require.Equal(t, "^spec-1-us1-ac1", strings.TrimSpace(content[blockRange.Start:blockRange.End]))
}

// TestBuildNodeBody_SpecWithInterleavedSections exercises the core contract:
// a typed note's body surfaces as an ordered block list with narrative prose,
// inline fields, child sections, and list-bound collections grouped together.
func TestBuildNodeBody_SpecWithInterleavedSections(t *testing.T) {
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
status:: DONE
^story-b
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	// Start from the UserStoriesSection so we exercise the SECTION branch of
	// the content-range helper plus the collection grouping logic. The spec
	// type's summaryField/etc. would only stress the note branch.
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	containerRef := note.Fields["userStoriesSection"].SectionNodes[0]
	container, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, containerRef)
	require.NoError(t, err)

	blocks := BuildNodeBody(container, schema)
	require.NotEmpty(t, blocks)

	// Expect a single collection block grouping the two stories.
	var collections int
	var otherKinds int
	for _, block := range blocks {
		if block.Kind == NodeBodyBlockKindCollection {
			collections++
			require.Equal(t, "stories", block.FieldName)
			require.Len(t, block.ChildRefs, 2)
			require.Equal(t, NodeKindEmbedded, block.ChildRefs[0].Kind)
			require.Equal(t, NodeKindEmbedded, block.ChildRefs[1].Kind)
			require.Equal(t, "^story-a", block.ChildRefs[0].Fragment)
			require.Equal(t, "^story-b", block.ChildRefs[1].Fragment)
		} else {
			otherKinds++
		}
	}
	require.Equal(t, 1, collections, "the two adjacent stories must collapse into one collection block")
	require.LessOrEqual(t, otherKinds, 1, "only whitespace between heading and first story may produce at most one narrative block")
}

// TestBuildNodeBody_EmbeddedWithInlineFieldsAndInlineChild walks a UserStory
// body: inline fields at the top (id::, status::), then a nested INLINE
// section (acceptanceCriteria). The block list must have inline_field blocks
// followed by a child_section block with the parent's declared display mode.
func TestBuildNodeBody_EmbeddedWithInlineFieldsAndInlineChild(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  status: String @field
  acceptanceCriteria: NarrativeSection @contains(level: H4, heading: "Acceptance Criteria", display: INLINE)
}

type NarrativeSection implements Section {
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*/spec.md"]) {
  summary: String!
  userStoriesSection: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Story A
id:: S-A
status:: DONE
^story-a

#### Acceptance Criteria

- must pass
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	containerRef := note.Fields["userStoriesSection"].SectionNodes[0]
	container, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, containerRef)
	require.NoError(t, err)
	storyRef := container.Fields["stories"].SectionNodes[0]
	story, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, storyRef)
	require.NoError(t, err)

	blocks := BuildNodeBody(story, schema)
	require.NotEmpty(t, blocks)

	// First non-narrative blocks should be the authored inline fields in source
	// order (id, then status), followed eventually by the child_section with
	// INLINE display from the @contains directive and the locator block field.
	var emitted []NodeBodyBlock
	for _, block := range blocks {
		switch block.Kind {
		case NodeBodyBlockKindInlineField, NodeBodyBlockKindChildSection:
			emitted = append(emitted, block)
		}
	}
	require.Len(t, emitted, 4)
	for i, expected := range []struct {
		kind   NodeBodyBlockKind
		field  string
		source string
	}{
		{NodeBodyBlockKindInlineField, "id", "id:: S-A"},
		{NodeBodyBlockKindInlineField, "status", "status:: DONE"},
		{NodeBodyBlockKindInlineField, "locator", "^story-a"},
		{NodeBodyBlockKindChildSection, "acceptanceCriteria", ""},
	} {
		require.Equal(t, expected.kind, emitted[i].Kind)
		require.Equal(t, expected.field, emitted[i].FieldName)
		if expected.source != "" {
			r := emitted[i].Range
			require.GreaterOrEqual(t, r.Start, 0)
			require.Greater(t, r.End, r.Start)
			require.LessOrEqual(t, r.End, len(story.Snapshot.Content))
			require.Equal(t, expected.source, story.Snapshot.Content[r.Start:r.End])
		}
	}
	require.Equal(t, SectionDisplayInline, emitted[3].SectionDisplay)
	require.NotNil(t, emitted[3].ChildRef)
}

func TestBuildNodeBody_NotePreservesPrefaceBeforeTransparentH1(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type SummarySection implements Section {
}

type Spec @node(paths: ["specs/*/spec.md"]) {
  summary: SummarySection @contains(level: H2, heading: "Summary")
}
`)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
---

Preface paragraph that should stay visible.

# Demo Spec

## Summary

Body.
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	note, err := ProjectNote(
		context.Background(),
		obsidian.VaultDefinition{Path: root},
		&obsidian.Note{},
		schema,
		"specs/001-test/spec.md",
	)
	require.NoError(t, err)

	blocks := BuildNodeBody(note, schema)
	require.Len(t, blocks, 2)
	require.Equal(t, NodeBodyBlockKindNarrative, blocks[0].Kind)
	require.Contains(t, blocks[0].Markdown, "Preface paragraph that should stay visible.")
	require.NotContains(t, blocks[0].Markdown, "# Demo Spec")
	require.Equal(t, NodeBodyBlockKindChildSection, blocks[1].Kind)
	require.NotNil(t, blocks[1].ChildRef)
	require.Equal(t, NodeKindSection, blocks[1].ChildRef.Kind)
	require.Contains(t, blocks[1].ChildRef.NodeID, "#")
}

func TestBuildNodeBody_NoteSkipsInlineSpansInsideAlreadyEmittedChildren(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Story implements Section @node(locator: EMBEDDED) {
  status: String @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type FollowUpSection implements Section {
}

type Spec @node(paths: ["specs/*/spec.md"]) {
  status: String @field(sourceKind: INLINE)
  storiesSection: StoriesSection @contains(level: H2, heading: "Stories")
  followUp: FollowUpSection @contains(level: H2, heading: "Follow Up")
}
`)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
---

# Demo

## Stories

### Story A
status:: child-only
^story-a

## Follow Up

Later sibling section.
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	note, err := ProjectNote(
		context.Background(),
		obsidian.VaultDefinition{Path: root},
		&obsidian.Note{},
		schema,
		"specs/001-test/spec.md",
	)
	require.NoError(t, err)

	blocks := BuildNodeBody(note, schema)
	require.Len(t, blocks, 2)
	require.Equal(t, NodeBodyBlockKindChildSection, blocks[0].Kind)
	require.Equal(t, "storiesSection", blocks[0].FieldName)
	require.Equal(t, NodeBodyBlockKindChildSection, blocks[1].Kind)
	require.Equal(t, "followUp", blocks[1].FieldName)
}
