package query

import (
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNativeRecordNumbersPreserveSpellingBeyondBrowserIntegerPrecision(t *testing.T) {
	env := newConfiguredQueryTestEnv(t, referenceRegressionSDL, map[string]string{"model.yaml": "rows: [{id: A, quota: 9007199254740993, 9007199254740993: large-key}]\n"}, []string{"*.yaml"})
	q, errs := Prepare(env.execSchema, `{row{workspace{source}}}`)
	require.Empty(t, errs)
	result := Execute(t.Context(), env.deps(nil), env.schema, q)
	require.Empty(t, result.Errors)
	source := result.Data["row"].([]any)[0].(map[string]any)["workspace"].(map[string]any)["source"].(ontology.NodeSourceContent)
	require.Equal(t, "9007199254740993", source.Value.Entries[1].Value.ScalarText)
	require.Equal(t, "9007199254740993", source.Value.Entries[2].KeyText)
}
