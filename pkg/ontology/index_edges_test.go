package ontology

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBuildIndex_BuildsStructuralAndAmbientEdges(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Team @node(paths: ["teams/*.md"]) {
  name: String!
  members: [Person!] @link(inverse: "team")
}

type Person @node(paths: ["people/*.md"]) {
  name: String!
  team: Team @link(inverse: "members")
  manager: Person @link(inverse: "reports")
  reports: [Person!] @link(inverse: "manager")
}
`)
	writeOntologyNote(t, root, "teams/Eng.md", `---
type: Team
name: Eng
members:
  - people/Alice.md
  - people/Bob.md
  - people/Carol.md
---
`)
	writeOntologyNote(t, root, "people/Alice.md", `---
type: Person
name: Alice
team: teams/Eng.md
reports:
  - people/Bob.md
---

Works closely with [[Carol]].
`)
	writeOntologyNote(t, root, "people/Bob.md", `---
type: Person
name: Bob
team: teams/Eng.md
manager: people/Alice.md
---
`)
	writeOntologyNote(t, root, "people/Carol.md", `---
type: Person
name: Carol
team: teams/Eng.md
---
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)

	require.Len(t, result.NoteTypes, 4)
	requireEdge(t, result.Edges, "people/Alice.md", "team", "teams/Eng.md", "field", true)
	requireEdge(t, result.Edges, "people/Bob.md", "manager", "people/Alice.md", "field", true)
	requireEdge(t, result.Edges, "people/Alice.md", "related", "people/Carol.md", "body_link", false)
	requireEdge(t, result.Edges, "people/Carol.md", "related", "people/Alice.md", "backlink", false)
}

func TestBuildIndex_EmitsNodeScopedEdgesForEmbeddedLinkFields(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/*.md"]) {
  name: String!
}

type UserStory implements Section @node(locator: EMBEDDED) {
  owner: Person @link
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: UserStoriesSection @contains(level: H2, heading: "Stories")
}
`)
	writeOntologyNote(t, root, "people/Alice.md", `---
type: Person
name: Alice
---
`)
	writeOntologyNote(t, root, "specs/demo.md", `---
type: Spec
summary: Demo
---

# Demo

## Stories

### Checkout

owner:: [[people/Alice]]
^checkout
`)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)

	var found codeanchorsqlite.OntologyEdgeRow
	for _, edge := range result.Edges {
		if edge.SrcPath == "specs/demo.md" && edge.RelationName == "owner" && edge.DstPath == "people/Alice.md" {
			found = edge
			break
		}
	}
	require.NotEmpty(t, found.SrcNodeID)
	require.Empty(t, found.DstNodeID)
	require.True(t, found.Structural)
}

func TestBuildIndex_EmitsNodeScopedEdgesForGlobalSourceLinkFields(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type Person @node(paths: ["people/*.md"]) {
  name: String!
}

type ActionItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
}
`)
	writeOntologyNote(t, root, "people/Alice.md", `---
type: Person
name: Alice
---
`)
	writeOntologyNote(t, root, "notes/scratch.md", `# Scratch

- [ ] Call Alice #action-item
  assignee:: [[people/Alice]]
`)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)

	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)

	var found codeanchorsqlite.OntologyEdgeRow
	for _, edge := range result.Edges {
		if edge.SrcPath == "notes/scratch.md" && edge.RelationName == "assignee" && edge.DstPath == "people/Alice.md" {
			found = edge
			break
		}
	}
	require.NotEmpty(t, found.SrcNodeID)
	require.True(t, found.Structural)
	require.Equal(t, "field", found.Provenance)
}

func TestBuildIndex_EmitsSectionScopedFrozenSpecEdges(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
interface SpecLike {
  summary: String!
}

type FrozenSpecSetSection implements Section {
  frozenSpecs: [SpecLike!] @neighbors(direction: OUTBOUND, type: "SpecLike", scope: SUBTREE)
}

type EffortNote @node(paths: ["efforts/*.md"]) {
  summary: String!
  specSetFrozen: FrozenSpecSetSection @contains(level: H2, heading: "Spec Set (Frozen)", required: true)
}

type ProcessSpec implements SpecLike @node(paths: ["specs/*.md"]) {
  summary: String!
}
`)
	writeOntologyNote(t, root, "specs/graph.md", `---
type: ProcessSpec
summary: Graph spec
---
`)
	writeOntologyNote(t, root, "efforts/graph.md", `---
type: EffortNote
summary: Graph effort
---

# Graph effort

## Spec Set (Frozen)

- [[specs/graph]] — SPEC-0001
`)

	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, result.ValidationIssues)

	var found codeanchorsqlite.OntologyEdgeRow
	for _, edge := range result.Edges {
		if edge.SrcPath == "efforts/graph.md" && edge.RelationName == "frozenSpecs" && edge.DstPath == "specs/graph.md" {
			found = edge
			break
		}
	}
	require.NotEmpty(t, found.SrcNodeID)
	require.Empty(t, found.DstNodeID)
	require.Equal(t, "section_neighbor", found.Provenance)
	require.True(t, found.Structural)
}
