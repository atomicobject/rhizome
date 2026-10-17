package schemadoc

import (
	"github.com/stretchr/testify/require"

	"strings"
	"testing"
)

func TestRenderMarkdown_RequestedTypeIncludesSupportingTypes(t *testing.T) {
	schema := testSchema(t)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Project"}})
	require.NoError(t, err)
	require.Contains(t, text, "# Ontology Authoring Guide")
	require.Contains(t, text, "## Project")
	require.Contains(t, text, "## Supporting Types")
	require.Contains(t, text, "### Person")
	require.NotContains(t, text, "### Decision")
	require.Contains(t, text, "`Decision`: referenced by `decisions`")
	require.Contains(t, text, "Before creating or substantially editing one, run `rzm agent ontology-authoring-guide --type Decision`.")
	require.Contains(t, text, "enum values: `ACTIVE`, `PAUSED`")
}

func TestRenderMarkdown_RelatedNeighborTypesUseCompactGuidance(t *testing.T) {
	schema := testCustomSchema(t, `
"""Reference docs preserve reusable context outside the story."""
type ReferenceDoc @node(paths: ["docs/reference/*.md"]) {
  summary: String!
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US", populate: ON_CREATE)
  references: [ReferenceDoc!] @neighbors(direction: OUTBOUND, type: "ReferenceDoc", scope: SUBTREE)
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"UserStory"}})
	require.NoError(t, err)

	require.NotContains(t, text, "### ReferenceDoc")
	require.Contains(t, text, "`ReferenceDoc`: referenced by `references`")
	require.Contains(t, text, "Reference docs preserve reusable context outside the story.")
	require.Contains(t, text, "Before creating or substantially editing one, run `rzm agent ontology-authoring-guide --type ReferenceDoc`.")
}

func TestRenderMarkdown_KeepsStoryIDAuthoredRequired(t *testing.T) {
	schema := testCustomSchema(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  """Required story identifier and block target."""
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US", populate: ON_CREATE)
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"UserStory"}})
	require.NoError(t, err)

	require.Contains(t, text, "`id`: required; inline property `id`; single ID value")
	require.Contains(t, text, "author and populate this field when creating the node")
	require.Contains(t, text, "id:: ")
}

func TestRenderMarkdown_EmbeddedNodesWithoutIdentifierExplainPlainLocators(t *testing.T) {
	schema := testCustomSchema(t, `
type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  verification: String @field
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"AcceptanceCriterion"}})
	require.NoError(t, err)

	require.Contains(t, text, "### Link Targets")
	require.Contains(t, text, "do not invent an `id::` property")
	require.Contains(t, text, "plain standalone `^block-id` locator")
	require.Contains(t, text, "leave uncited nodes without authored locators")
}

func TestRenderMarkdown_AllTypesSortedWhenNoFilter(t *testing.T) {
	schema := testSchema(t)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: nil})
	require.NoError(t, err)
	var headings []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "## ") {
			headings = append(headings, line)
		}
	}
	require.Equal(t, []string{"## Decision", "## Person", "## Project", "## Team"}, headings)
	require.NotContains(t, text, "## Supporting Types")
}

