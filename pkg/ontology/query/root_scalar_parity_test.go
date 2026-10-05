package query

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func scalarParityEnv(t *testing.T) *queryTestEnv {
	t.Helper()
	const fields = `
  key: String @field
  rank: Int @field
  amount: Float @field
  label: String @field
  due: Date @field
  at: DateTime @field
  ready: Boolean @field
  ranks: [Int!] @field
`
	schema := `type Task @node(paths: ["tasks/*.md"]) {` + fields + `}
type TaskItem implements Section @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#task-item") {` + fields + `}`
	entries := []struct {
		key, rank, amount, label, due, at, ready string
		ranks                                    []string
	}{
		{"a", "9007199254740993", "10.5", "10", "2026-06-01", "2026-06-01T08:00:00+02:00", "false", []string{"10", "02"}},
		{"b", "9007199254740992", "2.5", "02", "2026-01-01", "2026-06-01T07:00:00Z", "true", []string{"3", "20"}},
		{"c", "-9007199254740993", "-1.5", "-2", "2025-12-31", "2026-06-01T06:00:00Z", "1", []string{"-2", "15"}},
		{"d", "02", "0.5", "2", "2026-01-02", "2026-06-01T07:00:00.000Z", "0", []string{"-1", "30"}},
		{"e", "10", "2.50", "3", "2026-01-01", "2026-06-01T07:00:00Z", "true", []string{"8", "4"}},
		{"m", "", "", "", "", "", "", nil},
		{"x", "invalid", "invalid", "", "2026-02-30", "invalid", "invalid", nil},
	}
	notes := map[string]string{}
	var items strings.Builder
	items.WriteString("# Tasks\n\n")
	for _, entry := range entries {
		var note strings.Builder
		fmt.Fprintf(&note, "---\ntitle: Task\nkey: %s\n", entry.key)
		fmt.Fprintf(&items, "- [ ] Task #task-item\n  key:: %s\n", entry.key)
		for _, property := range []struct{ name, value string }{
			{"rank", entry.rank}, {"amount", entry.amount}, {"label", entry.label},
			{"due", entry.due}, {"at", entry.at}, {"ready", entry.ready},
		} {
			if property.value != "" {
				fmt.Fprintf(&note, "%s: %q\n", property.name, property.value)
				fmt.Fprintf(&items, "  %s:: %s\n", property.name, property.value)
			}
		}
		if len(entry.ranks) > 0 {
			quoted := make([]string, len(entry.ranks))
			for i, rank := range entry.ranks {
				quoted[i] = fmt.Sprintf("%q", rank)
			}
			fmt.Fprintf(&note, "ranks: [%s]\n", strings.Join(quoted, ", "))
			for _, rank := range entry.ranks {
				fmt.Fprintf(&items, "  ranks:: %s\n", rank)
			}
		}
		note.WriteString("---\n")
		notes["tasks/"+entry.key+".md"] = note.String()
		fmt.Fprintf(&items, "  ^%s\n", entry.key)
	}
	notes["notes/items.md"] = items.String()
	return newCustomQueryTestEnv(t, schema, notes)
}

func executeScalarParityRows(t *testing.T, env *queryTestEnv, root, args, field string) []any {
	t.Helper()
	prepared, errs := Prepare(env.execSchema, fmt.Sprintf(`{ %s(%s) { key %s } }`, root, args, field))
	require.Empty(t, errs)
	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data[root].([]any)
	for _, overlay := range scalarParityOverlays(t, env) {
		deps := env.deps(nil)
		deps.ReadOverlay = overlay
		staged := Execute(context.Background(), deps, env.schema, prepared)
		require.Empty(t, staged.Errors)
		stagedRows := staged.Data[root].([]any)
		require.Equal(t, rows, stagedRows, "staged %s(%s), touched=%d", root, args, len(overlay.UpdatedContentByPath))
	}
	return rows
}

