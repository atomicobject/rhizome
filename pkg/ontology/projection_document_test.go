package ontology

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProjectDocumentNodesFromSnapshotProjectsEveryNodeOnceInSourceOrder(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Criterion implements Section @node(locator: EMBEDDED) {
  title: String! @field(sourceKind: ITEM_TITLE)
}

type ActionItem implements Section @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}

type CriteriaSection implements Section {
  criteria: [Criterion!] @contains(shape: LIST_ITEM)
}

type WorkSection implements Section {
  criteria: CriteriaSection @contains(level: H3, heading: "Criteria")
}

type WorkNote @node(paths: ["notes/*.md"]) {
  work: WorkSection @contains(level: H2, heading: "Work")
}
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	content := `# Title

## Work

### Criteria

- first criterion
  - nested criterion

## Actions

- [ ] ship it #action-item
`
	snapshot, err := BuildDocumentSnapshot("notes/work.md", content, time.Time{})
	require.NoError(t, err)

	first, err := ProjectDocumentNodesFromSnapshot(snapshot, schema)
	require.NoError(t, err)
	second, err := ProjectDocumentNodesFromSnapshot(snapshot, schema)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.NotEmpty(t, first)
	require.Equal(t, NodeKindNote, first[0].Ref.Kind)

	seen := make(map[NodeRef]struct{}, len(first))
	for index, projection := range first {
		_, duplicate := seen[projection.Ref]
		require.Falsef(t, duplicate, "duplicate full ref at index %d: %#v", index, projection.Ref)
		seen[projection.Ref] = struct{}{}
		if index > 1 {
			require.LessOrEqual(t, first[index-1].Ref.StartByte, projection.Ref.StartByte)
		}
	}

	require.Len(t, first, 8, "root + four headings + two list items + checkbox")
	require.Equal(t, []NodeKind{
		NodeKindNote,
		NodeKindSection,
		NodeKindSection,
		NodeKindSection,
		NodeKindEmbedded,
		NodeKindSection,
		NodeKindSection,
		NodeKindEmbedded,
	}, projectedKinds(first))
	for _, projection := range first {
		switch projection.ResolvedType {
		case "Criterion":
			require.False(t, projection.Fields["title"].ValueRangesExact, "item title normalization must not claim its whole content span is exact")
		case "ActionItem":
			require.False(t, projection.Fields["done"].ValueRangesExact, "checkbox token bytes do not equal the semantic bool")
		}
	}
}

func projectedKinds(projections []*NodeProjection) []NodeKind {
	out := make([]NodeKind, 0, len(projections))
	for _, projection := range projections {
		out = append(out, projection.Ref.Kind)
	}
	return out
}

func TestProjectionFrontmatterValueRangesAreExactAndOrdered(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["specs/*.md"]) {
  id: String! @field @identifier(preferred: true)
  aliases: [String!] @field
  related: [String!] @field
}
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	content := `---
id: SPEC-0048
aliases:
  - SPEC-0047
  - SPEC-0048
related: [SPEC-0005, docs/other]
---
`
	snapshot, err := BuildDocumentSnapshot("specs/ids.md", content, time.Time{})
	require.NoError(t, err)
	projection, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: snapshot.NotePath, Kind: NodeKindNote})
	require.NoError(t, err)

	assertExactFrontmatterBinding(t, content, projection.Fields["id"], []string{"SPEC-0048"})
	assertExactFrontmatterBinding(t, content, projection.Fields["aliases"], []string{"SPEC-0047", "SPEC-0048"})
	assertExactFrontmatterBinding(t, content, projection.Fields["related"], []string{"SPEC-0005", "docs/other"})
}

func TestProjectionFrontmatterValueRangesRejectUnsafeYAMLShapes(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Spec @node(paths: ["specs/*.md"]) {
  quoted: String @field
  escaped: String @field
  folded: String @field
  aliases: [String!] @field
}
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	content := "---\nquoted: 'SPEC-0048'\nescaped: \"SPEC-\\u0030\\u0030\\u0034\\u0038\"\nfolded: >-\n  SPEC-0048\naliases:\n  - SPEC-0048\n  - 'SPEC-0049'\n---\n"
	snapshot, err := BuildDocumentSnapshot("specs/unsafe.md", content, time.Time{})
	require.NoError(t, err)
	projection, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: snapshot.NotePath, Kind: NodeKindNote})
	require.NoError(t, err)

	for _, fieldName := range []string{"quoted", "escaped", "folded", "aliases"} {
		binding := projection.Fields[fieldName]
		require.True(t, binding.Present)
		require.False(t, binding.ValueRangesExact, fieldName)
		require.Empty(t, binding.ValueRanges, fieldName)
	}
}

func TestProjectionInlineNormalizedValueRangeIsNotExact(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Story implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivable: false)
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`)
	content := "## Stories\n\n### First\nid:: ^SPEC-0048-US1\n"
	snapshot, err := BuildDocumentSnapshot("specs/inline.md", content, time.Time{})
	require.NoError(t, err)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	ref := mustFindEmbeddedNodeByTitle(t, snapshot, schema, "First")
	projection, err := ProjectNodeFromSnapshot(snapshot, schema, ref)
	require.NoError(t, err)

	binding := projection.Fields["id"]
	require.Equal(t, []string{"SPEC-0048-US1"}, binding.Values)
	require.Len(t, binding.ValueRanges, 1)
	require.Equal(t, "^SPEC-0048-US1", content[binding.ValueRanges[0].Start:binding.ValueRanges[0].End])
	require.False(t, binding.ValueRangesExact)
}

func assertExactFrontmatterBinding(t *testing.T, content string, binding FieldBinding, expected []string) {
	t.Helper()
	require.True(t, binding.Present)
	require.True(t, binding.ValueRangesExact)
	require.Equal(t, expected, binding.Values)
	require.Len(t, binding.ValueRanges, len(expected))
	for index, valueRange := range binding.ValueRanges {
		require.True(t, valueRange.Valid(len(content)))
		require.Equal(t, expected[index], content[valueRange.Start:valueRange.End])
	}
}
