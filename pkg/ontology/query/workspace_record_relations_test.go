package query

import (
	"github.com/atomicobject/rhizome/internal/testkit/recordfixture"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestRecordWorkspaceRelationsIncludeContainmentAndReferences(t *testing.T) {
	env := newConfiguredQueryTestEnv(t, strings.ReplaceAll(recordfixture.SDL, `,edge:{name:"made-of"}`, ""), recordfixture.Files(), recordfixture.Includes)
	q, errs := Prepare(env.execSchema, `{service(filters:[{field:"id",op:eq,value:"A"}]){workspace{relationGroups{items{title relationName direction ref{ref} structural}}}}}`)
	require.Empty(t, errs)
	result := Execute(t.Context(), env.deps(nil), env.schema, q)
	require.Empty(t, result.Errors)
	workspace := result.Data["service"].([]any)[0].(map[string]any)["workspace"].(map[string]any)
	items := map[string]map[string]any{}
	for _, group := range workspace["relationGroups"].([]any) {
		for _, item := range group.(map[string]any)["items"].([]any) {
			row := item.(map[string]any)
			items[row["relationName"].(string)+":"+row["title"].(string)] = row
		}
	}
	require.Contains(t, items, "parent:services")
	require.Contains(t, items, "children:plan")
	require.Contains(t, items, "children:ship")
	require.Equal(t, "INBOUND", items["parent:services"]["direction"])
	require.Equal(t, "OUTBOUND", items["children:plan"]["direction"])
	require.Contains(t, items, "uses:Shared title")
}

func TestRecordWorkspaceBacklinksKeepExactRecordIdentity(t *testing.T) {
	sdl := referenceRegressionSDL + `type Evidence @node(paths:["evidence.md"]){name:String}`
	files := map[string]string{"model.yaml": "rows: [{id: A}, {id: B}]\n", "evidence.md": "# Evidence\n"}
	initial := newConfiguredQueryTestEnv(t, sdl, files, []string{"*.md", "*.yaml"})
	records, err := initial.store.OntologyNodesByPaths(t.Context(), []string{"model.yaml"})
	require.NoError(t, err)
	var target string
	for _, row := range records {
		if row.TypeName == "Row" {
			target = row.SourceLocator
			break
		}
	}
	require.NotEmpty(t, target)
	files["evidence.md"] += "\nSee [[" + target + "|record]].\n"
	env := newConfiguredQueryTestEnv(t, sdl, files, []string{"*.md", "*.yaml"})
	candidates, err := env.store.GraphDocNoteLinkSourcesByTargets(t.Context(), []string{"model.yaml"})
	require.NoError(t, err)
	require.Equal(t, []string{"evidence.md"}, candidates)
	q, errs := Prepare(env.execSchema, `{row{ref{ref} workspace{relationGroups{items{title provenance direction ref{ref}}}}}}`)
	require.Empty(t, errs)
	result := Execute(t.Context(), env.deps(nil), env.schema, q)
	require.Empty(t, result.Errors)
	for _, raw := range result.Data["row"].([]any) {
		row := raw.(map[string]any)
		ref := row["ref"].(map[string]any)["ref"].(string)
		var backlinks []string
		for _, group := range row["workspace"].(map[string]any)["relationGroups"].([]any) {
			for _, item := range group.(map[string]any)["items"].([]any) {
				item := item.(map[string]any)
				if item["provenance"] == "backlink" {
					require.Equal(t, "INBOUND", item["direction"])
					backlinks = append(backlinks, item["ref"].(map[string]any)["ref"].(string))
				}
			}
		}
		if ref == target {
			require.Equal(t, []string{"evidence.md"}, backlinks)
		} else {
			require.Empty(t, backlinks)
		}
	}
}