func scalarParityOverlays(t *testing.T, env *queryTestEnv) []*ReadOverlay {
	t.Helper()
	paths := []string{}
	require.NoError(t, filepath.WalkDir(env.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".md") {
			paths = append(paths, path)
		}
		return nil
	}))
	overlays := make([]*ReadOverlay, 0, 2)
	for _, count := range []int{1, len(paths)} {
		overlay := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{}, Status: "dirty"}
		for _, path := range paths[:count] {
			content, err := os.ReadFile(path)
			require.NoError(t, err)
			relative, err := filepath.Rel(env.root, path)
			require.NoError(t, err)
			overlay.UpdatedContentByPath[filepath.ToSlash(relative)] = string(content) + "\nUnrelated staged prose.\n"
		}
		overlays = append(overlays, overlay)
	}
	return overlays
}

func scalarParityKeys(rows []any) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.(map[string]any)["key"].(string))
	}
	return keys
}

func TestExecute_TypedScalarSortParity(t *testing.T) {
	env := scalarParityEnv(t)
	for _, test := range []struct {
		field string
		keys  []string
	}{
		{"rank", []string{"c", "d", "e", "b", "a", "m", "x"}},
		{"amount", []string{"c", "d", "b", "e", "a", "m", "x"}},
		{"label", []string{"c", "b", "a", "d", "e", "m", "x"}},
		{"due", []string{"c", "b", "e", "d", "a", "m", "x"}},
		{"at", []string{"c", "b", "d", "e", "a", "m", "x"}},
	} {
		for _, direction := range []string{"asc", "desc"} {
			t.Run(test.field+"/"+direction, func(t *testing.T) {
				args := fmt.Sprintf(`sort: [{field: %q, direction: %s}]`, test.field, direction)
				indexed := executeScalarParityRows(t, env, "task", args, test.field)
				if direction == "asc" {
					require.Equal(t, test.keys, scalarParityKeys(indexed))
				}
				for _, variant := range []struct{ name, root, args string }{
					{"find", "task", `find: "Task", ` + args},
					{"property", "task", `property: {name: "title", value: "Task"}, ` + args},
					{"suffix", "task", fmt.Sprintf(`sort: [{field: %q, direction: %s}, {field: "title"}]`, test.field, direction)},
					{"embedded", "taskItem", args},
					{"embedded-suffix", "taskItem", fmt.Sprintf(`sort: [{field: %q, direction: %s}, {field: "title"}]`, test.field, direction)},
				} {
					t.Run(variant.name, func(t *testing.T) {
						require.Equal(t, indexed, executeScalarParityRows(t, env, variant.root, variant.args, test.field))
					})
				}
			})
		}
	}
}

func TestExecute_TypedScalarFilterParity(t *testing.T) {
	env := scalarParityEnv(t)
	for _, test := range []struct {
		field, predicate string
		keys             []string
	}{
		{"rank", `op: eq, value: "02"`, []string{"d"}},
		{"rank", `op: in, values: ["+02", "0010"]`, []string{"d", "e"}},
		{"rank", `op: gt, value: "9007199254740992"`, []string{"a"}},
		{"rank", `op: eq, value: "-9007199254740993"`, []string{"c"}},
		{"rank", `op: gte, value: "02"`, []string{"a", "b", "d", "e"}},
		{"rank", `op: lt, value: "02"`, []string{"c"}},
		{"rank", `op: lte, value: "02"`, []string{"c", "d"}},
		{"rank", `op: exists`, []string{"a", "b", "c", "d", "e", "x"}},
		{"rank", `op: eq, value: "invalid"`, []string{"x"}},
		{"amount", `op: eq, value: "2.500"`, []string{"b", "e"}},
		{"ready", `op: eq, value: "1"`, []string{"b", "c", "e"}},
		{"ready", `op: eq, value: "FALSE"`, []string{"a", "d"}},
		{"due", `op: eq, value: "2026-01-01"`, []string{"b", "e"}},
		{"at", `op: eq, value: "2026-06-01T07:00:00.000Z"`, []string{"b", "d", "e"}},
		{"at", `op: gt, value: "2026-06-01T07:00:00Z"`, []string{"a"}},
		{"due", `op: eq, value: "2026-02-30"`, []string{}},
		{"label", `op: eq, value: "02"`, []string{"b"}},
		{"ranks", `op: eq, value: "02"`, []string{"a"}},
		{"ranks", `op: gt, value: "20"`, []string{"d"}},
	} {
		t.Run(test.field+"/"+test.predicate, func(t *testing.T) {
			args := fmt.Sprintf(`filters: [{field: %q, %s}], sort: [{field: "key"}]`, test.field, test.predicate)
			indexed := executeScalarParityRows(t, env, "task", args, test.field)
			require.Equal(t, test.keys, scalarParityKeys(indexed))
			for _, variant := range []struct{ root, args string }{
				{"task", `find: "Task", ` + args},
				{"task", `property: {name: "title", value: "Task"}, ` + args},
				{"taskItem", args},
			} {
				require.Equal(t, indexed, executeScalarParityRows(t, env, variant.root, variant.args, test.field))
			}
		})
	}
}

