package query

import (
	"context"
	"fmt"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"sync/atomic"
	"testing"
)

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
