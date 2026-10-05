package ontology

import (
	"context"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestNodeLinkService_EmbeddedExistingBlockIDRendersStemWikilink(t *testing.T) {
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

	result, err := (&NodeLinkService{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		NoteReader: &obsidian.Note{},
		Schema:     schema,
	}).LinkTargets(context.Background(), LinkTargetRequest{
		Refs: []NodeRef{{
			NotePath: "specs/001-test/spec.md",
			Fragment: "^story-a",
			Kind:     NodeKindEmbedded,
		}},
		Ensure: EnsureLinkTargetNever,
	})
	require.NoError(t, err)
	target := result.Targets["specs/001-test/spec.md#^story-a"]
	require.Equal(t, "[[spec#^story-a]]", target.Wikilink)
	require.Equal(t, "specs/001-test/spec.md#^story-a", target.Markdown)
	require.True(t, target.Exists)
	require.False(t, target.RequiresFix)
	require.Nil(t, result.FixPlan)
}

func TestNodeLinkService_ItemBackedEmbeddedTargetsUseSourceSpanBlockIDs(t *testing.T) {
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

- [ ] Needs block #action-item
- [x] Already durable #action-item ^ai-2
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "meetings/sync.md")
	require.NoError(t, err)
	refs := note.Fields["actionItems"].SectionNodes
	require.Len(t, refs, 2)

	result, err := (&NodeLinkService{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		NoteReader: &obsidian.Note{},
		Schema:     schema,
	}).LinkTargets(context.Background(), LinkTargetRequest{
		Refs:   refs,
		Ensure: EnsureLinkTargetPlan,
	})
	require.NoError(t, err)

	missing := result.Targets[nodeLocatorMapKey(refs[0])]
	require.True(t, missing.RequiresFix)
	require.False(t, missing.Exists)
	require.NotEmpty(t, missing.BlockID)
	require.NotNil(t, result.FixPlan)
	require.Len(t, result.FixPlan.Actions, 1)

	durable := result.Targets[nodeLocatorMapKey(refs[1])]
	require.True(t, durable.Exists)
	require.False(t, durable.RequiresFix)
	require.Equal(t, "ai-2", durable.BlockID)
	require.Equal(t, "[[sync#^ai-2]]", durable.Wikilink)
}