func TestExecute_TypedScalarSortPagesAfterComparison(t *testing.T) {
	env := scalarParityEnv(t)
	args := `sort: [{field: "rank", direction: desc}], first: 1, offset: 1`
	indexed := executeScalarParityRows(t, env, "task", args, "rank")
	require.Equal(t, []any{map[string]any{"key": "b", "rank": int64(9007199254740992)}}, indexed)
	for _, variant := range []struct{ root, args string }{
		{"task", `find: "Task", ` + args},
		{"task", `sort: [{field: "rank", direction: desc}, {field: "title"}], first: 1, offset: 1`},
		{"taskItem", `sort: [{field: "rank", direction: desc}, {field: "title"}], first: 1, offset: 1`},
	} {
		require.Equal(t, indexed, executeScalarParityRows(t, env, variant.root, variant.args, "rank"))
	}
}

func TestExecute_ResidualScalarListSortAndStringRanges(t *testing.T) {
	env := scalarParityEnv(t)
	for _, test := range []struct {
		field, args string
		keys        []string
	}{
		{"ranks", `sort: [{field: "ranks", direction: asc}]`, []string{"c", "d", "a", "b", "e", "m", "x"}},
		{"ranks", `sort: [{field: "ranks", direction: desc}]`, []string{"d", "b", "c", "a", "e", "m", "x"}},
		{"ready", `sort: [{field: "ready", direction: asc}, {field: "key"}]`, []string{"a", "d", "b", "c", "e", "m", "x"}},
		{"ready", `sort: [{field: "ready", direction: desc}, {field: "key"}]`, []string{"b", "c", "e", "a", "d", "m", "x"}},
		{"label", `filters: [{field: "label", op: lt, value: "2"}], sort: [{field: "key"}]`, []string{"a", "b", "c"}},
	} {
		t.Run(test.args, func(t *testing.T) {
			for _, variant := range []struct{ root, args string }{
				{"task", test.args}, {"task", `find: "Task", ` + test.args}, {"taskItem", test.args},
			} {
				rows := executeScalarParityRows(t, env, variant.root, variant.args, test.field)
				require.Equal(t, test.keys, scalarParityKeys(rows))
			}
		})
	}
}

func TestExecute_TypedScalarEmbeddedTiesPreserveSourceOrder(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Item implements Section @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#item") { key: String @field rank: Int @field }`, map[string]string{
		"notes/a.md": "# Tasks\n\n- [ ] Task #item\n  key:: y\n  rank:: 2\n  ^y\n",
		"notes/b.md": "# Tasks\n\n- [ ] Task #item\n  key:: z\n  rank:: 02\n  ^z\n\n" + strings.Repeat("padding ", 20) + "\n\n- [ ] Task #item\n  key:: a\n  rank:: 2\n  ^a\n",
	})
	for _, direction := range []string{"asc", "desc"} {
		t.Run(direction, func(t *testing.T) {
			for _, page := range []struct {
				args string
				keys []string
			}{
				{"", []string{"y", "z", "a"}},
				{", first: 1, offset: 1", []string{"z"}},
				{", first: 1, offset: 2", []string{"a"}},
			} {
				indexedArgs := fmt.Sprintf(`sort: [{field: "rank", direction: %s}]%s`, direction, page.args)
				residualArgs := fmt.Sprintf(`sort: [{field: "rank", direction: %s}, {field: "title"}]%s`, direction, page.args)
				indexed := executeScalarParityRows(t, env, "item", indexedArgs, "rank")
				require.Equal(t, page.keys, scalarParityKeys(indexed))
				require.Equal(t, indexed, executeScalarParityRows(t, env, "item", residualArgs, "rank"))
			}
		})
	}
}

