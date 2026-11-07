package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// A typed relation declared on a section keeps its resolvable members when
// other values are missing, untyped or another type. Those stay visible per
// value in workspace links and as non-fatal warnings; no field or query fails.
func TestSectionLinkListsKeepResolvableMembers(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Project @node(paths: ["notes/projects/*.md"]) {
  connections: Connections @contains(level: H2, heading: "Connections")
}
type Connections implements Section @node(locator: EMBEDDED) {
  decisions: [Decision!] @link(includeBodyLinks: false, includeBacklinks: false)
  lead: Decision @link(includeBodyLinks: false, includeBacklinks: false)
}
type Decision @node(paths: ["notes/decisions/*.md"]) { name: String }
type Concept @node(paths: ["notes/concepts/*.md"]) { name: String }
`, map[string]string{
		"notes/projects/one.md":  "# Project\n\n## Connections\n\ndecisions:: [[a]]\ndecisions:: [[idea]]\ndecisions:: [[missing]]\ndecisions:: [[loose]]\ndecisions:: [[b]]\nlead:: [[idea]]\n",
		"notes/decisions/a.md":   "# A\n",
		"notes/decisions/b.md":   "# B\n",
		"notes/concepts/idea.md": "# Idea\n",
		"notes/loose.md":         "# Loose\n",
	})
	prepared, errs := Prepare(env.execSchema, `{ project(first: 1) { connections {
  all: decisions { path } capped: decisions(first: 2) { path } lead { path }
  workspace { fields { name links { value resolved ref { notePath typeName } } } }
} } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	connections := result.Data["project"].([]any)[0].(map[string]any)["connections"].(map[string]any)
	paths := func(value any) []string {
		var out []string
		for _, row := range value.([]any) {
			out = append(out, row.(map[string]any)["path"].(string))
		}
		return out
	}
	require.Equal(t, []string{"notes/decisions/a.md", "notes/decisions/b.md"}, paths(connections["all"]))
	require.Equal(t, []string{"notes/decisions/a.md", "notes/decisions/b.md"}, paths(connections["capped"]), "first counts resolvable members")
	require.Nil(t, connections["lead"])

	links := map[string]any{}
	for _, field := range connections["workspace"].(map[string]any)["fields"].([]any) {
		field := field.(map[string]any)
		if field["name"] == "decisions" {
			for _, link := range field["links"].([]any) {
				link := link.(map[string]any)
				links[link["value"].(string)] = link["ref"]
			}
		}
	}
	require.Len(t, links, 5)
	require.Nil(t, links["[[missing]]"])
	require.Equal(t, "Concept", links["[[idea]]"].(map[string]any)["typeName"])
	require.Equal(t, "notes/loose.md", links["[[loose]]"].(map[string]any)["notePath"])

	warnings, _ := result.Extensions["warnings"].([]Warning)
	type item struct{ code, path string }
	var got []item
	for _, warning := range warnings {
		require.Contains(t, warning.Message, "Connections.")
		got = append(got, item{warning.Code, warning.Path[len(warning.Path)-1]})
	}
	require.ElementsMatch(t, []item{
		{"wrong_target_type", "all"}, {"link_target_missing", "all"}, {"wrong_target_type", "all"},
		{"wrong_target_type", "capped"}, {"link_target_missing", "capped"}, {"wrong_target_type", "capped"},
		{"wrong_target_type", "lead"},
	}, got)
}
