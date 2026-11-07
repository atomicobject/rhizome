package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// RECORD reference fields declared as text (`[String!] @references`) publish
// field links like Markdown relation fields: one per authored value, titled
// when resolved, unresolved for missing or wildcard values. They never fall
// back to generic note resolution, so a same-named note cannot win.
func TestRecordReferenceFieldsPublishWorkspaceLinks(t *testing.T) {
	sdl := `type Host @node(default:true){status:String}
type Claim @node(paths:["claims/*.md"]){aoId:String @field(source:"id") name:String}
type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["rows.yaml"],select:"rows",key:{field:"id"},title:["name","$key"],space:"test",logicalKind:"row"){
 id:ID @field(path:"id")
 name:String @field(path:"name")
 uses:[String!] @field(path:"uses") @references(to:["Row"],mode:STRICT,values:ELEMENTS,allowWildcard:true,edge:{name:"uses"})
 owner:String @field(path:"owner") @references(to:["Row"],mode:STRICT,values:VALUE)
 cites:[String!] @field(path:"cites") @references(to:["Claim"],targetKey:"aoId",mode:STRICT,values:ELEMENTS,edge:{name:"evidenced-by"})
}`
	env := newConfiguredQueryTestEnv(t, sdl, map[string]string{
		"rows.yaml":     "rows:\n  - {id: A, name: Alpha, uses: [B, missing, '*', B], owner: B, cites: [a-1, a-2]}\n  - {id: B, name: Beta}\n",
		"claims/one.md": "---\nid: a-1\nname: One\n---\n# One\n",
		"missing.md":    "# missing\n",
	}, []string{"*.yaml", "*.md", "claims/*.md"})
	q, errs := Prepare(env.execSchema, `{row(filters:[{field:"id",op:eq,value:"A"}]){workspace{fields{name links{value resolved title ref{notePath fragment}}}}}}`)
	require.Empty(t, errs)
	result := Execute(t.Context(), env.deps(nil), env.schema, q)
	require.Empty(t, result.Errors)
	type link struct{ value, title, target string }
	got := map[string][]link{}
	for _, raw := range result.Data["row"].([]any)[0].(map[string]any)["workspace"].(map[string]any)["fields"].([]any) {
		field := raw.(map[string]any)
		for _, rawLink := range field["links"].([]any) {
			item := rawLink.(map[string]any)
			next := link{value: item["value"].(string)}
			if item["resolved"] == true {
				ref := item["ref"].(map[string]any)
				next.title, next.target = item["title"].(string), ref["notePath"].(string)
				if fragment, _ := ref["fragment"].(string); fragment != "" {
					next.target += "#record"
				}
			}
			got[field["name"].(string)] = append(got[field["name"].(string)], next)
		}
	}
	require.Equal(t, []link{{"B", "Beta", "rows.yaml#record"}, {value: "missing"}, {value: "*"}, {"B", "Beta", "rows.yaml#record"}}, got["uses"])
	require.Equal(t, []link{{"B", "Beta", "rows.yaml#record"}}, got["owner"])
	require.Equal(t, []link{{"a-1", "One", "claims/one.md"}, {value: "a-2"}}, got["cites"])
	require.Empty(t, got["name"], "plain text fields carry no links")
}
