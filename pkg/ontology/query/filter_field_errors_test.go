package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// SPEC-0136 R8: a filter on an unknown field, or on a computed @neighbors or
// @reverse relation, is an error naming the field instead of a complete,
// empty result.
func TestRootFiltersRejectUnknownAndComputedFields(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface Claim { summary: String }
type Opportunity implements Claim @node(paths: ["opportunities/*.md"]) {
  summary: String @field
  evidence: [Evidence!] @reverse(field: "opportunities")
  nearby: [Evidence!] @neighbors(direction: BOTH, type: "Evidence")
}
type Evidence implements Claim @node(paths: ["evidence/*.md"]) {
  summary: String @field
  opportunities: [Opportunity!] @link(includeBodyLinks: false, includeBacklinks: false)
}
`, map[string]string{
		"opportunities/rhizome.md": "---\nsummary: Rhizome\n---\n",
		"evidence/a.md":            "---\nsummary: A\nopportunities:\n  - '[[rhizome]]'\n---\n",
	})
	for _, tc := range []struct {
		name, query, wantErr string
	}{
		{"unknown field", `{ opportunity(filters: [{field: "sumary", op: eq, value: "Rhizome"}]) { path } }`, `unknown filter field "sumary"`},
		{"reverse relation", `{ opportunity(filters: [{field: "evidence", op: exists}]) { path } }`, `filter field "evidence" is a computed`},
		{"neighbors relation", `{ opportunity(filters: [{field: "nearby", op: exists}]) { path } }`, `filter field "nearby" is a computed`},
		{"unknown on an interface", `{ claim(filters: [{field: "nothing", op: exists}]) { summary } }`, `"nothing" is not a shared interface field`},
		{"stored field", `{ opportunity(filters: [{field: "summary", op: eq, value: "Rhizome"}]) { path } }`, ""},
		{"builtins", `{ opportunity(filters: [{field: "path", op: eq, value: "opportunities/rhizome.md"}, {field: "title", op: exists}]) { path } }`, ""},
		{"shared interface field", `{ claim(filters: [{field: "summary", op: exists}]) { summary } }`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, errs := Prepare(env.execSchema, tc.query)
			require.Empty(t, errs)
			result := Execute(t.Context(), env.deps(nil), env.schema, prepared)
			if tc.wantErr == "" {
				require.Empty(t, result.Errors)
				return
			}
			require.NotEmpty(t, result.Errors)
			require.Contains(t, result.Errors[0].Message, tc.wantErr)
		})
	}
}
