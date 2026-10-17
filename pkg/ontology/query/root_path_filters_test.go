package query

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecutePathFiltersPreserveResidualTextSemantics(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Item @node(paths: ["Notes/*.md", "NotesExtra/*.md"]) { key: String @field }`, map[string]string{
		"Notes/One.md":      "---\ntitle: Item\nkey: a\n---\n",
		"Notes/Two.md":      "---\ntitle: Item\nkey: b\n---\n",
		"NotesExtra/One.md": "---\ntitle: Item\nkey: c\n---\n",
	})
	for _, test := range []struct {
		predicate string
		keys      []string
	}{
		{`op: eq, value: "Notes/One.md"`, []string{"a"}},
		{`op: eq, value: "notes/one.md"`, []string{}},
		{`op: in, values: ["Notes/One.md", "Notes/Two.md"]`, []string{"a", "b"}},
		{`op: contains, value: "NOTES/"`, []string{"a", "b"}},
		{`op: neq, value: "NOTES/ONE.MD"`, []string{"b", "c"}},
		{`op: lt, value: "NOTES/TWO.MD"`, []string{"a"}},
		{`op: gt, value: "NOTES/ONE.MD"`, []string{"b", "c"}},
	} {
		for _, field := range []string{"path", "notePath"} {
			for _, selector := range []string{"", `find: "Item", `} {
				args := fmt.Sprintf(`%sfilters: [{field: %q, %s}], sort: [{field: "key"}]`, selector, field, test.predicate)
				require.Equal(t, test.keys, scalarParityKeys(executeScalarParityRows(t, env, "item", args, "")), args)
			}
		}
	}
}
