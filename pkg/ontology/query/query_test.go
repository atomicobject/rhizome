package query

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"
)

func testNoteMetadataIndexer(t testing.TB) notemeta.Indexer {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}

func TestBuildExecutableSchema_IncludesBuiltinsAndNeighborFields(t *testing.T) {
	env := newQueryTestEnv(t)
	require.Contains(t, env.execSchema.SDL, "interface NoteNode")
	require.Contains(t, env.execSchema.SDL, `@semantics(kind: BEHAVIORAL)`)
	require.Contains(t, env.execSchema.SDL, `Project note doc`)
	require.Contains(t, env.execSchema.SDL, "linked(type: String, first: Int = 20): [NoteNode!]!")
	require.Contains(t, env.execSchema.SDL, "decisions(first: Int): [Decision!]")
	require.Contains(t, env.execSchema.SDL, "type Query")
	require.Contains(t, env.execSchema.SDL, "ontology: OntologyRuntime!")
	require.Contains(t, env.execSchema.SDL, "code: CodeRuntime!")
	require.NotContains(t, env.execSchema.SDL, "agent: AgentRuntime!")
	require.NotContains(t, env.execSchema.SDL, "type AgentRuntime")
	require.Contains(t, env.execSchema.SDL, "score: Float")
	require.Contains(t, env.execSchema.SDL, "format: String!")
	require.Contains(t, env.execSchema.SDL, "sourceRepresentation: String!")
	require.Contains(t, env.execSchema.SDL, "evidenceRepresentation: String!")
	require.Contains(t, env.execSchema.SDL, "sourceCapabilities: [String!]!")
	require.Contains(t, env.execSchema.SDL, "enum FieldFilterOperator")
	require.Contains(t, env.execSchema.SDL, "op: FieldFilterOperator = eq")
	require.Contains(t, env.execSchema.SDL, "enum SortDirection")
	require.Contains(t, env.execSchema.SDL, "direction: SortDirection = asc")
	require.Contains(t, env.execSchema.SDL, "project(path: String, find: String, property: PropertyFilterInput, semantic: [String!], first: Int = 20, offset: Int = 0, filters: [FieldFilterInput!], sort: [SortInput!]): [Project!]!")
	require.Equal(t, RuntimeRootOntology, env.execSchema.RuntimeRoots["ontology"])
}

func TestBuildExecutableSchema_RootOnlyOntologySupportsSectionFragments(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Report @node(paths: ["reports/*.html"]) {
  title: String!
}
`, nil)
	prepared, errs := Prepare(env.execSchema, `{
  node(ref: "reports/example.html") {
    ... on Section { content notePath level }
  }
}`)
	require.Empty(t, errs)
	require.NotNil(t, prepared)
}

func TestResolveNodeFieldDisclosesProviderSourceContract(t *testing.T) {
	executor := &executor{}
	node := &noteRecord{
		Format:                 "html",
		SourceRepresentation:   ontology.SourceRepresentationUTF8,
		EvidenceRepresentation: ontology.EvidenceRepresentationProviderProjection,
		Capabilities:           []noteformat.Capability{noteformat.CapabilitySourceReading, noteformat.CapabilityActiveContentViewing},
	}
	tests := map[string]any{
		"format":                 "html",
		"sourceRepresentation":   "source_utf8",
		"evidenceRepresentation": "provider_projection",
		"sourceCapabilities":     []string{"source_reading", "active_content_viewing"},
	}
	for name, expected := range tests {
		value, ok := executor.resolveNodeField(context.Background(), node, &ast.Field{Name: name}, nil)
		require.True(t, ok, name)
		require.Equal(t, expected, value, name)
	}
}

func TestExecute_RuntimeIdentifierStrategyMetadata(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Spec @node(paths: ["notes/specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
}
`, nil)
	require.Contains(t, env.execSchema.SDL, "enum IdentifierStrategy { SEQUENTIAL DATETIME }")
	require.Contains(t, env.execSchema.SDL, "strategy: IdentifierStrategy!")
	prepared, errs := Prepare(env.execSchema, `{
  ontology {
    type(name: "Spec") {
      fields {
        name
        identifierFormat { strategy prefix separator pad }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	fields := result.Data["ontology"].(map[string]any)["type"].(map[string]any)["fields"].([]any)
	require.Equal(t, map[string]any{
		"name": "id",
		"identifierFormat": map[string]any{
			"strategy":  "SEQUENTIAL",
			"prefix":    "SPEC",
			"separator": "-",
			"pad":       4,
		},
	}, fields[0])
}

func TestBuildExecutableSchema_AuthoredNoteInterfaceFieldsAreUniversalOnNoteNode(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface Note {
  summary: String
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}

type Scratch @node(paths: ["notes/scratch/*.md"]) {
  label: String
}
`, map[string]string{
		"notes/projects/roadmap.md": `---
type: Project
name: Roadmap
summary: Shared note field
---
`,
		"notes/scratch/plain.md": `---
type: Scratch
label: Plain
---
`,
	})

	require.Contains(t, env.execSchema.SDL, "interface Note implements Node")
	require.Contains(t, env.execSchema.SDL, "  summary: String")
	require.Contains(t, env.execSchema.SDL, "interface NoteNode implements Node & Note")
	require.Contains(t, env.execSchema.SDL, "type Project implements NoteNode & Node & Note")
	require.Contains(t, env.execSchema.SDL, "type Scratch implements NoteNode & Node & Note")
	require.Contains(t, env.execSchema.SDL, "  summary: String")
}

func TestBuildExecutableSchema_AuthoredNoteInterfaceSkipsRuntimeFieldDuplicates(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface Note {
  summary: String
  tags: [String!]
  content: String
  title: String
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}
`, map[string]string{
		"notes/projects/roadmap.md": `---
type: Project
name: Roadmap
summary: Shared note field
tags:
  - planning
---
# Roadmap
`,
	})

	require.Contains(t, env.execSchema.SDL, "interface NoteNode implements Node & Note")
	require.Contains(t, env.execSchema.SDL, "  summary: String")
	require.Contains(t, env.execSchema.SDL, "type Project implements NoteNode & Node & Note")
	noteNodeBlock := requireSDLBlock(t, env.execSchema.SDL, "interface NoteNode implements Node & Note")
	projectBlock := requireSDLBlock(t, env.execSchema.SDL, "type Project implements NoteNode & Node & Note")
	for _, block := range []string{noteNodeBlock, projectBlock} {
		require.Equal(t, 1, strings.Count(block, "\n  tags: [String!]!\n"))
		require.Equal(t, 1, strings.Count(block, "\n  content: String!\n"))
		require.Equal(t, 1, strings.Count(block, "\n  title: String!\n"))
	}
}

func requireSDLBlock(t *testing.T, sdl, prefix string) string {
	t.Helper()
	start := strings.Index(sdl, prefix)
	require.NotEqual(t, -1, start)
	rest := sdl[start:]
	end := strings.Index(rest, "\n}\n")
	require.NotEqual(t, -1, end)
	return rest[:end+3]
}

func TestBuildExecutableSchema_RuntimeRootsAreReserved(t *testing.T) {
	root := t.TempDir()
	writeQueryTestConfig(t, root)
	writeQueryTestSchema(t, root, `
type Ontology @node(paths: ["notes/ontology/*.md"]) {
  name: String!
}
`)
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	_, err = BuildExecutableSchema(schema)
	require.Error(t, err)
	require.Contains(t, err.Error(), `generated query root "ontology"`)
}

func TestBuildExecutableSchema_RuntimeHelperTypeNamesAreReserved(t *testing.T) {
	root := t.TempDir()
	writeQueryTestConfig(t, root)
	writeQueryTestSchema(t, root, `
type CodeRuntime @node(paths: ["notes/code-runtime/*.md"]) {
  name: String!
}
`)
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	_, err = BuildExecutableSchema(schema)
	require.Error(t, err)
	require.Contains(t, err.Error(), `ontology type "CodeRuntime" conflicts with built-in runtime GraphQL type`)
}

func TestExplicitNodePathRefFromString(t *testing.T) {
	structural, ok := explicitNodePathRefFromString("docs/playground/pizza-party-2026.md#struct:abc123")
	require.True(t, ok)
	require.Equal(t, "docs/playground/pizza-party-2026.md", structural.NotePath)
	require.Equal(t, ontology.NodeKindSection, structural.Kind)
	require.Equal(t, "abc123", structural.Structural)

	note, ok := explicitNodePathRefFromString("docs/playground/pizza-party-2026.md")
	require.True(t, ok)
	require.Equal(t, ontology.NodeKindNote, note.Kind)

	_, ok = explicitNodePathRefFromString("SPEC-0042")
	require.False(t, ok)

	_, ok = explicitNodePathRefFromString("[[docs/playground/pizza-party-2026.md]]")
	require.False(t, ok)

	_, ok = explicitNodePathRefFromString("http://localhost:5173/notes?note=docs/playground/pizza-party-2026.md#yard-prep")
	require.False(t, ok)
}

func TestExecutePrepared_DispatchesIntrospection(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `{ __type(name: "Project") { name kind } }`)
	require.Empty(t, errs)
	require.True(t, prepared.Introspection)

	result := ExecutePrepared(context.Background(), env.deps(nil), env.schema, env.execSchema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, "Project", result.Data["__type"].(map[string]any)["name"])
	require.Equal(t, "OBJECT", result.Data["__type"].(map[string]any)["kind"])
}

func TestExecutePrepared_IntrospectionOmitsQueryMetaFields(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `{ __type(name: "Query") { fields { name } } }`)
	require.Empty(t, errs)

	result := ExecutePrepared(context.Background(), env.deps(nil), env.schema, env.execSchema, prepared)
	require.Empty(t, result.Errors)
	queryType := result.Data["__type"].(map[string]any)
	fields := queryType["fields"].([]any)
	for _, raw := range fields {
		name := raw.(map[string]any)["name"]
		require.NotEqual(t, "__schema", name)
		require.NotEqual(t, "__type", name)
	}
}

func TestBuildExecutableSchema_EmitsDescriptionsFromAST(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
"""
Hub note for a subsystem.
Collects design + runbooks for shared context.
"""
type Hub @node(paths: ["notes/hubs/*.md"]) {
  """Primary heading shown in the vault."""
  name: String!
}
`, map[string]string{
		"notes/hubs/cache.md": `---
type: Hub
name: Cache Hub
---

Shared cache docs.
`,
	})
	require.Contains(t, env.execSchema.SDL, "Hub note for a subsystem.")
	require.Contains(t, env.execSchema.SDL, "Collects design + runbooks for shared context.")
	require.Contains(t, env.execSchema.SDL, "Primary heading shown in the vault.")
}

func TestBuildExecutableSchemaEmitsRelationCountsAndPreservesAuthoredCollision(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision")
  decisionsCount: String @field
  related: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision")
}
`, map[string]string{
		"notes/projects/a.md":    "---\ndecisionsCount: authored\n---\n",
		"notes/decisions/one.md": "# One\n",
	})

	require.Contains(t, env.execSchema.SDL, "relatedCount: Int!")
	require.Contains(t, env.execSchema.SDL, "Count of the related relation.")
	require.Equal(t, 1, strings.Count(env.execSchema.SDL, "decisionsCount: String"))
	require.NotContains(t, env.execSchema.SDL, "decisionsCount: Int!")
	prepared, errs := Prepare(env.execSchema, `{ __type(name: "Project") { fields { name } } }`)
	require.Empty(t, errs)
	result := ExecutePrepared(context.Background(), env.deps(nil), env.schema, env.execSchema, prepared)
	require.Empty(t, result.Errors)
	fieldNames := map[string]bool{}
	for _, raw := range result.Data["__type"].(map[string]any)["fields"].([]any) {
		fieldNames[raw.(map[string]any)["name"].(string)] = true
	}
	require.True(t, fieldNames["relatedCount"])
}

func TestBuildExecutableSchemaEmitsRelationCountsOnInterfaces(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Spec @node(paths: ["specs/*.md"]) {
  name: String
}

interface Portfolio {
  specs: [Spec!] @neighbors(direction: INBOUND, type: "Spec")
}

type FeatureArea implements Portfolio @node(paths: ["areas/*.md"]) {
  specs: [Spec!] @neighbors(direction: INBOUND, type: "Spec")
}
`, map[string]string{
		"areas/one.md": "# One\n",
		"specs/one.md": "# Spec\n",
	})
	require.Contains(t, env.execSchema.SDL, "interface Portfolio")
	require.Equal(t, 2, strings.Count(env.execSchema.SDL, "specsCount: Int!"))
}

func TestExecuteRelationCountResolvesAndBatchesAcrossParents(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision")
}
`, map[string]string{
		"notes/projects/alpha.md": "---\nname: Alpha\n---\nSee [[one]].\n",
		"notes/projects/beta.md":  "---\nname: Beta\n---\nSee [[one]] and [[two]].\n",
		"notes/decisions/one.md":  "---\nname: One\n---\n",
		"notes/decisions/two.md":  "---\nname: Two\n---\n",
	})
	prepared, errs := Prepare(env.execSchema, `{ project(find: "Project", first: 2) { name decisionsCount } }`)
	require.Empty(t, errs)
	spy := &relationBatchSpyStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = spy
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["project"].([]any)
	require.Equal(t, 1, rows[0].(map[string]any)["decisionsCount"])
	require.Equal(t, 2, rows[1].(map[string]any)["decisionsCount"])
	require.Equal(t, 1, spy.ambientCalls)
	require.Equal(t, 2, spy.maxAmbientSources)
}

func TestBuildExecutableSchema_EmitsEnumAndEnumValueDescriptions(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
"""Lifecycle for public API specs."""
enum SpecStatus {
  """Still being shaped."""
  PROPOSED
  """Ready for implementation."""
  ACTIVE
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  """Current lifecycle state."""
  status: SpecStatus!
}
`, map[string]string{
		"notes/specs/api.md": `---
status: PROPOSED
---
`,
	})

	require.Contains(t, env.execSchema.SDL, "Lifecycle for public API specs.")
	require.Contains(t, env.execSchema.SDL, "Still being shaped.")
	require.Contains(t, env.execSchema.SDL, "Ready for implementation.")
	require.Contains(t, env.execSchema.SDL, "Current lifecycle state.")
}

func TestBuildExecutableSchema_IncludesInterfacesAndSectionTypes(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface SummaryDoc {
  summary: String!
}

type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec implements SummaryDoc @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## Requirements

See [[decision-one]].
`,
		"notes/decisions/decision-one.md": `---
type: Decision
summary: Decision summary
---
`,
	})

	require.Contains(t, env.execSchema.SDL, "interface SummaryDoc")
	require.Contains(t, env.execSchema.SDL, "type RequirementsSection implements Section")
	require.Contains(t, env.execSchema.SDL, "requirements: RequirementsSection")
}

func TestBuildExecutableSchema_IncludesBuiltinsForEmbeddedSectionNodes(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  status: String @field
  related: Spec @link
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
aliases:
  - SPEC-ALIAS
---

## User Stories

### Story one

Status:: planned
`,
	})

	require.Contains(t, env.execSchema.SDL, "type UserStory implements Section")
	require.Contains(t, env.execSchema.SDL, "id: ID!")
	require.Contains(t, env.execSchema.SDL, "notePath: String!")
	require.Contains(t, env.execSchema.SDL, "locator: NodeLocator!")
	require.Contains(t, env.execSchema.SDL, "children(first: Int = 20): [Section!]!")
}

