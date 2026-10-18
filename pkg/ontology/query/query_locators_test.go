package query

import (
	"context"
	"encoding/json"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