func TestNodeLinkService_DuplicateFilenameStemUsesVaultRelativeWikilink(t *testing.T) {
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
	writeOntologyNote(t, root, "specs/002-test/spec.md", `---
type: Spec
summary: Other
---

# Spec
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)

	result, err := (&NodeLinkService{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		NoteReader: &obsidian.Note{},
		Schema:     schema,
	}).LinkTargets(context.Background(), LinkTargetRequest{
		Refs: []NodeRef{{
			NotePath: "specs/001-test/spec.md",
			Fragment: "^story-a",
			Kind:     NodeKindEmbedded,
		}},
		Ensure: EnsureLinkTargetNever,
	})
	require.NoError(t, err)
	target := result.Targets["specs/001-test/spec.md#^story-a"]
	require.Equal(t, "[[specs/001-test/spec#^story-a]]", target.Wikilink)
}

func TestNodeLinkService_LinkTargetsKeepsUnfragmentedEmbeddedRefsDistinct(t *testing.T) {
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
	stories := note.Fields["userStoriesSection"].SectionNodes[0]
	container, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, stories)
	require.NoError(t, err)
	refs := append([]NodeRef(nil), container.Fields["stories"].SectionNodes...)
	require.Len(t, refs, 2)
	for i := range refs {
		refs[i].Fragment = ""
	}

	result, err := (&NodeLinkService{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		NoteReader: &obsidian.Note{},
		Schema:     schema,
	}).LinkTargets(context.Background(), LinkTargetRequest{
		Refs:   refs,
		Ensure: EnsureLinkTargetNever,
	})
	require.NoError(t, err)
	require.Len(t, result.Targets, 2)
	require.Equal(t, "[[spec#^story-a]]", result.Targets[nodeLocatorMapKey(refs[0])].Wikilink)
	require.Equal(t, "[[spec#^story-b]]", result.Targets[nodeLocatorMapKey(refs[1])].Wikilink)
}

func TestNodeLinkService_MissingBlockIDPlansAuthoredIDFix(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
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
id:: SPEC-0023.US1
status:: TODO
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	storyRef := firstStoryRef(t, root, schema)

	result, err := (&NodeLinkService{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		NoteReader: &obsidian.Note{},
		Schema:     schema,
	}).LinkTargets(context.Background(), LinkTargetRequest{
		Refs:   []NodeRef{storyRef},
		Ensure: EnsureLinkTargetPlan,
	})
	require.NoError(t, err)
	target := result.Targets[storyRef.String()]
	require.True(t, target.RequiresFix)
	require.False(t, target.Exists)
	require.Equal(t, "SPEC-0023-US1", target.BlockID)
	require.Equal(t, "[[spec#^SPEC-0023-US1]]", target.Wikilink)
	require.Len(t, result.FixPlan.Actions, 1)
	require.Equal(t, "SPEC-0023-US1", result.FixPlan.Actions[0].BlockID)
}

func TestNodeLinkService_ApplyMissingBlockIDUsesIdentifierField(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
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
id:: SPEC-0023.US1
status:: TODO
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	storyRef := firstStoryRef(t, root, schema)

	result, err := (&NodeLinkService{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		NoteReader: &obsidian.Note{},
		Schema:     schema,
	}).LinkTargets(context.Background(), LinkTargetRequest{
		Refs:   []NodeRef{storyRef},
		Ensure: EnsureLinkTargetApply,
	})
	require.NoError(t, err)
	target := result.Targets[storyRef.String()]
	require.True(t, target.Exists)
	require.False(t, target.RequiresFix)
	require.Equal(t, "SPEC-0023-US1", target.BlockID)
	require.Equal(t, "[[spec#^SPEC-0023-US1]]", target.Wikilink)

	content, err := (&obsidian.Note{}).GetContents(obsidian.VaultDefinition{Path: root}, "specs/001-test/spec.md")
	require.NoError(t, err)
	require.Contains(t, content, "id:: ^SPEC-0023-US1")
	require.NotContains(t, content, "\n^SPEC-0023-US1\n")

	projection, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "^SPEC-0023-US1",
		Kind:     NodeKindEmbedded,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"SPEC-0023-US1"}, projection.Fields["id"].Values)
}

func TestNodeLinkService_ApplyMintsPlainLocatorForEmbeddedNodeWithoutIdentifier(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  verification: String @field
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(level: H5)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US", populate: ON_CREATE)
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
`)
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
id:: ^SPEC-0042-US1

#### Acceptance Criteria

##### Alpha first criterion
##### Alpha second criterion
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)

	snapshot, err := LoadDocumentSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "specs/001-test/spec.md")
	require.NoError(t, err)
	ac2Ref := mustFindEmbeddedNodeByTitle(t, snapshot, schema, "Alpha second criterion")

	result, err := (&NodeLinkService{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		NoteReader: &obsidian.Note{},
		Schema:     schema,
	}).LinkTargets(context.Background(), LinkTargetRequest{
		Refs:   []NodeRef{ac2Ref},
		Ensure: EnsureLinkTargetApply,
	})
	require.NoError(t, err)
	target := result.Targets[ac2Ref.String()]
	require.True(t, target.Exists, "after Apply the target should exist in source")
	require.False(t, target.RequiresFix)
	require.Equal(t, "SPEC-0042-US1-AC2", target.BlockID, "plain locator is parent-scoped")
	require.Equal(t, "[[spec#^SPEC-0042-US1-AC2]]", target.Wikilink)

	content, err := (&obsidian.Note{}).GetContents(obsidian.VaultDefinition{Path: root}, "specs/001-test/spec.md")
	require.NoError(t, err)
	require.Contains(t, content, "\n^SPEC-0042-US1-AC2\n", "minted line uses a plain standalone locator")
	require.NotContains(t, content, "id:: ^SPEC-0042-US1-AC2")

	// The first criterion remains uncited.
	require.NotContains(t, content, "id:: ^SPEC-0042-US1-AC1")
	require.NotContains(t, content, "\n^SPEC-0042-US1-AC1\n")
}

