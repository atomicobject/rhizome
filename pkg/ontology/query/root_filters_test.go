package query

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecute_NoteAndEmbeddedResidualFiltersKeepNullsAndStableTies(t *testing.T) {
	const schema = `
type Task @node(paths: ["tasks/*.md"]) {
  key: String @field
  owner: String @field
}
type TaskItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#task-item") {
  key: String @field
  owner: String @field
}
`
	notes := map[string]string{}
	var items strings.Builder
	items.WriteString("# Tasks\n\n")
	for _, entry := range []struct{ key, owner string }{
		{"z", "Alice"}, {"b", "Bob"}, {"n", ""}, {"a", "Alice"}, {"c", "Eve"},
	} {
		body := fmt.Sprintf("---\ntype: Task\ntitle: Task\nkey: %s\n", entry.key)
		if entry.owner != "" {
			body += "owner: " + entry.owner + "\n"
		}
		notes["tasks/"+entry.key+".md"] = body + "---\n"
		fmt.Fprintf(&items, "- [ ] Task #task-item\n  key:: %s\n", entry.key)
		if entry.owner != "" {
			fmt.Fprintf(&items, "  owner:: %s\n", entry.owner)
		}
		fmt.Fprintf(&items, "  ^%s\n", entry.key)
	}
	notes["notes/items.md"] = items.String()
	env := newCustomQueryTestEnv(t, schema, notes)

	// Find uses the note residual path; embedded roots residualize these
	// operators and the sort suffix after the unindexed title field.
	prepared, errs := Prepare(env.execSchema, `{
  task(find: "Task",
    filters: [{ field: "title", op: contains, value: "ask" }, { field: "owner", op: neq, value: "Eve" }]
    sort: [{ field: "title", direction: asc }, { field: "owner", direction: desc }]
  ) { key }
  taskItem(
    filters: [{ field: "title", op: contains, value: "ask" }, { field: "owner", op: neq, value: "Eve" }]
    sort: [{ field: "title", direction: asc }, { field: "owner", direction: desc }]
  ) { key }
  indexedItems: taskItem(
    filters: [{ field: "owner", op: neq, value: "Eve" }]
    sort: [{ field: "owner", direction: desc }]
  ) { key }
}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	want := []any{
		map[string]any{"key": "b"},
		map[string]any{"key": "a"},
		map[string]any{"key": "z"},
		map[string]any{"key": "n"},
	}
	require.Equal(t, want, result.Data["task"])
	indexedItems := []any{
		map[string]any{"key": "b"},
		map[string]any{"key": "z"},
		map[string]any{"key": "a"},
		map[string]any{"key": "n"},
	}
	require.Equal(t, indexedItems, result.Data["indexedItems"])
	require.Equal(t, result.Data["indexedItems"], result.Data["taskItem"])
}
