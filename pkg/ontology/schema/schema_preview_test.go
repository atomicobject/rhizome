package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSchema_CompilesPreviewOnSectionType(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stories.graphql"), []byte(`
enum StoryStatus { PLANNED IN_PROGRESS COMPLETE }

type Story implements Section @preview(template: "[[{{title}}]] - [[{{status}}]]", collapsed: true) {
  status: StoryStatus @field
  owner: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	story := schema.Types["Story"]
	require.NotNil(t, story)
	require.NotNil(t, story.Preview)
	require.Equal(t, "[[{{title}}]] - [[{{status}}]]", story.Preview.Template)
	require.Equal(t, []string{"title", "status"}, story.Preview.Placeholders)
	require.True(t, story.Preview.Collapsed)
}

func TestLoadSchema_PreviewDefaultsCollapsedTrue(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stories.graphql"), []byte(`
type Story implements Section @preview(template: "{{title}}") {
  owner: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.True(t, schema.Types["Story"].Preview.Collapsed)
}

// Regression: the section-prelude injector used to require `implements
// Section` and the opening `{` on the same line. Splitting the header across
// lines (a natural formatting for long directives like `@preview`) would
// silently skip the synthetic builtin-field injection and trip gqlparser's
// interface-conformance check instead. Multi-line headers must work.
func TestLoadSchema_CompilesMultiLineSectionHeader(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stories.graphql"), []byte(`
type Story
  implements Section
  @preview(template: "{{title}} · {{status}}", collapsed: true) {
  status: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	story := schema.Types["Story"]
	require.NotNil(t, story)
	require.NotNil(t, story.Preview)
	require.Equal(t, "{{title}} · {{status}}", story.Preview.Template)
}

func TestLoadSchema_RejectsPreviewOnNoteType(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Spec @node(paths: ["specs/*.md"]) @preview(template: "{{title}}") {
  summary: String!
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	requireCompileError(t, err, "preview_type_role")
}

func TestLoadSchema_RejectsPreviewUnknownPlaceholder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Story implements Section @preview(template: "{{title}} · {{nonexistent}}") {
  owner: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	requireCompileError(t, err, "preview_unknown_placeholder")
}

func TestLoadSchema_RejectsPreviewEmptyTemplate(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Story implements Section @preview(template: "") {
  owner: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: [Story!] @contains(level: H3)
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	requireCompileError(t, err, "preview_empty_template")
}

func TestLoadSchema_CompilesInterfaceInheritance(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "interfaces.graphql"), []byte(`
interface Entity {
  summary: String!
}

interface SummaryDoc implements Entity {
  summary: String!
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.Equal(t, []string{"Entity"}, schema.Interfaces["SummaryDoc"].Implements)
}

func TestLoadSchema_RejectsObjectMissingInheritedInterface(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ok.graphql"), []byte(`
interface Entity {
  summary: String!
}

interface SummaryDoc implements Entity {
  summary: String!
}

type Project implements SummaryDoc @node(paths: ["notes/projects/*.md"]) {
  summary: String!
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	requireGraphQLCompileError(t, err)
}

func TestLoadSchema_RejectsUnsupportedDefinitions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
input EntityInput {
  id: ID
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	requireCompileError(t, err, "unsupported_definition")
}

func TestLoadSchema_PreservesRetrievalAndTraversalAnnotations(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.graphql"), []byte(`
type Project
  @node(paths: ["notes/projects/*.md"])
  @retrieval(intents: ["search"], boost: 1.2, relationBoost: 1.1)
  @traversal(intents: ["related_to_seed"], includeAmbient: true, minStructuralHits: 2) {
  decisions: [Decision!] @neighbors(direction: BOTH, type: "Decision") @retrieval(intents: ["docs_for_code"], propertyBoost: 1.3)
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)
	project := schema.Types["Project"]
	require.NotNil(t, project)
	require.NotNil(t, project.Annotations["retrieval"])
	require.InDelta(t, 1.2, project.Annotations["retrieval"]["boost"], 0.0001)
	require.Equal(t, true, project.Annotations["traversal"]["includeAmbient"])
	require.EqualValues(t, 2, project.Annotations["traversal"]["minStructuralHits"])
	require.InDelta(t, 1.3, project.ByName["decisions"].Annotations["retrieval"]["propertyBoost"], 0.0001)
}

func TestLoadSchema_DefaultsPropertyNamesToKebabCaseAndSupportsOverrides(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.graphql"), []byte(`
type Project @node(paths: ["notes/projects/*.md"]) {
  openQuestions: [OpenQuestion!] @link(inverse: "project")
  ownerName: String
  legacyStatus: String @field(sources: ["legacy-status", "legacyStatus"])
}

type OpenQuestion @node(paths: ["notes/questions/*.md"]) {
  project: Project! @link(inverse: "openQuestions")
}

type CamelProject @node(paths: ["notes/camel/*.md"], propertyCase: CAMEL) {
  releaseStage: String
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	project := schema.Types["Project"]
	require.NotNil(t, project)
	require.Equal(t, PropertyCaseKebab, project.PropertyCase)
	require.Equal(t, "open-questions", project.ByName["openQuestions"].Source)
	require.Equal(t, "owner-name", project.ByName["ownerName"].Source)
	require.Equal(t, "legacy-status", project.ByName["legacyStatus"].Source)
	require.Equal(t, []string{"legacyStatus"}, project.ByName["legacyStatus"].SourceAliases)

	camel := schema.Types["CamelProject"]
	require.NotNil(t, camel)
	require.Equal(t, PropertyCaseCamel, camel.PropertyCase)
	require.Equal(t, "releaseStage", camel.ByName["releaseStage"].Source)
}

func TestLoadSchema_PreservesAuthoredEmbeddedIDField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "embedded.graphql"), []byte(`
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  status: String @field
}

type Spec @node(paths: ["specs/*.md"]) {
  stories: [UserStory!] @contains(level: H3)
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	story := schema.Types["UserStory"]
	require.NotNil(t, story)
	require.Contains(t, story.ByName, "id")
	require.Equal(t, FieldSourceInline, story.ByName["id"].SourceKind)
}

func TestLoadSchema_CompilesCompanionDocs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.graphql"), []byte(`
type Project
  @node(paths: ["notes/projects/*.md"])
  @companionDocs(paths: ["docs/reference/guides/Project workflow.md"], purpose: "workflow") {
  name: String!
  summary: String!
  owner: Person! @link(inverse: "projects") @companionDocs(paths: ["docs/reference/guides/Owner expectations.md"], purpose: "field guidance")
}

type Person @node(paths: ["notes/people/*.md"]) {
  name: String!
  projects: [Project!] @link(inverse: "owner")
}
`), 0o644))

	schema, err := LoadSchema(root)
	require.NoError(t, err)

	project := schema.Types["Project"]
	require.NotNil(t, project)
	require.Len(t, project.CompanionDocs, 1)
	require.Equal(t, "docs/reference/guides/Project workflow.md", project.CompanionDocs[0].Path)
	require.Equal(t, "workflow", project.CompanionDocs[0].Purpose)

	owner := project.ByName["owner"]
	require.NotNil(t, owner)
	require.Len(t, owner.CompanionDocs, 1)
	require.Equal(t, "docs/reference/guides/Owner expectations.md", owner.CompanionDocs[0].Path)
	require.Equal(t, "field guidance", owner.CompanionDocs[0].Purpose)
}
