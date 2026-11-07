package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const referenceRegressionSDL = `type Host @node(default:true){status:String}
type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"rows",key:{field:"id"},title:["id"],space:"test",logicalKind:"row") {
 id:ID @field(path:"id")
 refs:[Row!] @field(path:"refs") @references(to:["Row"],mode:STRICT,values:ELEMENTS,edge:{name:"uses"})
}`

func TestRichRecordReferencesPreserveGlobalAmbiguity(t *testing.T) {
	env := newConfiguredQueryTestEnv(t, referenceRegressionSDL, map[string]string{
		"a.yaml": "rows: [{id: A, refs: [B]}, {id: B}]\n",
		"b.yaml": "rows: [{id: B}]\n",
	}, []string{"*.yaml"})
	for _, selection := range []string{"id refs {id}", "id content refs {id}"} {
		q, errs := Prepare(env.execSchema, `{row(filters:[{field:"id",op:eq,value:"A"}]){`+selection+`}}`)
		require.Empty(t, errs)
		result := Execute(t.Context(), env.deps(nil), env.schema, q)
		require.Empty(t, result.Errors)
		rows := result.Data["row"].([]any)
		require.Len(t, rows, 1)
		require.Empty(t, rows[0].(map[string]any)["refs"], selection)
	}
}

func TestPresentEmptyRecordReferencesMatchExists(t *testing.T) {
	env := newConfiguredQueryTestEnv(t, referenceRegressionSDL, map[string]string{
		"a.yaml": "rows: [{id: empty, refs: []}, {id: missing}]\n",
	}, []string{"*.yaml"})
	for _, selection := range []string{"id refs {id}", "id content refs {id}"} {
		q, errs := Prepare(env.execSchema, `{row(filters:[{field:"refs",op:exists}]){`+selection+`}}`)
		require.Empty(t, errs)
		result := Execute(t.Context(), env.deps(nil), env.schema, q)
		require.Empty(t, result.Errors)
		rows := result.Data["row"].([]any)
		require.Len(t, rows, 1)
		require.Equal(t, "empty", rows[0].(map[string]any)["id"])
		require.Empty(t, rows[0].(map[string]any)["refs"])
	}
}

func TestWorkspaceLinksKeepIdenticalIDsInDifferentKindsDistinct(t *testing.T) {
	sdl := `type Host @node(default:true){status:String}
type Alpha @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"alphas",key:{field:"id"},title:["name"],space:"test",logicalKind:"alpha"){ id:ID @field(path:"id") name:String @field(path:"name") }
type Beta @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"betas",key:{field:"id"},title:["name"],space:"test",logicalKind:"beta"){ id:ID @field(path:"id") name:String @field(path:"name") }
type Item @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"items",key:{field:"id"},title:["id"],space:"test",logicalKind:"item"){
 id:ID @field(path:"id")
 alpha:Alpha @field(path:"alpha") @references(to:["Alpha"],mode:STRICT,values:VALUE)
 beta:Beta @field(path:"beta") @references(to:["Beta"],mode:STRICT,values:VALUE)
}`
	env := newConfiguredQueryTestEnv(t, sdl, map[string]string{
		"model.yaml": "alphas: [{id: X, name: Alpha X}]\nbetas: [{id: X, name: Beta X}]\nitems: [{id: I, alpha: X, beta: X}]\n",
	}, []string{"*.yaml"})
	q, errs := Prepare(env.execSchema, `{item{alpha{name} beta{name} workspace{fields{name links{value resolved title}}}}}`)
	require.Empty(t, errs)
	result := Execute(t.Context(), env.deps(nil), env.schema, q)
	require.Empty(t, result.Errors)
	item := result.Data["item"].([]any)[0].(map[string]any)
	require.Equal(t, map[string]any{"name": "Alpha X"}, item["alpha"])
	require.Equal(t, map[string]any{"name": "Beta X"}, item["beta"])
	titles := map[string]any{}
	for _, raw := range item["workspace"].(map[string]any)["fields"].([]any) {
		field := raw.(map[string]any)
		for _, link := range field["links"].([]any) {
			titles[field["name"].(string)] = link.(map[string]any)["title"]
		}
	}
	require.Equal(t, map[string]any{"alpha": "Alpha X", "beta": "Beta X"}, titles)
}