func TestExecute_TypedScalarNoteTiesPreservePathOrder(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Task @node(paths: ["tasks/*.md"]) { key: String @field rank: Int @field }`, map[string]string{
		"tasks/a.md": "---\ntitle: Task\nkey: z\nrank: \"02\"\n---\n",
		"tasks/z.md": "---\ntitle: Task\nkey: a\nrank: 2\n---\n",
	})
	for _, direction := range []string{"asc", "desc"} {
		args := fmt.Sprintf(`sort: [{field: "rank", direction: %s}], first: 1`, direction)
		indexed := executeScalarParityRows(t, env, "task", args, "rank")
		require.Equal(t, []string{"z"}, scalarParityKeys(indexed))
		for _, residual := range []string{
			`find: "Task", ` + args,
			fmt.Sprintf(`sort: [{field: "rank", direction: %s}, {field: "title"}], first: 1`, direction),
		} {
			require.Equal(t, indexed, executeScalarParityRows(t, env, "task", residual, "rank"))
		}
	}
}

func TestExecute_TypedScalarBlankStringSortParity(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Task @node(paths: ["tasks/*.md"]) { key: String @field label: String @field }
type Item implements Section @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#item") { key: String @field label: String @field }`, map[string]string{
		"tasks/a.md":     "---\ntitle: Task\nkey: a\nlabel: \"\"\n---\n",
		"tasks/b.md":     "---\ntitle: Task\nkey: b\nlabel: B\n---\n",
		"tasks/c.md":     "---\ntitle: Task\nkey: c\n---\n",
		"tasks/d.md":     "---\ntitle: Task\nkey: d\nlabel: null\n---\n",
		"tasks/e.md":     "---\ntitle: Task\nkey: e\nlabel: []\n---\n",
		"tasks/f.md":     "---\ntitle: Task\nkey: f\nlabel: \"   \"\n---\n",
		"tasks/g.md":     "---\ntitle: Task\nkey: g\nlabel: a\n---\n",
		"notes/items.md": "# Tasks\n\n- [ ] Task #item\n  key:: a\n  label:: \n  ^a\n- [ ] Task #item\n  key:: b\n  label:: B\n  ^b\n- [ ] Task #item\n  key:: c\n  ^c\n",
	})
	for _, direction := range []string{"asc", "desc"} {
		t.Run(direction, func(t *testing.T) {
			for _, page := range []string{"", ", first: 1", ", first: 1, offset: 1"} {
				args := fmt.Sprintf(`sort: [{field: "label", direction: %s}]%s`, direction, page)
				indexed := executeScalarParityRows(t, env, "task", args, "label")
				if page == "" {
					keys := []string{"a", "d", "e", "f", "g", "b", "c"}
					if direction == "desc" {
						keys = []string{"b", "g", "a", "d", "e", "f", "c"}
					}
					require.Equal(t, keys, scalarParityKeys(indexed))
				}
				for _, variant := range []struct{ root, args string }{
					{"task", `find: "Task", ` + args},
					{"task", `property: {name: "title", value: "Task"}, ` + args},
					{"task", fmt.Sprintf(`sort: [{field: "label", direction: %s}, {field: "title"}]%s`, direction, page)},
				} {
					require.Equal(t, indexed, executeScalarParityRows(t, env, variant.root, variant.args, "label"), "%s(%s)", variant.root, variant.args)
				}
				embedded := executeScalarParityRows(t, env, "item", args, "label")
				if page == "" {
					require.Equal(t, []string{"b", "a", "c"}, scalarParityKeys(embedded))
				}
				residual := fmt.Sprintf(`sort: [{field: "label", direction: %s}, {field: "title"}]%s`, direction, page)
				require.Equal(t, embedded, executeScalarParityRows(t, env, "item", residual, "label"))
			}
		})
	}
}

