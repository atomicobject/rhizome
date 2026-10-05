package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func executeOverlaySelection(t *testing.T, env *queryTestEnv, text string, overlay *ReadOverlay) Result {
	t.Helper()
	p, errs := Prepare(env.execSchema, text)
	require.Empty(t, errs)
	deps := env.deps(nil)
	deps.ReadOverlay = overlay
	result := Execute(context.Background(), deps, env.schema, p)
	wire, err := json.Marshal(result)
	require.NoError(t, err)
	var serialized Result
	require.NoError(t, json.Unmarshal(wire, &serialized))
	require.Equal(t, result.Errors, serialized.Errors)
	t.Logf("%s", wire)
	return result
}

func TestExecute_ReadOverlayTypedSelection(t *testing.T) {
	for _, required := range []bool{false, true} {
		for _, baselineValid := range []bool{false, true} {
			t.Run(fmt.Sprintf("required=%t/baseline-valid=%t", required, baselineValid), func(t *testing.T) {
				suffix := ""
				if required {
					suffix = "!"
				}
				fields := fmt.Sprintf("key: String @field rank: Int%s @field ready: Boolean%s @field", suffix, suffix)
				schema := `type Task @node(paths: ["tasks/*.md"]) { ` + fields + ` }
 type TaskItem implements Section @node(locator: EMBEDDED) @source(shape: CHECKBOX_ITEM, marker: "#task-item") { ` + fields + ` }`
				note := func(valid bool) string {
					rank, ready := "invalid", "invalid"
					if valid {
						rank, ready = "2", "true"
					}
					return "---\ntitle: Subject\nkey: subject\nrank: '" + rank + "'\nready: '" + ready + "'\n---\n\n# Body\n\n- [ ] Item #task-item\n  key:: item\n  rank:: " + rank + "\n  ready:: " + ready + "\n  ^item\n"
				}
				original := note(baselineValid)
				env := newCustomQueryTestEnv(t, schema, map[string]string{"tasks/subject.md": original, "other.md": "# Other\n"})
				for _, tc := range []struct{ name, changedPath, content string }{
					{"committed", "", ""},
					{"untouched", "other.md", "# Other\n\nUnrelated draft.\n"},
					{"same-fields-prose-only", "tasks/subject.md", original + "\nUnrelated draft.\n"},
					{"invalid-edit", "tasks/subject.md", note(false)},
					{"valid-edit", "tasks/subject.md", note(true)},
				} {
					t.Run(tc.name, func(t *testing.T) {
						var overlay *ReadOverlay
						if tc.changedPath != "" {
							overlay = &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{tc.changedPath: tc.content}, Status: "dirty"}
						}
						for _, root := range []string{"task", "taskItem"} {
							t.Run(root, func(t *testing.T) {
								result := executeOverlaySelection(t, env, `{ `+root+`(first:10) { key rank ready } }`, overlay)
								require.Len(t, result.Data[root].([]any), 1)
								row := result.Data[root].([]any)[0].(map[string]any)
								key := "subject"
								if root == "taskItem" {
									key = "item"
								}
								require.Equal(t, key, row["key"])
								currentValid := baselineValid
								if tc.name == "valid-edit" {
									currentValid = true
								}
								if tc.name == "invalid-edit" {
									currentValid = false
								}
								if currentValid {
									require.Empty(t, result.Errors)
									require.Equal(t, int64(2), row["rank"])
									require.Equal(t, true, row["ready"])
								} else if required {
									require.Len(t, result.Errors, 2)
									require.Equal(t, []string{root, "rank"}, result.Errors[0].Path)
									require.Equal(t, []string{root, "ready"}, result.Errors[1].Path)
									require.NotContains(t, row, "rank")
									require.NotContains(t, row, "ready")
								} else {
									require.Empty(t, result.Errors)
									require.Contains(t, row, "rank")
									require.Nil(t, row["rank"])
									require.Contains(t, row, "ready")
									require.Nil(t, row["ready"])
								}
							})
						}
					})
				}
				require.Contains(t, env.execSchema.SDL, "rank: Int"+suffix)
				require.Contains(t, env.execSchema.SDL, "ready: Boolean"+suffix)
			})
		}
	}
}

