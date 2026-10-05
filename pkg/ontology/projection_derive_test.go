package ontology

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const derivedIdentifierSchema = `
type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "AC")
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(level: H5)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  acceptanceCriteria: AcceptanceCriteriaSection @contains(level: H4, heading: "Acceptance Criteria")
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*/spec.md"]) {
  summary: String!
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
  userStoriesSection: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`

// TestProjection_DerivesEmbeddedIdsFromStructure confirms that when an embedded
// preferred-identifier field is `derivable` (default for @node(locator: EMBEDDED)
// types per SPEC-0023.US8), the projection layer surfaces a synthesized id
// matching `${parent.id}-${derivedSuffix}${n}` for nodes whose `id::` line is
// not authored.
func TestProjection_DerivesEmbeddedIdsFromStructure(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, derivedIdentifierSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
id: SPEC-0042
summary: Test
aliases:
  - SPEC-0042
---

# Spec

## User Stories

### Story Alpha

#### Acceptance Criteria

##### Alpha first criterion
##### Alpha second criterion

### Story Beta

#### Acceptance Criteria

##### Beta only criterion
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := LoadDocumentSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "specs/001-test/spec.md")
	require.NoError(t, err)

	cases := []struct {
		title       string
		expectedID  string
		expectedDer bool
	}{
		{"Story Alpha", "SPEC-0042-US1", true},
		{"Story Beta", "SPEC-0042-US2", true},
		{"Alpha first criterion", "SPEC-0042-US1-AC1", true},
		{"Alpha second criterion", "SPEC-0042-US1-AC2", true},
		{"Beta only criterion", "SPEC-0042-US2-AC1", true},
	}

	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			ref := mustFindEmbeddedNodeByTitle(t, snapshot, schema, tc.title)
			proj, err := ProjectNodeFromSnapshot(snapshot, schema, ref)
			require.NoError(t, err)
			require.Equal(t, []string{tc.expectedID}, proj.Fields["id"].Values)
			require.Equal(t, tc.expectedDer, proj.Fields["id"].Derived)
		})
	}
}

// TestProjection_DerivedIdsRespectAuthoredSiblings exercises the increment-max
// rule: when some siblings have authored `id::` lines, derived ids fill the
// lowest-unused integers and never reassign authored numbers.
func TestProjection_DerivedIdsRespectAuthoredSiblings(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, derivedIdentifierSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
id: SPEC-0042
summary: Test
aliases:
  - SPEC-0042
---

# Spec

## User Stories

### Story Alpha
id:: ^SPEC-0042-US3

#### Acceptance Criteria

##### Alpha first criterion
##### Alpha second criterion
id:: ^SPEC-0042-US3-AC5
##### Alpha third criterion
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := LoadDocumentSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "specs/001-test/spec.md")
	require.NoError(t, err)

	cases := []struct {
		title       string
		expectedID  string
		expectedDer bool
	}{
		{"Story Alpha", "SPEC-0042-US3", false},
		{"Alpha first criterion", "SPEC-0042-US3-AC1", true},
		{"Alpha second criterion", "SPEC-0042-US3-AC5", false},
		{"Alpha third criterion", "SPEC-0042-US3-AC2", true},
	}

	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			ref := mustFindEmbeddedNodeByTitle(t, snapshot, schema, tc.title)
			proj, err := ProjectNodeFromSnapshot(snapshot, schema, ref)
			require.NoError(t, err)
			require.Equal(t, []string{tc.expectedID}, proj.Fields["id"].Values)
			require.Equal(t, tc.expectedDer, proj.Fields["id"].Derived)
		})
	}
}

func TestProjection_ProjectsMarkerGatedCheckboxItems(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type ActionItem implements Section @node(locator: EMBEDDED) {
  done: Boolean! @field(sourceKind: CHECKBOX)
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

- [ ] Update docs #action-item
  assignee:: Gabe
  - nested detail #action-item
- [x] Already done assignee:: Maya #action-item ^done-1
- [ ] Ordinary checkbox
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := LoadDocumentSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "meetings/sync.md")
	require.NoError(t, err)

	note, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: "meetings/sync.md", Kind: NodeKindNote})
	require.NoError(t, err)
	binding := note.Fields["actionItems"]
	require.Len(t, binding.SectionNodes, 2)

	first, err := ProjectNodeFromSnapshot(snapshot, schema, binding.SectionNodes[0])
	require.NoError(t, err)
	require.Equal(t, NodeKindEmbedded, first.Ref.Kind)
	require.Equal(t, "ActionItem", first.ResolvedType)
	require.Equal(t, []string{"false"}, first.Fields["done"].Values)
	require.Equal(t, []string{"Gabe"}, first.Fields["assignee"].Values)
	require.Contains(t, first.Ref.Fragment, "item-")
	require.NotEmpty(t, first.Ref.ParentID)
	require.NotEmpty(t, first.Ref.Structural)

	second, err := ProjectNodeFromSnapshot(snapshot, schema, binding.SectionNodes[1])
	require.NoError(t, err)
	require.Equal(t, []string{"true"}, second.Fields["done"].Values)
	require.Equal(t, "^done-1", second.Ref.Fragment)
	require.Equal(t, []string{"Maya"}, second.Fields["assignee"].Values)
}

func TestProjection_ItemMarkerGateIgnoresNestedChildMarkers(t *testing.T) {
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

- [ ] Parent coordination
  - [ ] Nested follow-up #action-item
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := LoadDocumentSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "meetings/sync.md")
	require.NoError(t, err)

	note, err := ProjectNodeFromSnapshot(snapshot, schema, NodeRef{NotePath: "meetings/sync.md", Kind: NodeKindNote})
	require.NoError(t, err)
	binding := note.Fields["actionItems"]
	require.Len(t, binding.SectionNodes, 1)

	item, err := ProjectNodeFromSnapshot(snapshot, schema, binding.SectionNodes[0])
	require.NoError(t, err)
	require.Equal(t, "Nested follow-up", item.Snapshot.SourceSpansByID[item.Ref.NodeID].Title)
	require.Equal(t, []string{"false"}, item.Fields["done"].Values)
}

func mustFindEmbeddedNodeByTitle(t *testing.T, snapshot *DocumentSnapshot, schema *Schema, title string) NodeRef {
	t.Helper()
	resolver, err := newProjectionResolver(snapshot, schema)
	require.NoError(t, err)
	for _, section := range allSections(snapshot.Sections) {
		if section.Title == title {
			return resolver.sectionNodeRef(section)
		}
	}
	t.Fatalf("section %q not found", title)
	return NodeRef{}
}

func allSections(nodes []*SectionNode) []*SectionNode {
	out := []*SectionNode{}
	for _, n := range nodes {
		if n == nil {
			continue
		}
		out = append(out, n)
		out = append(out, allSections(n.Children)...)
	}
	return out
}