func TestExecute_ReturnsLocatorForNotesAndEmbeddedSections(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  status: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  related: [Spec!] @link(source: "related")
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
related:
  - notes/specs/related.md
---

## User Stories

### Story one
status:: planned
related:: notes/specs/related.md
^story-one
`,
		"notes/specs/related.md": `---
type: Spec
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    locator { status wikilink exists requiresFix }
    userStories {
      stories {
        locator { status wikilink markdown exists requiresFix linkTarget { blockId } }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	noteLocator := record["locator"].(map[string]any)
	require.Equal(t, "linkable", noteLocator["status"])
	require.Equal(t, "[[spec]]", noteLocator["wikilink"])
	require.Equal(t, true, noteLocator["exists"])
	require.Equal(t, false, noteLocator["requiresFix"])

	stories := record["userStories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 1)
	storyLocator := stories[0].(map[string]any)["locator"].(map[string]any)
	require.Equal(t, "linkable", storyLocator["status"])
	require.Equal(t, "[[spec#^story-one]]", storyLocator["wikilink"])
	require.Equal(t, "notes/specs/spec.md#^story-one", storyLocator["markdown"])
	require.Equal(t, true, storyLocator["exists"])
	require.Equal(t, false, storyLocator["requiresFix"])
	require.Equal(t, "story-one", storyLocator["linkTarget"].(map[string]any)["blockId"])
}

func TestExecute_ExposesCanonicalLocatorFixContract(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  status: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
---

## User Stories

### Story without a durable locator
status:: planned
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    userStories {
      stories {
        locator {
          ref { ref kind notePath nodeId typeName }
          kind
          status
          linkTarget { ref { ref kind notePath nodeId typeName } blockId requiresFix }
          diagnostics { code notePath ref { ref kind } blockId message }
          fixActions { ref { ref kind notePath nodeId typeName } blockId }
        }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	story := result.Data["spec"].([]any)[0].(map[string]any)["userStories"].(map[string]any)["stories"].([]any)[0].(map[string]any)
	locator := story["locator"].(map[string]any)
	require.Equal(t, "requires_fix", locator["status"])
	require.Equal(t, "EMBEDDED", locator["kind"])
	require.Equal(t, "EMBEDDED", locator["ref"].(map[string]any)["kind"])
	require.Equal(t, locator["ref"].(map[string]any)["ref"], locator["linkTarget"].(map[string]any)["ref"].(map[string]any)["ref"])
	require.Equal(t, true, locator["linkTarget"].(map[string]any)["requiresFix"])
	require.Equal(t, "missing_embedded_block_id", locator["diagnostics"].([]any)[0].(map[string]any)["code"])
	fixActions := locator["fixActions"].([]any)
	require.Len(t, fixActions, 1)
	require.Equal(t, locator["linkTarget"].(map[string]any)["blockId"], fixActions[0].(map[string]any)["blockId"])
	require.Equal(t, locator["ref"].(map[string]any)["ref"], fixActions[0].(map[string]any)["ref"].(map[string]any)["ref"])
}

func TestExecute_ListItemAcceptanceCriteriaUnderStory(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
enum UserStoryStatus { ready }

type AcceptanceCriterion implements Section @node(locator: EMBEDDED) {
  acTitle: String @field(sourceKind: ITEM_TITLE)
  summary: String! @field(sourceKind: ITEM_SUMMARY)
  detail: String @field(sourceKind: ITEM_DETAIL)
  verification: String @field
}

type AcceptanceCriteriaSection implements Section {
  criteria: [AcceptanceCriterion!] @contains(shape: LIST_ITEM)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  summary: String! @field
  status: UserStoryStatus! @field
  acceptanceCriteria: AcceptanceCriteriaSection @contains(level: H4, heading: "Acceptance Criteria", required: true)
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
---

## User Stories

### US1 - Unified context

- summary:: Search code and notes together.
- status:: ready

#### Acceptance Criteria

- **Mixed result handles**: Search returns mixed code-anchor and doc-section results.
  Scenario: docs and code both match
  Given a task mentions a known symbol
  When search runs
  Then code anchors and doc sections both appear
  verification:: go test ./pkg/search

  > [!example]- Gherkin
  > Scenario: exact symbol beats prose
  > Given a query exactly matches a symbol
  > When results are ranked
  > Then the symbol appears first

  - Given nested context
  - Then nested bullets remain detail

- Payloads truncate with continuation support.
`,
	})
	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    userStories {
      stories {
        title
        summary
        status
        acceptanceCriteria {
          criteria {
            title
            acTitle
            summary
            detail
            verification
          }
        }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	stories := rows[0].(map[string]any)["userStories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 1)
	require.Equal(t, "US1 - Unified context", stories[0].(map[string]any)["title"])
	require.Equal(t, "Search code and notes together.", stories[0].(map[string]any)["summary"])
	criteria := stories[0].(map[string]any)["acceptanceCriteria"].(map[string]any)["criteria"].([]any)
	require.Len(t, criteria, 2)
	firstCriterion := criteria[0].(map[string]any)
	require.Equal(t, "Mixed result handles", firstCriterion["title"])
	require.Equal(t, "Mixed result handles", firstCriterion["acTitle"])
	require.Equal(t, "Search returns mixed code-anchor and doc-section results.", firstCriterion["summary"])
	require.Contains(t, firstCriterion["detail"], "Scenario: docs and code both match")
	require.Contains(t, firstCriterion["detail"], "> [!example]- Gherkin")
	require.Contains(t, firstCriterion["detail"], "- Given nested context")
	require.NotContains(t, firstCriterion["detail"], "verification::")
	require.Equal(t, "go test ./pkg/search", firstCriterion["verification"])
	secondCriterion := criteria[1].(map[string]any)
	require.Equal(t, "Payloads truncate with continuation support.", secondCriterion["title"])
	require.Nil(t, secondCriterion["acTitle"])
	require.Equal(t, "Payloads truncate with continuation support.", secondCriterion["summary"])
}

func TestExecute_JSONPreservesSelectionOrderAndProjectsNestedHelperFields(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Spec @node(paths: ["notes/specs/*.md"]) {
  name: String
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
name: Public API
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    title
    path
    locator { wikilink }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	body := string(encoded)
	require.Contains(t, body, `"locator":{"wikilink":"[[spec]]"}`)
	require.NotContains(t, body[:strings.Index(body, `"extensions"`)], `"status"`)
	require.Less(t, strings.Index(body, `"title"`), strings.Index(body, `"path"`))
	require.Less(t, strings.Index(body, `"path"`), strings.Index(body, `"locator"`))
}

func TestExecute_BatchesNoteLocatorsForConnectionItems(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String @field
}
`, map[string]string{
		"notes/specs/a.md": `---
type: Spec
---
summary:: A
`,
		"notes/specs/b.md": `---
type: Spec
---
summary:: B
`,
		"notes/specs/c.md": `---
type: Spec
---
summary:: C
`,
	})
	prepared, errs := Prepare(env.execSchema, `{
  notes(type: "Spec", first: 3) {
    nodes {
      path
      locator { wikilink exists requiresFix }
    }
  }
}`)
	require.Empty(t, errs)

	reader := &countingNoteReader{inner: &obsidian.Note{}}
	deps := env.deps(nil)
	deps.NoteReader = reader
	deps.Service = ontology.NewService(obsidian.VaultDefinition{Path: env.root}, reader, env.store, env.schema)

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	connection := result.Data["notes"].(map[string]any)
	items := connection["nodes"].([]any)
	require.Len(t, items, 3)
	for _, item := range items {
		locator := item.(map[string]any)["locator"].(map[string]any)
		require.Equal(t, true, locator["exists"])
		require.Equal(t, false, locator["requiresFix"])
		require.NotEmpty(t, locator["wikilink"])
	}
	require.Equal(t, int64(1), reader.notesListCalls.Load())
}

func TestExecute_NodeRootResolvesNotesSectionsAndCode(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  status: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
---

## User Stories

### Story one
status:: planned
^story-one
`,
	})
	require.NoError(t, os.MkdirAll(filepath.Join(env.root, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(env.root, "src", "app.go"), []byte("package src\n\nfunc Run() {}\n"), 0o644))
	require.NoError(t, env.store.ReplaceIntelCodeFile(context.Background(), "src/app.go", []codeanchor.IntelAnchor{{
		AnchorID:    "go:src/app.go:Run",
		Lang:        codeanchor.LangGo,
		Kind:        string(codeanchor.SymFunc),
		Path:        "src/app.go",
		Symbol:      "Run",
		FQN:         "src.Run",
		Fingerprint: "run-fingerprint",
	}}, nil, nil))
	nodesByFragment, err := env.store.OntologyNodesByNoteFragments(context.Background(), []string{"notes/specs/spec.md#^story-one"})
	require.NoError(t, err)
	storyNodeID := nodesByFragment["notes/specs/spec.md#^story-one"].NodeID
	require.NotEmpty(t, storyNodeID)
	require.NoError(t, env.store.ReplaceOntologyEdgesForSources(context.Background(), []string{"notes/specs/spec.md"}, []codeanchorsqlite.OntologyEdgeRow{
		{SrcPath: "notes/specs/spec.md", RelationName: "related", DstPath: "notes/specs/related.md", DstType: "Spec", Provenance: "field", Structural: true},
		{SrcPath: "notes/specs/spec.md", SrcNodeID: storyNodeID, RelationName: "related", DstPath: "notes/specs/related.md", DstType: "Spec", Provenance: "field", Structural: true},
		{SrcPath: "notes/specs/spec.md", RelationName: "contains", DstPath: "notes/specs/spec.md", DstNodeID: storyNodeID, DstType: "UserStory", Provenance: "field", Structural: true},
	}))

	variables := map[string]any{
		"note":       "notes/specs/spec.md",
		"story":      "[[spec#^story-one]]",
		"code":       "code:src/app.go",
		"bareCode":   "src/app.go",
		"symbol":     "src/app.go#symbol:src.Run",
		"bareSymbol": "src/app.go#Run",
		"copyURL":    "http://localhost:5173/notes?note=notes/specs/spec.md#%5Estory-one",
	}
	prepared, errs := PrepareWithVariables(env.execSchema, `query NodeContract($note: String!, $story: String!, $code: String!, $bareCode: String!, $symbol: String!, $bareSymbol: String!, $copyURL: String!) {
  noteNode: node(ref: $note) {
    ref { ref kind path notePath typeName }
    nodeId
    nodeKind
    title
    resolvedType
    neighborhood(first: 10) {
      edges { source { kind notePath } target { kind notePath fragment typeName } direction relation structural targetType depth }
      nodes { nodeKind title resolvedType }
      truncated
    }
    localGraph(nodeLimit: 20, edgeLimit: 40) {
      nodes { id nodeKind title typeName sourceLocator }
      edges { source target kind relation structural }
      truncated
    }
    ... on Note {
      content
    }
  }
  storyNode: node(ref: $story) {
    ref { ref kind path notePath fragment typeName }
    nodeKind
    title
    resolvedType
    neighborhood(direction: BOTH, first: 10) {
      edges { source { kind } target { kind typeName } direction structural }
      truncated
    }
    localGraph(nodeLimit: 20, edgeLimit: 40) {
      nodes { id nodeKind title typeName }
      edges { source target kind relation structural }
    }
    ... on Section {
      notePath
      level
    }
  }
  codeNode: node(ref: $code) {
    ref { ref kind path }
    nodeKind
    title
    resolvedType
    locator { ref { ref kind path } kind linkTarget { ref { ref kind path } } }
    ... on CodeFile {
      language
      symbols { nodeKind title fqn language }
    }
  }
  bareCodeNode: node(ref: $bareCode) {
    ref { ref kind path }
    nodeKind
    resolvedType
  }
  symbolNode: node(ref: $symbol) {
    ref { ref kind path fragment }
    nodeKind
    resolvedType
    ... on CodeSymbol {
      fqn
      symbol
    }
  }
  bareSymbolNode: node(ref: $bareSymbol) {
    ref { ref kind path fragment }
    nodeKind
    resolvedType
    ... on CodeSymbol {
      fqn
      symbol
    }
  }
  resolved: resolve(ref: "src/app.go") {
    found
    kind
    path
    resolvedType
  }
  resolvedAlias: resolve(ref: "spec") {
    found
    ref { kind notePath typeName }
    path
    resolvedType
  }
  resolvedCopyURL: resolve(ref: $copyURL) {
    found
    ref { kind notePath fragment typeName }
    path
    resolvedType
  }
}`, variables)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	noteNode := result.Data["noteNode"].(map[string]any)
	require.Equal(t, "NOTE", noteNode["nodeKind"])
	require.Equal(t, "Spec", noteNode["resolvedType"])
	require.Equal(t, "notes/specs/spec.md", noteNode["ref"].(map[string]any)["notePath"])
	require.Contains(t, noteNode["content"], "User Stories")
	noteNeighborhood := noteNode["neighborhood"].(map[string]any)
	require.Equal(t, false, noteNeighborhood["truncated"])
	noteNeighborhoodEdges := noteNeighborhood["edges"].([]any)
	require.NotEmpty(t, noteNeighborhoodEdges)
	require.True(t, graphQLListContainsNestedValue(noteNeighborhoodEdges, "target", "kind", "EMBEDDED"))
	noteNeighborhoodNodes := noteNeighborhood["nodes"].([]any)
	require.IsType(t, []any{}, noteNeighborhoodNodes)
	noteLocalGraph := noteNode["localGraph"].(map[string]any)
	require.Equal(t, false, noteLocalGraph["truncated"])
	require.NotEmpty(t, noteLocalGraph["nodes"].([]any))
	require.NotEmpty(t, noteLocalGraph["edges"].([]any))

	storyNode := result.Data["storyNode"].(map[string]any)
	require.Equal(t, "EMBEDDED", storyNode["nodeKind"])
	require.Equal(t, "Story one", storyNode["title"])
	require.Equal(t, "UserStory", storyNode["resolvedType"])
	require.Equal(t, "notes/specs/spec.md", storyNode["notePath"])
	storyNeighborhood := storyNode["neighborhood"].(map[string]any)
	require.IsType(t, []any{}, storyNeighborhood["edges"].([]any))
	storyLocalGraph := storyNode["localGraph"].(map[string]any)
	require.NotEmpty(t, storyLocalGraph["nodes"].([]any))
	require.NotEmpty(t, storyLocalGraph["edges"].([]any))

	codeNode := result.Data["codeNode"].(map[string]any)
	require.Equal(t, "CODE_FILE", codeNode["nodeKind"])
	require.Equal(t, "CodeFile", codeNode["resolvedType"])
	require.Equal(t, "src/app.go", codeNode["ref"].(map[string]any)["ref"])
	codeLocator := codeNode["locator"].(map[string]any)
	require.Equal(t, "CODE_FILE", codeLocator["kind"])
	require.Equal(t, "src/app.go", codeLocator["ref"].(map[string]any)["ref"])
	require.Equal(t, codeLocator["ref"], codeLocator["linkTarget"].(map[string]any)["ref"])
	symbols := codeNode["symbols"].([]any)
	require.Len(t, symbols, 1)
	require.Equal(t, "Run", symbols[0].(map[string]any)["title"])
	require.Equal(t, "src.Run", symbols[0].(map[string]any)["fqn"])

	bareCodeNode := result.Data["bareCodeNode"].(map[string]any)
	require.Equal(t, "CODE_FILE", bareCodeNode["nodeKind"])
	require.Equal(t, "CodeFile", bareCodeNode["resolvedType"])
	require.Equal(t, "src/app.go", bareCodeNode["ref"].(map[string]any)["ref"])

	symbolNode := result.Data["symbolNode"].(map[string]any)
	require.Equal(t, "CODE_SYMBOL", symbolNode["nodeKind"])
	require.Equal(t, "CodeSymbol", symbolNode["resolvedType"])
	require.Equal(t, "src/app.go#symbol:src.Run", symbolNode["ref"].(map[string]any)["ref"])
	require.Equal(t, "src.Run", symbolNode["fqn"])
	require.Equal(t, "Run", symbolNode["symbol"])

	bareSymbolNode := result.Data["bareSymbolNode"].(map[string]any)
	require.Equal(t, "CODE_SYMBOL", bareSymbolNode["nodeKind"])
	require.Equal(t, "CodeSymbol", bareSymbolNode["resolvedType"])
	require.Equal(t, "src/app.go#symbol:src.Run", bareSymbolNode["ref"].(map[string]any)["ref"])
	require.Equal(t, "src.Run", bareSymbolNode["fqn"])
	require.Equal(t, "Run", bareSymbolNode["symbol"])

	resolved := result.Data["resolved"].(map[string]any)
	require.Equal(t, true, resolved["found"])
	require.Equal(t, "CODE_FILE", resolved["kind"])
	require.Equal(t, "CodeFile", resolved["resolvedType"])

	resolvedAlias := result.Data["resolvedAlias"].(map[string]any)
	require.Equal(t, true, resolvedAlias["found"])
	require.Equal(t, "notes/specs/spec.md", resolvedAlias["path"])
	require.Equal(t, "Spec", resolvedAlias["resolvedType"])

	resolvedCopyURL := result.Data["resolvedCopyURL"].(map[string]any)
	require.Equal(t, true, resolvedCopyURL["found"])
	require.Equal(t, "UserStory", resolvedCopyURL["resolvedType"])
	copyURLRef := resolvedCopyURL["ref"].(map[string]any)
	require.Equal(t, "EMBEDDED", copyURLRef["kind"])
	require.Equal(t, "^story-one", copyURLRef["fragment"])
}

func TestExecute_NodeWorkspaceProjectionIncludesDocumentAndCodeRelations(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/source.md"]) {
  name: String!
}
`, map[string]string{
		"notes/source.md": `---
name: Source
---
See [[connected]].
`,
		"notes/connected.md": "# Connected\n",
		"notes/backlink.md":  "See [[source]].\n",
	})
	ctx := context.Background()
	require.NoError(t, env.store.ReplaceDocLinksForPath(ctx, "pkg/roadmap.go", []codeanchor.DocLink{{
		SrcType: "code",
		SrcPath: "pkg/roadmap.go",
		DstKind: "note",
		DstPath: "notes/source.md",
	}}))

	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) {
    workspace {
      relationGroups { key items { ref { ref kind } provenance } }
    }
  }
}`, map[string]any{"ref": "notes/source.md"})
	require.Empty(t, errs)

	result := Execute(ctx, env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
	groups := workspace["relationGroups"].([]any)
	byKey := make(map[string][]any, len(groups))
	for _, raw := range groups {
		group := raw.(map[string]any)
		byKey[group["key"].(string)] = group["items"].([]any)
	}
	require.Equal(t, "notes/connected.md", byKey["connected"][0].(map[string]any)["ref"].(map[string]any)["ref"], "ordinary outbound wikilinks remain visible")
	require.Equal(t, "notes/backlink.md", byKey["backlinks"][0].(map[string]any)["ref"].(map[string]any)["ref"], "ordinary inbound wikilinks remain visible")
	require.NotEmpty(t, byKey["code"], "note-code links remain visible")
	codeRef := byKey["code"][0].(map[string]any)["ref"].(map[string]any)
	require.Equal(t, "CODE_FILE", codeRef["kind"])
	require.Equal(t, "pkg/roadmap.go", codeRef["ref"])
}

func TestExecute_NodeWorkspaceRelationGroupsTitleFragmentTargetsFromTheNode(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["notes/people/*.md"]) {
  displayName: String @field
}

type Plan @node(paths: ["notes/plan.md"]) {
  assignee: Person! @link
}

type Task implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#task", paths: ["notes/**/*.md"]) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person! @link
}
`, map[string]string{
		"notes/people/ada.md": "---\ndisplayName: Ada Lovelace\n---\n",
		"notes/plan.md":       "---\nassignee: '[[ada]]'\n---\n# Household plan\n\n- [ ] Order extra firewood #task assignee:: [[ada]]\n",
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) {
    workspace {
      relationGroups { key items { ref { ref kind notePath fragment } title relationName } }
    }
  }
}`, map[string]any{"ref": "notes/people/ada.md"})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
	groups := workspace["relationGroups"].([]any)
	var structural []any
	for _, raw := range groups {
		group := raw.(map[string]any)
		if group["key"] == "structural" {
			structural = group["items"].([]any)
		}
	}
	require.Len(t, structural, 2, "both document and embedded assignee relations are structural")
	var item map[string]any
	for _, raw := range structural {
		candidate := raw.(map[string]any)
		if candidate["ref"].(map[string]any)["kind"] == "NOTE" {
			require.Equal(t, "Household plan", candidate["title"], "an embedded title must not replace its document title")
		} else {
			item = candidate
		}
	}
	require.NotNil(t, item)
	ref := item["ref"].(map[string]any)
	require.Equal(t, "EMBEDDED", ref["kind"], "the target keeps its embedded node kind")
	require.Equal(t, "notes/plan.md", ref["notePath"], "the target note path stays separable from the fragment")
	require.NotEmpty(t, ref["fragment"])
	require.Equal(t, "assignee", item["relationName"])
	require.Equal(
		t,
		"Order extra firewood",
		item["title"],
		"a fragment target is titled from its own node, not from the raw fragment",
	)
	require.NotEqual(t, ref["fragment"], item["title"])
}

func TestExecute_NodeWorkspaceRelationGroupsTitleNoteTargetsFromTheNode(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/project.md"]) {
  plan: Material! @link
}

type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
}
`, map[string]string{
		"notes/project.md": `---
plan: "[[materials/opaque-slug]]"
---
# Project
`,
		"notes/materials/opaque-slug.md": `---
summary: Delivery plan
---
# Human-readable plan title
`,
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) {
    workspace {
      relationGroups { key items { ref { ref kind notePath } title relationName } }
    }
  }
}`, map[string]any{"ref": "notes/project.md"})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
	groups := workspace["relationGroups"].([]any)
	var structural []any
	for _, raw := range groups {
		group := raw.(map[string]any)
		if group["key"] == "structural" {
			structural = group["items"].([]any)
		}
	}
	require.Len(t, structural, 1)
	item := structural[0].(map[string]any)
	ref := item["ref"].(map[string]any)
	require.Equal(t, "NOTE", ref["kind"])
	require.Equal(t, "notes/materials/opaque-slug.md", ref["notePath"])
	require.Equal(t, "plan", item["relationName"])
	require.Equal(t, "Human-readable plan title", item["title"])
}

func TestExecute_NodeWorkspaceRelationGroupsBuildSchemaDeclaredNavigation(t *testing.T) {
	const sharedPlan = "# Shared delivery plan\n\n## Details\n\nImplementation detail.\n"
	env := newCustomQueryTestEnv(t, `
type Workspace @node(paths: ["notes/workspaces/*.md"]) @display(singular: "Effort", plural: "Efforts") {
  plan: Material! @link @workspaceMember(label: "Implementation plan")
  workLog: Material! @link @workspaceMember(label: "Work log")
  materials: [Material!] @link @workspaceMember
  governing: Material! @link
}

type DetailsSection implements Section {
}

type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
  details: DetailsSection @contains(level: H2, heading: "Details")
}
`, map[string]string{
		"notes/workspaces/alpha.md": `---
plan: "[[../materials/shared-plan]]"
work-log: "[[../materials/alpha-log]]"
materials:
  - "[[../materials/alpha-report]]"
governing: "[[../materials/governing]]"
---
# Alpha effort
`,
		"notes/workspaces/beta.md": `---
plan: "[[../materials/shared-plan]]"
work-log: "[[../materials/beta-log]]"
materials: []
governing: "[[../materials/governing]]"
---
# Beta effort
`,
		"notes/materials/shared-plan.md":  sharedPlan,
		"notes/materials/alpha-log.md":    "# Alpha execution log\n",
		"notes/materials/alpha-report.md": "# Verification report\n",
		"notes/materials/beta-log.md":     "# Beta execution log\n",
		"notes/materials/governing.md":    "# Governing contract\n",
	})

	query := `query Workspace($ref: String!) {
  node(ref: $ref) {
    workspace {
      relationGroups {
        key label ownerTitle navigation
        items { ref { notePath } title targetTitle relationName current }
      }
    }
  }
}`
	execute := func(ref string) []any {
		prepared, errs := PrepareWithVariables(env.execSchema, query, map[string]any{"ref": ref})
		require.Empty(t, errs)
		result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
		require.Empty(t, result.Errors)
		workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
		return workspace["relationGroups"].([]any)
	}

	ownerGroups := execute("notes/workspaces/alpha.md")
	var navigation, structural map[string]any
	for _, raw := range ownerGroups {
		group := raw.(map[string]any)
		if group["navigation"] == true {
			navigation = group
		}
		if group["key"] == "structural" {
			structural = group
		}
	}
	require.NotNil(t, navigation)
	require.NotNil(t, structural, "the unpromoted governing relation remains structural context")
	require.Equal(t, true, navigation["navigation"])
	require.Equal(t, "In this effort", navigation["label"])
	require.Equal(t, "Alpha effort", navigation["ownerTitle"])
	items := navigation["items"].([]any)
	require.Equal(t, []string{"Overview", "Implementation plan", "Work log", "Verification report"}, []string{
		items[0].(map[string]any)["title"].(string),
		items[1].(map[string]any)["title"].(string),
		items[2].(map[string]any)["title"].(string),
		items[3].(map[string]any)["title"].(string),
	})
	require.Equal(t, true, items[0].(map[string]any)["current"])
	require.Equal(t, "Shared delivery plan", items[1].(map[string]any)["targetTitle"])
	require.Equal(t, "structural", structural["key"])
	require.Equal(t, "Governing contract", structural["items"].([]any)[0].(map[string]any)["title"])

	childGroups := execute("notes/materials/shared-plan.md")
	ownerTitles := make([]string, 0, 2)
	for _, raw := range childGroups {
		group := raw.(map[string]any)
		if group["navigation"] != true {
			continue
		}
		require.Equal(t, true, group["navigation"])
		ownerTitles = append(ownerTitles, group["ownerTitle"].(string))
		var currentItems int
		for _, rawItem := range group["items"].([]any) {
			item := rawItem.(map[string]any)
			if item["current"] == true {
				currentItems++
				require.Equal(t, "Implementation plan", item["title"])
			}
		}
		require.Equal(t, 1, currentItems)
	}
	require.Len(t, ownerTitles, 2, "the shared material exposes each explicit owner separately")
	require.Equal(t, []string{"Alpha effort", "Beta effort"}, ownerTitles)

	sections := ontology.ParseSections("notes/materials/shared-plan.md", sharedPlan)
	require.Len(t, sections, 1)
	require.Len(t, sections[0].Children, 1)
	sectionGroups := execute(sections[0].Children[0].ID)
	var sectionOwners []string
	for _, raw := range sectionGroups {
		group := raw.(map[string]any)
		if group["navigation"] != true {
			continue
		}
		sectionOwners = append(sectionOwners, group["ownerTitle"].(string))
		var currentItems int
		for _, rawItem := range group["items"].([]any) {
			item := rawItem.(map[string]any)
			if item["current"] == true {
				currentItems++
				require.Equal(t, "Implementation plan", item["title"])
			}
		}
		require.Equal(t, 1, currentItems, "focusing a section keeps its containing member current")
	}
	require.Equal(t, ownerTitles, sectionOwners, "section focus preserves the document's workspace navigation")
}

func TestExecute_NodeWorkspaceNavigationPreservesRoleLabelsMatchingFilenames(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Workspace @node(paths: ["notes/Overview.md"]) {
  plan: Material! @link @workspaceMember(label: "Plan")
}
type Material @node(paths: ["notes/Plan.md"]) {
  summary: String
}
`, map[string]string{
		"notes/Overview.md": "---\nplan: '[[Plan]]'\n---\n# Delivery workspace\n",
		"notes/Plan.md":     "# Approved implementation details\n",
	})
	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) { workspace {
    relationGroups { navigation ownerTitle items { title targetTitle } }
  } }
}`, map[string]any{"ref": "notes/Overview.md"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
	groups := workspace["relationGroups"].([]any)
	require.Len(t, groups, 1)
	group := groups[0].(map[string]any)
	require.Equal(t, true, group["navigation"])
	require.Equal(t, "Delivery workspace", group["ownerTitle"])
	items := group["items"].([]any)
	require.Len(t, items, 2)
	require.Equal(t, "Overview", items[0].(map[string]any)["title"])
	require.Equal(t, "Delivery workspace", items[0].(map[string]any)["targetTitle"])
	require.Equal(t, "Plan", items[1].(map[string]any)["title"])
	require.Equal(t, "Approved implementation details", items[1].(map[string]any)["targetTitle"])
}

func TestExecute_NodeWorkspaceNavigationFiltersBeforeOwnerLimit(t *testing.T) {
	notes := map[string]string{
		"notes/z-workspaces/valid.md": "---\nplan: '[[../materials/shared]]'\n---\n# Valid effort\n",
		"notes/materials/shared.md":   "# Shared material\n",
		"notes/materials/other.md":    "# Other material\n",
	}
	for i := 0; i < workspaceNavigationMaxOwnerRelations+5; i++ {
		path := fmt.Sprintf("notes/a-noise-workspaces/noise-%03d.md", i)
		notes[path] = fmt.Sprintf("---\nplan: '[[../materials/shared]]'\nmaterials:\n  - '[[../materials/other]]'\n---\n# Noise %03d\n", i)
	}

	env := newCustomQueryTestEnv(t, `
type PlanWorkspace @node(paths: ["notes/z-workspaces/*.md"]) {
  plan: Material! @link @workspaceMember(label: "Plan")
}

type MaterialsWorkspace @node(paths: ["notes/a-noise-workspaces/*.md"]) {
  plan: Material! @link
  materials: [Material!] @link @workspaceMember
}

type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
}
`, notes)
	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) { workspace { relationGroups { navigation ownerTitle items { title } } } }
}`, map[string]any{"ref": "notes/materials/shared.md"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	groups := result.Data["node"].(map[string]any)["workspace"].(map[string]any)["relationGroups"].([]any)
	require.Condition(t, func() bool {
		for _, raw := range groups {
			group := raw.(map[string]any)
			if group["navigation"] == true && group["ownerTitle"] == "Valid effort" {
				return true
			}
		}
		return false
	}, "unannotated relations on another workspace type cannot consume the owner limit")
}

func TestExecute_NodeWorkspaceNavigationBatchesManyOwners(t *testing.T) {
	const ownerCount = 24
	notes := map[string]string{"notes/materials/shared.md": "# Shared plan\n"}
	for i := 0; i < ownerCount; i++ {
		notes[fmt.Sprintf("notes/workspaces/owner-%02d.md", i)] = fmt.Sprintf("---\nmember: '[[../materials/shared]]'\n---\n# Owner %02d\n", i)
	}
	env := newCustomQueryTestEnv(t, `
type Workspace @node(paths: ["notes/workspaces/*.md"]) {
  member: Material! @link @workspaceMember
}
type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
}
`, notes)
	spy := &workspaceNavigationSpyStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = spy
	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) { workspace { relationGroups { navigation ownerTitle items { title } } } }
}`, map[string]any{"ref": "notes/materials/shared.md"})
	require.Empty(t, errs)
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	groups := result.Data["node"].(map[string]any)["workspace"].(map[string]any)["relationGroups"].([]any)
	var navigationCount int
	for _, raw := range groups {
		if raw.(map[string]any)["navigation"] == true {
			navigationCount++
		}
	}
	require.Equal(t, ownerCount, navigationCount)
	require.Equal(t, 2, spy.memberCalls, "owner discovery and member expansion each use one relation read")
	require.Equal(t, ownerCount, spy.maxMemberSources, "all owners expand through one batched request")
}

func TestExecute_NodeWorkspaceNavigationReportsMemberTruncation(t *testing.T) {
	notes := make(map[string]string)
	var owner strings.Builder
	owner.WriteString("---\nmembers:\n")
	for i := 0; i <= workspaceNavigationMaxMemberRelationsPerOwner; i++ {
		owner.WriteString(fmt.Sprintf("  - '[[../materials/member-%03d]]'\n", i))
		notes[fmt.Sprintf("notes/materials/member-%03d.md", i)] = fmt.Sprintf("# Member %03d\n", i)
	}
	owner.WriteString("---\n# Large workspace\n")
	notes["notes/workspaces/large.md"] = owner.String()
	env := newCustomQueryTestEnv(t, `
type Workspace @node(paths: ["notes/workspaces/*.md"]) {
  members: [Material!] @link @workspaceMember
}
type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
}
`, notes)
	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) { workspace { relationGroups { navigation items { title } } } }
}`, map[string]any{"ref": "notes/workspaces/large.md"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.NotEmpty(t, result.Errors)
	require.Contains(t, result.Errors[0].Message, "more than 200 outbound membership relations")
}

func TestExecute_NodeWorkspaceNavigationReportsOwnerTruncation(t *testing.T) {
	notes := map[string]string{"notes/materials/shared.md": "# Shared plan\n"}
	for i := 0; i <= workspaceNavigationMaxOwnerRelations; i++ {
		notes[fmt.Sprintf("notes/workspaces/owner-%03d.md", i)] = fmt.Sprintf("---\nmember: '[[../materials/shared]]'\n---\n# Owner %03d\n", i)
	}
	env := newCustomQueryTestEnv(t, `
type Workspace @node(paths: ["notes/workspaces/*.md"]) {
  member: Material! @link @workspaceMember
}
type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
}
`, notes)
	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) { workspace { relationGroups { navigation items { title } } } }
}`, map[string]any{"ref": "notes/materials/shared.md"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.NotEmpty(t, result.Errors)
	require.Contains(t, result.Errors[0].Message, "more than 200 inbound membership relations")
}

func TestExecute_NodeWorkspaceNavigationAllowsExactOwnerLimitBeforeEmptyTypeBatch(t *testing.T) {
	notes := map[string]string{
		"notes/materials/shared.md":   "# Shared plan\n",
		"notes/materials/other.md":    "# Other plan\n",
		"notes/z-workspaces/empty.md": "---\nlog: '[[../materials/other]]'\n---\n# Unrelated workspace\n",
	}
	for i := 0; i < workspaceNavigationMaxOwnerRelations; i++ {
		notes[fmt.Sprintf("notes/a-workspaces/owner-%03d.md", i)] = fmt.Sprintf("---\nplan: '[[../materials/shared]]'\n---\n# Owner %03d\n", i)
	}
	env := newCustomQueryTestEnv(t, `