func TestExecute_TypedScalarBlankStringExists(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Task @node(paths: ["tasks/*.md"]) { key: String @field label: String @field }`, map[string]string{
		"tasks/a.md": "---\ntitle: Task\nkey: a\nlabel: B\n---\n",
		"tasks/b.md": "---\ntitle: Task\nkey: b\nlabel: ''\n---\n",
		"tasks/c.md": "---\ntitle: Task\nkey: c\nlabel: '  '\n---\n",
		"tasks/d.md": "---\ntitle: Task\nkey: d\nlabel: null\n---\n",
		"tasks/e.md": "---\ntitle: Task\nkey: e\nlabel: []\n---\n",
		"tasks/f.md": "---\ntitle: Task\nkey: f\n---\n",
	})
	for _, selector := range []string{"", `find: "Task", `} {
		rows := executeScalarParityRows(t, env, "task", selector+`filters: [{field: "label", op: exists}], sort: [{field: "key"}]`, "label")
		require.Equal(t, []string{"a"}, scalarParityKeys(rows))
	}
}

func TestExecute_ReadOverlayBuiltinPathParity(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Task @node(paths: ["tasks/*.md"]) { key: String @field }`, map[string]string{
		"tasks/B.md": "---\ntitle: Task\nkey: B\n---\n",
		"tasks/a.md": "---\ntitle: Task\nkey: a\n---\n",
	})
	selectors := []string{"", `find: "Task", `, `property: {name: "title", value: "Task"}, `}
	for _, field := range []string{"path", "notePath"} {
		for _, tc := range []struct {
			name, args string
			keys       []string
		}{
			{"eq", fmt.Sprintf(`filters: [{field: %q, op: eq, value: "tasks/B.md"}]`, field), []string{"B"}},
			{"in", fmt.Sprintf(`filters: [{field: %q, op: in, values: ["tasks/B.md"]}]`, field), []string{"B"}},
			{"wrong case eq", fmt.Sprintf(`filters: [{field: %q, op: eq, value: "tasks/b.md"}]`, field), []string{}},
			{"wrong case in", fmt.Sprintf(`filters: [{field: %q, op: in, values: ["tasks/b.md"]}]`, field), []string{}},
			{"asc", fmt.Sprintf(`sort: [{field: %q, direction: asc}]`, field), []string{"B", "a"}},
			{"desc", fmt.Sprintf(`sort: [{field: %q, direction: desc}]`, field), []string{"a", "B"}},
			{"asc page", fmt.Sprintf(`sort: [{field: %q, direction: asc}], first: 1`, field), []string{"B"}},
			{"desc page", fmt.Sprintf(`sort: [{field: %q, direction: desc}], first: 1`, field), []string{"a"}},
			{"asc offset", fmt.Sprintf(`sort: [{field: %q, direction: asc}], first: 1, offset: 1`, field), []string{"a"}},
			{"coherent residual page", fmt.Sprintf(`sort: [{field: %q, direction: asc}, {field: "title"}], first: 1`, field), []string{"B"}},
		} {
			for index, selector := range selectors {
				t.Run(fmt.Sprintf("%s/%s/selector%d", field, tc.name, index), func(t *testing.T) {
					rows := executeScalarParityRows(t, env, "task", selector+tc.args, "path")
					require.Equal(t, tc.keys, scalarParityKeys(rows))
				})
			}
		}
	}
	for _, tc := range []struct {
		name, args string
		keys       []string
	}{
		{"authored text eq", `filters: [{field: "key", op: eq, value: "b"}]`, []string{"B"}},
		{"authored text in", `filters: [{field: "key", op: in, values: ["b"]}]`, []string{"B"}},
		{"authored text sort", `sort: [{field: "key", direction: asc}], first: 1`, []string{"a"}},
	} {
		for index, selector := range selectors {
			t.Run(fmt.Sprintf("%s/selector%d", tc.name, index), func(t *testing.T) {
				rows := executeScalarParityRows(t, env, "task", selector+tc.args, "path")
				require.Equal(t, tc.keys, scalarParityKeys(rows))
			})
		}
	}
}