func TestExecute_ReadOverlayAuthoredValuePresentation(t *testing.T) {
	const schema = `type Task @node(paths: ["tasks/*.md"]) { rank: Int @field ready: Boolean @field }`
	for _, baselineValid := range []bool{false, true} {
		t.Run(fmt.Sprintf("baseline-valid=%t", baselineValid), func(t *testing.T) {
			original := "---\ntitle: Subject\nrank: invalid\nready: invalid\n---\n"
			if baselineValid {
				original = strings.ReplaceAll(original, "rank: invalid", "rank: 2")
				original = strings.ReplaceAll(original, "ready: invalid", "ready: true")
			}
			env := newCustomQueryTestEnv(t, schema, map[string]string{"tasks/subject.md": original})
			staged := "---\ntitle: Subject\nrank: invalid\nready: invalid\n---\n\nDraft.\n"
			for _, tc := range []struct {
				name    string
				overlay *ReadOverlay
			}{
				{"committed", nil},
				{"staged", &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{"tasks/subject.md": staged}, Status: "dirty"}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					result := executeOverlaySelection(t, env, `{ task(first:10) { rank ready frontmatter workspace { fields { name values present issues { code message } } assessment { fields { name values validValues issues { code message } } } } } }`, tc.overlay)
					require.Empty(t, result.Errors)
					row := result.Data["task"].([]any)[0].(map[string]any)
					values := map[string]string{"rank": "invalid", "ready": "invalid"}
					if baselineValid && tc.overlay == nil {
						values = map[string]string{"rank": "2", "ready": "true"}
						require.Equal(t, int64(2), row["rank"])
						require.Equal(t, true, row["ready"])
					} else {
						require.Nil(t, row["rank"])
						require.Nil(t, row["ready"])
					}
					frontmatter := row["frontmatter"].(map[string]any)
					workspace := row["workspace"].(map[string]any)
					fields := map[string]map[string]any{}
					for _, raw := range workspace["fields"].([]any) {
						field := raw.(map[string]any)
						fields[field["name"].(string)] = field
					}
					for name, value := range values {
						require.Equal(t, value, fmt.Sprint(frontmatter[name]))
						require.Equal(t, []string{value}, fields[name]["values"])
						require.Equal(t, true, fields[name]["present"])
					}
					require.Empty(t, workspace["assessment"].(map[string]any)["fields"])
				})
			}
		})
	}
}

func TestExecute_ReadOverlaySelectionShapeAndEnum(t *testing.T) {
	for _, required := range []bool{false, true} {
		suffix := ""
		if required {
			suffix = "!"
		}
		for _, tc := range []struct {
			name, field, source, validSource string
			validValue                       any
		}{
			{"singular-multiple", "rank", "rank: [2, 10]\n", "rank: 2\n", int64(2)},
			{"list-invalid-member", "ranks", "ranks: [2, invalid]\n", "ranks: [2, 10]\n", []any{int64(2), int64(10)}},
			{"enum-invalid", "status", "status: BAD\n", "status: OPEN\n", "OPEN"},
		} {
			t.Run(fmt.Sprintf("%s/required=%t", tc.name, required), func(t *testing.T) {
				schema := fmt.Sprintf(`enum Status { OPEN DONE } type Task @node(paths:["tasks/*.md"]) { key: String @field rank: Int%s @field ranks: [Int!]%s @field status: Status%s @field }`, suffix, suffix, suffix)
				content := "---\ntitle: Subject\nkey: subject\n" + tc.source + "---\n"
				env := newCustomQueryTestEnv(t, schema, map[string]string{"tasks/subject.md": content})
				for _, overlay := range []*ReadOverlay{nil, {SourceFormat: "markdown", UpdatedContentByPath: map[string]string{"tasks/subject.md": content + "\nDraft.\n"}, Status: "dirty"}} {
					result := executeOverlaySelection(t, env, `{ task(first:10) { key `+tc.field+` } }`, overlay)
					row := result.Data["task"].([]any)[0].(map[string]any)
					require.Equal(t, "subject", row["key"])
					if required {
						require.Len(t, result.Errors, 1)
						require.Equal(t, []string{"task", tc.field}, result.Errors[0].Path)
						require.NotContains(t, row, tc.field)
					} else {
						require.Empty(t, result.Errors)
						require.Contains(t, row, tc.field)
						require.Nil(t, row[tc.field])
					}
				}
				repaired := "---\ntitle: Subject\nkey: subject\n" + tc.validSource + "---\n"
				overlay := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{"tasks/subject.md": repaired}, Status: "dirty"}
				result := executeOverlaySelection(t, env, `{ task(first:10) { key `+tc.field+` } }`, overlay)
				require.Empty(t, result.Errors)
				require.Equal(t, tc.validValue, result.Data["task"].([]any)[0].(map[string]any)[tc.field])
			})
		}
	}
}