type AWorkspace @node(paths: ["notes/a-workspaces/*.md"]) {
  plan: Material! @link @workspaceMember
}
type ZWorkspace @node(paths: ["notes/z-workspaces/*.md"]) {
  log: Material! @link @workspaceMember
}
type Material @node(paths: ["notes/materials/*.md"]) {
  summary: String
}
`, notes)
	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) { workspace { relationGroups { navigation items { title } } } }
}`, map[string]any{"ref": "notes/materials/shared.md"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	groups := result.Data["node"].(map[string]any)["workspace"].(map[string]any)["relationGroups"].([]any)
	var navigationCount int
	for _, raw := range groups {
		if raw.(map[string]any)["navigation"] == true {
			navigationCount++
		}
	}
	require.Equal(t, workspaceNavigationMaxOwnerRelations, navigationCount)
}

func TestWorkspaceNavigationPromotionPreservesFullNodeIdentity(t *testing.T) {
	promoted := ontology.NodeRef{NotePath: "notes/material.md", Fragment: "shared", NodeID: "member", Kind: ontology.NodeKindEmbedded}
	unrelated := promoted
	unrelated.NodeID = "other"
	refs := workspaceNavigationRefSet([]workspaceRelationGroup{{Items: []workspaceRelationItem{{Ref: promoted}}}})
	require.True(t, workspaceNavigationContainsRef(refs, promoted))
	require.False(t, workspaceNavigationContainsRef(refs, unrelated), "matching author-facing locators do not collapse distinct embedded nodes")
}