func TestNodeLinkService_ListItemCriterionPlanApplyResolveAndReapply(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  summary: String! @field(sourceKind: ITEM_SUMMARY)
  verification: String @field
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(shape: LIST_ITEM)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US", populate: ON_CREATE)
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
`)
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
id:: ^SPEC-0042-US1

#### Acceptance Criteria

- Alpha first criterion
- Alpha second criterion
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	vaultDef := obsidian.VaultDefinition{Path: root}
	note, err := ProjectNote(context.Background(), vaultDef, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	storiesRef := note.Fields["userStoriesSection"].SectionNodes[0]
	stories, err := ProjectNode(context.Background(), vaultDef, &obsidian.Note{}, schema, storiesRef)
	require.NoError(t, err)
	story, err := ProjectNode(context.Background(), vaultDef, &obsidian.Note{}, schema, stories.Fields["stories"].SectionNodes[0])
	require.NoError(t, err)
	criteria, err := ProjectNode(context.Background(), vaultDef, &obsidian.Note{}, schema, story.Fields["acceptanceCriteria"].SectionNodes[0])
	require.NoError(t, err)
	ac2Ref := criteria.Fields["criteria"].SectionNodes[1]
	service := &NodeLinkService{
		VaultDef:   vaultDef,
		NoteReader: &obsidian.Note{},
		Schema:     schema,
	}

	before, err := (&obsidian.Note{}).GetContents(vaultDef, "specs/001-test/spec.md")
	require.NoError(t, err)
	plan, err := service.LinkTargets(context.Background(), LinkTargetRequest{
		Refs:   []NodeRef{ac2Ref},
		Ensure: EnsureLinkTargetPlan,
	})
	require.NoError(t, err)
	planned := plan.Targets[ac2Ref.String()]
	require.False(t, planned.Exists)
	require.True(t, planned.RequiresFix)
	require.Len(t, plan.FixPlan.Actions, 1)
	afterPlan, err := (&obsidian.Note{}).GetContents(vaultDef, "specs/001-test/spec.md")
	require.NoError(t, err)
	require.Equal(t, before, afterPlan, "planning must not persist a locator")

	result, err := service.LinkTargets(context.Background(), LinkTargetRequest{
		Refs:   []NodeRef{ac2Ref},
		Ensure: EnsureLinkTargetApply,
	})
	require.NoError(t, err)
	target := result.Targets[ac2Ref.String()]
	require.True(t, target.Exists, "after Apply the target should exist in source")
	require.False(t, target.RequiresFix)
	require.Equal(t, planned.BlockID, target.BlockID, "apply commits the locator shown by plan")
	require.Equal(t, "[[spec#^"+target.BlockID+"]]", target.Wikilink)

	content, err := (&obsidian.Note{}).GetContents(vaultDef, "specs/001-test/spec.md")
	require.NoError(t, err)
	require.Contains(t, content, "- Alpha second criterion ^"+target.BlockID, "list-item locator stays on its owning line")
	require.NotContains(t, content, "id:: ^"+target.BlockID)

	resolved, err := ProjectNode(context.Background(), vaultDef, &obsidian.Note{}, schema, target.Ref)
	require.NoError(t, err)
	require.Equal(t, []string{"Alpha second criterion"}, resolved.Fields["summary"].Values)

	second, err := service.LinkTargets(context.Background(), LinkTargetRequest{
		Refs:   []NodeRef{ac2Ref},
		Ensure: EnsureLinkTargetApply,
	})
	require.NoError(t, err)
	require.False(t, second.Targets[ac2Ref.String()].RequiresFix)
	afterSecond, err := (&obsidian.Note{}).GetContents(vaultDef, "specs/001-test/spec.md")
	require.NoError(t, err)
	require.Equal(t, content, afterSecond, "reapplying the original structural ref is idempotent")

	// The first criterion remains uncited.
	require.Contains(t, content, "- Alpha first criterion\n")
	require.Equal(t, 1, strings.Count(content, "^"+target.BlockID))
}

func TestNodeLinkService_ApplyMintsPlainLocatorAfterSiblingMax(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  verification: String @field
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(level: H5)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US", populate: ON_CREATE)
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
`)
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
id:: ^SPEC-0042-US1

