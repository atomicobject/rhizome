package query

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

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