func TestExecute_NodeWorkspaceProjectionUsesEditOverlay(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Story implements Section @node(locator: EMBEDDED) {
  status: String! @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

enum SpecStage {
  shaping @view(order: 10)
  ready_for_decision @view(label: "Ready for decision", order: 20, tone: "warning")
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String! @display(importance: KEY)
  stage: SpecStage
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
summary: Before
stage: shaping
---

## Stories

### First story
status:: planned
^story-one
`,
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query Workspace($ref: String!) {
  node(ref: $ref) {
    workspace {
      fields {
        name
        present
        values
        capability {
          ownerRef { ref kind typeName }
          ownerType
          typeName
          valueKind
          list
          required
          enumValues
          enumOptions { value label tone }
          targetType
          sourceKind
          valueOrigin
          identifier
          preferredIdentifier
          displayImportance
          writeOperation
          readOnlyReason
        }
        issues { code message }
        status { validation { issueCount } }
      }
      collections { name items { ref { ref kind nodeId } } }
      capabilities { canEdit canEditCollections }
      status { validation { issueCount } hasWarnings }
      version
      sourceRevision { notePath contentFingerprint content }
      assessment { resolvedType fields { name values validValues } }
      structure { ref { ref kind nodeId } parentRef { ref } title level content }
      relationGroups { key label items { ref { ref } title relationName provenance structural } }
      loaded { rendered assessment structure relations }
    }
    ... on Note { content }
  }
}`, map[string]any{"ref": "notes/specs/spec.md"})
	require.Empty(t, errs)

	deps := env.deps(nil)
	deps.ReadOverlay = &ReadOverlay{SourceFormat: "markdown",
		UpdatedContentByPath: map[string]string{
			"notes/specs/spec.md": `---
summary: After
---

## Stories

### First story
status:: done
^story-one
`,
		},
		TouchedPaths: []string{"notes/specs/spec.md"},
		Status:       "dirty",
	}
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	node := result.Data["node"].(map[string]any)
	require.Contains(t, node["content"], "summary: After")
	workspace := node["workspace"].(map[string]any)
	require.NotEmpty(t, workspace["version"])
	sourceRevision := workspace["sourceRevision"].(map[string]any)
	require.Equal(t, "notes/specs/spec.md", sourceRevision["notePath"])
	require.NotEmpty(t, sourceRevision["contentFingerprint"])
	require.Contains(t, sourceRevision["content"], "summary: After")
	require.NotEmpty(t, workspace["fields"].([]any))
	require.NotEmpty(t, workspace["structure"].([]any))
	loaded := workspace["loaded"].(map[string]any)
	require.Equal(t, true, loaded["rendered"])
	require.Equal(t, true, loaded["assessment"])
	require.Equal(t, true, loaded["structure"])
	require.Equal(t, true, loaded["relations"])
	fields := workspace["fields"].([]any)
	var summaryValues []string
	var stageOptions any
	for _, raw := range fields {
		field := raw.(map[string]any)
		if field["name"] == "summary" {
			summaryValues = field["values"].([]string)
			capability := field["capability"].(map[string]any)
			require.Equal(t, "Spec", capability["ownerType"])
			require.Equal(t, "String", capability["typeName"])
			require.Equal(t, "text", capability["valueKind"])
			require.Equal(t, "setField", capability["writeOperation"])
			require.Equal(t, "KEY", capability["displayImportance"])
			require.Equal(t, "notes/specs/spec.md", capability["ownerRef"].(map[string]any)["ref"])
		}
		if field["name"] == "stage" {
			stageOptions = field["capability"].(map[string]any)["enumOptions"]
		}
	}
	require.Equal(t, []string{"After"}, summaryValues)
	require.Equal(t, []any{
		map[string]any{"value": "shaping", "label": "shaping", "tone": "neutral", "stage": "open"},
		map[string]any{"value": "ready_for_decision", "label": "Ready for decision", "tone": "warning", "stage": "open"},
	}, stageOptions)
}

func TestExecute_NodeWorkspaceProjectionBodiesPreserveStructuralDescendants(t *testing.T) {
	const specContent = `---
summary: Test
---

# Spec

## User Stories

### Story A
id:: STORY-A
status:: planned
summary:: Build the workflow
^story-a

#### Acceptance Criteria

Must pass.
`
	env := newCustomQueryTestEnv(t, `
type AcceptanceCriteria implements Section {
}

type UserStory implements Section @node(locator: EMBEDDED) @preview(template: "{{ title }} — {{summary}}", collapsed: true) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
  summary: String @field
  optional: String @field
  acceptanceCriteria: AcceptanceCriteria @contains(level: H4, heading: "Acceptance Criteria", display: INLINE)
}

type UserStories implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String! @field
  userStories: UserStories @contains(level: H2, heading: "User Stories")
}
	`, map[string]string{
		"notes/specs/spec.md": specContent,
	})
	require.NotNil(t, env.schema.Types["UserStory"].Preview)

	prepared, errs := PrepareWithVariables(env.execSchema, `query WorkspaceBodies($note: String!, $story: String!) {
  note: node(ref: $note) {
    workspace { bodies(first: 20) { ref { ref kind nodeId typeName } title resolvedType locator parentRef { ref } markdown binding { typeName fieldName fieldPath fieldList sectionDisplay properties identifierField previewTemplate collapsed } fields { name values } collections { name items { ref { ref } } } blocks { kind range { start end } markdown fieldName rawKey childRef { ref kind nodeId typeName } childRefs { ref } sectionDisplay } } }
  }
  section: spec(path: $note) {
    userStories {
      workspace { bodies(first: 20) { ref { ref kind nodeId typeName } title resolvedType locator parentRef { ref } markdown binding { typeName fieldName fieldPath fieldList sectionDisplay properties identifierField previewTemplate collapsed } fields { name values } collections { name items { ref { ref } } } blocks { kind range { start end } markdown fieldName rawKey childRef { ref kind nodeId typeName } childRefs { ref } sectionDisplay } } }
    }
  }
  story: node(ref: $story) {
    workspace { parentRef { ref kind notePath nodeId typeName } bodies(first: 20) { ref { ref kind nodeId typeName } title resolvedType locator parentRef { ref } markdown binding { typeName fieldName fieldPath fieldList sectionDisplay properties identifierField previewTemplate collapsed } fields { name values } collections { name items { ref { ref } } } blocks { kind range { start end } markdown fieldName rawKey childRef { ref kind nodeId typeName } childRefs { ref } sectionDisplay } } }
  }
  limited: node(ref: $note) {
    workspace { bodies(first: 1) { ref { kind } } }
  }
  empty: node(ref: $note) {
    workspace { bodies(first: 0) { ref { kind } } }
  }
}`, map[string]any{
		"note":  "notes/specs/spec.md",
		"story": "notes/specs/spec.md#^story-a",
	})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	noteBodies := result.Data["note"].(map[string]any)["workspace"].(map[string]any)["bodies"].([]any)
	require.Len(t, noteBodies, 4, "structural descendants carry metadata even when their bodies are not inline")
	noteBody := noteBodies[0].(map[string]any)
	require.Equal(t, "NOTE", noteBody["ref"].(map[string]any)["kind"])
	require.Equal(t, "FILE", noteBody["locator"])
	require.Contains(t, noteBody["blocks"].([]any)[0].(map[string]any)["childRef"].(map[string]any)["ref"], "#")

	section := result.Data["section"].([]any)[0].(map[string]any)
	sectionBodies := section["userStories"].(map[string]any)["workspace"].(map[string]any)["bodies"].([]any)
	require.Len(t, sectionBodies, 3)
	sectionBody := sectionBodies[0].(map[string]any)
	require.Equal(t, "SECTION", sectionBody["ref"].(map[string]any)["kind"])
	collections := sectionBody["collections"].([]any)
	require.Len(t, collections, 1)
	require.Equal(t, "stories", collections[0].(map[string]any)["name"])
	collectionStory := sectionBodies[1].(map[string]any)
	require.Empty(t, collectionStory["blocks"].([]any), "PANE collection rows keep metadata without eagerly loading a nested body")
	collectionBinding := collectionStory["binding"].(map[string]any)
	require.Equal(t, "stories", collectionBinding["fieldName"])
	require.Equal(t, true, collectionBinding["fieldList"])
	require.Equal(t, map[string]string{"id": "STORY-A", "status": "planned", "summary": "Build the workflow"}, collectionBinding["properties"])
	require.Equal(t, "Story A — Build the workflow", collectionBinding["previewTemplate"])
	require.Equal(t, true, collectionBinding["collapsed"])

	storyBodies := result.Data["story"].(map[string]any)["workspace"].(map[string]any)["bodies"].([]any)
	storyWorkspace := result.Data["story"].(map[string]any)["workspace"].(map[string]any)
	storyParent := storyWorkspace["parentRef"].(map[string]any)
	require.Equal(t, "NOTE", storyParent["kind"])
	require.Equal(t, "notes/specs/spec.md", storyParent["notePath"])
	require.Equal(t, "Spec", storyParent["typeName"])
	require.Len(t, storyBodies, 2, "INLINE child sections receive a body payload for recursive walkers")
	storyBody := storyBodies[0].(map[string]any)
	require.Equal(t, "EMBEDDED", storyBody["ref"].(map[string]any)["kind"])
	require.Equal(t, "EMBEDDED", storyBody["locator"])
	storyFields := storyBody["fields"].([]any)
	var storyID []string
	var optionalValues []string
	for _, raw := range storyFields {
		field := raw.(map[string]any)
		if field["name"] == "id" {
			storyID = field["values"].([]string)
		}
		if field["name"] == "optional" {
			optionalValues = field["values"].([]string)
		}
	}
	require.Equal(t, []string{"STORY-A"}, storyID)
	require.NotNil(t, optionalValues, "non-null GraphQL lists must encode missing field values as []")
	require.Empty(t, optionalValues)
	storyBlocks := storyBody["blocks"].([]any)
	inlineChild := storyBlocks[len(storyBlocks)-1].(map[string]any)
	require.Equal(t, "child_section", inlineChild["kind"])
	require.Equal(t, "INLINE", inlineChild["sectionDisplay"])

	acceptanceBody := storyBodies[1].(map[string]any)
	require.Equal(t, "Acceptance Criteria", acceptanceBody["title"])
	require.Equal(t, "SECTION", acceptanceBody["ref"].(map[string]any)["kind"])
	require.Contains(t, acceptanceBody["markdown"], "Must pass.")

	limitedBodies := result.Data["limited"].(map[string]any)["workspace"].(map[string]any)["bodies"].([]any)
	require.Len(t, limitedBodies, 1)
	emptyBodies := result.Data["empty"].(map[string]any)["workspace"].(map[string]any)["bodies"].([]any)
	require.Empty(t, emptyBodies)
}

func TestExecute_NodeWorkspaceSourceLinksPreserveAuthoredOrderAndResolution(t *testing.T) {
	const ignoredCodeLink = "`[[ignored-code]]`"
	env := newCustomQueryTestEnv(t, `
type Focus implements Section {
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  focus: Focus @contains(level: H2, heading: "Focus")
}

type Target @node(paths: ["notes/target-a.md"]) {
  summary: String @field
}
`, map[string]string{
		"notes/specs/source.md": `---
type: Spec
---

[[target-a|First]] then [Second](../target-b.md#Heading) then ![[assets/image.png]] then ![Diagram](assets/diagram.png) then [[missing]] then [[target-a]] then [[#Focus]] then [[#^source-block]], and [external](https://example.com) ` + ignoredCodeLink + `.

## Focus

[[section-target]]
^source-block
`,
		"notes/target-a.md": `---
type: Target
---

# Target A

summary:: Indexed target preview

^target-a-block
`,
		"notes/target-b.md":       "# Target B\n",
		"notes/section-target.md": "# Section target\n",
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query WorkspaceSourceLinks($note: String!) {
  note: node(ref: $note) {
    workspace {
      sourceLinks(first: 20) { target authoredTarget text kind anchor embed targetKind resolved resolvedRef { ref kind notePath fragment } title preview }
      limited: sourceLinks(first: 3) { authoredTarget }
	  empty: sourceLinks(first: 0) { authoredTarget }
    }
  }
  section: spec(path: $note) {
    focus { workspace { sourceLinks(first: 20) { target authoredTarget resolved } } }
  }
}`, map[string]any{
		"note": "notes/specs/source.md",
	})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	noteWorkspace := result.Data["note"].(map[string]any)["workspace"].(map[string]any)
	links := noteWorkspace["sourceLinks"].([]any)
	require.Len(t, links, 9, "each authored link remains visible, including duplicate targets, embeds, same-note anchors, and descendant content")
	authoredTargets := make([]string, 0, len(links))
	for _, raw := range links {
		authoredTargets = append(authoredTargets, raw.(map[string]any)["authoredTarget"].(string))
	}
	require.Equal(t, []string{"target-a", "../target-b.md#Heading", "assets/image.png", "assets/diagram.png", "missing", "target-a", "#Focus", "#^source-block", "section-target"}, authoredTargets)
	first := links[0].(map[string]any)
	require.Equal(t, "notes/target-a.md", first["target"])
	require.Equal(t, "First", first["text"])
	require.Equal(t, "wikilink", first["kind"])
	require.Equal(t, "note", first["targetKind"])
	require.Equal(t, true, first["resolved"])
	require.Equal(t, "notes/target-a.md", first["resolvedRef"].(map[string]any)["notePath"])
	require.Equal(t, "Indexed target preview", first["preview"])
	second := links[1].(map[string]any)
	require.Equal(t, "notes/target-b.md#Heading", second["target"])
	require.Equal(t, "mdlink", second["kind"])
	require.Equal(t, "Heading", second["anchor"])
	require.Equal(t, true, second["resolved"])
	firstEmbed := links[2].(map[string]any)
	require.Equal(t, true, firstEmbed["embed"])
	require.Equal(t, "attachment", firstEmbed["targetKind"])
	secondEmbed := links[3].(map[string]any)
	require.Equal(t, true, secondEmbed["embed"])
	missing := links[4].(map[string]any)
	require.Equal(t, false, missing["resolved"])
	require.Nil(t, missing["resolvedRef"])
	sameHeading := links[6].(map[string]any)
	require.Equal(t, true, sameHeading["resolved"])
	require.Equal(t, "notes/specs/source.md#Focus", sameHeading["target"])
	require.Equal(t, "Focus", sameHeading["anchor"])
	sameBlock := links[7].(map[string]any)
	require.Equal(t, true, sameBlock["resolved"])
	require.Equal(t, "notes/specs/source.md#^source-block", sameBlock["target"])
	require.Equal(t, "^source-block", sameBlock["anchor"])
	limited := noteWorkspace["limited"].([]any)
	require.Len(t, limited, 3)
	require.Empty(t, noteWorkspace["empty"].([]any))
	section := result.Data["section"].([]any)[0].(map[string]any)["focus"].(map[string]any)
	sectionLinks := section["workspace"].(map[string]any)["sourceLinks"].([]any)
	require.Len(t, sectionLinks, 1)
	require.Equal(t, "section-target", sectionLinks[0].(map[string]any)["authoredTarget"])

	markdownDisabledDeps := env.deps(nil)
	markdownDisabledDeps.VaultDef.Links = obsidian.LinkTypeWikilinks
	markdownDisabled := Execute(context.Background(), markdownDisabledDeps, env.schema, prepared)
	require.Empty(t, markdownDisabled.Errors)
	disabledLinks := markdownDisabled.Data["note"].(map[string]any)["workspace"].(map[string]any)["sourceLinks"].([]any)
	disabledMarkdown := disabledLinks[1].(map[string]any)
	require.Equal(t, "../target-b.md#Heading", disabledMarkdown["target"])
	require.Equal(t, false, disabledMarkdown["resolved"])
	require.Nil(t, disabledMarkdown["resolvedRef"])

	t.Run("public source link cap", func(t *testing.T) {
		var body strings.Builder
		body.WriteString("# Source\n\n")
		for i := range 101 {
			fmt.Fprintf(&body, "[[target|Link %03d]]\n", i)
		}
		overflowEnv := newCustomQueryTestEnv(t, `type Source @node(paths: ["notes/source.md"]) { title: String }`, map[string]string{
			"notes/source.md": body.String(),
			"notes/target.md": "# Target\n",
		})
		prepared, errs := Prepare(overflowEnv.execSchema, `{
  node(ref: "notes/source.md") { workspace {
    none: sourceLinks(first: 0) { text }
    one: sourceLinks(first: 1) { text }
    overflow: sourceLinks(first: 101) { text }
  } }
}`)
		require.Empty(t, errs)
		result := Execute(context.Background(), overflowEnv.deps(nil), overflowEnv.schema, prepared)
		require.Empty(t, result.Errors)
		workspace := result.Data["node"].(map[string]any)["workspace"].(map[string]any)
		require.Empty(t, workspace["none"].([]any))
		require.Equal(t, []any{map[string]any{"text": "Link 000"}}, workspace["one"])
		links := workspace["overflow"].([]any)
		require.Len(t, links, 100)
		require.Equal(t, "Link 000", links[0].(map[string]any)["text"])
		require.Equal(t, "Link 099", links[99].(map[string]any)["text"])
	})
}

func TestExecute_NodeRefPrefersUntypedMarkdownNoteOverCodeFile(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String @field
}
`, map[string]string{
		"notes/task-flow.md": `---
title: Task Flow
---

# Task Flow

Task creation validates required fields.
`,
	})
	require.NoError(t, env.store.ReplaceIntelCodeFile(context.Background(), "notes/task-flow.md", nil, nil, nil))

	prepared, errs := PrepareWithVariables(env.execSchema, `query PublicNodeDetail($ref: String!) {
  node(ref: $ref) {
    nodeKind
    title
    path
    resolvedType
    ... on Note {
      content
    }
    ... on CodeFile {
      language
    }
  }
  resolved: resolve(ref: $ref) {
    found
    kind
    path
    resolvedType
  }
}`, map[string]any{"ref": "notes/task-flow.md"})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	node := result.Data["node"].(map[string]any)
	require.Equal(t, "NOTE", node["nodeKind"])
	require.Equal(t, "Task Flow", node["title"])
	require.Equal(t, "notes/task-flow.md", node["path"])
	require.Equal(t, "", node["resolvedType"])
	require.Contains(t, node["content"], "Task creation validates required fields.")
	require.NotContains(t, node, "language")

	resolved := result.Data["resolved"].(map[string]any)
	require.Equal(t, true, resolved["found"])
	require.Equal(t, "NOTE", resolved["kind"])
	require.Equal(t, "notes/task-flow.md", resolved["path"])
	require.Equal(t, "", resolved["resolvedType"])
}

func TestExecute_NodeRefDoesNotDefaultUnknownPathToCode(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String @field
}
`, nil)
	require.NoError(t, os.MkdirAll(filepath.Join(env.root, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(env.root, "src", "unknown.go"), []byte("package src\n"), 0o644))

	prepared, errs := PrepareWithVariables(env.execSchema, `query UnknownPath($ref: String!) {
  node(ref: $ref) { nodeKind }
  explicitCode: node(ref: "code:src/unknown.go") { nodeKind }
}`, map[string]any{"ref": "src/unknown.go"})
	require.Empty(t, errs)

	deps := env.deps(nil)
	deps.PathOwner = func(string) PathOwner { return PathOwnerUnknown }
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Nil(t, result.Data["node"])
	require.Nil(t, result.Data["explicitCode"])
}

func TestExecute_NodeRefResolvesIdentifierBackedEmbeddedNodeOnMarkdownCodePath(t *testing.T) {
	env := newEmbeddedDeepLinkQueryTestEnv(t)

	prepared, errs := PrepareWithVariables(env.execSchema, `query PublicNodeDetail($ref: String!) {
  node(ref: $ref) {
    ref { ref kind notePath fragment typeName }
    nodeKind
    title
    resolvedType
    ... on UserStory { id status }
    ... on CodeFile { language }
  }
  resolved: resolve(ref: $ref) {
    found
    reason
    kind
    resolvedType
    ref { ref kind notePath fragment typeName }
  }
}`, map[string]any{"ref": "docs/specs/technical/php-language-support.md#^SPEC-0068-US1"})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	node := result.Data["node"].(map[string]any)
	require.Equal(t, "EMBEDDED", node["nodeKind"])
	require.Equal(t, "UserStory", node["resolvedType"])
	require.Equal(t, "^SPEC-0068-US1", node["id"])
	require.Equal(t, "ready", node["status"])
	require.NotContains(t, node, "language")
	nodeRef := node["ref"].(map[string]any)
	require.Equal(t, "docs/specs/technical/php-language-support.md#^SPEC-0068-US1", nodeRef["ref"])
	require.Equal(t, "EMBEDDED", nodeRef["kind"])
	require.Equal(t, "docs/specs/technical/php-language-support.md", nodeRef["notePath"])
	require.Equal(t, "^SPEC-0068-US1", nodeRef["fragment"])
	require.Equal(t, "UserStory", nodeRef["typeName"])

	resolved := result.Data["resolved"].(map[string]any)
	require.Equal(t, true, resolved["found"])
	require.Equal(t, "EMBEDDED", resolved["kind"])
	require.Equal(t, "UserStory", resolved["resolvedType"])
	resolvedRef := resolved["ref"].(map[string]any)
	require.Equal(t, "docs/specs/technical/php-language-support.md#^SPEC-0068-US1", resolvedRef["ref"])
	require.Equal(t, "^SPEC-0068-US1", resolvedRef["fragment"])
}

func TestExecute_ExplicitEmbeddedRefMissDoesNotFallbackToCodeFile(t *testing.T) {
	env := newEmbeddedDeepLinkQueryTestEnv(t)

	prepared, errs := PrepareWithVariables(env.execSchema, `query PublicNodeDetail($ref: String!) {
  node(ref: $ref) {
    nodeKind
    resolvedType
  }
  resolved: resolve(ref: $ref) {
    found
    reason
    kind
    resolvedType
    ref { ref }
  }
}`, map[string]any{"ref": "docs/specs/technical/php-language-support.md#^SPEC-0068-US99"})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Nil(t, result.Data["node"])

	resolved := result.Data["resolved"].(map[string]any)
	require.Equal(t, false, resolved["found"])
	require.Equal(t, "unresolved", resolved["reason"])
	require.Nil(t, resolved["ref"])
}

func TestExecute_ExplicitBlockRefDoesNotCollideWithGeneratedHeadingFragment(t *testing.T) {
	const path = "docs/specs/technical/php-language-support.md"
	const prefix = `---
type: TechnicalSpec
id: SPEC-0068
---

# PHP Language Support

## User Stories

### collision

- status:: generated-heading
`
	nodes := ontology.ParseSections(path, prefix)
	require.Len(t, nodes, 1)
	require.Len(t, nodes[0].Children, 1)
	require.Len(t, nodes[0].Children[0].Children, 1)
	generatedFragment := queryFragmentFromSectionID(path, nodes[0].Children[0].Children[0].ID)
	require.NotEmpty(t, generatedFragment)

	content := prefix + fmt.Sprintf(`

### Explicit block target

- id:: ^%s
- status:: explicit-block
`, generatedFragment)
	env := newEmbeddedDeepLinkQueryTestEnvWithContent(t, content)
	ref := path + "#^" + generatedFragment

	prepared, errs := PrepareWithVariables(env.execSchema, `query PublicNodeDetail($ref: String!) {
  node(ref: $ref) {
    nodeKind
    resolvedType
    ref { fragment }
    ... on UserStory { id status }
  }
  resolved: resolve(ref: $ref) {
    found
    resolvedType
    ref { fragment }
  }
}`, map[string]any{"ref": ref})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	node := result.Data["node"].(map[string]any)
	require.Equal(t, "EMBEDDED", node["nodeKind"])
	require.Equal(t, "UserStory", node["resolvedType"])
	require.Equal(t, "^"+generatedFragment, node["id"])
	require.Equal(t, "explicit-block", node["status"])
	require.Equal(t, "^"+generatedFragment, node["ref"].(map[string]any)["fragment"])

	resolved := result.Data["resolved"].(map[string]any)
	require.Equal(t, true, resolved["found"])
	require.Equal(t, "UserStory", resolved["resolvedType"])
	require.Equal(t, "^"+generatedFragment, resolved["ref"].(map[string]any)["fragment"])
}

func TestExecute_ExplicitGeneratedHeadingFragmentResolvesSection(t *testing.T) {
	const path = "docs/specs/product/init-starter-workflow.md"
	const content = `# Init Starter Workflow

## Goals

Make repository setup predictable.
`
	env := newCustomQueryTestEnv(t, `
type NarrativeSection implements Section {
  summary: String @field
}

type ProductSpec @node(paths: ["docs/specs/product/*.md"]) {
  goals: NarrativeSection @contains(level: H2, heading: "Goals")
}
`, map[string]string{path: content})

	sections := ontology.ParseSections(path, content)
	require.Len(t, sections, 1)
	require.Len(t, sections[0].Children, 1)
	fragment := queryFragmentFromSectionID(path, sections[0].Children[0].ID)
	require.NotEmpty(t, fragment)

	prepared, errs := PrepareWithVariables(env.execSchema, `query PublicNodeDetail($ref: String!, $missing: String!) {
  node(ref: $ref) {
    nodeKind
    title
    path
    ref { ref kind notePath fragment }
    ... on Section { content }
  }
  resolved: resolve(ref: $ref) {
    found
    kind
    path
    ref { ref kind notePath fragment }
  }
  missing: node(ref: $missing) { nodeKind }
}`, map[string]any{
		"ref":     path + "#" + fragment,
		"missing": path + "#missing-heading-9999",
	})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	node := result.Data["node"].(map[string]any)
	require.Equal(t, "SECTION", node["nodeKind"])
	require.Equal(t, "Goals", node["title"])
	require.Equal(t, path+"#"+fragment, node["path"])
	require.Contains(t, node["content"], "Make repository setup predictable.")
	ref := node["ref"].(map[string]any)
	require.Equal(t, "SECTION", ref["kind"])
	require.Equal(t, path, ref["notePath"])
	require.Equal(t, fragment, ref["fragment"])

	resolved := result.Data["resolved"].(map[string]any)
	require.Equal(t, true, resolved["found"])
	require.Equal(t, "SECTION", resolved["kind"])
	require.Equal(t, path+"#"+fragment, resolved["path"])
	require.Equal(t, ref, resolved["ref"])
	require.Nil(t, result.Data["missing"], "an unresolved section fragment must not widen to the note root")
}

func TestExecute_NodeRefPreservesLegitimateTypedH1EmbeddedNode(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type HeroSection implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "HERO")
  status: String @field
}

type LandingPage @node(matches: ["type:LandingPage"]) {
  id: ID! @field @identifier(preferred: true)
  hero: HeroSection @contains(level: H1, heading: "Launch")
}
`, map[string]string{
		"docs/launch.md": `---
type: LandingPage
id: PAGE-0001
---

# Launch

- id:: ^HERO-0001
- status:: ready
`,
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query {
  node(ref: "docs/launch.md#^HERO-0001") {
    nodeKind
    resolvedType
    ... on HeroSection { id status }
  }
  resolved: resolve(ref: "docs/launch.md#^HERO-0001") {
    found
    resolvedType
  }
}`, nil)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	node := result.Data["node"].(map[string]any)
	require.Equal(t, "EMBEDDED", node["nodeKind"])
	require.Equal(t, "HeroSection", node["resolvedType"])
	require.Equal(t, "^HERO-0001", node["id"])
	require.Equal(t, "ready", node["status"])
	require.Equal(t, true, result.Data["resolved"].(map[string]any)["found"])
	require.Equal(t, "HeroSection", result.Data["resolved"].(map[string]any)["resolvedType"])
}

func TestExecute_NodeRefDoesNotPropagateTypeThroughNestedUnmatchedWrappers(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H4)
}

type TechnicalSpec @node(matches: ["type:TechnicalSpec"]) {
  id: ID! @field @identifier(preferred: true)
  userStories: UserStoriesSection @contains(level: H3, heading: "User Stories")
}
`, map[string]string{
		"docs/specs/technical/php-language-support.md": `---
type: TechnicalSpec
id: SPEC-0068
---

# First narrative wrapper

## Second narrative wrapper

### User Stories

#### US1 - Resolve embedded node deep links

- id:: ^SPEC-0068-US1
- status:: ready
`,
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query {
  node(ref: "docs/specs/technical/php-language-support.md#^SPEC-0068-US1") {
    nodeKind
    resolvedType
    ... on UserStory { id status }
  }
  resolved: resolve(ref: "docs/specs/technical/php-language-support.md#^SPEC-0068-US1") {
    found
    reason
    resolvedType
  }
}`, nil)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Nil(t, result.Data["node"])
	resolved := result.Data["resolved"].(map[string]any)
	require.Equal(t, false, resolved["found"])
	require.Equal(t, "unresolved", resolved["reason"])
}

func newEmbeddedDeepLinkQueryTestEnv(t *testing.T) *queryTestEnv {
	t.Helper()
	return newEmbeddedDeepLinkQueryTestEnvWithContent(t, `---
type: TechnicalSpec
id: SPEC-0068
---

# PHP Language Support

## User Stories

### US1 - Resolve embedded node deep links

- id:: ^SPEC-0068-US1
- status:: ready
`)
}

func newEmbeddedDeepLinkQueryTestEnvWithContent(t *testing.T, content string) *queryTestEnv {
	t.Helper()
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type TechnicalSpec @node(matches: ["type:TechnicalSpec"]) {
  id: ID! @field @identifier(preferred: true)
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"docs/specs/technical/php-language-support.md": content,
	})
	require.NoError(t, env.store.ReplaceIntelCodeFile(context.Background(), "docs/specs/technical/php-language-support.md", nil, nil, nil))
	return env
}

func TestExecute_NodeRefResolvesStructuralFingerprintSection(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ProductSpec @node(paths: ["specs/*.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  summary: String @field
}
`, map[string]string{
		"specs/product.md": `# Product

## Stories

### Story A
summary:: Shipped
`,
	})

	nodes, err := env.store.AllOntologyNodes(context.Background())
	require.NoError(t, err)
	var structural string
	for _, node := range nodes {
		if node.Title == "Stories" {
			structural = node.StructuralFingerprint
			break
		}
	}
	require.NotEmpty(t, structural)

	ref := fmt.Sprintf("specs/product.md#struct:%s", structural)
	prepared, errs := PrepareWithVariables(env.execSchema, `query PublicNodeDetail($ref: String!) {
  node(ref: $ref) {
    nodeKind
    title
    path
    resolvedType
  }
}`, map[string]any{"ref": ref})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	node := result.Data["node"].(map[string]any)
	require.Equal(t, "SECTION", node["nodeKind"])
	require.Equal(t, "Stories", node["title"])
	require.Equal(t, "specs/product.md#stories-11", node["path"])
	require.Equal(t, "StoriesSection", node["resolvedType"])
}

func TestExecute_NodeRefRejectsUnknownStructuralFingerprint(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ProductSpec @node(paths: ["specs/*.md"]) {
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type UserStory implements Section @node(locator: EMBEDDED) {
  summary: String @field
}
`, map[string]string{
		"specs/product.md": "# Product\n\n## Stories\n\n### Story A\nsummary:: Shipped\n",
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query PublicNodeDetail($ref: String!) {
  node(ref: $ref) { title }
}`, map[string]any{"ref": "specs/product.md#struct:missing"})
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.NotEmpty(t, result.Errors)
	require.Nil(t, result.Data["node"])
}

func TestExecute_ResolveRefSupportsPreferredIdentifiers(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"notes/specs/public-api.md": `---
type: Spec
id: SPEC-0042
aliases:
  - Public API Contract
---

## User Stories

### Story one
id:: SPEC-0042.US1
status:: planned
^story-one
`,
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query {
  noteID: resolve(ref: "SPEC-0042") {
    found
    ref { kind notePath typeName }
    path
    resolvedType
  }
  storyID: resolve(ref: "SPEC-0042.US1") {
    found
    ref { kind notePath fragment typeName }
    path
    resolvedType
  }
  noteNode: node(ref: "SPEC-0042") {
    nodeKind
    resolvedType
    ... on Note { path }
  }
  storyNode: node(ref: "SPEC-0042.US1") {
    nodeKind
    title
    resolvedType
    ... on Section { notePath }
  }
}`, nil)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	noteID := result.Data["noteID"].(map[string]any)
	require.Equal(t, true, noteID["found"])
	require.Equal(t, "notes/specs/public-api.md", noteID["path"])
	require.Equal(t, "Spec", noteID["resolvedType"])

	storyID := result.Data["storyID"].(map[string]any)
	require.Equal(t, true, storyID["found"])
	require.Equal(t, "UserStory", storyID["resolvedType"])
	storyRef := storyID["ref"].(map[string]any)
	require.Equal(t, "EMBEDDED", storyRef["kind"])
	require.Equal(t, "notes/specs/public-api.md", storyRef["notePath"])
	require.Equal(t, "^story-one", storyRef["fragment"])

	noteNode := result.Data["noteNode"].(map[string]any)
	require.Equal(t, "NOTE", noteNode["nodeKind"])
	require.Equal(t, "Spec", noteNode["resolvedType"])
	require.Equal(t, "notes/specs/public-api.md", noteNode["path"])

	storyNode := result.Data["storyNode"].(map[string]any)
	require.Equal(t, "EMBEDDED", storyNode["nodeKind"])
	require.Equal(t, "Story one", storyNode["title"])
	require.Equal(t, "UserStory", storyNode["resolvedType"])
	require.Equal(t, "notes/specs/public-api.md", storyNode["notePath"])
}

func TestExecute_ResolveRefReportsAmbiguousIdentifierCandidates(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Spec @node(paths: ["notes/specs/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
}
`, map[string]string{
		"notes/specs/a.md": `---
type: Spec
id: SPEC-0042
---
`,
		"notes/specs/b.md": `---
type: Spec
id: SPEC-0042
---
`,
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query {
  resolved: resolve(ref: "SPEC-0042") {
    found
    reason
    ref { notePath }
    candidates { notePath kind typeName }
  }
  node(ref: "SPEC-0042") { nodeKind }
}`, nil)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	resolved := result.Data["resolved"].(map[string]any)
	require.Equal(t, false, resolved["found"])
	require.Equal(t, "ambiguous", resolved["reason"])
	require.Nil(t, resolved["ref"])
	candidates := resolved["candidates"].([]any)
	require.Len(t, candidates, 2)
	require.Equal(t, "notes/specs/a.md", candidates[0].(map[string]any)["notePath"])
	require.Equal(t, "notes/specs/b.md", candidates[1].(map[string]any)["notePath"])
	for _, candidate := range candidates {
		value := candidate.(map[string]any)
		require.Equal(t, "NOTE", value["kind"])
		require.Equal(t, "Spec", value["typeName"])
	}
	require.Nil(t, result.Data["node"])
}

func TestExecute_ResolveRefPreservesMixedNoteAndEmbeddedIdentifierAmbiguity(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"notes/specs/mixed.md": `---
type: Spec
id: SPEC-0042.US1
aliases:
  - SPEC-0042.US1
---

## User Stories

### Duplicate identifier
id:: SPEC-0042.US1
^duplicate-story
`,
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query {
  resolved: resolve(ref: "SPEC-0042.US1") {
    found
    reason
    ref { notePath kind }
    candidates { notePath fragment kind typeName }
  }
  node(ref: "SPEC-0042.US1") { nodeKind }
}`, nil)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	resolved := result.Data["resolved"].(map[string]any)
	require.Equal(t, false, resolved["found"])
	require.Equal(t, "ambiguous", resolved["reason"])
	require.Nil(t, resolved["ref"])
	candidates := resolved["candidates"].([]any)
	require.Len(t, candidates, 2)
	require.Equal(t, "NOTE", candidates[0].(map[string]any)["kind"])
	require.Equal(t, "EMBEDDED", candidates[1].(map[string]any)["kind"])
	require.Nil(t, result.Data["node"])
}

func TestExecute_ResolveRefKeepsNoteAliasPrecedenceOverUnrelatedEmbeddedIdentifier(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  id: String! @field(source: "id") @identifier(preferred: true, prefix: "SPEC")
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"notes/specs/alias.md": `---
type: Spec
id: SPEC-0042
aliases:
  - SPEC-9999.US1
---
`,
		"notes/specs/embedded.md": `---
type: Spec
id: SPEC-9999
---

## User Stories

### Unrelated identifier
id:: SPEC-9999.US1
^unrelated-story
`,
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query {
  resolved: resolve(ref: "SPEC-9999.US1") {
    found
    reason
    ref { notePath kind }
    candidates { notePath fragment kind }
  }
  node(ref: "SPEC-9999.US1") {
    nodeKind
    ... on Note { path }
  }
	}`, nil)
	require.Empty(t, errs)

	deps := env.deps(nil)
	store := &identifierScanCountingStore{Store: env.store}
	deps.Store = store
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	resolved := result.Data["resolved"].(map[string]any)
	require.Equal(t, true, resolved["found"])
	require.Empty(t, resolved["reason"])
	require.Empty(t, resolved["candidates"])
	ref := resolved["ref"].(map[string]any)
	require.Equal(t, "notes/specs/alias.md", ref["notePath"])
	require.Equal(t, "NOTE", ref["kind"])
	node := result.Data["node"].(map[string]any)
	require.Equal(t, "NOTE", node["nodeKind"])
	require.Equal(t, "notes/specs/alias.md", node["path"])
	require.Zero(t, store.preferredIdentifierScans.Load())
	require.Zero(t, store.embeddedIdentifierScans.Load())
}

func TestExecute_ItemBackedEmbeddedNodesAsQueryRows(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section @node(locator: EMBEDDED) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: String @field
}

type Conversation @node(paths: ["meetings/*.md"]) {
  title: String!
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}
`, map[string]string{
		"meetings/sync.md": `---
type: Conversation
title: Sync
---

## Action Items

- [ ] Update docs assignee:: Gabe #action-item
- [x] Already done assignee:: Maya #action-item ^done-1
- [ ] Ordinary checkbox
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  conversation(find: "Sync") {
    actionItems {
      title
      content
      done
      assignee
      notePath
      ref { kind nodeId fragment typeName }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	conversations := result.Data["conversation"].([]any)
	require.Len(t, conversations, 1)
	rows := conversations[0].(map[string]any)["actionItems"].([]any)
	require.Len(t, rows, 2)

	first := rows[0].(map[string]any)
	require.Equal(t, false, first["done"])
	require.Equal(t, "Gabe", first["assignee"])
	require.Contains(t, first["content"], "Update docs")
	firstRef := first["ref"].(map[string]any)
	require.Equal(t, "EMBEDDED", firstRef["kind"])
	require.Equal(t, "ActionItem", firstRef["typeName"])
	require.Contains(t, firstRef["fragment"], "item-")

	second := rows[1].(map[string]any)
	require.Equal(t, true, second["done"])
	require.Equal(t, "Maya", second["assignee"])
	secondRef := second["ref"].(map[string]any)
	require.Equal(t, "^done-1", secondRef["fragment"])
}

func TestExecute_EmbeddedRootFiltersSupportInAndComparisons(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: Date @field
  assignee: String @field
}

type Conversation @node(paths: ["meetings/*.md"]) {
  title: String!
}
`, map[string]string{
		"meetings/sync.md": `---
type: Conversation
title: Sync
---

## Action Items

- [ ] First #action-item
  due:: 2026-05-08
  assignee:: Gabe
- [ ] Second #action-item
  due:: 2026-05-10
  assignee:: Maya
- [ ] Third #action-item
  due:: 2026-05-12
  assignee:: Drew
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  actionItem(
    filters: [
      { field: "assignee", op: in, values: ["Maya", "Drew"] }
      { field: "due", op: gt, value: "2026-05-10" }
    ]
  ) {
    title
    due
    assignee
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["actionItem"].([]any)
	require.Len(t, rows, 1)
	require.Equal(t, "Third", rows[0].(map[string]any)["title"])

	sorted, errs := Prepare(env.execSchema, `{
  actionItem(
    filters: [
      { field: "done", op: eq, value: "false" }
      { field: "due", op: exists }
    ]
    sort: [{ field: "due", direction: desc }]
    first: 2
  ) {
    title
    due
    done
  }
}`)
	require.Empty(t, errs)
	sortedResult := Execute(context.Background(), env.deps(nil), env.schema, sorted)
	require.Empty(t, sortedResult.Errors)
	sortedRows := sortedResult.Data["actionItem"].([]any)
	require.Len(t, sortedRows, 2)
	require.Equal(t, "Third", sortedRows[0].(map[string]any)["title"])
	require.Equal(t, "Second", sortedRows[1].(map[string]any)["title"])

	bad, errs := Prepare(env.execSchema, `{
  actionItem(filters: [{ field: "due", op: startsWith, value: "2026" }]) {
    title
  }
}`)
	require.Nil(t, bad)
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0].Message, "startsWith")
}

func TestExecute_EmbeddedRootIndexedLinkEqualityAndTieBreaks(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["people/*.md"]) {
  name: String! @field
}

type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: Date @field
  assignee: Person! @link
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
aliases:
  - Alice Example
---
`,
		"people/bob.md": `---
type: Person
name: Bob
---
`,
		"notes/b.md": `---
title: B
---

- [ ] Beta #action-item
  due:: 2026-05-10
  assignee:: [[people/alice]]
`,
		"notes/a.md": `---
title: A
---

- [ ] Alpha #action-item
  due:: 2026-05-10
  assignee:: [[people/alice]]
- [ ] Done #action-item
  due:: 2026-05-09
  assignee:: [[people/bob]]
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  actionItem(
    filters: [
      { field: "assignee", op: in, values: ["people/alice"] }
      { field: "due", op: gte, value: "2026-05-10" }
      { field: "due", op: lte, value: "2026-05-10" }
    ]
    sort: [{ field: "due", direction: asc }, { field: "notePath", direction: desc }]
    first: 2
  ) {
    title
    due
    notePath
    assignee { path name }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["actionItem"].([]any)
	require.Len(t, rows, 2)
	require.Equal(t, "Beta", rows[0].(map[string]any)["title"])
	require.Equal(t, "notes/b.md", rows[0].(map[string]any)["notePath"])
	require.Equal(t, "people/alice.md", rows[0].(map[string]any)["assignee"].(map[string]any)["path"])
	require.Equal(t, "Alpha", rows[1].(map[string]any)["title"])
	require.Equal(t, "notes/a.md", rows[1].(map[string]any)["notePath"])
}

func TestExecute_GenericEmbeddedRootIndexedFieldsCoverTypedValues(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
enum RiskStatus {
  open
  mitigated
}

type Person @node(paths: ["people/*.md"]) {
  name: String! @field
}

type RiskItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: LIST_ITEM, marker: "#risk") {
  status: RiskStatus @field
  active: Boolean @field
  due: Date @field
  owner: Person @link
  labels: [String!] @field
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
---
`,
		"people/bob.md": `---
type: Person
name: Bob
---
`,
		"notes/risks.md": `# Risks

- Cache invalidation #risk
  status:: open
  active:: true
  due:: 2026-05-08
  owner:: [[people/alice]]
  labels:: api
  labels:: indexed
- Archive cleanup #risk
  status:: mitigated
  active:: false
  due:: 2026-05-06
  owner:: [[people/bob]]
  labels:: storage
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  riskItem(
    filters: [
      { field: "status", op: eq, value: "open" }
      { field: "active", op: eq, value: "true" }
      { field: "owner", op: eq, value: "people/alice" }
      { field: "labels", op: in, values: ["indexed"] }
    ]
    sort: [{ field: "due", direction: asc }]
  ) {
    title
    status
    active
    due
    labels
    owner { path name }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["riskItem"].([]any)
	require.Len(t, rows, 1)
	row := rows[0].(map[string]any)
	require.Equal(t, "Cache invalidation", row["title"])
	require.Equal(t, "open", row["status"])
	require.Equal(t, true, row["active"])
	require.Equal(t, "2026-05-08", row["due"])
	require.Equal(t, []any{"api", "indexed"}, row["labels"])
	require.Equal(t, "people/alice.md", row["owner"].(map[string]any)["path"])
}

func TestExecute_OrdinaryNoteActionItemsUseIndexedSummaryWithoutProjection(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["people/*.md"]) {
  name: String! @field
}

type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: Date @field
  assignee: Person @link
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
---
`,
		"people/bob.md": `---
type: Person
name: Bob
---
`,
		"notes/team.md": `# Team notes

- [ ] Alice later #action-item
  due:: 2026-05-12
  assignee:: [[people/alice]]
- [ ] Bob task #action-item
  due:: 2026-05-01
  assignee:: [[people/bob]]
`,
		"notes/personal.md": `# Personal

- [ ] Alice earlier #action-item
  due:: 2026-05-08
  assignee:: [[people/alice]]
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  actionItem(
    filters: [
      { field: "done", op: eq, value: "false" }
      { field: "assignee", op: eq, value: "people/alice" }
    ]
    sort: [{ field: "due", direction: asc }]
  ) {
    title
    done
    due
    notePath
    ref { kind typeName }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), Deps{
		VaultDef:   obsidian.VaultDefinition{Path: env.root},
		NoteReader: contentFailingQueryNoteReader{inner: &obsidian.Note{}},
		Store:      env.store,
	}, env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["actionItem"].([]any)
	require.Len(t, rows, 2)
	require.Equal(t, "Alice earlier", rows[0].(map[string]any)["title"])
	require.Equal(t, "2026-05-08", rows[0].(map[string]any)["due"])
	require.Equal(t, "Alice later", rows[1].(map[string]any)["title"])
	require.Equal(t, "2026-05-12", rows[1].(map[string]any)["due"])
}

func TestExecute_EmbeddedRootIndexedLevelSelectionForcesProjection(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: Date @field
  assignee: String @field
}
`, map[string]string{
		"notes/actions.md": `---
title: Actions
---

- [ ] Update docs #action-item
  due:: 2026-05-10
  assignee:: Gabe
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  actionItem(filters: [{ field: "assignee", op: eq, value: "Gabe" }]) {
    title
    level
  }
}`)
	require.Empty(t, errs)

	// contentFailingQueryNoteReader fails any markdown projection. Selecting
	// `level` must force projection because indexed catalog rows do not carry
	// the section heading depth; the projection error proves the indexed path
	// did not silently return level="".
	result := Execute(context.Background(), Deps{
		VaultDef:   obsidian.VaultDefinition{Path: env.root},
		NoteReader: contentFailingQueryNoteReader{inner: &obsidian.Note{}},
		Store:      env.store,
	}, env.schema, prepared)
	require.NotEmpty(t, result.Errors)
	containsProjection := false
	for _, err := range result.Errors {
		if strings.Contains(err.Message, "unexpected markdown projection") {
			containsProjection = true
			break
		}
	}
	require.True(t, containsProjection, "expected projection attempt; errors=%v", result.Errors)

	planQuery, errs := Prepare(env.execSchema, `{
  ontology {
    queryPlan(
      type: "ActionItem"
      select: ["level"]
    ) {
      projection
    }
  }
}`)
	require.Empty(t, errs)
	planResult := Execute(context.Background(), env.deps(nil), env.schema, planQuery)
	require.Empty(t, planResult.Errors)
	plan := planResult.Data["ontology"].(map[string]any)["queryPlan"].(map[string]any)
	require.Equal(t, "required", plan["projection"])
}

func TestExecute_IndexedRootNotePathFilterPreservesAuthoredExtension(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`, map[string]string{
		"notes/actions.md": `---
title: Actions
---

- [ ] Update docs #action-item
`,
		"notes/other.md": `---
title: Other
---

- [ ] Skip me #action-item
`,
	})

	cases := []struct {
		name  string
		value string
		found bool
	}{
		{"bare", "notes/actions", false},
		{"with-md", "notes/actions.md", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prepared, errs := Prepare(env.execSchema, fmt.Sprintf(`{
  actionItem(filters: [{ field: "notePath", op: eq, value: %q }]) {
    title
    notePath
  }
}`, tc.value))
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			require.Empty(t, result.Errors)
			rows := result.Data["actionItem"].([]any)
			if !tc.found {
				require.Empty(t, rows)
				return
			}
			require.Len(t, rows, 1)
			require.Equal(t, "Update docs", rows[0].(map[string]any)["title"])
			require.Equal(t, "notes/actions.md", rows[0].(map[string]any)["notePath"])
		})
	}

	// `in` retains authored identity and does not invent a Markdown suffix.
	prepared, errs := Prepare(env.execSchema, `{
  actionItem(filters: [{ field: "path", op: in, values: ["notes/actions", "notes/other.md"] }]) {
    title
  }
}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["actionItem"].([]any)
	require.Len(t, rows, 1)
}

func TestExecute_IndexedRecordsUsePropertyCaseFromEnclosingNoteType(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Sprint @node(paths: ["sprints/*.md"], propertyCase: SNAKE) {
  name: String!
}

type ActionItem implements Section
  @node(locator: EMBEDDED, propertyCase: KEBAB)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  followUp: String @field
}
`, map[string]string{
		"sprints/sprint-7.md": `---
type: Sprint
name: Sprint 7
---

- [ ] Update docs #action-item
  follow_up:: ping reviewer
`,
	})

	loaders, err := newLoaders(env.deps(nil), env.schema)
	require.NoError(t, err)
	exec := &executor{
		schema:           env.schema,
		deps:             env.deps(nil),
		loaders:          loaders,
		assessmentByPath: make(map[string]*ontology.NoteAssessment),
		assessmentKnown:  make(map[string]bool),
		sectionsByPath:   make(map[string][]*ontology.SectionNode),
	}

	rows, err := env.store.OntologyNodesByTypePlan(context.Background(), codeanchor.OntologyNodeQueryPlan{TypeNames: []string{"ActionItem"}, Limit: 100})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	records, err := exec.sectionRecordsFromCatalogRows(context.Background(), rows)
	require.NoError(t, err)
	require.NotEmpty(t, records)
	for _, record := range records {
		require.Equal(t, ontology.PropertyCaseSnake, record.PropertyCase, "indexed record %q must inherit enclosing note type's case", record.Note.Path)
	}
}

func TestNormalizeNotePathPreservesMixedCaseAuthoredExtensions(t *testing.T) {
	t.Parallel()

	vaultPaths, err := paths.NewVaultPaths(t.TempDir())
	require.NoError(t, err)

	for _, input := range []string{"notes/Decision.MD", "notes/Reference.HTML"} {
		t.Run(input, func(t *testing.T) {
			got, err := normalizeNotePath(vaultPaths, input)
			require.NoError(t, err)
			require.Equal(t, input, got)
		})
	}
}

func TestExecute_IndexedEmbeddedRootBindsFilterAndSortVariables(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: Date @field
  assignee: String @field
}
`, map[string]string{
		"notes/team.md": `# Team

- [ ] Alpha #action-item
  due:: 2026-05-08
  assignee:: Maya
- [ ] Beta #action-item
  due:: 2026-05-12
  assignee:: Maya
- [ ] Gamma #action-item
  due:: 2026-05-10
  assignee:: Drew
`,
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query AssignedDescending($assignee: String!, $direction: SortDirection!) {
  actionItem(
    filters: [{ field: "assignee", op: eq, value: $assignee }]
    sort: [{ field: "due", direction: $direction }]
  ) {
    title
    due
    done
    assignee
    notePath
    ref { kind typeName }
  }
}`, map[string]any{"assignee": "Maya", "direction": "desc"})
	require.Empty(t, errs)

	// Use the contentFailingQueryNoteReader so any markdown projection
	// surfaces as an error. The indexed plan satisfies the listed
	// selections without projection, so a clean response proves the
	// indexed path absorbed the variable-bound filter and sort.
	result := Execute(context.Background(), Deps{
		VaultDef:   obsidian.VaultDefinition{Path: env.root},
		NoteReader: contentFailingQueryNoteReader{inner: &obsidian.Note{}},
		Store:      env.store,
	}, env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["actionItem"].([]any)
	require.Len(t, rows, 2)
	require.Equal(t, "Beta", rows[0].(map[string]any)["title"])
	require.Equal(t, "2026-05-12", rows[0].(map[string]any)["due"])
	require.Equal(t, "Alpha", rows[1].(map[string]any)["title"])
	require.Equal(t, "2026-05-08", rows[1].(map[string]any)["due"])
	for _, row := range rows {
		item := row.(map[string]any)
		require.Equal(t, false, item["done"])
		require.Equal(t, "Maya", item["assignee"])
		require.Equal(t, "notes/team.md", item["notePath"])
		ref := item["ref"].(map[string]any)
		require.Equal(t, "EMBEDDED", ref["kind"])
		require.Equal(t, "ActionItem", ref["typeName"])
	}

	// queryPlan with the same variable-bound filter/sort should report the
	// indexed execution and pushed plan items.
	planQuery, errs := PrepareWithVariables(env.execSchema, `query Plan($assignee: String!, $direction: SortDirection!) {
  ontology {
    queryPlan(
      type: "ActionItem"
      filters: [{ field: "assignee", op: eq, value: $assignee }]
      sort: [{ field: "due", direction: $direction }]
      select: ["title", "due", "notePath"]
    ) {
      execution
      projection
      pushedFilters
      pushedSort
      residualFilters
      residualSort
    }
  }
}`, map[string]any{"assignee": "Maya", "direction": "desc"})
	require.Empty(t, errs)
	planResult := Execute(context.Background(), env.deps(nil), env.schema, planQuery)
	require.Empty(t, planResult.Errors)
	plan := planResult.Data["ontology"].(map[string]any)["queryPlan"].(map[string]any)
	require.Equal(t, "indexed", plan["execution"])
	require.Equal(t, "not_required", plan["projection"])
	require.Equal(t, []any{"assignee eq"}, plan["pushedFilters"])
	require.Equal(t, []any{"due desc"}, plan["pushedSort"])
	require.Empty(t, plan["residualFilters"])
	require.Empty(t, plan["residualSort"])
}

func TestExecute_IndexedEmbeddedLinkFilterNormalizesInputs(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["people/**/*.md"]) {
  name: String! @field
  aliases: [String!] @field
}

type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
aliases:
  - A. Example
---
# Alice
`,
		"people/bob.md": `---
type: Person
name: Bob
aliases:
  - B. Example
---
# Bob
`,
		"notes/tasks.md": `# Tasks

- [ ] Alias task #action-item
  assignee:: Alice
- [ ] Wikilink task #action-item
  assignee:: [[people/alice]]
- [ ] Bob task #action-item
  assignee:: [[people/bob]]
`,
	})

	query := `query Assigned($assignee: String!) {
  actionItem(filters: [{ field: "assignee", op: eq, value: $assignee }]) {
    title
  }
}`
	cases := map[string][]string{
		"Alice":            {"Alias task", "Wikilink task"},
		"A. Example":       {"Wikilink task"},
		"B. Example":       {"Bob task"},
		"people/alice":     {"Wikilink task"},
		"people/alice.md":  {"Wikilink task"},
		"[[people/alice]]": {"Wikilink task"},
	}
	for assignee, wantTitles := range cases {
		t.Run(assignee, func(t *testing.T) {
			prepared, errs := PrepareWithVariables(env.execSchema, query, map[string]any{"assignee": assignee})
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			require.Empty(t, result.Errors)
			rows := result.Data["actionItem"].([]any)
			require.ElementsMatch(t, wantTitles, titles(rows))
		})
	}

	prepared, errs := Prepare(env.execSchema, `{
  actionItem(filters: [{ field: "assignee", op: in, values: ["A. Example", "people/bob"] }]) {
    title
  }
}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	require.ElementsMatch(t, []string{"Wikilink task", "Bob task"}, titles(result.Data["actionItem"].([]any)))
}

func TestExecute_IndexedEmbeddedLinkFilterDoesNotWidenAmbiguousAliases(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["people/**/*.md"]) {
  name: String! @field
  aliases: [String!] @field
}

type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
aliases:
  - Shared Alias
---
# Alice
`,
		"people/alicia.md": `---
type: Person
name: Alicia
aliases:
  - Shared Alias
---
# Alicia
`,
		"notes/tasks.md": `# Tasks

- [ ] Canonical task #action-item
  assignee:: [[people/alice]]
`,
	})

	prepared, errs := PrepareWithVariables(env.execSchema, `query Assigned($assignee: String!) {
  actionItem(filters: [{ field: "assignee", op: eq, value: $assignee }]) {
    title
  }
}`, map[string]any{"assignee": "Shared Alias"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.NotEmpty(t, result.Errors)
	require.Contains(t, result.Errors[0].Message, "resolved to multiple")
	require.Nil(t, result.Data["actionItem"], "ambiguous author-facing inputs must not widen to every possible target")

	planQuery, errs := PrepareWithVariables(env.execSchema, `query Plan($assignee: String!) {
  ontology {
    queryPlan(
      type: "ActionItem"
      filters: [{ field: "assignee", op: eq, value: $assignee }]
      select: ["title"]
    ) {
      warnings { code }
    }
  }
}`, map[string]any{"assignee": "Shared Alias"})
	require.Empty(t, errs)
	planResult := Execute(context.Background(), env.deps(nil), env.schema, planQuery)
	require.Empty(t, planResult.Errors)
	warnings := planResult.Data["ontology"].(map[string]any)["queryPlan"].(map[string]any)["warnings"].([]any)
	require.Contains(t, warningCodes(warnings), "link_filter_ambiguous")
}

func TestExecute_OntologyRuntimeQueryPlanSupportsNoteRoots(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["people/**/*.md"]) {
  name: String! @field
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
---
# Alice
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  ontology {
    queryPlan(
      type: "Person"
      filters: [{ field: "name", op: eq, value: "Alice" }]
      select: ["title", "name"]
    ) {
      execution
      projection
      pushedFilters
      warnings { code }
    }
  }
}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	plan := result.Data["ontology"].(map[string]any)["queryPlan"].(map[string]any)
	require.Equal(t, "indexed", plan["execution"])
	require.Equal(t, "not_required", plan["projection"])
	require.Equal(t, []any{"name eq"}, plan["pushedFilters"])
	require.NotContains(t, warningCodes(plan["warnings"].([]any)), "non_embedded_root")
}

func TestExecute_OntologyRuntimeFieldCapabilitiesSplitIndexedAndResidualOps(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignee: Person @link
  summary: String @field
}

type Person @node(paths: ["people/**/*.md"]) {
  name: String! @field
}
`, map[string]string{
		"people/alice.md": `---
type: Person
name: Alice
---
`,
		"notes/tasks.md": `# Tasks

- [ ] Canonical task #action-item
  assignee:: [[people/alice]]
  summary:: Follow up
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  ontology {
    type(name: "ActionItem") {
      fields {
        name
        capability {
          valueKind
          filterOps
          indexedFilterOps
          residualFilterOps
          sortable
          targetType
        }
      }
    }
  }
}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	fields := result.Data["ontology"].(map[string]any)["type"].(map[string]any)["fields"].([]any)
	summary := ontologyFieldByName(fields, "summary")
	summaryCap := summary["capability"].(map[string]any)
	require.ElementsMatch(t, []any{"eq", "in", "exists", "neq", "contains"}, summaryCap["filterOps"])
	require.ElementsMatch(t, []any{"eq", "in", "exists"}, summaryCap["indexedFilterOps"])
	require.ElementsMatch(t, []any{"neq", "contains"}, summaryCap["residualFilterOps"])
	require.True(t, summaryCap["sortable"].(bool))

	assignee := ontologyFieldByName(fields, "assignee")
	assigneeCap := assignee["capability"].(map[string]any)
	require.Equal(t, "Person", assigneeCap["targetType"])
	require.ElementsMatch(t, []any{"eq", "in", "exists"}, assigneeCap["indexedFilterOps"])
	require.Empty(t, assigneeCap["residualFilterOps"])
}

