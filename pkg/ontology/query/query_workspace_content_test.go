package query

import (
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

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
	var paneSection map[string]any
	for _, raw := range noteBodies {
		body := raw.(map[string]any)
		if binding, ok := body["binding"].(map[string]any); ok && binding["fieldName"] == "userStories" {
			paneSection = body
		}
	}
	require.NotNil(t, paneSection)
	require.Equal(t, "PANE", paneSection["binding"].(map[string]any)["sectionDisplay"])
	require.NotEmpty(t, paneSection["blocks"].([]any), "PANE sections carry blocks so they read and edit in place")

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
