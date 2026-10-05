package query

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestExecute_WorkspaceNarrativePreservesWhitespaceForEditReplay(t *testing.T) {
	const notePath = "notes/specs/story.md"
	const content = "# Spec\n\n## Stories\n\n### Story\n^story-001\n\n    Indented narrative.\n"
	env := newCustomQueryTestEnv(t, `
type Story implements Section @node(locator: EMBEDDED) {}
type Stories implements Section { stories: [Story!] @contains(level: H3) }
type Spec @node(paths: ["notes/specs/*.md"]) {
  stories: Stories @contains(level: H2, heading: "Stories")
}
`, map[string]string{notePath: content})
	prepared, errs := PrepareWithVariables(env.execSchema, `query Narrative($ref: String!) {
  node(ref: $ref) {
    workspace { bodies(first: 1) { blocks { kind range { start end } markdown } } }
  }
}`, map[string]any{"ref": notePath + "#^story-001"})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	bodies := result.Data["node"].(map[string]any)["workspace"].(map[string]any)["bodies"].([]any)
	require.Len(t, bodies, 1)
	blocks := bodies[0].(map[string]any)["blocks"].([]any)
	var narrative map[string]any
	for _, raw := range blocks {
		block := raw.(map[string]any)
		if block["kind"] == "narrative" {
			narrative = block
		}
	}
	require.NotNil(t, narrative)
	markdown := narrative["markdown"].(string)
	require.Equal(t, "\n\n    Indented narrative.", markdown)
	span := narrative["range"].(map[string]any)
	start, end := span["start"].(int), span["end"].(int)
	require.Equal(t, strings.TrimRight(content[start:end], " \t\n"), markdown)

	ref := ontology.NodeRef{NotePath: notePath, Fragment: "^story-001", Kind: ontology.NodeKindEmbedded}
	for _, prefix := range []string{"\n\n", "\n\n\n"} {
		t.Run(strings.ReplaceAll(prefix, "\n", "newline"), func(t *testing.T) {
			session := ontology.NewEditSession(obsidian.VaultDefinition{Path: env.root}, &obsidian.Note{}, env.schema)
			first := prefix + "    First queued edit. "
			second := prefix + "    Second queued edit with a different length."
			require.NoError(t, session.SetNarrative(ref, start, end, markdown, first))
			require.NoError(t, session.SetNarrative(ref, start, end, first, second))
			plan, err := session.Preview(context.Background())
			require.NoError(t, err)
			require.Len(t, plan.Files, 1)
			require.Equal(t, content[:start]+second+"\n", plan.Files[0].UpdatedContentPreview)
			snapshot, err := ontology.BuildDocumentSnapshot(notePath, plan.Files[0].UpdatedContentPreview, time.Time{})
			require.NoError(t, err)
			_, err = ontology.ProjectNodeFromSnapshot(snapshot, env.schema, ref)
			require.NoError(t, err, "the explicit block locator must survive consecutive narrative edits")
		})
	}
}

func TestExecute_WorkspaceBodiesProjectUndeclaredSectionsInPlace(t *testing.T) {
	const notePath = "notes/plain.md"
	const content = "# Plain\n\nIntro.\n\n## Alpha\n\nAlpha body.\n\n### Alpha One\n\nAlpha one body.\n\n## Beta\n\nBeta body.\n"
	env := newCustomQueryTestEnv(t, `
type Spec @node(paths: ["notes/specs/*.md"]) {
  summary: String @field
}
`, map[string]string{notePath: content})
	prepared, errs := PrepareWithVariables(env.execSchema, `query Plain($ref: String!) {
  node(ref: $ref) {
    workspace { bodies(first: 20) { ref { ref kind nodeId } title parentRef { ref } blocks { kind range { start end } markdown fieldName childRef { ref nodeId } } } }
  }
}`, map[string]any{"ref": notePath})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	byTitle := map[string]map[string]any{}
	for _, raw := range result.Data["node"].(map[string]any)["workspace"].(map[string]any)["bodies"].([]any) {
		body := raw.(map[string]any)
		byTitle[body["title"].(string)] = body
	}
	require.Contains(t, byTitle, "Alpha")
	require.Contains(t, byTitle, "Alpha One")
	refOf := func(title string) string {
		return byTitle[title]["ref"].(map[string]any)["ref"].(string)
	}
	parentOf := func(title string) string {
		parent, _ := byTitle[title]["parentRef"].(map[string]any)
		ref, _ := parent["ref"].(string)
		return ref
	}
	require.Equal(t, notePath, parentOf("Alpha"))
	require.Equal(t, refOf("Alpha"), parentOf("Alpha One"), "a nested heading belongs to the heading that contains it")

	alphaBlocks := byTitle["Alpha"]["blocks"].([]any)
	require.Len(t, alphaBlocks, 2, "undeclared sections carry editable body blocks")
	require.Equal(t, "narrative", alphaBlocks[0].(map[string]any)["kind"])
	require.Equal(t, "\nAlpha body.", alphaBlocks[0].(map[string]any)["markdown"])
	child := alphaBlocks[1].(map[string]any)
	require.Equal(t, "child_section", child["kind"])
	require.Equal(t, refOf("Alpha One"), child["childRef"].(map[string]any)["ref"])

	oneBlocks := byTitle["Alpha One"]["blocks"].([]any)
	require.Len(t, oneBlocks, 1)
	narrative := oneBlocks[0].(map[string]any)
	span := narrative["range"].(map[string]any)
	ref := ontology.NodeRef{
		NotePath: notePath,
		NodeID:   byTitle["Alpha One"]["ref"].(map[string]any)["nodeId"].(string),
		Kind:     ontology.NodeKindSection,
	}
	session := ontology.NewEditSession(obsidian.VaultDefinition{Path: env.root}, &obsidian.Note{}, env.schema)
	require.NoError(t, session.SetNarrative(ref, span["start"].(int), span["end"].(int), narrative["markdown"].(string), "\nRewritten."))
	plan, err := session.Preview(context.Background())
	require.NoError(t, err)
	require.Len(t, plan.Files, 1)
	require.Equal(t, strings.Replace(content, "Alpha one body.", "Rewritten.", 1), plan.Files[0].UpdatedContentPreview)
}