func TestExecute_ReadOverlaySelectionAliasesSerializeFieldErrors(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Task @node(paths:["tasks/*.md"]) { key: String @field rank: Int! @field ready: Boolean @field }`, map[string]string{
		"tasks/subject.md": "---\ntitle: Subject\nkey: subject\nrank: 2\nready: true\n---\n",
	})
	overlay := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{"tasks/subject.md": "---\ntitle: Subject\nkey: subject\nrank: invalid\nready: invalid\n---\n"}, Status: "dirty"}
	result := executeOverlaySelection(t, env, `{ selected: task(first:10) { key count: rank active: ready } }`, overlay)
	wire, err := json.Marshal(result)
	require.NoError(t, err)
	var serialized map[string]any
	require.NoError(t, json.Unmarshal(wire, &serialized))
	row := serialized["data"].(map[string]any)["selected"].([]any)[0].(map[string]any)
	require.Equal(t, map[string]any{"key": "subject", "active": nil}, row)
	errors := serialized["errors"].([]any)
	require.Len(t, errors, 1)
	require.Equal(t, "field rank value \"invalid\" does not match Int", errors[0].(map[string]any)["message"])
	require.Equal(t, []any{"selected", "count"}, errors[0].(map[string]any)["path"])
}

func TestExecute_ReadOverlaySelectionNullAndListControls(t *testing.T) {
	for _, tc := range []struct{ name, fieldType, source string }{
		{"optional-missing", "Int", ""},
		{"optional-null", "Int", "value: null\n"},
		{"required-missing", "Int!", ""},
		{"required-null", "Int!", "value: null\n"},
		{"optional-empty-list", "[Int!]", "value: []\n"},
		{"required-empty-list", "[Int!]!", "value: []\n"},
		{"blank-string", "String", "value: ''\n"},
		{"valid-integer-spelling", "Int", "value: '02'\n"},
		{"valid-boolean-spelling", "Boolean", "value: '1'\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := `type Task @node(paths:["tasks/*.md"]) { value: ` + tc.fieldType + ` @field }`
			content := "---\ntitle: Subject\n" + tc.source + "---\n"
			env := newCustomQueryTestEnv(t, schema, map[string]string{"tasks/subject.md": content})
			before := executeOverlaySelection(t, env, `{ task(first:10) { value } }`, nil)
			overlay := &ReadOverlay{SourceFormat: "markdown", UpdatedContentByPath: map[string]string{"tasks/subject.md": content + "\nDraft.\n"}, Status: "dirty"}
			after := executeOverlaySelection(t, env, `{ task(first:10) { value } }`, overlay)
			require.Equal(t, before.Data, after.Data)
			require.Equal(t, len(before.Errors), len(after.Errors))
		})
	}
}