func TestExecute_IndexedEmbeddedRootResidualSortLoadsFullCandidateSet(t *testing.T) {
	// Regression: when the sort key is not pushable into the field index
	// (e.g. title), residual sort runs on the candidate set returned by the
	// store. The planner used to leave plan.Limit at the requested `first`,
	// so the store returned only the catalog-order top-N and the residual
	// sort could only re-order that subset. Rows that ranked higher under
	// the residual sort but appeared later in catalog order were silently
	// dropped. The fix sets plan.Limit to the residual cap so the residual
	// sort sees the full bounded candidate set before truncation.
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`, map[string]string{
		// Catalog/document order is Zulu, Alpha, Yankee, Bravo. Title-sorted
		// asc is Alpha, Bravo, Yankee, Zulu. Top-2 by sort != top-2 by catalog.
		"notes/team.md": `# Team

- [ ] Zulu #action-item
- [ ] Alpha #action-item
- [ ] Yankee #action-item
- [ ] Bravo #action-item
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  actionItem(sort: [{ field: "title", direction: asc }], first: 2) {
    title
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["actionItem"].([]any)
	require.Len(t, rows, 2)
	require.Equal(t, "Alpha", rows[0].(map[string]any)["title"])
	require.Equal(t, "Bravo", rows[1].(map[string]any)["title"])
}

func TestExecute_OntologyRuntimeQueryPlanProjectionMatchesIndexedExecution(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
}
`, map[string]string{
		"notes/actions.md": `---
title: Actions
---

- [ ] Update docs #action-item
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  actionItem(first: 1) { title }
  ontology {
    typenamePlan: queryPlan(type: "ActionItem", select: ["__typename"]) {
      projection
    }
    titlePlan: queryPlan(type: "ActionItem", select: ["title"]) {
      projection
    }
  }
}`)
	require.Empty(t, errs)
	deps := env.deps(nil)
	deps.NoteReader = contentFailingQueryNoteReader{inner: &obsidian.Note{}}
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Len(t, result.Data["actionItem"].([]any), 1)
	require.Equal(t, "Update docs", result.Data["actionItem"].([]any)[0].(map[string]any)["title"])
	ontologyResult := result.Data["ontology"].(map[string]any)
	require.Equal(t, "not_required", ontologyResult["typenamePlan"].(map[string]any)["projection"])
	require.Equal(t, "not_required", ontologyResult["titlePlan"].(map[string]any)["projection"])
}

func TestExecute_OntologyRuntimeQueryPlanExplainsIndexedEmbeddedRoots(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  due: Date @field
  assignee: String @field
}
`, map[string]string{
		"notes/actions.md": `---
title: Actions
---

- [ ] Update docs #action-item
  due:: 2026-05-10
  assignee:: Gabe
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  ontology {
    queryPlan(
      type: "ActionItem"
      filters: [
        { field: "assignee", op: eq, value: "Gabe" }
        { field: "title", op: contains, value: "docs" }
      ]
      sort: [{ field: "due", direction: asc }]
      select: ["title", "due", "assignee"]
    ) {
      type
      root
      execution
      projection
      first
      pushedFilters
      residualFilters
      pushedSort
      residualSort
      warnings { code }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	plan := result.Data["ontology"].(map[string]any)["queryPlan"].(map[string]any)
	require.Equal(t, "ActionItem", plan["type"])
	require.Equal(t, "actionItem", plan["root"])
	require.Equal(t, "indexed_with_residual", plan["execution"])
	require.Equal(t, "not_required", plan["projection"])
	require.Equal(t, []any{"assignee eq"}, plan["pushedFilters"])
	require.Equal(t, []any{"title contains"}, plan["residualFilters"])
	require.Equal(t, []any{"due asc"}, plan["pushedSort"])
	require.Empty(t, plan["residualSort"])
	warnings := plan["warnings"].([]any)
	require.Len(t, warnings, 1)
	require.Equal(t, "residual_constraints", warnings[0].(map[string]any)["code"])
}

func TestExecute_ItemBackedEmbeddedNodesNestedUnderSectionAndRoundTripRef(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type ActionItem implements Section @node(locator: EMBEDDED) {
  done: Boolean! @field(sourceKind: CHECKBOX)
}

type ActionItemsSection implements Section {
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}

type Conversation @node(paths: ["meetings/*.md"]) {
  title: String!
  actionItemsSection: ActionItemsSection @contains(level: H2, heading: "Action Items")
}
`, map[string]string{
		"meetings/sync.md": `---
type: Conversation
title: Sync
---

## Action Items

- [ ] Update docs #action-item
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  conversation(find: "Sync") {
    actionItemsSection {
      actionItems {
        title
        done
        ref { kind fragment }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	conversations := result.Data["conversation"].([]any)
	rows := conversations[0].(map[string]any)["actionItemsSection"].(map[string]any)["actionItems"].([]any)
	require.Len(t, rows, 1)
	row := rows[0].(map[string]any)
	require.Equal(t, false, row["done"])
	ref := row["ref"].(map[string]any)
	require.Equal(t, "EMBEDDED", ref["kind"])
	require.Contains(t, ref["fragment"], "item-")

	nodeRef := "meetings/sync.md#" + ref["fragment"].(string)
	prepared, errs = PrepareWithVariables(env.execSchema, `query($ref: String!) {
  node(ref: $ref) {
    nodeKind
    resolvedType
    ... on ActionItem {
      done
    }
  }
}`, map[string]any{"ref": nodeRef})
	require.Empty(t, errs)

	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	node := result.Data["node"].(map[string]any)
	require.Equal(t, "EMBEDDED", node["nodeKind"])
	require.Equal(t, "ActionItem", node["resolvedType"])
	require.Equal(t, false, node["done"])
}

func TestPrepare_RejectsUnsupportedQueryForms(t *testing.T) {
	env := newQueryTestEnv(t)

	_, errs := Prepare(env.execSchema, "{ notes { nodes { path } } }")
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0].Message, "requires at least one of")

	_, errs = Prepare(env.execSchema, "{ project { name } }")
	require.Empty(t, errs)

	_, errs = Prepare(env.execSchema, "{ ... on Query { project { name } } }")
	require.Empty(t, errs)
}

func TestPrepareWithVariables_ValidatesAndExecutesVariableArguments(t *testing.T) {
	env := newQueryTestEnv(t)

	prepared, errs := PrepareWithVariables(env.execSchema, `
query ProjectByPath($path: String!, $first: Int = 1, $neighborType: String!) {
  project(path: $path, first: $first) {
    path
    name
    decisions {
      name
    }
    linked(type: $neighborType, first: $first) {
      title
    }
  }
}
`, map[string]any{
		"path":         "notes/projects/roadmap-refresh.md",
		"neighborType": "Decision",
	})
	require.Empty(t, errs)
	require.Equal(t, map[string]any{
		"path":         "notes/projects/roadmap-refresh.md",
		"first":        int64(1),
		"neighborType": "Decision",
	}, prepared.Variables)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	project := rows[0].(map[string]any)
	require.Equal(t, "Roadmap Refresh", project["name"])
	require.Len(t, project["linked"].([]any), 1)
}

func TestPrepareWithVariables_AllowsRuntimeRootArguments(t *testing.T) {
	env := newQueryTestEnv(t)

	prepared, errs := PrepareWithVariables(env.execSchema, `
query RuntimeContext($type: String!, $path: String!, $first: Int = 2) {
  ontology {
    schemaHash
    authoringGuide(type: $type) { available warnings { code } }
    nextId(type: $type, count: 3, paths: ["specs/2026-08-05-14-32-a.md", "specs/2026-08-05-14-32-b.md"]) {
      available errorCode ids count last paths
      allocations { path id base disambiguator }
    }
  }
  code {
    docsForCode(path: $path, first: $first) { available warnings { code path } }
  }
}
`, map[string]any{
		"type": "Project",
		"path": "pkg/ontology/query/schema.go",
	})
	require.Empty(t, errs)
	require.NotNil(t, prepared)
}

func TestExecute_RuntimeNextIDPreservesProspectivePathOrder(t *testing.T) {
	env := newQueryTestEnv(t)
	provider := &recordingOntologyRuntime{}
	prepared, errs := Prepare(env.execSchema, `{
  ontology {
    nextId(type: "Project", paths: ["notes/2026-08-05-14-32-b.md", "notes/2026-08-05-14-32-a.md"]) {
      available
      paths
      allocations { path id base disambiguator }
    }
  }
}`)
	require.Empty(t, errs)
	deps := env.deps(nil)
	deps.OntologyRuntime = provider

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, OntologyNextIDRequest{
		Type:  "Project",
		Count: 1,
		Paths: []string{"notes/2026-08-05-14-32-b.md", "notes/2026-08-05-14-32-a.md"},
	}, provider.request)
	next := result.Data["ontology"].(map[string]any)["nextId"].(map[string]any)
	require.Equal(t, []any{"notes/2026-08-05-14-32-b.md", "notes/2026-08-05-14-32-a.md"}, next["paths"])
	require.Equal(t, []any{
		map[string]any{
			"path":          "notes/2026-08-05-14-32-b.md",
			"id":            "PROJECT-2026-08-05-14-32",
			"base":          "PROJECT-2026-08-05-14-32",
			"disambiguator": nil,
		},
		map[string]any{
			"path":          "notes/2026-08-05-14-32-a.md",
			"id":            "PROJECT-2026-08-05-14-32-2",
			"base":          "PROJECT-2026-08-05-14-32",
			"disambiguator": 2,
		},
	}, next["allocations"])
}

func TestExecute_RuntimeIdentifierFormatOmitsPadForDateTime(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type EffortNote @node(paths: ["efforts/*.md"]) {
  id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
`, nil)
	prepared, errs := Prepare(env.execSchema, `{
  ontology {
    type(name: "EffortNote") {
      fields { name identifierFormat { strategy pad } }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	fields := result.Data["ontology"].(map[string]any)["type"].(map[string]any)["fields"].([]any)
	for _, raw := range fields {
		field := raw.(map[string]any)
		if field["name"] == "id" {
			format := field["identifierFormat"].(map[string]any)
			require.Equal(t, "DATETIME", format["strategy"])
			require.Nil(t, format["pad"])
			return
		}
	}
	t.Fatal("id field not found")
}

func TestExecute_RuntimeRootsReturnPartialUnavailableData(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  ontology {
    schemaHash
    type(name: "Project") {
      name
      role
      fields {
        name
        identifierFormat { prefix pad }
      }
    }
    authoringGuide(type: "Project") {
      available
      warnings { code message }
    }
    nextId(type: "Project", count: 2) {
      available
      errorCode
      ids
      count
      warnings { code }
    }
  }
  code {
    docsForCode(path: "pkg/ontology/query/schema.go") {
      inputPath
      available
      warnings { code path }
    }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	ontologyRoot := result.Data["ontology"].(map[string]any)
	require.Equal(t, env.schema.Hash, ontologyRoot["schemaHash"])
	projectType := ontologyRoot["type"].(map[string]any)
	require.Equal(t, "Project", projectType["name"])
	require.Equal(t, "NOTE", projectType["role"])
	require.NotEmpty(t, projectType["fields"].([]any))
	require.False(t, ontologyRoot["authoringGuide"].(map[string]any)["available"].(bool))
	require.Equal(t, "ontology_runtime_unavailable", ontologyRoot["nextId"].(map[string]any)["errorCode"])
	require.Empty(t, ontologyRoot["nextId"].(map[string]any)["ids"].([]any))

	codeRoot := result.Data["code"].(map[string]any)
	require.False(t, codeRoot["docsForCode"].(map[string]any)["available"].(bool))
}

func TestExecute_RuntimeRootsResolveNestedInlineFragments(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  code {
    docsForCode(path: "pkg/ontology/query/schema.go") {
      ... on CodeContextPack {
        available
        docs {
          ... on RuntimePath {
            path
            kind
          }
        }
      }
    }
  }
}
`)
	require.Empty(t, errs)

	deps := env.deps(nil)
	deps.CodeRuntime = fakeCodeRuntime{
		docs: CodeContextPack{
			InputPath:      "pkg/ontology/query/schema.go",
			NormalizedPath: "pkg/ontology/query/schema.go",
			Available:      true,
			Docs:           []RuntimePath{{Path: "pkg/ontology/query/CONTEXT.md", Kind: "ancestor_doc"}},
		},
	}

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)

	codeRoot := result.Data["code"].(map[string]any)
	docsPack := codeRoot["docsForCode"].(map[string]any)
	require.True(t, docsPack["available"].(bool))
	docs := docsPack["docs"].([]any)
	require.Equal(t, "pkg/ontology/query/CONTEXT.md", docs[0].(map[string]any)["path"])
}