#### Acceptance Criteria

##### Alpha first criterion
^SPEC-0042-US1-AC5

##### Alpha second criterion
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)

	snapshot, err := LoadDocumentSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "specs/001-test/spec.md")
	require.NoError(t, err)
	ac2Ref := mustFindEmbeddedNodeByTitle(t, snapshot, schema, "Alpha second criterion")

	projection, err := ProjectNodeFromSnapshot(snapshot, schema, ac2Ref)
	require.NoError(t, err)
	require.NotContains(t, projection.Fields, "id")

	result, err := (&NodeLinkService{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		NoteReader: &obsidian.Note{},
		Schema:     schema,
	}).LinkTargets(context.Background(), LinkTargetRequest{
		Refs:   []NodeRef{ac2Ref},
		Ensure: EnsureLinkTargetApply,
	})
	require.NoError(t, err)
	target := result.Targets[ac2Ref.String()]
	require.True(t, target.Exists, "after Apply the target should exist in source")
	require.False(t, target.RequiresFix)
	require.Equal(t, "SPEC-0042-US1-AC6", target.BlockID, "minted line follows increment-max, not the gap-filling read id")
	require.Equal(t, "[[spec#^SPEC-0042-US1-AC6]]", target.Wikilink)

	content, err := (&obsidian.Note{}).GetContents(obsidian.VaultDefinition{Path: root}, "specs/001-test/spec.md")
	require.NoError(t, err)
	require.Contains(t, content, "\n^SPEC-0042-US1-AC5\n")
	require.Contains(t, content, "\n^SPEC-0042-US1-AC6\n")
	require.NotContains(t, content, "id:: ^SPEC-0042-US1-AC6")
}

func TestNodeLinkService_MissingBlockIDIgnoresUnmarkedIDField(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  status: String @field
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
id:: SPEC-0023.US1
status:: TODO
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	storyRef := firstStoryRef(t, root, schema)

	result, err := (&NodeLinkService{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		NoteReader: &obsidian.Note{},
		Schema:     schema,
	}).LinkTargets(context.Background(), LinkTargetRequest{
		Refs:   []NodeRef{storyRef},
		Ensure: EnsureLinkTargetApply,
	})
	require.NoError(t, err)
	target := result.Targets[storyRef.String()]
	require.True(t, target.Exists)
	require.False(t, target.RequiresFix)
	require.NotEqual(t, "SPEC-0023-US1", target.BlockID)
	require.Contains(t, target.BlockID, "userstory-story-a-")

	content, err := (&obsidian.Note{}).GetContents(obsidian.VaultDefinition{Path: root}, "specs/001-test/spec.md")
	require.NoError(t, err)
	require.Contains(t, content, "id:: SPEC-0023.US1")
	require.Contains(t, content, "^"+target.BlockID)
}

