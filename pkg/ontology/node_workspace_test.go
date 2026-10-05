package ontology

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestBuildNodeWorkspaceFromProjection_VersionStaysStableForUnchangedSiblingNode(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	notePath := "specs/001-test/spec.md"
	writeOntologyNote(t, root, notePath, `---
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

	storyBRef := NodeRef{
		NotePath: notePath,
		Fragment: "^story-b",
		Kind:     NodeKindEmbedded,
	}
	before, err := BuildNodeWorkspace(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, storyBRef)
	require.NoError(t, err)

	path := filepath.Join(root, notePath)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	updated := strings.Replace(string(content), "status:: TODO", "status:: DONE", 1)
	require.NoError(t, os.WriteFile(path, []byte(updated), 0o644))

	after, err := BuildNodeWorkspace(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, storyBRef)
	require.NoError(t, err)
	require.NotEmpty(t, before.Version)
	require.NotEmpty(t, after.Version)
	require.Equal(t, before.Version, after.Version)

	changedSource := strings.Replace(updated, "### Story B\nstatus:: TODO", "### Story B\nstatus:: DONE", 1)
	require.NotEqual(t, updated, changedSource)
	updated = changedSource
	require.NoError(t, os.WriteFile(path, []byte(updated), 0o644))
	changed, err := BuildNodeWorkspace(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, storyBRef)
	require.NoError(t, err)
	require.NotEmpty(t, changed.Version)
	require.NotEqual(t, after.Version, changed.Version)
}

func TestBuildNodeWorkspaceFromProjection_NoteTitlePrefersH1OverSummary(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	notePath := "specs/001-test/spec.md"
	writeOntologyNote(t, root, notePath, `---
type: Spec
summary: Summary should stay in the summary field
---

# Real note title

## User Stories

### Story A
status:: TODO
^story-a
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	workspace, err := BuildNodeWorkspace(
		context.Background(),
		obsidian.VaultDefinition{Path: root},
		&obsidian.Note{},
		schema,
		NodeRef{NotePath: notePath, Kind: NodeKindNote},
	)
	require.NoError(t, err)
	require.Equal(t, "Real note title", workspace.Node.Title)
	var summaryValues []string
	for _, field := range workspace.Fields {
		if field.Name == "summary" {
			summaryValues = field.Values
			break
		}
	}
	require.Equal(t, []string{"Summary should stay in the summary field"}, summaryValues)
}

func TestBuildNodeWorkspaceFromProjection_MultipleH1sUseFilenameTitle(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	notePath := "specs/001-test/use-filename.md"
	writeOntologyNote(t, root, notePath, `---
type: Spec
summary: Summary should stay in the summary field
---

# Decision

First decision.

# Decision

Second decision.
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	workspace, err := BuildNodeWorkspace(
		context.Background(),
		obsidian.VaultDefinition{Path: root},
		&obsidian.Note{},
		schema,
		NodeRef{NotePath: notePath, Kind: NodeKindNote},
	)
	require.NoError(t, err)
	require.Equal(t, "use-filename", workspace.Node.Title)
}

func TestBuildNodeWorkspaceFromProjection_H1TitleIsPlainText(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, embeddedProjectionSchema)
	notePath := "notes/sync.md"
	writeOntologyNote(t, root, notePath, "# [[Tech Lead Sync]] **2022-05-12**\n\nBody.\n")

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	workspace, err := BuildNodeWorkspace(
		context.Background(),
		obsidian.VaultDefinition{Path: root},
		&obsidian.Note{},
		schema,
		NodeRef{NotePath: notePath, Kind: NodeKindNote},
	)
	require.NoError(t, err)
	require.Equal(t, "Tech Lead Sync 2022-05-12", workspace.Node.Title)
}

func TestBuildNodeWorkspace_EmbeddedCriterionParentUsesNearestSemanticNode(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `
enum UserStoryStatus { draft ready satisfied }

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(level: H5)
}

type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  verification: String @field
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  status: UserStoryStatus! @field
  acceptanceCriteria: AcceptanceCriteriaSection @contains(level: H4, heading: "Acceptance Criteria", required: true)
}

type ProductSpec @node(paths: ["docs/specs/product/*.md"]) {
  id: String! @field(source: "id")
  summary: String!
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories", required: true)
}
`)
	notePath := "docs/specs/product/browser.md"
	writeOntologyNote(t, root, notePath, `---
type: ProductSpec
summary: Product spec summary
id: SPEC-1000
---

# Browser spec

## User Stories

### Browse typed notes

id:: SPEC-1000.US1
status:: ready
^spec-1000-us1

#### Acceptance Criteria

##### Shows type families
id:: SPEC-1000.US1.AC1
^spec-1000-us1-ac1
`)

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	storyWorkspace, err := BuildNodeWorkspace(
		context.Background(),
		obsidian.VaultDefinition{Path: root},
		&obsidian.Note{},
		schema,
		NodeRef{NotePath: notePath, Fragment: "^spec-1000-us1", Kind: NodeKindEmbedded},
	)
	require.NoError(t, err)
	require.NotNil(t, storyWorkspace.Node.ParentRef)
	require.Equal(t, NodeKindNote, storyWorkspace.Node.ParentRef.Kind)
	require.Equal(t, notePath, storyWorkspace.Node.ParentRef.NotePath)

	criterionWorkspace, err := BuildNodeWorkspace(
		context.Background(),
		obsidian.VaultDefinition{Path: root},
		&obsidian.Note{},
		schema,
		NodeRef{NotePath: notePath, Fragment: "^spec-1000-us1-ac1", Kind: NodeKindEmbedded},
	)
	require.NoError(t, err)
	require.NotNil(t, criterionWorkspace.Node.ParentRef)
	require.Equal(t, NodeKindEmbedded, criterionWorkspace.Node.ParentRef.Kind)
	require.Equal(t, "UserStory", criterionWorkspace.Node.ParentRef.TypeName)
	require.Equal(t, "^spec-1000-us1", criterionWorkspace.Node.ParentRef.Fragment)
}

func TestBuildNodeVersion_TracksWorkspaceOnlyFields(t *testing.T) {
	base := &NodeWorkspace{
		Node: NodeDescriptor{
			Ref: NodeRef{
				NotePath: "specs/001-test/spec.md",
				Kind:     NodeKindNote,
			},
			NotePath: "specs/001-test/spec.md",
			Locator:  "FILE",
		},
		Content: NodeContent{Markdown: "# Spec"},
		Capabilities: NodeCapabilities{
			CanEdit:       true,
			CanSubscribe:  true,
			CanEditFields: true,
		},
		Status: NodeStatus{
			Validation: NodeValidationStatus{IssueCount: 1},
		},
	}

	before := buildNodeVersion(base)
	require.NotEmpty(t, before)

	changedCapabilities := *base
	changedCapabilities.Capabilities.CanSubscribe = false
	require.NotEqual(t, before, buildNodeVersion(&changedCapabilities))

	changedStatus := *base
	changedStatus.Status.Validation.IssueCount = 2
	require.NotEqual(t, before, buildNodeVersion(&changedStatus))
}
