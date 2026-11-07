package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// An inbound edge row carries only the source's path and provider-local node
// ID; keyed and positional RECORD sources must still come back as canonical
// records, from a record focus and from a Markdown focus.
func TestInboundRecordEndpointsKeepCanonicalIdentity(t *testing.T) {
	sdl := `type Host @node(default:true){status:String}
type Claim @node(paths:["claims/*.md"]){name:String}
type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["rows.yaml"],select:"rows",key:{field:"id"},title:["id"],space:"test",logicalKind:"row"){
 id:ID @field(path:"id")
 uses:[Row!] @field(path:"uses") @references(to:["Row"],mode:STRICT,values:ELEMENTS,edge:{name:"uses"})
 cites:[Claim!] @field(path:"cites") @references(to:["Claim"],mode:STRICT,values:ELEMENTS,edge:{name:"cites"})
}
type Step @node(locator:EMBEDDED) @source(shape:RECORD,paths:["steps.yaml"],select:"steps",key:null,title:["name"],space:"test",logicalKind:"step"){
 name:String @field(path:"name")
 uses:[Row!] @field(path:"uses") @references(to:["Row"],mode:STRICT,values:ELEMENTS,edge:{name:"uses"})
}`
	env := newConfiguredQueryTestEnv(t, sdl, map[string]string{
		"rows.yaml":     "rows: [{id: A, uses: [B], cites: [claims/one.md]}, {id: B}]\n",
		"steps.yaml":    "steps: [{name: First step, uses: [B]}]\n",
		"claims/one.md": "---\nname: One\n---\n# One\n",
	}, []string{"*.yaml", "claims/*.md"})
	const selection = `in: neighborhood(direction: INBOUND) { edges { relation target { kind fragment typeName } } nodes { __typename title } }
	  workspace { relationGroups { items { title relationName ref { fragment typeName } } } }`
	for _, tc := range []struct {
		name, query string
		read        func(map[string]any) map[string]any
	}{
		{"record focus", `{row(filters:[{field:"id",op:eq,value:"B"}]){` + selection + `}}`, func(d map[string]any) map[string]any { return d["row"].([]any)[0].(map[string]any) }},
		{"markdown focus", `{claim{` + selection + `}}`, func(d map[string]any) map[string]any { return d["claim"].([]any)[0].(map[string]any) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, errs := Prepare(env.execSchema, tc.query)
			require.Empty(t, errs)
			result := Execute(t.Context(), env.deps(nil), env.schema, q)
			require.Empty(t, result.Errors)
			node := tc.read(result.Data)
			in := node["in"].(map[string]any)
			targets := map[string]string{}
			for _, raw := range in["edges"].([]any) {
				target := raw.(map[string]any)["target"].(map[string]any)
				require.Equal(t, "EMBEDDED", target["kind"])
				require.NotEmpty(t, target["fragment"], "inbound record endpoint lost its fragment")
				targets[target["typeName"].(string)] = target["fragment"].(string)
			}
			titles := map[string]string{}
			for _, raw := range in["nodes"].([]any) {
				node := raw.(map[string]any)
				titles[node["title"].(string)] = node["__typename"].(string)
			}
			groupTitles := map[string]string{}
			for _, group := range node["workspace"].(map[string]any)["relationGroups"].([]any) {
				for _, raw := range group.(map[string]any)["items"].([]any) {
					item := raw.(map[string]any)
					ref := item["ref"].(map[string]any)
					if ref["fragment"] != nil {
						groupTitles[item["title"].(string)] = ref["typeName"].(string)
					}
				}
			}
			if tc.name == "record focus" {
				require.Regexp(t, `^record:v1:`, targets["Row"])
				require.Contains(t, targets, "Step")
				require.Regexp(t, `^record-pos:`, targets["Step"])
				require.Equal(t, map[string]string{"A": "Row", "First step": "Step"}, titles)
				require.Equal(t, map[string]string{"A": "Row", "First step": "Step"}, groupTitles)
			} else {
				require.Contains(t, targets, "Row")
				require.Equal(t, map[string]string{"A": "Row"}, titles)
				require.Equal(t, "Row", groupTitles["A"])
			}
		})
	}
}
