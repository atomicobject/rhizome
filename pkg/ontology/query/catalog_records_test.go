package query

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"

	"github.com/stretchr/testify/require"
)

func TestExecuteTypeMatchingKeepsInterfaceAndNodePolicies(t *testing.T) {
	const projectContent = "---\nlabel: Project label\n---\n# Alpha\n\nSee [[decisions/chosen]] and [[other/unrelated]].\n\n## Details\nlabel:: Detail label\n"
	env := newCustomQueryTestEnv(t, `
interface Labeled { label: String }
interface Work implements Labeled { label: String }
type Project implements Work & Labeled @node(paths: ["projects/*.md"]) {
  label: String @field
  details: Detail @contains(level: H2, heading: "Details")
}
type Decision implements Work & Labeled @node(paths: ["decisions/*.md"]) {
  label: String @field
}
type Task implements Work & Labeled & Section @node(locator: EMBEDDED)
  @source(shape: LIST_ITEM, marker: "#task") {
  label: String @field
}
type Detail implements Section { label: String @field }
type Other @node(paths: ["other/*.md"]) { label: String @field }
`, map[string]string{
		"projects/alpha.md":   projectContent,
		"decisions/chosen.md": "---\nlabel: Decision label\n---\n# Chosen\n",
		"other/unrelated.md":  "---\nlabel: Other label\n---\n# Unrelated\n",
		"notes/items.md":      "# Items\n\n- First #task ^task-one\n  label:: Task label\n",
	})
	sections := ontology.ParseSections("projects/alpha.md", projectContent)
	require.Len(t, sections, 1)
	require.Len(t, sections[0].Children, 1)
	sectionRef := "projects/alpha.md#" + queryFragmentFromSectionID("projects/alpha.md", sections[0].Children[0].ID)
	prepared, errs := PrepareWithVariables(env.execSchema, `query($sectionRef: String!) {
  note: node(ref: "projects/alpha.md") {
    ... on Node { node: __typename }
    ... on Note { note: path }
    ... on Section { section: notePath }
    ... on Labeled { ancestor: label }
    ... on Work { intermediate: label }
    ... on Project { exact: label }
    ... on Other { unrelated: label }
  }
  embedded: node(ref: "notes/items.md#^task-one") {
    ... on Node { node: __typename }
    ... on Note { note: path }
    ... on Section { section: notePath }
    ... on Labeled { ancestor: label }
    ... on Work { intermediate: label }
    ... on Task { exact: label }
    ... on Other { unrelated: label }
  }
  section: node(ref: $sectionRef) {
    ... on Node { node: __typename }
    ... on Note { note: path }
    ... on Section { section: notePath }
    ... on Labeled { ancestor: label }
    ... on Work { intermediate: label }
    ... on Detail { exact: label }
    ... on Other { unrelated: label }
  }
  project(path: "projects/alpha.md") {
    linked(type: "Labeled") { title }
  }
}`, map[string]any{"sectionRef": sectionRef})
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, map[string]any{
		"node": "Project", "note": "projects/alpha.md", "ancestor": "Project label", "intermediate": "Project label", "exact": "Project label",
	}, result.Data["note"])
	require.Equal(t, map[string]any{
		"node": "Task", "section": "notes/items.md", "ancestor": "Task label", "intermediate": "Task label", "exact": "Task label",
	}, result.Data["embedded"])
	require.Equal(t, map[string]any{
		"node": "Detail", "section": "projects/alpha.md", "exact": "Detail label",
	}, result.Data["section"])
	require.Equal(t, []any{map[string]any{"linked": []any{map[string]any{"title": "Chosen"}}}}, result.Data["project"])

	executor := &executor{schema: env.schema}
	for _, names := range [][2]string{{"", "Node"}, {"Project", ""}, {" ", "Labeled"}, {"Project", " "}} {
		require.False(t, executor.typeMatchesOrImplements(names[0], names[1]), "empty type names do not match: %q", names)
	}
}