func TestExecute_CodeRuntimeErrorKeepsPartialProviderData(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  code {
    docsForCode(path: "../outside.go") {
      inputPath
      normalizedPath
      available
      warnings { code path }
    }
  }
}
`)
	require.Empty(t, errs)

	deps := env.deps(nil)
	deps.CodeRuntime = fakeCodeRuntime{
		docs: CodeContextPack{
			InputPath:      "../outside.go",
			NormalizedPath: "",
			Available:      false,
			Warnings:       []RuntimeWarning{{Code: "invalid_code_path", Path: "../outside.go"}},
		},
		err: os.ErrPermission,
	}

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Len(t, result.Errors, 1)

	codeRoot := result.Data["code"].(map[string]any)
	docsPack := codeRoot["docsForCode"].(map[string]any)
	require.False(t, docsPack["available"].(bool))
	require.Equal(t, "../outside.go", docsPack["inputPath"])
	warnings := docsPack["warnings"].([]any)
	require.Equal(t, "invalid_code_path", warnings[0].(map[string]any)["code"])
}

func TestPrepareWithVariables_RejectsMissingAndUnboundedVariables(t *testing.T) {
	env := newQueryTestEnv(t)

	_, errs := PrepareWithVariables(env.execSchema, `query Test($path: String!) { note(path: $path) { path } }`, nil)
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0].Message, "must be defined")

	_, errs = PrepareWithVariables(env.execSchema, `query Test($path: String) { project(path: $path) { name } }`, nil)
	require.Empty(t, errs)
}

func TestPrepare_DetectsSemanticInsideRootInlineFragment(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  ... on Query {
    project(semantic: "roadmap") {
      name
    }
  }
}
`)
	require.Empty(t, errs)
	require.True(t, prepared.UsesSemantic)
}

func TestExecute_ResolvesStructuralAndAmbientFields(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  project(path: "notes/projects/roadmap-refresh.md") {
    path
    title
    name
    owner { title }
    decisionNotes { title }
    linked(type: "Decision") { title }
    backlinked(type: "Decision") { title }
    connected(type: "Decision") { title }
    decisions { title }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	root := rows[0].(map[string]any)
	require.Equal(t, "notes/projects/roadmap-refresh.md", root["path"])
	require.Equal(t, "Roadmap Refresh", root["name"])
	require.Equal(t, "Alice", root["owner"].(map[string]any)["title"])

	decisionNotes := titles(root["decisionNotes"].([]any))
	require.Equal(t, []string{"Structured Ontology Decision"}, decisionNotes)

	linked := titles(root["linked"].([]any))
	require.Equal(t, []string{"Ambient Linked Decision"}, linked)

	backlinked := titles(root["backlinked"].([]any))
	require.Equal(t, []string{"Backlink Decision"}, backlinked)

	connected := titles(root["connected"].([]any))
	require.Equal(t, []string{"Backlink Decision", "Ambient Linked Decision"}, connected)

	neighbors := titles(root["decisions"].([]any))
	require.Equal(t, []string{"Backlink Decision", "Ambient Linked Decision"}, neighbors)
}

func TestExecute_BatchesRelationFieldsAcrossNodeLists(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["notes/people/*.md"]) {
  name: String!
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  owner: Person! @link
  decisions: [Decision!] @neighbors(direction: BOTH, type: "Decision")
}
`, map[string]string{
		"notes/people/alice.md": `---
type: Person
name: Alice
---
`,
		"notes/projects/alpha.md": `---
type: Project
name: Alpha Project
owner: notes/people/alice.md
---

See [[decision-alpha]].
`,
		"notes/projects/beta.md": `---
type: Project
name: Beta Project
owner: notes/people/alice.md
---

See [[decision-beta]].
`,
		"notes/decisions/decision-alpha.md": `---
type: Decision
name: Decision Alpha
---

References [[alpha]].
`,
		"notes/decisions/decision-beta.md": `---
type: Decision
name: Decision Beta
---

References [[beta]].
`,
	})
	prepared, errs := Prepare(env.execSchema, `
{
  project(find: "Project", first: 2) {
    path
    owner { path title }
    linked(type: "Decision") { path title }
    backlinked(type: "Decision") { path title }
    connected(type: "Decision") { path title }
    decisions { path title }
  }
}
`)
	require.Empty(t, errs)

	spy := &relationBatchSpyStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = spy

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)

	rows := result.Data["project"].([]any)
	require.Len(t, rows, 2)
	require.ElementsMatch(t, []string{"notes/projects/alpha.md", "notes/projects/beta.md"}, []string{
		rows[0].(map[string]any)["path"].(string),
		rows[1].(map[string]any)["path"].(string),
	})
	for _, row := range rows {
		project := row.(map[string]any)
		path := project["path"].(string)
		decisionPath := "notes/decisions/decision-alpha.md"
		decisionTitle := "Decision Alpha"
		if path == "notes/projects/beta.md" {
			decisionPath, decisionTitle = "notes/decisions/decision-beta.md", "Decision Beta"
		} else {
			require.Equal(t, "notes/projects/alpha.md", path)
		}
		require.Equal(t, "notes/people/alice.md", project["owner"].(map[string]any)["path"])
		require.Equal(t, "Alice", project["owner"].(map[string]any)["title"])
		for _, field := range []string{"linked", "backlinked", "connected", "decisions"} {
			require.Equal(t, []any{map[string]any{"path": decisionPath, "title": decisionTitle}}, project[field], field)
		}
	}
	require.Equal(t, 1, spy.structuralCalls)
	require.Equal(t, 3, spy.ambientCalls)
	require.Equal(t, 2, spy.maxStructuralSources)
	require.Equal(t, 2, spy.maxAmbientSources)
}

func TestExecute_BatchesNestedRelationFieldsAcrossParentLists(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}
`, map[string]string{
		"notes/projects/alpha.md": `---
type: Project
name: Alpha Project
---

See [[decision-alpha]].
`,
		"notes/projects/beta.md": `---
type: Project
name: Beta Project
---

See [[decision-beta]].
`,
		"notes/decisions/decision-alpha.md": `---
type: Decision
name: Decision Alpha
---

See [[decision-shared]].
`,
		"notes/decisions/decision-beta.md": `---
type: Decision
name: Decision Beta
---

See [[decision-shared]].
`,
		"notes/decisions/decision-shared.md": `---
type: Decision
name: Decision Shared
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `
{
  project(find: "Project", first: 2) {
    linked(type: "Decision") {
      title
      connected(type: "Decision") { title }
    }
  }
}
`)
	require.Empty(t, errs)

	spy := &relationBatchSpyStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = spy

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)

	rows := result.Data["project"].([]any)
	require.Len(t, rows, 2)
	firstLinked := rows[0].(map[string]any)["linked"].([]any)
	require.NotEmpty(t, firstLinked)
	require.Equal(t, []string{"Decision Shared"}, titles(firstLinked[0].(map[string]any)["connected"].([]any)))
	require.Equal(t, 2, spy.ambientCalls)
	require.Equal(t, 2, spy.maxAmbientSources)
}

func TestExecute_SemanticScoreDoesNotLeakToSiblingRoot(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  semantic: project(semantic: "roadmap") {
    name
    score
  }
  exact: project(find: "Roadmap") {
    name
    score
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(fakeSemanticSearcher{
		surveyResults: []semantic.Result{{NoteID: "notes/projects/roadmap-refresh.md", Score: 0.91}},
	}), env.schema, prepared)
	require.Empty(t, result.Errors)

	semanticRows := result.Data["semantic"].([]any)
	require.Len(t, semanticRows, 1)
	require.Equal(t, "Roadmap Refresh", semanticRows[0].(map[string]any)["name"])
	require.InDelta(t, 0.91, semanticRows[0].(map[string]any)["score"], 0.001)

	exactRows := result.Data["exact"].([]any)
	require.Len(t, exactRows, 1)
	require.Equal(t, "Roadmap Refresh", exactRows[0].(map[string]any)["name"])
	require.Nil(t, exactRows[0].(map[string]any)["score"])
}

func TestPrepareWithVariables_AllowsSemanticListAndDetectsSemantic(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := PrepareWithVariables(env.execSchema, `
query Survey($topics: [String!]) {
  project(semantic: $topics) { name }
}
`, map[string]any{"topics": []any{"roadmap", "planning"}})
	require.Empty(t, errs)
	require.True(t, prepared.UsesSemantic)

	_, errs = PrepareWithVariables(env.execSchema, `
query Survey($topics: [String!]) {
  project(semantic: $topics) { name }
}
`, map[string]any{"topics": []any{}})
	require.Empty(t, errs)
}

func TestPrepareWithVariables_PreservesSemanticStringVariableCompatibility(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := PrepareWithVariables(env.execSchema, `
query Survey($topic: String!) {
  semantic: project(semantic: $topic) { name score }
  exact: project(find: $topic) { name score }
}
`, map[string]any{"topic": "roadmap"})
	require.Empty(t, errs)
	require.True(t, prepared.UsesSemantic)

	result := Execute(context.Background(), env.deps(fakeSemanticSearcher{
		surveyResults: []semantic.Result{{NoteID: "notes/projects/roadmap-refresh.md", Score: 0.91}},
	}), env.schema, prepared)
	require.Empty(t, result.Errors)
	semanticRows := result.Data["semantic"].([]any)
	require.Len(t, semanticRows, 1)
	require.Equal(t, "Roadmap Refresh", semanticRows[0].(map[string]any)["name"])
	require.InDelta(t, 0.91, semanticRows[0].(map[string]any)["score"], 0.001)

	exactRows := result.Data["exact"].([]any)
	require.Len(t, exactRows, 1)
	require.Equal(t, "Roadmap Refresh", exactRows[0].(map[string]any)["name"])
	require.Nil(t, exactRows[0].(map[string]any)["score"])
}

func TestExecute_PublicConnectionBatchSearchAndValidationRoots(t *testing.T) {
	env := newQueryTestEnv(t)
	publishQueryValidationSnapshot(t, env.store, codeanchorsqlite.ValidationSnapshot{
		SelectedChecks: []string{"broken_links"}, IssueCount: 1, AffectedFileCount: 1, AffectedNoteCount: 1,
		Checks: []codeanchorsqlite.ValidationCheckSnapshot{{Check: "broken_links", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 1, Summary: "one broken link"}},
		Diagnostics: []codeanchorsqlite.ValidationDiagnostic{{
			IssueKey: "broken:1", Check: "broken_links", Code: "broken_link", Message: "Missing target",
			Evidence: json.RawMessage(`{"target":"missing"}`), PrimaryPath: "notes/projects/roadmap-refresh.md",
			AffectedPaths: []string{"notes/projects/roadmap-refresh.md"}, AffectedNotePaths: []string{"notes/projects/roadmap-refresh.md"},
			Location: &codeanchorsqlite.ValidationDiagnosticLocation{Unit: codeanchorsqlite.ValidationLocationUnitLine, Start: 12, End: 12},
		}},
	})

	prepared, errs := Prepare(env.execSchema, `
{
  batch: nodes(refs: [
    "notes/projects/roadmap-refresh.md",
    "notes/missing.md"
  ]) {
    pageInfo { queryShapeVersion requestedFirst returnedCount truncated maxFirst }
    items {
      requestedRef
      ref { kind notePath }
      node { nodeKind title }
      error { code message }
    }
  }
  page: notes(type: "Project", first: 1) {
    pageInfo { requestedFirst returnedCount truncated maxFirst }
    nodes { title }
    warnings { code }
  }
  search(query: "roadmap", type: "Project", first: 5) {
    query
    pageInfo { returnedCount truncated }
    nodes { title score }
    warnings { code message }
  }
  validation(firstIssues: 1) {
    ok
    issueCount
    selectedChecks
    checks {
      name
      ok
      issueCount
      issues { code path message line data }
    }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.depsWithSearch(&fakeNoteSearcher{
		hits: []NoteSearchHit{{Path: "notes/projects/roadmap-refresh.md", Score: 0.91}},
	}), env.schema, prepared)
	require.Empty(t, result.Errors)

	batch := result.Data["batch"].(map[string]any)
	require.Equal(t, "v1", batch["pageInfo"].(map[string]any)["queryShapeVersion"])
	require.Equal(t, 2, batch["pageInfo"].(map[string]any)["returnedCount"])
	batchItems := batch["items"].([]any)
	require.Equal(t, "NOTE", batchItems[0].(map[string]any)["node"].(map[string]any)["nodeKind"])
	require.Nil(t, batchItems[0].(map[string]any)["error"])
	require.Equal(t, "unresolved_ref", batchItems[1].(map[string]any)["error"].(map[string]any)["code"])

	page := result.Data["page"].(map[string]any)
	require.Equal(t, 1, page["pageInfo"].(map[string]any)["requestedFirst"])
	require.Equal(t, 1, page["pageInfo"].(map[string]any)["returnedCount"])
	require.Equal(t, false, page["pageInfo"].(map[string]any)["truncated"])
	require.Empty(t, page["warnings"].([]any))

	search := result.Data["search"].(map[string]any)
	require.Equal(t, []any{"roadmap"}, search["query"])
	require.Equal(t, 1, search["pageInfo"].(map[string]any)["returnedCount"])
	require.Equal(t, false, search["pageInfo"].(map[string]any)["truncated"])
	require.Empty(t, search["warnings"].([]any))
	require.InDelta(t, 0.91, search["nodes"].([]any)[0].(map[string]any)["score"], 0.001)

	validation := result.Data["validation"].(map[string]any)
	require.Equal(t, false, validation["ok"])
	require.Equal(t, 1, validation["issueCount"])
	require.Equal(t, []any{"broken_links"}, validation["selectedChecks"])
	check := validation["checks"].([]any)[0].(map[string]any)
	require.Equal(t, "broken_links", check["name"])
	issue := check["issues"].([]any)[0].(map[string]any)
	require.Equal(t, "broken_link", issue["code"])
	require.Equal(t, "missing", issue["data"].(map[string]any)["target"])
}

func TestExecute_ValidationCanonicalizesAliasesAndReportsUnavailableSelections(t *testing.T) {
	env := newQueryTestEnv(t)
	publishQueryValidationSnapshot(t, env.store, codeanchorsqlite.ValidationSnapshot{
		SelectedChecks: []string{"broken_links", "identifiers"}, IssueCount: 3,
		Checks: []codeanchorsqlite.ValidationCheckSnapshot{
			{Check: "broken_links", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 2},
			{Check: "identifiers", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 1},
		},
		Diagnostics: []codeanchorsqlite.ValidationDiagnostic{
			{IssueKey: "broken:1", Check: "broken_links", Code: "broken"},
			{IssueKey: "broken:2", Check: "broken_links", Code: "broken"},
			{IssueKey: "duplicate:1", Check: "identifiers", Code: "duplicate"},
		},
	})

	prepared, errs := Prepare(env.execSchema, `{ validation(check: "aliases") { selectedChecks checks { name issueCount } } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	validation := result.Data["validation"].(map[string]any)
	require.Equal(t, []any{"identifiers"}, validation["selectedChecks"])
	checks := validation["checks"].([]any)
	require.Len(t, checks, 1)
	require.Equal(t, "identifiers", checks[0].(map[string]any)["name"])

	prepared, errs = Prepare(env.execSchema, `{ validation(check: "unknown") { selectedChecks checks { name } } }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "unknown_validation_check", result.Errors[0].Extensions["code"])
	require.Contains(t, result.Errors[0].Message, "unknown")

	prepared, errs = Prepare(env.execSchema, `{ validation(check: "views") { selectedChecks checks { name } } }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "validation_check_unavailable", result.Errors[0].Extensions["code"])
	require.Contains(t, result.Errors[0].Message, "unavailable")
}

func TestExecute_ValidationIssuesHonorsNestedFirstArgument(t *testing.T) {
	env := newQueryTestEnv(t)
	publishQueryValidationSnapshot(t, env.store, codeanchorsqlite.ValidationSnapshot{
		SelectedChecks: []string{"broken_links", "identifiers"}, IssueCount: 3,
		Checks: []codeanchorsqlite.ValidationCheckSnapshot{
			{Check: "broken_links", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 2},
			{Check: "identifiers", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 1},
		},
		Diagnostics: []codeanchorsqlite.ValidationDiagnostic{
			{IssueKey: "one", Check: "broken_links", Code: "one"},
			{IssueKey: "two", Check: "broken_links", Code: "two"},
			{IssueKey: "three", Check: "identifiers", Code: "three"},
		},
	})

	prepared, errs := Prepare(env.execSchema, `{ validation(firstIssues: 1) { checks { name issues(first: 2) { code } } } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	checks := result.Data["validation"].(map[string]any)["checks"].([]any)
	require.Len(t, checks, 2)
	firstIssues := checks[0].(map[string]any)["issues"].([]any)
	require.Len(t, firstIssues, 2)
	require.Equal(t, "one", firstIssues[0].(map[string]any)["code"])
	require.Equal(t, "two", firstIssues[1].(map[string]any)["code"])

	prepared, errs = Prepare(env.execSchema, `{ validation(firstIssues: 201) { checks { issues { code } } } }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "validation_page_limit_invalid", result.Errors[0].Extensions["code"])

	prepared, errs = Prepare(env.execSchema, `{ validation { checks { issues(first: 201) { code } } } }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "validation_page_limit_invalid", result.Errors[0].Extensions["code"])
}

func TestExecute_ValidationRetainsPublishedSnapshotDuringFailedRefresh(t *testing.T) {
	env := newQueryTestEnv(t)
	publishQueryValidationSnapshot(t, env.store, codeanchorsqlite.ValidationSnapshot{
		SelectedChecks: []string{"broken_links"}, IssueCount: 1,
		Checks:      []codeanchorsqlite.ValidationCheckSnapshot{{Check: "broken_links", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 1}},
		Diagnostics: []codeanchorsqlite.ValidationDiagnostic{{IssueKey: "broken:1", Check: "broken_links", Code: "broken"}},
	})
	published, ok, err := env.store.GetPublishedValidationSnapshot(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	require.NotZero(t, published.Generation)
	refreshGeneration, err := env.store.SetValidationRunning(context.Background())
	require.NoError(t, err)
	updated, err := env.store.SetValidationError(context.Background(), refreshGeneration, "refresh failed", 8)
	require.NoError(t, err)
	require.True(t, updated)

	prepared, errs := Prepare(env.execSchema, `{ validation { status generation publishedGeneration completion error issueCount checks { name outcome issues { issueKey } } } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	validation := result.Data["validation"].(map[string]any)
	require.Equal(t, codeanchorsqlite.ValidationStatusError, validation["status"])
	require.Equal(t, int(refreshGeneration), validation["generation"])
	require.Equal(t, int(published.Generation), validation["publishedGeneration"])
	require.Equal(t, codeanchorsqlite.ValidationCompletionComplete, validation["completion"])
	require.Equal(t, "refresh failed", validation["error"])
	require.Equal(t, 1, validation["issueCount"])
	require.Equal(t, "completed", validation["checks"].([]any)[0].(map[string]any)["outcome"])
}

func TestExecute_ValidationScopeSummaryAndDetailMatchRESTContract(t *testing.T) {
	env := newQueryTestEnv(t)
	publishQueryValidationSnapshot(t, env.store, codeanchorsqlite.ValidationSnapshot{
		SelectedChecks: []string{"broken_links"}, IssueCount: 2, AffectedFileCount: 2, AffectedNoteCount: 2, RepairActionCount: 1,
		Checks: []codeanchorsqlite.ValidationCheckSnapshot{{Check: "broken_links", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 2}},
		Diagnostics: []codeanchorsqlite.ValidationDiagnostic{
			{IssueKey: "broken:a", Check: "broken_links", Code: "broken", PrimaryPath: "a.md", AffectedPaths: []string{"a.md"}, AffectedNotePaths: []string{"a.md"}, AffectedNodeIDs: []string{"node-a"}, AffectedTypes: []string{"Story"}, AffectedInterfaces: []string{"WorkItem"}, ActionIDs: []string{"fix-a"}},
			{IssueKey: "broken:b", Check: "broken_links", Code: "broken", PrimaryPath: "b.md", AffectedPaths: []string{"b.md"}, AffectedNotePaths: []string{"b.md"}, AffectedNodeIDs: []string{"node-b"}},
		},
		Actions: []codeanchorsqlite.ValidationActionSnapshot{{ID: "fix-a", Check: "broken_links", Kind: "repair", Safety: "safe", Title: "Fix A", IssueKeys: []string{"broken:a"}, AffectedPaths: []string{"a.md"}}},
	})

	prepared, errs := Prepare(env.execSchema, `{ validation(check: "broken-links", scopeKind: "node", scopeKey: "node-a") { scope { kind key } issueCount affectedFileCount affectedNoteCount repairActionCount checks { name issueCount issues { issueKey affectedNodeIds } } } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	validation := result.Data["validation"].(map[string]any)
	require.Equal(t, map[string]any{"kind": "node", "key": "node-a"}, validation["scope"])
	require.Equal(t, 1, validation["issueCount"])
	require.Equal(t, 1, validation["affectedFileCount"])
	require.Equal(t, 1, validation["affectedNoteCount"])
	require.Equal(t, 1, validation["repairActionCount"])
	check := validation["checks"].([]any)[0].(map[string]any)
	require.Equal(t, "broken_links", check["name"])
	require.Equal(t, 1, check["issueCount"])
	require.Equal(t, "broken:a", check["issues"].([]any)[0].(map[string]any)["issueKey"])

	for _, scope := range []struct {
		kind  string
		key   string
		count int
	}{
		{kind: "global", count: 2},
		{kind: "file", key: "a.md", count: 1},
		{kind: "note", key: "a.md", count: 1},
		{kind: "node", key: "node-a", count: 1},
		{kind: "type", key: "Story", count: 1},
		{kind: "interface", key: "WorkItem", count: 1},
	} {
		t.Run(scope.kind, func(t *testing.T) {
			arguments := fmt.Sprintf(`scopeKind: %q`, scope.kind)
			if scope.key != "" {
				arguments += fmt.Sprintf(`, scopeKey: %q`, scope.key)
			}
			prepared, errs := Prepare(env.execSchema, fmt.Sprintf(`{ validation(%s) { scope { kind key } ok issueCount checks { issueCount issues { issueKey } } } }`, arguments))
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			require.Empty(t, result.Errors)
			validation := result.Data["validation"].(map[string]any)
			require.Equal(t, scope.count, validation["issueCount"])
			require.Equal(t, scope.count == 0, validation["ok"])
			require.Equal(t, scope.count, validation["checks"].([]any)[0].(map[string]any)["issueCount"])
			require.Len(t, validation["checks"].([]any)[0].(map[string]any)["issues"], scope.count)
		})
	}

	prepared, errs = Prepare(env.execSchema, `{ validation(scopeKind: "note", scopeKey: "clean.md") { ok issueCount } }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	cleanValidation := result.Data["validation"].(map[string]any)
	require.Equal(t, 0, cleanValidation["issueCount"])
	require.Equal(t, true, cleanValidation["ok"])

	prepared, errs = Prepare(env.execSchema, `{ validation(scopeKind: "node") { issueCount } }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "validation_scope_invalid", result.Errors[0].Extensions["code"])
}

func publishQueryValidationSnapshot(t *testing.T, store *codeanchorsqlite.Store, snapshot codeanchorsqlite.ValidationSnapshot) {
	t.Helper()
	generation, err := store.SetValidationRunning(context.Background())
	require.NoError(t, err)
	snapshot.Generation = generation
	snapshot.VaultIdentity = "query-test"
	snapshot.Scope = "vault"
	snapshot.Completion = codeanchorsqlite.ValidationCompletionComplete
	snapshot.Checks = append([]codeanchorsqlite.ValidationCheckSnapshot(nil), snapshot.Checks...)
	published, err := store.PublishValidationSnapshot(context.Background(), snapshot)
	require.NoError(t, err)
	require.True(t, published)
}

func TestPrepare_SearchSetsUsesSearchNotSemantic(t *testing.T) {
	env := newQueryTestEnv(t)

	prepared, errs := Prepare(env.execSchema, `
{
  search(query: "roadmap") {
    nodes { title }
  }
}
`)
	require.Empty(t, errs)
	require.True(t, prepared.UsesSearch)
	require.False(t, prepared.UsesSemantic)

	_, errs = Prepare(env.execSchema, `
{
  search(query: "roadmap", mode: SEMANTIC) {
    nodes { title }
  }
}
`)
	require.NotEmpty(t, errs)
}

func TestExecute_AllowsLargePublicRootFirstLimit(t *testing.T) {
	notes := make(map[string]string, 250)
	for i := 1; i <= 250; i++ {
		notes[fmt.Sprintf("notes/projects/project-%03d.md", i)] = fmt.Sprintf(`---
type: Project
name: Project %03d
---
`, i)
	}
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}


`, notes)

	prepared, errs := Prepare(env.execSchema, `
{
  notes(type: "Project", first: 250) {
    pageInfo { requestedFirst returnedCount maxFirst truncated }
    nodes { title }
  }
  project(find: "Project", first: 250) {
    title
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	connection := result.Data["notes"].(map[string]any)
	pageInfo := connection["pageInfo"].(map[string]any)
	require.Equal(t, 250, pageInfo["requestedFirst"])
	require.Equal(t, 250, pageInfo["returnedCount"])
	require.Equal(t, publicRootFirstMax, pageInfo["maxFirst"])
	require.Equal(t, false, pageInfo["truncated"])
	require.Len(t, connection["nodes"].([]any), 250)
	require.Len(t, result.Data["project"].([]any), 250)
}

func TestExecute_TypedRootCoverageAndContinuation(t *testing.T) {
	notes := make(map[string]string, 101)
	for i := 0; i < 101; i++ {
		notes[fmt.Sprintf("notes/projects/project-%03d.md", i)] = fmt.Sprintf("---\ntype: Project\nname: Project %03d\n---\n", i)
	}
	env := newCustomQueryTestEnv(t, `type Project @node(paths: ["notes/projects/*.md"]) { name: String! }`, notes)
	prepared, errs := Prepare(env.execSchema, `{
	  empty: project(find: "absent", first: 100) { path }
	  below: project(first: 102) { path }
	  exact: project(first: 101) { path }
	  capped: project(first: 100) { path }
	  next: project(first: 100, offset: 100) { path }
	  pathPastEnd: project(path: "notes/projects/project-000.md", first: 1, offset: 1) { path }
	}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	coverage := result.Extensions["typedRoots"].(map[string]typedRootCoverage)
	for _, tc := range []struct {
		alias, status string
		count         int
		hasMore       bool
		hasNext       bool
	}{
		{alias: "empty", status: "complete", count: 0},
		{alias: "below", status: "complete", count: 101},
		{alias: "exact", status: "complete", count: 101},
		{alias: "capped", status: "capped", count: 100, hasMore: true, hasNext: true},
		{alias: "next", status: "complete", count: 1},
		{alias: "pathPastEnd", status: "complete", count: 0},
	} {
		require.Len(t, result.Data[tc.alias].([]any), tc.count, tc.alias)
		page := coverage[tc.alias]
		require.Equal(t, tc.status, page.Status, tc.alias)
		require.Equal(t, tc.count, page.ReturnedCount, tc.alias)
		require.NotNil(t, page.HasMore, tc.alias)
		require.Equal(t, tc.hasMore, *page.HasMore, tc.alias)
		if tc.hasNext {
			require.NotNil(t, page.NextOffset, tc.alias)
			require.Equal(t, 100, *page.NextOffset, tc.alias)
		} else {
			require.Nil(t, page.NextOffset, tc.alias)
		}
	}
	require.Equal(t, "notes/projects/project-100.md", result.Data["next"].([]any)[0].(map[string]any)["path"])
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"extensions":{"typedRoots":`)
	require.Contains(t, string(encoded), `"nextOffset":100`)

	_, errs = Prepare(env.execSchema, `{ project(first: 10, offset: -1) { path } }`)
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0].Message, "offset must be >= 0")
}

func TestExecute_TypedSelectorAppliesFiltersAndSortBeforePage(t *testing.T) {
	notes := make(map[string]string, 5)
	for i := 0; i < 5; i++ {
		notes[fmt.Sprintf("notes/projects/project-%03d.md", i)] = fmt.Sprintf("---\ntype: Project\nname: Project %03d\n---\n", i)
	}
	env := newCustomQueryTestEnv(t, `type Project @node(paths: ["notes/projects/*.md"]) { name: String @field }`, notes)
	prepared, errs := Prepare(env.execSchema, `{
	  later: project(find: "Project", first: 1, filters: [{field: "name", op: eq, value: "Project 004"}]) { path }
	  sorted: project(find: "Project", first: 1, sort: [{field: "name", direction: desc}]) { path }
	  second: project(find: "Project", first: 1, offset: 1, sort: [{field: "name", direction: desc}]) { path }
	}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, "notes/projects/project-004.md", result.Data["later"].([]any)[0].(map[string]any)["path"])
	require.Equal(t, "notes/projects/project-004.md", result.Data["sorted"].([]any)[0].(map[string]any)["path"])
	require.Equal(t, "notes/projects/project-003.md", result.Data["second"].([]any)[0].(map[string]any)["path"])
	coverage := result.Extensions["typedRoots"].(map[string]typedRootCoverage)
	require.Equal(t, "complete", coverage["later"].Status)
	require.Equal(t, "capped", coverage["sorted"].Status)
	require.Equal(t, 1, *coverage["sorted"].NextOffset)
	require.Equal(t, "capped", coverage["second"].Status)
	require.Equal(t, 2, *coverage["second"].NextOffset)
}

func TestPrepare_RejectsOversizedPublicFirstLimit(t *testing.T) {
	env := newQueryTestEnv(t)
	_, errs := Prepare(env.execSchema, `{ notes(type: "Project", first: 5001) { nodes { title } } }`)
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0].Message, "first must be <= 5000")
}

func TestPrepare_RejectsOversizedNestedFirstLimit(t *testing.T) {
	env := newQueryTestEnv(t)
	_, errs := Prepare(env.execSchema, `{ search(query: "roadmap", first: 201) { nodes { title } } }`)
	require.NotEmpty(t, errs)
	require.Contains(t, errs[0].Message, "first must be <= 200")
}

func TestExecute_SearchReportsUnavailableSearcher(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  search(query: "roadmap") {
    nodes { title }
    warnings { code message }
    pageInfo { returnedCount }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	search := result.Data["search"].(map[string]any)
	require.Empty(t, search["nodes"].([]any))
	warnings := search["warnings"].([]any)
	require.Len(t, warnings, 1)
	require.Equal(t, "search_unavailable", warnings[0].(map[string]any)["code"])
}

func TestExecute_SearchUsesNoteSearcherAndTypeFilter(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  search(query: ["roadmap", "refresh"], type: "Project", first: 5) {
    query
    nodes { title score }
    warnings { code message }
  }
}
`)
	require.Empty(t, errs)

	searcher := &fakeNoteSearcher{
		hits: []NoteSearchHit{
			{Path: "notes/projects/roadmap-refresh.md", Score: 0.91},
			{Path: "notes/decisions/adopt-rhizome.md", Score: 0.42},
		},
		warnings: []RuntimeWarning{{Code: "vector_unavailable", Message: "embeddings are unavailable"}},
	}
	result := Execute(context.Background(), env.depsWithSearch(searcher), env.schema, prepared)
	require.Empty(t, result.Errors)

	require.Len(t, searcher.requests, 1)
	require.Equal(t, []string{"roadmap", "refresh"}, searcher.requests[0].Queries)
	require.Equal(t, []string{"Project"}, searcher.requests[0].NoteTypes)
	require.Equal(t, 6, searcher.requests[0].First)

	search := result.Data["search"].(map[string]any)
	require.Equal(t, []any{"roadmap", "refresh"}, search["query"])
	nodes := search["nodes"].([]any)
	require.Len(t, nodes, 1)
	require.Equal(t, "Roadmap Refresh", nodes[0].(map[string]any)["title"])
	require.Equal(t, 0.91, nodes[0].(map[string]any)["score"])
	warnings := search["warnings"].([]any)
	require.Len(t, warnings, 1)
	require.Equal(t, "vector_unavailable", warnings[0].(map[string]any)["code"])
}

func TestExecute_InterfaceTypeFilterOverfetchesOnlyOnePageRecord(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface SummaryDoc {
  name: String!
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project implements SummaryDoc @node(paths: ["notes/projects/*.md"]) {
  name: String!
}
`, map[string]string{
		"notes/decisions/one.md": `---
type: Decision
name: Decision One
---
`,
		"notes/decisions/two.md": `---
type: Decision
name: Decision Two
---
`,
		"notes/projects/one.md": `---
type: Project
name: Project One
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `{ notes(type: "SummaryDoc", first: 1) { nodes { path title } } }`)
	require.Empty(t, errs)

	spy := &spyQueryStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = spy
	deps.ExactNoteMetadataRows = nil

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, 2, spy.maxMetadataPathsBatch)
	rows := result.Data["notes"].(map[string]any)["nodes"].([]any)
	require.Len(t, rows, 1)
}

func TestExecute_CachesNoteRecordsWithinRequest(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  project(path: "notes/projects/roadmap-refresh.md") {
    primaryOwner: owner { title }
    repeatedOwner: owner { title }
  }
}
`)
	require.Empty(t, errs)

	spy := &spyQueryStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = spy
	deps.ExactNoteMetadataRows = nil

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	projects := result.Data["project"].([]any)
	require.Len(t, projects, 1)
	project := projects[0].(map[string]any)
	require.Equal(t, "Alice", project["primaryOwner"].(map[string]any)["title"])
	require.Equal(t, "Alice", project["repeatedOwner"].(map[string]any)["title"])
	require.Equal(t, 1, spy.metadataPathLoads["notes/people/alice.md"])
}

func TestExecute_DedupesDuplicateAmbientRelationRows(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type concept @node(paths: ["concepts/*.md"]) {
  name: String!
  connectedConcepts: [concept!] @neighbors(direction: OUTBOUND, type: "concept")
}
`, map[string]string{
		"concepts/information-velocity.md": `---
type: concept
name: Information Velocity
---

See [[clarity-loop]] and [[signal-processing-cycle]].
`,
		"concepts/clarity-loop.md": `---
type: concept
name: Clarity Loop
---
`,
		"concepts/signal-processing-cycle.md": `---
type: concept
name: Signal Processing Cycle
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `{ note(path: "concepts/information-velocity.md") { ... on concept { connectedConcepts { title } } } }`)
	require.Empty(t, errs)

	deps := env.deps(nil)
	deps.Store = &duplicateAmbientStore{Store: env.store}

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)

	note := result.Data["note"].(map[string]any)
	paths := titles(note["connectedConcepts"].([]any))
	require.Equal(t, []string{"Clarity Loop", "Signal Processing Cycle"}, paths)
}

func TestExecute_RequiredListFieldMissingReturnsPartialDataWithError(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  aliases: [String!]! @field(source: "aliases")
}
`, map[string]string{
		"notes/projects/roadmap.md": `---
type: Project
name: Roadmap
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `{ project(path: "notes/projects/roadmap.md") { name aliases } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	require.Equal(t, "Roadmap", record["name"])
	_, hasAliases := record["aliases"]
	require.False(t, hasAliases)
}

func TestExecute_MixedCaseSourceAndEmptyListRoundTripFromMetadata(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  status: String! @field(source: "Status")
  aliases: [String!]! @field(source: "Aliases")
}
`, map[string]string{
		"notes/projects/roadmap.md": `---
type: Project
name: Roadmap
Status: active
Aliases: []
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `{ project(path: "notes/projects/roadmap.md") { status aliases } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	require.Equal(t, "active", record["status"])
	require.Equal(t, []any{}, record["aliases"])
}

func TestExecute_DoesNotParsePathsMissingFromMetadataRows(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["notes/people/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  owner: Person! @link(source: "owner")
}
`, map[string]string{
		"notes/projects/roadmap.md": `---
type: Project
name: Roadmap
owner: notes/people/alice.md
---
`,
		"notes/people/alice.md": `---
type: Person
name: Alice
---
`,
	})
	require.NoError(t, env.store.ApplyNoteMetadataDelta(context.Background(), codeanchorsqlite.NoteMetadataDelta{
		State:        codeanchorsqlite.NoteMetadataState{LoadedAt: 99, Ready: true},
		DeletedPaths: []string{"notes/people/alice.md"},
	}))

	prepared, errs := Prepare(env.execSchema, `{ project(path: "notes/projects/roadmap.md") { name owner { name } } }`)
	require.Empty(t, errs)
	reader := &inventoryQueryReader{NoteReader: &obsidian.Note{}, contents: map[string]int{}}
	deps := env.deps(nil)
	deps.NoteReader = reader
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.NotEmpty(t, result.Errors)
	require.Contains(t, result.Errors[0].Message, "required relation Project.owner is missing")
	require.Equal(t, []string{"project", "owner"}, result.Errors[0].Path)
	projects := result.Data["project"].([]any)
	require.Len(t, projects, 1)
	require.Equal(t, "Roadmap", projects[0].(map[string]any)["name"])
	require.Zero(t, reader.contents["notes/people/alice.md"])
}

func TestExecute_InvalidTypedNotesStillResolveAtRoot(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}
`, map[string]string{
		"notes/projects/good.md": `---
type: Project
name: Good
---
`,
		"notes/projects/bad.md": `---
type: Project
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `{ project(find: "project") { path name } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 2)
	require.Equal(t, "notes/projects/bad.md", rows[0].(map[string]any)["path"])
	require.Equal(t, "notes/projects/good.md", rows[1].(map[string]any)["path"])
}

func TestExecute_ProjectKBStarterPatternResolvesStructuralRelations(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
enum ArtifactStatus {
  DRAFT
  ACTIVE
}

enum QuestionStatus {
  OPEN
  ANSWERED
}

"""
Project operating hub.
Create one when the repo needs a durable center of gravity for decisions, artifacts, and open questions.
"""
type Project @node(paths: ["notes/projects/*.md"]) {
  """Project summary."""
  summary: String!
  decisions: [Decision!] @link(inverse: "project")
  artifacts: [Artifact!] @link(inverse: "project")
  openQuestions: [OpenQuestion!] @link(inverse: "project")
}

"""Decision note."""
type Decision @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
  project: Project! @link(inverse: "decisions")
}

"""Artifact note."""
type Artifact @node(paths: ["notes/artifacts/*.md"]) {
  summary: String!
  status: ArtifactStatus!
  project: Project! @link(inverse: "artifacts")
}

"""Open question note."""
type OpenQuestion @node(paths: ["notes/questions/*.md"]) {
  summary: String!
  status: QuestionStatus!
  project: Project! @link(inverse: "openQuestions")
}
`, map[string]string{
		"notes/projects/atlas.md": `---
type: Project
summary: Atlas operating hub
decisions:
  - notes/decisions/scope-freeze.md
artifacts:
  - notes/artifacts/brief.md
open-questions:
  - notes/questions/api-shape.md
---
`,
		"notes/decisions/scope-freeze.md": `---
type: Decision
summary: Freeze scope for v1
project: notes/projects/atlas.md
---
`,
		"notes/artifacts/brief.md": `---
type: Artifact
summary: Client brief
status: ACTIVE
project: notes/projects/atlas.md
---
`,
		"notes/questions/api-shape.md": `---
type: OpenQuestion
summary: Finalize public API shape
status: OPEN
project: notes/projects/atlas.md
---
`,
	})

	prepared, errs := Prepare(env.execSchema, `
{
  project(path: "notes/projects/atlas.md") {
    summary
    decisions { title }
    artifacts { title }
    openQuestions { title }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	require.Equal(t, "Atlas operating hub", record["summary"])
	require.Equal(t, []string{"scope-freeze"}, titles(record["decisions"].([]any)))
	require.Equal(t, []string{"brief"}, titles(record["artifacts"].([]any)))
	require.Equal(t, []string{"api-shape"}, titles(record["openQuestions"].([]any)))
}

func TestExecute_ResolvesInterfacesAndSections(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface SummaryDoc {
  summary: String!
}

type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec implements SummaryDoc @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

# Intro

Context.

## Requirements

See [[decision-one]].

### Details

Extra detail.
`,
		"notes/decisions/decision-one.md": `---
type: Decision
summary: Decision summary
---
`,
	})

	prepared, errs := Prepare(env.execSchema, `
{
  notes(type: "SummaryDoc", find: "notes") {
    nodes {
      ... on SummaryDoc { summary }
      ... on Spec {
        requirements {
          title
          level
          content
          children { title }
          decisions { title }
        }
      }
    }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	rows := result.Data["notes"].(map[string]any)["nodes"].([]any)
	require.Len(t, rows, 2)

	var spec map[string]any
	for _, row := range rows {
		record := row.(map[string]any)
		if _, ok := record["requirements"]; ok {
			spec = record
			break
		}
	}
	require.NotNil(t, spec)
	require.Equal(t, "Spec summary", spec["summary"])

	requirements := spec["requirements"].(map[string]any)
	require.Equal(t, "Requirements", requirements["title"])
	require.Equal(t, "H2", requirements["level"])
	require.Contains(t, requirements["content"], "See [[decision-one]]")
	require.Equal(t, []string{"Details"}, titles(requirements["children"].([]any)))
	require.Equal(t, []string{"decision-one"}, titles(requirements["decisions"].([]any)))
}

func TestExecute_ResolvesBareInterfaceTypeFilter(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface SummaryDoc {
  summary: String!
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec implements SummaryDoc @node(paths: ["notes/specs/*.md"]) {
  summary: String!
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---
`,
		"notes/decisions/decision-one.md": `---
type: Decision
summary: Decision summary
---
`,
	})

	prepared, errs := Prepare(env.execSchema, `
{
  notes(type: "SummaryDoc") {
    nodes {
      title
      ... on SummaryDoc { summary }
    }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["notes"].(map[string]any)["nodes"].([]any)
	require.Len(t, rows, 2)
	require.Equal(t, []string{"decision-one", "spec"}, titles(rows))
}

func TestExecute_AmbientBuiltinsApplyFirstAfterInterfaceFiltering(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface SummaryDoc {
  summary: String!
}

type Alert @node(paths: ["notes/alerts/*.md"]) {
  summary: String!
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  summary: String!
}
`, map[string]string{
		"notes/projects/roadmap.md": `---
type: Project
summary: Roadmap
---

See [[alert-one]] and [[decision-one]].
`,
		"notes/alerts/alert-one.md": `---
type: Alert
summary: Alert summary
---
`,
		"notes/decisions/decision-one.md": `---
type: Decision
summary: Decision summary
---
`,
	})

	prepared, errs := Prepare(env.execSchema, `{ project(path: "notes/projects/roadmap.md") { linked(type: "SummaryDoc", first: 1) { title } } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	require.Equal(t, []string{"decision-one"}, titles(record["linked"].([]any)))
}

func TestExecute_SectionNeighborsStayInsideSubtree(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

See [[outside-decision]].

## Requirements

See [[inside-decision]].
` + "\n```md\n[[fenced-decision]]\n```\n\nInline example `[[inline-decision]]`.\n",
		"notes/decisions/inside-decision.md": `---
type: Decision
summary: Inside
---
`,
		"notes/decisions/outside-decision.md": `---
type: Decision
summary: Outside
---
`,
		"notes/decisions/fenced-decision.md": "---\ntype: Decision\nsummary: Fenced\n---\n",
		"notes/decisions/inline-decision.md": "---\ntype: Decision\nsummary: Inline\n---\n",
	})

	prepared, errs := Prepare(env.execSchema, `{ spec(path: "notes/specs/spec.md") { requirements { decisions { title } } } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	requirements := record["requirements"].(map[string]any)
	require.Equal(t, []string{"inside-decision"}, titles(requirements["decisions"].([]any)))
}

func TestExecute_SectionInboundNeighborsResolveAnchoredBacklinks(t *testing.T) {
	for _, tc := range []struct {
		name, heading, block, target string
	}{
		{"block ID", "Alpha", "^US-001\n", "spec#^US-001"},
		{"heading", "Alpha Story", "", "spec#Alpha Story"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newCustomQueryTestEnv(t, `
type Effort @node(paths: ["notes/efforts/*.md"]) {
  summary: String!
}

type StorySection implements Section {
  efforts: [Effort!] @neighbors(direction: INBOUND, type: "Effort", scope: SUBTREE)
}

type StoriesSection implements Section {
  stories: [StorySection!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories", required: true)
}
`, map[string]string{
				"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## Stories

### ` + tc.heading + `

` + tc.block + `

Alpha body.
`,
				"notes/efforts/effort.md": `---
type: Effort
summary: Effort summary
---

Targets [[` + tc.target + `]].
`,
			})

			prepared, errs := Prepare(env.execSchema, `{ spec(path: "notes/specs/spec.md") { stories { stories { efforts { title } } } } }`)
			require.Empty(t, errs)

			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			require.Empty(t, result.Errors)
			rows := result.Data["spec"].([]any)
			require.Len(t, rows, 1)
			record := rows[0].(map[string]any)
			storiesSection := record["stories"].(map[string]any)
			stories := storiesSection["stories"].([]any)
			require.Len(t, stories, 1)
			story := stories[0].(map[string]any)
			require.Equal(t, []string{"effort"}, titles(story["efforts"].([]any)))
		})
	}
}

