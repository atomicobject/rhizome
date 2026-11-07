package query

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFrontmatterJSONPreservesNestedValues(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Assertion @node(paths: ["notes/*.md"]) { headline: String }`, map[string]string{
		"notes/claim.md": "---\nprovenance:\n  speaker: Ada\n  count: 2\n  active: true\n  missing: null\n  empty: {}\nconnections:\n  - supports: [one, two]\n  - [nested, {key: value}]\nslots: [null, '', [], {}]\n---\n# Claim\n",
	})
	for _, selection := range []string{"frontmatter", "frontmatter content"} {
		prepared, errs := Prepare(env.execSchema, `{ assertion(first: 1) { `+selection+` } }`)
		require.Empty(t, errs)
		result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
		require.Empty(t, result.Errors)
		encoded, err := json.Marshal(result.Data["assertion"].([]any)[0].(map[string]any)["frontmatter"])
		require.NoError(t, err)
		require.JSONEq(t, `{"provenance":{"speaker":"Ada","count":2,"active":true,"missing":null,"empty":{}},"connections":[{"supports":["one","two"]},["nested",{"key":"value"}]],"slots":[null,"",[],{}]}`, string(encoded))
	}
}
