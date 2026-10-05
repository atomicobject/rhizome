package query

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSectionSummaryRetainsExecutableContentSelection(t *testing.T) {
	for _, sectionType := range []string{"Section", "Synopsis"} {
		t.Run(sectionType, func(t *testing.T) {
			env := newCustomQueryTestEnv(t, fmt.Sprintf(`
 type Synopsis implements Section { detail: String @field }
 type Entry @node(paths: ["notes/*.md"]) {
 summary: %s @contains(level: H2, heading: "Summary") @display(role: SUMMARY)
 }`, sectionType), map[string]string{
				"notes/entry.md":   "# Entry\n\n## Summary\n\nFirst paragraph.\n\nSecond paragraph.\n\n## Evidence\nOther text.\n",
				"notes/missing.md": "# Missing\n\n## Evidence\nOther text.\n",
			})
			prepared, errs := Prepare(env.execSchema, `{ entry(path: "notes/entry.md") { summary { content } } missing: entry(path: "notes/missing.md") { summary { content } } }`)
			require.Empty(t, errs)
			result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
			require.Empty(t, result.Errors)
			rows := result.Data["entry"].([]any)
			require.Len(t, rows, 1)
			summary := rows[0].(map[string]any)["summary"].(map[string]any)
			require.Equal(t, "First paragraph.\n\nSecond paragraph.", summary["content"])
			missing := result.Data["missing"].([]any)
			require.Len(t, missing, 1)
			require.Nil(t, missing[0].(map[string]any)["summary"])
		})
	}
}
