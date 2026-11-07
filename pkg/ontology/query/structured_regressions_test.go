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