func TestExecute_SectionInboundNeighborsResolveFrozenStoryEdges(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type EffortNote @node(paths: ["notes/efforts/*.md"]) {
  summary: String!
}

type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
  efforts: [EffortNote!] @neighbors(direction: INBOUND, type: "EffortNote", scope: SUBTREE)
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories", required: true)
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## User Stories

### Alpha

id:: SPEC-1234.US1
status:: ready
^spec-1234-us1
`,
		"notes/efforts/effort.md": `---
type: EffortNote
summary: Effort summary
---

## Stories In Scope (Frozen)

- SPEC-1234.US1
`,
	})

	prepared, errs := Prepare(env.execSchema, `{ spec(path: "notes/specs/spec.md") { userStories { stories { efforts { title } } } } }`)
	require.Empty(t, errs)

	edges, err := env.store.OntologyEdgesForPaths(context.Background(), []string{"notes/efforts/effort.md"}, false, "frozenStories", 0)
	require.NoError(t, err)
	require.Len(t, edges, 1)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	storiesSection := record["userStories"].(map[string]any)
	stories := storiesSection["stories"].([]any)
	require.Len(t, stories, 1)
	story := stories[0].(map[string]any)
	require.Equal(t, []string{"effort"}, titles(story["efforts"].([]any)))
}

func TestExecute_ResolvesNestedTypedSubsections(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type DetailsSection implements Section {
}

type RequirementsSection implements Section {
  details: [DetailsSection!] @contains(level: H3, heading: "Details")
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## Requirements

Top.

### Details

First details.

### Details

Second details.
`,
	})

	prepared, errs := Prepare(env.execSchema, `{ spec(path: "notes/specs/spec.md") { requirements { details { title content } } } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	requirements := record["requirements"].(map[string]any)
	details := requirements["details"].([]any)
	require.Len(t, details, 2)
	require.Contains(t, details[0].(map[string]any)["content"], "First details.")
	require.Contains(t, details[1].(map[string]any)["content"], "Second details.")
}

func TestExecute_ChildrenPreserveConcreteSectionTypes(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type DetailSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type RequirementsSection implements Section {
  details: DetailSection @contains(level: H3, heading: "Details")
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`, map[string]string{
		"notes/specs/spec.md":             "---\ntype: Spec\nsummary: Spec summary\n---\n\n## Requirements\n\nTop.\n\n### Details\n\nSee [[decision-one]].\n\n### Misc\n\nUnmatched child.\n",
		"notes/decisions/decision-one.md": "---\ntype: Decision\nsummary: Decision summary\n---\n",
	})

	prepared, errs := Prepare(env.execSchema, `
{
  spec(path: "notes/specs/spec.md") {
    requirements {
      children {
        title
        ... on DetailSection {
          decisions { title }
        }
      }
    }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	requirements := record["requirements"].(map[string]any)
	children := requirements["children"].([]any)
	require.Len(t, children, 2)
	details := children[0].(map[string]any)
	require.Equal(t, "Details", details["title"])
	require.Equal(t, []string{"decision-one"}, titles(details["decisions"].([]any)))

	misc := children[1].(map[string]any)
	require.Equal(t, "Misc", misc["title"])
	_, ok := misc["decisions"]
	require.False(t, ok)
}

func TestExecute_ChildrenFallBackToSectionWhenMatchIsAmbiguous(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type DetailSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type AlternateDetailSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type RequirementsSection implements Section {
  details: DetailSection @contains(level: H3, heading: "Details")
  alternateDetails: AlternateDetailSection @contains(level: H3, heading: "Details")
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`, map[string]string{
		"notes/specs/spec.md":             "---\ntype: Spec\nsummary: Spec summary\n---\n\n## Requirements\n\n### Details\n\nSee [[decision-one]].\n",
		"notes/decisions/decision-one.md": "---\ntype: Decision\nsummary: Decision summary\n---\n",
	})

	prepared, errs := Prepare(env.execSchema, `
{
  spec(path: "notes/specs/spec.md") {
    requirements {
      children {
        title
        ... on DetailSection {
          decisions { title }
        }
      }
    }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	requirements := record["requirements"].(map[string]any)
	children := requirements["children"].([]any)
	require.Len(t, children, 1)
	child := children[0].(map[string]any)
	require.Equal(t, "Details", child["title"])
	_, ok := child["decisions"]
	require.False(t, ok)
}

func TestExecute_SingularSectionDuplicateReturnsError(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## Requirements

See [[decision-one]].

## Requirements

See [[decision-two]].
`,
		"notes/decisions/decision-one.md": `---
type: Decision
summary: One
---
`,
		"notes/decisions/decision-two.md": `---
type: Decision
summary: Two
---
`,
	})

	prepared, errs := Prepare(env.execSchema, `{ spec(path: "notes/specs/spec.md") { summary requirements { title } } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, "must match a single heading")

	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	require.Equal(t, "Spec summary", record["summary"])
	_, ok := record["requirements"]
	require.False(t, ok)
}

func TestExecute_ListSectionsPreserveOrderAndResolveMarkdownLinks(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface SummaryDoc {
  summary: String!
}

type ReferenceSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec implements SummaryDoc @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  references: [ReferenceSection!] @contains(level: H2, heading: "Reference")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## Reference

[First](../decisions/decision-one.md)

## Reference

[[decision-two]]
`,
		"notes/decisions/decision-one.md": `---
type: Decision
summary: One
---
`,
		"notes/decisions/decision-two.md": `---
type: Decision
summary: Two
---
`,
	})

	prepared, errs := Prepare(env.execSchema, `
{
  notes(type: "SummaryDoc", find: "notes/specs/spec") {
    nodes {
      ... on SummaryDoc { summary }
      ... on Spec {
        references {
          content
          decisions { title }
        }
      }
    }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	rows := result.Data["notes"].(map[string]any)["nodes"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	require.Equal(t, "Spec summary", record["summary"])
	references := record["references"].([]any)
	require.Len(t, references, 2)
	require.Contains(t, references[0].(map[string]any)["content"], "[First]")
	require.Equal(t, []string{"decision-one"}, titles(references[0].(map[string]any)["decisions"].([]any)))
	require.Contains(t, references[1].(map[string]any)["content"], "[[decision-two]]")
	require.Equal(t, []string{"decision-two"}, titles(references[1].(map[string]any)["decisions"].([]any)))
}

// TestExecute_ListSectionWithoutHeadingMatchesDirectChildren verifies the
// new list-no-heading semantics: `[X!] @contains(level: H3)` picks up every
// direct child of the enclosing scope at that level, in document order.
func TestExecute_ListSectionWithoutHeadingMatchesDirectChildren(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
enum UserStoryStatus { PLANNED IN_PROGRESS COMPLETE }

type UserStory implements Section {
  status: UserStoryStatus @field
  owner: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## User Stories

### Resume after compaction

Status:: IN_PROGRESS
Owner:: drew

Body for first story.

### Schedule future indexing

Status:: PLANNED

Body for second story.

### Other thing

Status:: COMPLETE
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    userStories {
      stories {
        title
        ... on UserStory { status owner }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["spec"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	userStories := record["userStories"].(map[string]any)
	stories := userStories["stories"].([]any)
	require.Len(t, stories, 3)

	first := stories[0].(map[string]any)
	require.Equal(t, "Resume after compaction", first["title"])
	require.Equal(t, "IN_PROGRESS", first["status"])
	require.Equal(t, "drew", first["owner"])

	second := stories[1].(map[string]any)
	require.Equal(t, "Schedule future indexing", second["title"])
	require.Equal(t, "PLANNED", second["status"])
	require.Nil(t, second["owner"])

	third := stories[2].(map[string]any)
	require.Equal(t, "Other thing", third["title"])
	require.Equal(t, "COMPLETE", third["status"])
}

func TestExecute_ListSectionWithoutHeadingUnwrapsSingleH1(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section {
  status: String @field
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  stories: [UserStory!] @contains(level: H2)
}
`, map[string]string{
		"notes/specs/spec.md": `# Spec title

## First story

Status:: in-progress

## Second story

Status:: planned
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    stories {
      title
      ... on UserStory { status }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	stories := result.Data["spec"].([]any)[0].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 2)
	require.Equal(t, "First story", stories[0].(map[string]any)["title"])
	require.Equal(t, "Second story", stories[1].(map[string]any)["title"])
}

// TestExecute_ListSectionWithoutHeading_EmptyIsNotAnError verifies that a
// non-required list-no-heading field returns an empty list when the parent
// has no matching children, rather than raising an error.
func TestExecute_ListSectionWithoutHeading_EmptyIsNotAnError(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type UserStory implements Section {
  status: String @field
}

type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  userStories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## User Stories

Nothing under here.
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    userStories { stories { title } }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	record := result.Data["spec"].([]any)[0].(map[string]any)
	stories := record["userStories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 0)
}

// TestExecute_SectionScalarField_ReusedAcrossPropertyCases verifies that the
// same section type can be reused under note types with different property
// cases — the inline key is derived lazily from the enclosing note type.
func TestExecute_SectionScalarField_ReusedAcrossPropertyCases(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Story implements Section {
  storyStatus: String @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type SpecKebab @node(paths: ["notes/kebab/*.md"], propertyCase: KEBAB) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type SpecCamel @node(paths: ["notes/camel/*.md"], propertyCase: CAMEL) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`, map[string]string{
		"notes/kebab/spec.md": `---
type: SpecKebab
summary: kebab summary
---

## Stories

### One

story-status:: kebab-value
`,
		"notes/camel/spec.md": `---
type: SpecCamel
summary: camel summary
---

## Stories

### Two

storyStatus:: camel-value
`,
	})

	query := func(path string) map[string]any {
		prepared, errs := Prepare(env.execSchema, `{
  note(path: "`+path+`") {
    ... on SpecKebab { stories { stories { title ... on Story { storyStatus } } } }
    ... on SpecCamel { stories { stories { title ... on Story { storyStatus } } } }
  }
}`)
		require.Empty(t, errs)
		result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
		require.Empty(t, result.Errors)
		return result.Data["note"].(map[string]any)
	}

	kebab := query("notes/kebab/spec.md")
	kebabStories := kebab["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, kebabStories, 1)
	require.Equal(t, "kebab-value", kebabStories[0].(map[string]any)["storyStatus"])

	camel := query("notes/camel/spec.md")
	camelStories := camel["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, camelStories, 1)
	require.Equal(t, "camel-value", camelStories[0].(map[string]any)["storyStatus"])
}

func TestExecute_EmbeddedNodeLinkField_ResolvesTypedTarget(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Effort @node(paths: ["notes/efforts/*.md"]) {
  summary: String!
}

type Story implements Section @node(locator: EMBEDDED) {
  status: String @field
  effort: Effort @link
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: spec summary
---

## Stories

### Story one

status:: ready
effort:: [[notes/efforts/effort.md]]
`,
		"notes/efforts/effort.md": `---
type: Effort
summary: effort summary
---
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    stories {
      stories {
        title
        ... on Story {
          status
          effort { path summary }
        }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	record := result.Data["spec"].([]any)[0].(map[string]any)
	stories := record["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 1)
	story := stories[0].(map[string]any)
	require.Equal(t, "ready", story["status"])
	effort := story["effort"].(map[string]any)
	require.Equal(t, "notes/efforts/effort.md", effort["path"])
	require.Equal(t, "effort summary", effort["summary"])
}

func TestExecute_EmbeddedNodePackedInlineProperties(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
enum StoryStatus { ready draft }

type Story implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  summary: String! @field
  status: StoryStatus! @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: spec summary
---

## Stories

### Fleet-wide cart health

id:: SPEC-0013.US1 summary:: A manager can review aggregate fleet health and open a detail view of carts that need
attention. status:: ready
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    stories {
      stories {
        title
        ... on Story {
          summary
          status
        }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	record := result.Data["spec"].([]any)[0].(map[string]any)
	stories := record["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 1)
	story := stories[0].(map[string]any)
	require.Equal(t, "A manager can review aggregate fleet health and open a detail view of carts that need attention.", story["summary"])
	require.Equal(t, "ready", story["status"])
}

func TestExecute_EmbeddedNodeAuthoredIDFieldOverridesSyntheticSectionID(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Story implements Section @node(locator: EMBEDDED) {
  id: ID! @field
  status: String @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: spec summary
---

## Stories

### Story one

id:: SPEC-42.US1
status:: ready
`,
	})
	idCount := 0
	for _, field := range env.execSchema.Schema.Types["Story"].Fields {
		if field.Name == "id" {
			idCount++
			require.Equal(t, "ID!", field.Type.String())
		}
	}
	require.Equal(t, 1, idCount)

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    stories {
      stories {
        ... on Story { id status }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	record := result.Data["spec"].([]any)[0].(map[string]any)
	stories := record["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 1)
	story := stories[0].(map[string]any)
	require.Equal(t, "SPEC-42.US1", story["id"])
	require.Equal(t, "ready", story["status"])
}

func TestExecute_SectionScalarField_RequiredInvalidEnumReturnsError(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
enum StoryStatus { PLANNED IN_PROGRESS COMPLETE }

type Story implements Section {
  status: StoryStatus! @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## Stories

### Bad story

Status:: STARTED
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    stories {
      stories {
        title
        ... on Story { status }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, `field status value "STARTED" does not match StoryStatus`)

	record := result.Data["spec"].([]any)[0].(map[string]any)
	stories := record["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 1)
	story := stories[0].(map[string]any)
	require.Equal(t, "Bad story", story["title"])
	_, hasStatus := story["status"]
	require.False(t, hasStatus)
}

func TestExecute_SectionScalarField_InvalidScalarDoesNotLeakRawString(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Story implements Section {
  points: Int @field
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

## Stories

### Bad story

Points:: not-a-number
`,
	})

	prepared, errs := Prepare(env.execSchema, `{
  spec(path: "notes/specs/spec.md") {
    stories {
      stories {
        title
        ... on Story { points }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	record := result.Data["spec"].([]any)[0].(map[string]any)
	stories := record["stories"].(map[string]any)["stories"].([]any)
	require.Len(t, stories, 1)
	story := stories[0].(map[string]any)
	require.Equal(t, "Bad story", story["title"])
	value, hasPoints := story["points"]
	require.True(t, hasPoints)
	require.Nil(t, value)
}

type queryTestEnv struct {
	root         string
	schema       *ontology.Schema
	execSchema   *ExecutableSchema
	store        *codeanchorsqlite.Store
	noteMetadata notemeta.Indexer
}

type identifierScanCountingStore struct {
	*codeanchorsqlite.Store
	preferredIdentifierScans atomic.Int64
	embeddedIdentifierScans  atomic.Int64
}

func (s *identifierScanCountingStore) CurrentNotePathsByPropertyValue(ctx context.Context, name, value string, source codeanchorsqlite.NotePropertySource) ([]string, error) {
	s.preferredIdentifierScans.Add(1)
	return s.Store.CurrentNotePathsByPropertyValue(ctx, name, value, source)
}

func (s *identifierScanCountingStore) OntologyPathsByType(ctx context.Context, typeName string, limit int) ([]string, error) {
	s.embeddedIdentifierScans.Add(1)
	return s.Store.OntologyPathsByType(ctx, typeName, limit)
}

type contentFailingQueryNoteReader struct {
	inner obsidian.NoteReader
}

func (r contentFailingQueryNoteReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return "", fmt.Errorf("unexpected markdown projection")
}

func (r contentFailingQueryNoteReader) GetNotesList(vault obsidian.VaultDefinition) ([]string, error) {
	return r.inner.GetNotesList(vault)
}

func (r contentFailingQueryNoteReader) GetModTime(vault obsidian.VaultDefinition, noteName string) (time.Time, error) {
	return r.inner.GetModTime(vault, noteName)
}

func (r contentFailingQueryNoteReader) Title(noteName string) (string, bool) {
	return r.inner.Title(noteName)
}

type countingNoteReader struct {
	inner          obsidian.NoteReader
	notesListCalls atomic.Int64
}

func (r *countingNoteReader) GetContents(vault obsidian.VaultDefinition, noteName string) (string, error) {
	return r.inner.GetContents(vault, noteName)
}

func (r *countingNoteReader) GetNotesList(vault obsidian.VaultDefinition) ([]string, error) {
	r.notesListCalls.Add(1)
	return r.inner.GetNotesList(vault)
}

func (r *countingNoteReader) GetModTime(vault obsidian.VaultDefinition, noteName string) (time.Time, error) {
	return r.inner.GetModTime(vault, noteName)
}

func (r *countingNoteReader) Title(noteName string) (string, bool) {
	return r.inner.Title(noteName)
}

func newQueryTestEnv(t *testing.T) *queryTestEnv {
	t.Helper()
	return newCustomQueryTestEnv(t, `
type Team @node(paths: ["notes/teams/*.md"]) {
  name: String!
  members: [Person!] @link(inverse: "team")
}

type Person @node(paths: ["notes/people/*.md"]) {
  """Team member name."""
  name: String!
  role: String
  team: Team @link(inverse: "members")
  projects: [Project!] @link(inverse: "owner")
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

"""Project note doc."""
type Project @node(paths: ["notes/projects/*.md"]) @semantics(kind: BEHAVIORAL) {
  """Display name."""
  name: String!
  owner: Person! @link(inverse: "projects")
  decisionNotes: [Decision!] @link(source: "decisionNotes")
  decisions: [Decision!] @neighbors(direction: BOTH, type: "Decision")
}
`, map[string]string{
		"notes/teams/platform-team.md": `---
type: Team
name: Platform Team
members:
  - notes/people/alice.md
---
`,
		"notes/people/alice.md": `---
type: Person
name: Alice
role: Staff Engineer
team: notes/teams/platform-team.md
projects:
  - notes/projects/roadmap-refresh.md
---
`,
		"notes/projects/roadmap-refresh.md": `---
type: Project
name: Roadmap Refresh
owner: notes/people/alice.md
decisionNotes:
  - notes/decisions/structured-ontology-decision.md
---

We finalized follow-up work in [[ambient-linked-decision]].
`,
		"notes/decisions/structured-ontology-decision.md": `---
type: Decision
name: Structured Ontology Decision
---

Canonical structural decision note for the roadmap refresh effort.
`,
		"notes/decisions/ambient-linked-decision.md": `---
type: Decision
name: Ambient Linked Decision
---

Ambiently linked from the project body.
`,
		"notes/decisions/backlink-decision.md": `---
type: Decision
name: Backlink Decision
---

This decision references [[roadmap-refresh]] from the opposite direction.
`,
	})
}

func newCustomQueryTestEnv(t *testing.T, schemaBody string, notes map[string]string) *queryTestEnv {
	t.Helper()
	root := t.TempDir()
	writeQueryTestConfig(t, root)
	writeQueryTestSchema(t, root, schemaBody)
	for path, body := range notes {
		writeQueryTestNote(t, root, path, body)
	}

	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	execSchema, err := BuildExecutableSchema(schema)
	require.NoError(t, err)

	dbPath := filepath.Join(root, ".rhizome", "db.sqlite")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	store, err := sqlitefixture.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	noteMetadata := testNoteMetadataIndexer(t)
	_, err = noteMetadata.EnsureIndexed(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store)
	require.NoError(t, err)

	result, err := ontology.BuildIndexWithStore(context.Background(), noteMetadata, obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, store, schema, "notes-hash")
	require.NoError(t, err)

	require.NoError(t, store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		Assessments: result.AssessmentRows,
		NoteTypes:   result.NoteTypes,
		Edges:       result.Edges,
		SchemaState: codeanchorsqlite.OntologySchemaState{
			SchemaHash: schema.Hash,
			NotesHash:  result.NotesHash,
			LoadedAt:   1,
			Ready:      true,
		},
	}))
	require.NoError(t, store.ReplaceOntologyNodeReadModel(context.Background(), codeanchor.IntelOntologyNodeReadModel{
		NotePaths:   result.NodePaths,
		Nodes:       result.Nodes,
		FieldValues: result.NodeFieldValues,
	}))

	return &queryTestEnv{
		root:         root,
		schema:       schema,
		execSchema:   execSchema,
		store:        store,
		noteMetadata: noteMetadata,
	}
}

func (e *queryTestEnv) deps(searcher SemanticSearcher) Deps {
	return Deps{
		VaultDef:         obsidian.VaultDefinition{Path: e.root},
		NoteReader:       &obsidian.Note{},
		Store:            e.store,
		SemanticSearcher: searcher,
		Service:          ontology.NewService(obsidian.VaultDefinition{Path: e.root}, &obsidian.Note{}, e.store, e.schema),
		ExactNoteMetadataRows: func(ctx context.Context, paths []string) (map[string]codeanchorsqlite.NoteMetadataRow, error) {
			return e.noteMetadata.UsableNoteMetadataRowsByPaths(ctx, e.store, paths)
		},
	}
}

func (e *queryTestEnv) depsWithSearch(searcher NoteSearcher) Deps {
	deps := e.deps(nil)
	deps.NoteSearcher = searcher
	return deps
}

type fakeNoteSearcher struct {
	hits     []NoteSearchHit
	warnings []RuntimeWarning
	requests []NoteSearchRequest
}

func (f *fakeNoteSearcher) SearchNotes(_ context.Context, request NoteSearchRequest) (NoteSearchResponse, error) {
	f.requests = append(f.requests, request)
	return NoteSearchResponse{Hits: f.hits, Warnings: f.warnings}, nil
}

type fakeSemanticSearcher struct {
	results       []semantic.Result
	surveyResults []semantic.Result
}

func (f fakeSemanticSearcher) Search(context.Context, semantic.SearchRequest) ([]semantic.Result, error) {
	return f.results, nil
}

func (f fakeSemanticSearcher) SurveyNodesByType(context.Context, semantic.SurveyNodesByTypeRequest) ([]semantic.Result, error) {
	if f.surveyResults != nil {
		return f.surveyResults, nil
	}
	return f.results, nil
}

type fakeCodeRuntime struct {
	docs  CodeContextPack
	code  CodeContextPack
	tests CodeContextPack
	err   error
}

type recordingOntologyRuntime struct {
	request OntologyNextIDRequest
}

func (*recordingOntologyRuntime) AuthoringGuide(context.Context, OntologyAuthoringGuideRequest) (OntologyAuthoringGuide, error) {
	return OntologyAuthoringGuide{}, nil
}

func (r *recordingOntologyRuntime) NextID(_ context.Context, request OntologyNextIDRequest) (OntologyNextID, error) {
	r.request = request
	disambiguator := 2
	return OntologyNextID{
		Type:  request.Type,
		Count: len(request.Paths),
		Paths: append([]string(nil), request.Paths...),
		Allocations: []OntologyIDAllocation{
			{
				Path: request.Paths[0],
				ID:   "PROJECT-2026-08-05-14-32",
				Base: "PROJECT-2026-08-05-14-32",
			},
			{
				Path:          request.Paths[1],
				ID:            "PROJECT-2026-08-05-14-32-2",
				Base:          "PROJECT-2026-08-05-14-32",
				Disambiguator: &disambiguator,
			},
		},
		Available: true,
	}, nil
}

func (*recordingOntologyRuntime) CurrentUser(context.Context) (OntologyCurrentUser, error) {
	return OntologyCurrentUser{}, nil
}

func (f fakeCodeRuntime) DocsForCode(context.Context, CodeRuntimeRequest) (CodeContextPack, error) {
	return f.docs, f.err
}

func (f fakeCodeRuntime) CodeForNote(context.Context, CodeRuntimeRequest) (CodeContextPack, error) {
	return f.code, f.err
}

func (f fakeCodeRuntime) TestsForCode(context.Context, CodeRuntimeRequest) (CodeContextPack, error) {
	return f.tests, f.err
}

type spyQueryStore struct {
	*codeanchorsqlite.Store
	maxMetadataPathsBatch int
	metadataPathLoads     map[string]int
}

func (s *spyQueryStore) CurrentNoteMetadataRowsByPaths(ctx context.Context, paths []string) (map[string]codeanchorsqlite.NoteMetadataRow, error) {
	if len(paths) > s.maxMetadataPathsBatch {
		s.maxMetadataPathsBatch = len(paths)
	}
	if s.metadataPathLoads == nil {
		s.metadataPathLoads = make(map[string]int)
	}
	for _, path := range paths {
		s.metadataPathLoads[path]++
	}
	return s.Store.CurrentNoteMetadataRowsByPaths(ctx, paths)
}

type duplicateAmbientStore struct {
	*codeanchorsqlite.Store
}

func (s *duplicateAmbientStore) OntologyAmbientEdgesBySources(ctx context.Context, paths []string, relation string, limit int) ([]codeanchorsqlite.OntologyEdgeRow, error) {
	rows, err := s.Store.OntologyAmbientEdgesBySources(ctx, paths, relation, limit)
	if err != nil || len(rows) == 0 {
		return rows, err
	}
	dup := rows[0]
	rows = append(rows, dup)
	return rows, nil
}

type relationBatchSpyStore struct {
	*codeanchorsqlite.Store
	structuralCalls      int
	ambientCalls         int
	maxStructuralSources int
	maxAmbientSources    int
}

type workspaceNavigationSpyStore struct {
	*codeanchorsqlite.Store
	memberCalls      int
	maxMemberSources int
}

func (s *workspaceNavigationSpyStore) OntologyEdgesForPaths(ctx context.Context, paths []string, includeInbound bool, relation string, limit int) ([]codeanchorsqlite.OntologyEdgeRow, error) {
	if relation == "member" {
		s.memberCalls++
		if len(paths) > s.maxMemberSources {
			s.maxMemberSources = len(paths)
		}
	}
	return s.Store.OntologyEdgesForPaths(ctx, paths, includeInbound, relation, limit)
}

func (s *relationBatchSpyStore) OntologyStructuralEdgesBySources(ctx context.Context, paths []string, relation string, limit int) ([]codeanchorsqlite.OntologyEdgeRow, error) {
	s.structuralCalls++
	if len(paths) > s.maxStructuralSources {
		s.maxStructuralSources = len(paths)
	}
	return s.Store.OntologyStructuralEdgesBySources(ctx, paths, relation, limit)
}

func (s *relationBatchSpyStore) OntologyAmbientEdgesBySources(ctx context.Context, paths []string, relation string, limit int) ([]codeanchorsqlite.OntologyEdgeRow, error) {
	s.ambientCalls++
	if len(paths) > s.maxAmbientSources {
		s.maxAmbientSources = len(paths)
	}
	return s.Store.OntologyAmbientEdgesBySources(ctx, paths, relation, limit)
}

func writeQueryTestConfig(t *testing.T, root string) {
	t.Helper()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
}

func writeQueryTestSchema(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(body), 0o644))
}

func writeQueryTestNote(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

func titles(rows []any) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		item := row.(map[string]any)
		out = append(out, item["title"].(string))
	}
	return out
}

func warningCodes(rows []any) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		item := row.(map[string]any)
		if code, ok := item["code"].(string); ok {
			out = append(out, code)
		}
	}
	return out
}

func ontologyFieldByName(rows []any, name string) map[string]any {
	for _, row := range rows {
		item := row.(map[string]any)
		if item["name"] == name {
			return item
		}
	}
	return nil
}

func graphQLListContainsNestedValue(items []any, parent string, field string, expected any) bool {
	for _, item := range items {
		current, ok := item.(map[string]any)
		if !ok {
			continue
		}
		nested, ok := current[parent].(map[string]any)
		if ok && nested[field] == expected {
			return true
		}
	}
	return false
}