func TestRenderMarkdown_ProjectKBGuideCarriesTypeContract(t *testing.T) {
	schema := testCustomSchema(t, `
enum QuestionStatus {
  OPEN
  ANSWERED
}

"""
Project hub for one initiative.
Create one when the repo needs a durable center of gravity for goals, decisions, and open questions.
Agents should preserve the reading order and linked note families.
"""
type Project @node(paths: ["notes/projects/*.md"]) {
  """Short explanation of project scope and why this project note exists."""
  summary: String!
  """Open questions that should be read with the project."""
  openQuestions: [OpenQuestion!] @link(inverse: "project", contextInclude: true)
}

"""
Open question note for unresolved issues.
Create one when uncertainty should stay explicit instead of being buried in project prose.
"""
type OpenQuestion @node(paths: ["notes/questions/*.md"]) {
  """One-sentence statement of the unknown and why it matters."""
  summary: String!
  status: QuestionStatus!
  """Owning project for this question."""
  project: Project! @link(inverse: "openQuestions", contextInclude: true)
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Project"}})
	require.NoError(t, err)
	require.Contains(t, text, "Create one when the repo needs a durable center of gravity")
	require.Contains(t, text, "Agents should preserve the reading order")
	require.Contains(t, text, "default property case: `kebab`")
	require.Contains(t, text, "frontmatter key `open-questions`")
	require.Contains(t, text, "### OpenQuestion")
}

func TestRenderMarkdown_IncludesSourceAliases(t *testing.T) {
	schema := testCustomSchema(t, `
type Project @node(paths: ["notes/projects/*.md"]) {
  legacyStatus: String @field(sources: ["legacy-status", "legacyStatus"])
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Project"}})
	require.NoError(t, err)
	require.Contains(t, text, "frontmatter key `legacy-status`; also accepts `legacyStatus`")
}

func TestRenderGuideMarkdown_IncludesCompanionDocs(t *testing.T) {
	schema := testCustomSchema(t, `
"""
Project hub for one initiative.
"""
type Project
  @node(paths: ["notes/projects/*.md"])
  @companionDocs(paths: ["docs/reference/guides/Project workflow.md"], purpose: "workflow") {
  """Short explanation of project scope."""
  summary: String!
  """Project owner note."""
  owner: Person! @link(inverse: "projects") @companionDocs(paths: ["docs/reference/guides/Owner expectations.md"], purpose: "field guidance")
}

type Person @node(paths: ["notes/people/*.md"]) {
  name: String!
  projects: [Project!] @link(inverse: "owner")
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Project"}, ResolveCompanion: func(path string) (CompanionDocMeta, bool) {
		switch path {
		case "docs/reference/guides/Project workflow.md":
			return CompanionDocMeta{Summary: "How project, decision, and question notes stay aligned."}, true
		case "docs/reference/guides/Owner expectations.md":
			return CompanionDocMeta{Summary: "What ownership means for linked project notes."}, true
		default:
			return CompanionDocMeta{}, false
		}
	}})
	require.NoError(t, err)
	require.Contains(t, text, "### Companion Docs")
	require.Contains(t, text, "`workflow`: `docs/reference/guides/Project workflow.md` — How project, decision, and question notes stay aligned.")
	require.Contains(t, text, "companion docs: `field guidance`: `docs/reference/guides/Owner expectations.md` — What ownership means for linked project notes.")
}

func TestRenderMarkdown_IncludesSectionStructureGuidance(t *testing.T) {
	schema := testCustomSchema(t, `
type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Spec"}})
	require.NoError(t, err)
	require.Contains(t, text, "markdown heading")
	require.Contains(t, text, "heading `## Requirements`")
	require.Contains(t, text, "missing `requirements` raises `missing_required_section`")
	require.Contains(t, text, "## Requirements")
	require.Contains(t, text, "section type: heading-derived structure used inside parent note bodies")
}

func TestRenderMarkdown_DistinguishesEmbeddedNodesFromStructuralSections(t *testing.T) {
	schema := testCustomSchema(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  storyId: String! @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories", required: true)
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Spec"}})
	require.NoError(t, err)
	require.Contains(t, text, "section type: heading-derived structure used inside parent note bodies")

	embeddedText, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"UserStory"}})
	require.NoError(t, err)
	require.Contains(t, embeddedText, "embedded node type: persisted inside a parent note body but treated as a first-class node")
	require.Contains(t, embeddedText, "shape: `SECTION` — instances derive from matching markdown headings inside an owning note")
	require.Contains(t, embeddedText, "author embedded-node properties as metadata bullets by default")
	require.Contains(t, embeddedText, "- key:: value")
	require.Contains(t, embeddedText, "inline property `id`")
	require.Contains(t, embeddedText, "id::")
	require.Contains(t, embeddedText, "inline property `story-id`")
	require.Contains(t, embeddedText, "story-id::")
}

func TestRenderMarkdown_IncludesSharedInterfacesAndNestedSectionSkeletons(t *testing.T) {
	schema := testCustomSchema(t, `
interface SummaryDoc {
  """Shared summary."""
  summary: String!
}

type DetailsSection implements Section {
}

type RequirementsSection implements Section {
  details: DetailsSection @contains(level: H3, heading: "Details", required: true)
}

type Spec implements SummaryDoc @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Spec"}})
	require.NoError(t, err)
	require.Contains(t, text, "### SummaryDoc")
	require.Contains(t, text, "Shared summary.")
	require.Contains(t, text, "## Requirements\n\n### Details")
}

func TestRenderMarkdown_OmitsDefaultPaneDisplayFromContainsTooling(t *testing.T) {
	schema := testCustomSchema(t, `
type RequirementsSection implements Section {
}

type SummarySection implements Section {
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
  summary: SummarySection @contains(level: H2, heading: "Summary", display: INLINE)
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Spec"}})
	require.NoError(t, err)
	require.Contains(t, text, `tooling: @contains {"heading":"Requirements","level":"H2","required":true}`)
	require.NotContains(t, text, `tooling: @contains {"display":"PANE","heading":"Requirements","level":"H2","required":true}`)
	require.Contains(t, text, `tooling: @contains {"display":"INLINE","heading":"Summary","level":"H2","required":false}`)
}

func TestRenderMarkdown_IncludesGuidanceAndEnumPolicyForAuthoring(t *testing.T) {
	schema := testCustomSchema(t, `
"""Spec lifecycle state."""
enum SpecStatus {
  """Normal iteration."""
  active
  """Frozen execution contract."""
  frozen
    @guidance(
      meaning: "The spec is acting as a frozen contract for execution.",
      authoring: "Avoid semantic edits unless explicitly reopening the contract.",
      agentImplications: "Confirm with a user before making semantic changes."
    )
    @policy(
      requiresUserConfirmation: true,
      forbidAutonomousSemanticEdits: true,
      reason: "Frozen specs should not be changed autonomously."
    )
}

"""Spec note."""
type Spec
  @node(paths: ["notes/specs/*.md"])
  @guidance(
    authoring: "Keep the spec stable enough to freeze into an effort.",
    agentImplications: "Preserve lifecycle semantics while editing."
  ) {
  """Lifecycle state for the spec."""
  status: SpecStatus!
    @guidance(
      meaning: "Controls whether the spec is still evolving or frozen.",
      authoring: "Choose the narrowest truthful state.",
      agentImplications: "Use the enum value guidance before editing."
    )
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Spec"}})
	require.NoError(t, err)
	require.Contains(t, text, "Authoring guidance: Keep the spec stable enough to freeze into an effort.")
	require.Contains(t, text, "Agent implications: Preserve lifecycle semantics while editing.")
	require.Contains(t, text, "value `frozen`: Frozen execution contract.")
	require.Contains(t, text, "policy: requires user confirmation; forbids autonomous semantic edits; Frozen specs should not be changed autonomously.")
}

func TestRenderMarkdown_TreatsDerivableIdentifiersAsOptionalAuthoredLocators(t *testing.T) {
	schema := testCustomSchema(t, `
type Milestone implements Section @node(locator: EMBEDDED) {
  """Derived milestone identifier; author only when a durable locator is needed."""
  id: ID! @field @identifier(preferred: true, derivedSuffix: "MS")
  verification: String @field
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Milestone"}})
	require.NoError(t, err)

	require.Contains(t, text, "`id`: optional; inline property `id`; single ID value")
	require.NotContains(t, text, "missing `id` raises `missing_required_field`")
	require.NotContains(t, text, "id:: \nverification::")
}

func TestRenderMarkdown_EmbeddedCheckboxItemRendersCheckboxSkeleton(t *testing.T) {
	schema := testCustomSchema(t, `
"""Checkbox action item that can appear in any in-scope note."""
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item", paths: ["**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person! @link
  due: Date @field
}

type Person @node(paths: ["people/*.md"]) {
  displayName: String! @field
  aliases: [String!] @field
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"ActionItem"}})
	require.NoError(t, err)

	// Resolution narrative is shape-aware.
	require.Contains(t, text, "shape: `CHECKBOX_ITEM` — each instance is a markdown checkbox list item")
	require.Contains(t, text, "marker: every instance must include the literal tag `#action-item`")
	require.Contains(t, text, "do not add a `type:` frontmatter key")
	require.Contains(t, text, "indented continuation lines directly under the checkbox item")
	require.NotContains(t, text, "derive from matching markdown headings")

	// Source paths of `**/*.md` are noise — should not be listed.
	require.NotContains(t, text, "source paths: **/*.md")

	// Checkbox-source field is described as a checkbox state token, not a
	// frontmatter key with an empty source name.
	require.Contains(t, text, "`done`: required; checkbox state token (`[ ]` unchecked, `[x]` checked) on the host item; single Boolean value")
	require.NotContains(t, text, "frontmatter key ``")

	// Validation traps for the checkbox-source field are suppressed because
	// authors cannot omit or duplicate the `[ ]` token.
	require.NotContains(t, text, "missing `done` raises `missing_required_field`")
	require.NotContains(t, text, "multiple `done` values raise `field_shape_mismatch`")

	// Skeleton renders as a real checkbox item with the marker and indented
	// inline properties — not a frontmatter-wrapped note.
	require.Contains(t, text, "- [ ] <task description> #action-item\n  assignee:: \n  due:: ")
	require.NotContains(t, text, "type: ActionItem")

	// @link / @field annotation payloads strip default-valued args so the
	// tooling hint doesn't contradict the resolved location label
	// (e.g. claim `sourceKind:"FRONTMATTER"` on an inline-source field).
	require.NotContains(t, text, `@link {"contextInclude":false`)
	require.NotContains(t, text, `"sourceKind":"FRONTMATTER"`)
}

func TestRenderMarkdown_RendersArgumentlessWorkspaceMemberAnnotation(t *testing.T) {
	schema := testCustomSchema(t, `
type Material @node(paths: ["materials/*.md"]) {
  title: String
}

type Workspace @node(paths: ["workspaces/*.md"]) {
  materials: [Material!] @link @workspaceMember
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Workspace"}})
	require.NoError(t, err)
	require.Contains(t, text, "tooling: @link; @workspaceMember")
	require.NotContains(t, text, "@workspaceMember null")
}

func TestRenderMarkdown_EmbeddedListItemRendersListSkeleton(t *testing.T) {
	schema := testCustomSchema(t, `
"""Plain list-item embedded node."""
type Bookmark implements Section
  @node(locator: EMBEDDED)
  @source(shape: LIST_ITEM, marker: "#bookmark", paths: ["notes/**/*.md"]) {
  url: String! @field
}
`)

	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Bookmark"}})
	require.NoError(t, err)

	require.Contains(t, text, "shape: `LIST_ITEM` — each instance is a plain markdown list item")
	require.Contains(t, text, "marker: every instance must include the literal tag `#bookmark`")
	require.Contains(t, text, "source paths: notes/**/*.md")
	require.Contains(t, text, "- <item text> #bookmark\n  url:: ")
	require.NotContains(t, text, "type: Bookmark")
}

func TestRenderMarkdown_EmbeddedIdentifierGuidesBlockSafeInlineID(t *testing.T) {
	schema := testCustomSchema(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US", populate: ON_CREATE)
  summary: String! @field
}
`)
	text, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"UserStory"}})
	require.NoError(t, err)
	require.Contains(t, text, "### ID Allocation")
	require.Contains(t, text, "id:: ^<id>")
	require.Contains(t, text, "author the identifier on creation")
	require.NotContains(t, text, "rzm agent next-id --type UserStory")
}

var errExample = newErr("lookup unavailable")

type rendererTestErr string

func (e rendererTestErr) Error() string { return string(e) }

func newErr(s string) error { return rendererTestErr(s) }
