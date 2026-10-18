package query

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecute_AuthoredScalarParity(t *testing.T) {
	const fields = `key: String @field label: String @field at: DateTime @field rank: Int @field amount: Float @field ready: Boolean @field state: State @field`
	const schema = `enum State { OPEN CLOSED }
type Task @node(paths: ["tasks/*.md"]) {` + fields + `}
type TaskItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#task-item") {` + fields + `}`
	entries := []struct{ key, label, at, state string }{
		{"a", "Ship | Docs", "2026-06-01T08:00:00+02:00", "OPEN"},
		{"b", "Ship | Tests", "2026-06-01T06:00:00.1Z", "CLOSED"},
		{"c", "[[Plain]]", "2026-06-01T06:00:00Z", "OPEN"},
		{"d", "ÄrGeR", "2026-01-01T00:00:00Z", "CLOSED"},
	}
	notes := map[string]string{}
	var embedded strings.Builder
	for _, entry := range entries {
		// Native YAML scalars must retain the same typed value as inline strings.
		notes["tasks/"+entry.key+".md"] = fmt.Sprintf("---\ntitle: Task\nkey: %s\nlabel: %q\nat: %s\nrank: 7\namount: 1.50\nready: true\nstate: %s\n---\n", entry.key, entry.label, entry.at, entry.state)
		fmt.Fprintf(&embedded, "- [ ] Task #task-item\n  key:: %s\n  label:: %s\n  at:: %s\n  rank:: 7\n  amount:: 1.50\n  ready:: true\n  state:: %s\n  ^%s\n\n", entry.key, entry.label, entry.at, entry.state, entry.key)
	}
	notes["notes/items.md"] = embedded.String()
	env := newCustomQueryTestEnv(t, schema, notes)
	for _, test := range []struct {
		field, predicate string
		keys             []string
	}{
		{"label", `op: eq, value: "Ship | Docs"`, []string{"a"}},
		{"label", `op: eq, value: "ship"`, []string{}},
		{"label", `op: contains, value: "docs"`, []string{"a"}},
		{"label", `op: neq, value: "Ship | Tests"`, []string{"a", "c", "d"}},
		{"label", `op: in, values: ["[[Plain]]", "ärger"]`, []string{"c", "d"}},
		{"label", `op: exists`, []string{"a", "b", "c", "d"}},
		{"state", `op: eq, value: "OPEN"`, []string{"a", "c"}},
		{"rank", `op: eq, value: "07"`, []string{"a", "b", "c", "d"}},
		{"amount", `op: eq, value: "1.5"`, []string{"a", "b", "c", "d"}},
		{"ready", `op: eq, value: "true"`, []string{"a", "b", "c", "d"}},
		{"at", `op: eq, value: "2026-06-01T06:00:00Z"`, []string{"a", "c"}},
		{"at", `op: gt, value: "2026-06-01T06:00:00Z"`, []string{"b"}},
		{"at", `op: lte, value: "2026-01-01T00:00:00Z"`, []string{"d"}},
	} {
		t.Run(test.field+"/"+test.predicate, func(t *testing.T) {
			args := fmt.Sprintf(`filters: [{field: %q, %s}], sort: [{field: "key"}]`, test.field, test.predicate)
			for _, variant := range []struct{ root, args string }{
				{"task", args}, {"task", `find: "Task", ` + args},
				{"task", `property: {name: "title", value: "Task"}, ` + args}, {"taskItem", args},
			} {
				require.Equal(t, test.keys, scalarParityKeys(executeScalarParityRows(t, env, variant.root, variant.args, "")), "%s(%s)", variant.root, variant.args)
			}
		})
	}
	for _, root := range []string{"task", "taskItem"} {
		for _, prefix := range []string{"", `find: "Task", `} {
			if root == "taskItem" && prefix != "" {
				continue
			}
			var paged []string
			for offset := range entries {
				args := fmt.Sprintf(`%ssort: [{field: "at"}], first: 1, offset: %d`, prefix, offset)
				paged = append(paged, scalarParityKeys(executeScalarParityRows(t, env, root, args, ""))...)
			}
			require.Equal(t, []string{"d", "a", "c", "b"}, paged)
		}
	}
}

func TestExecuteBlankTextPredicatesPreserveParity(t *testing.T) {
	const fields = `key: String @field label: String @field`
	env := newCustomQueryTestEnv(t, `type Task @node(paths: ["tasks/*.md"]) {`+fields+`}
type TaskItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#task-item") {`+fields+`}`, map[string]string{
		"tasks/a.md":     "---\ntitle: Task\nkey: a\nlabel: value\n---\n",
		"tasks/b.md":     "---\ntitle: Task\nkey: b\nlabel: ''\n---\n",
		"tasks/c.md":     "---\ntitle: Task\nkey: c\n---\n",
		"notes/items.md": "- [ ] Task #task-item\n  key:: a\n  label:: value\n  ^a\n\n- [ ] Task #task-item\n  key:: b\n  label::\n  ^b\n\n- [ ] Task #task-item\n  key:: c\n  ^c\n",
	})
	for _, test := range []struct {
		predicate string
		keys      []string
	}{
		{`op: eq, value: ""`, []string{}},
		{`op: in, values: ["", " "]`, []string{}},
		{`op: in, values: ["", "value"]`, []string{"a"}},
		{`op: exists`, []string{"a"}},
		{`op: neq, value: "value"`, []string{"b", "c"}},
	} {
		for _, variant := range []struct{ root, prefix string }{{"task", ""}, {"task", `find: "Task", `}, {"taskItem", ""}} {
			args := variant.prefix + `filters: [{field: "label", ` + test.predicate + `}], sort: [{field: "key"}]`
			require.Equal(t, test.keys, scalarParityKeys(executeScalarParityRows(t, env, variant.root, args, "")), "%s(%s)", variant.root, args)
		}
	}
}