func TestNodeLinkService_LocatorsExposeStatusesAndFixActions(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
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
`)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Existing
status:: TODO
^existing

### Missing
status:: TODO
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	storyRef := firstStoryRef(t, root, schema)
	containerRef := NodeRef{
		NotePath: "specs/001-test/spec.md",
		Fragment: "User Stories",
		Kind:     NodeKindSection,
	}
	noteRef := NodeRef{NotePath: "specs/001-test/spec.md", Kind: NodeKindNote}
	missingRef := storyRef
	missingRef.Fragment = "Missing"
	missingRef.NodeID = "specs/001-test/spec.md#Missing"

	locators, err := (&NodeLinkService{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		NoteReader: &obsidian.Note{},
		Schema:     schema,
	}).Locators(context.Background(), LinkTargetRequest{
		Refs:   []NodeRef{noteRef, storyRef, missingRef, containerRef},
		Ensure: EnsureLinkTargetPlan,
	})
	require.NoError(t, err)

	noteLocator := locators[noteRef.String()]
	require.Equal(t, NodeLocatorLinkable, noteLocator.Status)
	require.Equal(t, "[[spec]]", noteLocator.LinkTarget.Wikilink)

	existingLocator := locators[storyRef.String()]
	require.Equal(t, NodeLocatorLinkable, existingLocator.Status)
	require.Equal(t, "[[spec#^existing]]", existingLocator.LinkTarget.Wikilink)

	missingLocator := locators[missingRef.String()]
	require.Equal(t, NodeLocatorRequiresFix, missingLocator.Status)
	require.True(t, missingLocator.LinkTarget.RequiresFix)
	require.Len(t, missingLocator.FixActions, 1)

	sectionLocator := locators[containerRef.String()]
	require.Equal(t, NodeLocatorUnsupported, sectionLocator.Status)
	require.NotEmpty(t, sectionLocator.Diagnostics)
}

func TestEnsureBlockIDInSnapshotIsIdempotentAndInsertsBeforeChildren(t *testing.T) {
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

#### Acceptance Criteria

- Works
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	storyRef := firstStoryRef(t, root, schema)

	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.EnsureBlockID(storyRef, "story-a"))
	require.NoError(t, session.EnsureBlockID(storyRef, "story-a"))
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	updated := plan.Files[0].UpdatedContentPreview
	require.Equal(t, 1, strings.Count(updated, "^story-a"))
	require.Less(t, strings.Index(updated, "^story-a"), strings.Index(updated, "#### Acceptance Criteria"))
}

func TestValidateEmbeddedBlockIDsClassifiesMissingMalformedAndDuplicate(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Missing
status:: TODO

### Malformed
status:: TODO
^bad id

### Duplicate A
status:: TODO
^dup

### Duplicate B
status:: TODO
^dup
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := LoadDocumentSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "specs/001-test/spec.md")
	require.NoError(t, err)

	issues := ValidateEmbeddedBlockIDs(snapshot, schema)
	requireBlockIDIssueCode(t, issues, "missing_embedded_block_id")
	requireBlockIDIssueCode(t, issues, "malformed_block_id")
	requireBlockIDIssueCode(t, issues, "duplicate_block_id")
}

func firstStoryRef(t *testing.T, root string, schema *Schema) NodeRef {
	t.Helper()
	note, err := ProjectNote(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "specs/001-test/spec.md")
	require.NoError(t, err)
	containerRef := note.Fields["userStoriesSection"].SectionNodes[0]
	container, err := ProjectNode(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, containerRef)
	require.NoError(t, err)
	return container.Fields["stories"].SectionNodes[0]
}

func requireBlockIDIssueCode(t *testing.T, issues []BlockIDValidationIssue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("missing issue code %q in %+v", code, issues)
}

// TestSectionNodeRef_ClassificationIndependentOfBlockID guards SPEC-0023.US5.AC1
// and AC2: schema role + parent containment determine EMBEDDED, not the
// presence of a `^block-id` line. A stray block ID on a structural section
// must not promote that section to EMBEDDED; instead it surfaces through the
// stray_block_id_on_non_embedded_section diagnostic.
// Coderefs: [[linkable-embedded-node-identifiers#^spec-0023-us5]]
func TestSectionNodeRef_ClassificationIndependentOfBlockID(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec

## Summary

Some prose.
^stray-on-summary

## User Stories

### Promoted by schema
status:: TODO
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := LoadDocumentSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "specs/001-test/spec.md")
	require.NoError(t, err)

	resolver, err := newProjectionResolver(snapshot, schema)
	require.NoError(t, err)

	var summaryRef, storyRef NodeRef
	var walk func([]*SectionNode)
	walk = func(nodes []*SectionNode) {
		for _, node := range nodes {
			ref := resolver.sectionNodeRef(node)
			switch strings.TrimSpace(node.Title) {
			case "Summary":
				summaryRef = ref
			case "Promoted by schema":
				storyRef = ref
			}
			walk(node.Children)
		}
	}
	walk(snapshot.Sections)

	require.Equal(t, NodeKindSection, summaryRef.Kind, "structural section with stray ^block-id must NOT be promoted to EMBEDDED")
	require.Equal(t, NodeKindEmbedded, storyRef.Kind, "schema-bound child type must classify as EMBEDDED even without a ^block-id")

	issues := ValidateEmbeddedBlockIDs(snapshot, schema)
	requireBlockIDIssueCode(t, issues, "stray_block_id_on_non_embedded_section")
}

// TestSectionNodeRef_RemoveBlockIDPreservesClassification guards
// SPEC-0023.US5.AC3 (revised): removing a `^block-id` from an embedded node
// MUST NOT change the node's EMBEDDED classification. The rendered NodeID
// and the current structural fingerprint may shift because the fingerprint
// fast-path keys on the authored anchor when present (see
// stableSectionFingerprint in pkg/ontology/projection.go); the resolver
// handles the cross-shape lookup via heading slug + sibling ordinal in the
// no-anchor case.
//
// WHY: making the structural fingerprint truly anchor-invariant would invalidate
// existing index keys for any frozen reference that captured a `block:...`
// fingerprint. That refactor is tracked as a Compounding Follow-up on EFF-0015.
func TestSectionNodeRef_RemoveBlockIDPreservesClassification(t *testing.T) {
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
id:: SPEC-1.US1
status:: TODO
^story-a
`)
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	snapshot, err := LoadDocumentSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "specs/001-test/spec.md")
	require.NoError(t, err)
	resolver, err := newProjectionResolver(snapshot, schema)
	require.NoError(t, err)
	var withAnchor NodeRef
	var walk func([]*SectionNode)
	walk = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if strings.TrimSpace(node.Title) == "Story A" {
				withAnchor = resolver.sectionNodeRef(node)
			}
			walk(node.Children)
		}
	}
	walk(snapshot.Sections)

	require.Equal(t, NodeKindEmbedded, withAnchor.Kind)
	withAnchorStructural := withAnchor.Structural

	// Re-render the same fixture without the ^story-a anchor and confirm the
	// embedded classification and structural fingerprint are stable.
	writeOntologyNote(t, root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---

# Spec

## User Stories

### Story A
id:: SPEC-1.US1
status:: TODO
`)
	snapshot, err = LoadDocumentSnapshot(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, "specs/001-test/spec.md")
	require.NoError(t, err)
	resolver, err = newProjectionResolver(snapshot, schema)
	require.NoError(t, err)
	var withoutAnchor NodeRef
	walk = func(nodes []*SectionNode) {
		for _, node := range nodes {
			if strings.TrimSpace(node.Title) == "Story A" {
				withoutAnchor = resolver.sectionNodeRef(node)
			}
			walk(node.Children)
		}
	}
	walk(snapshot.Sections)

	require.Equal(t, withAnchor.Kind, withoutAnchor.Kind, "kind must survive block-id removal")
	require.NotEmpty(t, withAnchorStructural, "structural fingerprint must be populated for embedded nodes")
	require.NotEmpty(t, withoutAnchor.Structural, "structural fingerprint must remain populated after anchor removal so resolver fallback still works")
}
