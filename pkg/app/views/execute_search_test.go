package views

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchMatchesRowValues(t *testing.T) {
	cases := []struct {
		name   string
		row    TableRow
		search string
	}{
		{"title", TableRow{Title: "Release PLAN"}, " plan "},
		{"path", TableRow{Path: "Docs/Release.md"}, "DOCS/"},
		{"type", TableRow{ResolvedType: "ActionItem"}, "ACTIONITEM"},
		{"tags across entries", TableRow{Tags: []string{"Work", "Ready"}}, "WORK READY"},
		{"string", TableRow{Fields: map[string]any{"status": "Ready"}}, "READY"},
		{"strings across entries", TableRow{Fields: map[string]any{"owners": []string{"Alice", "Bob"}}}, "ALICE BOB"},
		{"mixed list", TableRow{Fields: map[string]any{"values": []any{"Priority", 42, true}}}, "PRIORITY 42 TRUE"},
		{"number", TableRow{Fields: map[string]any{"priority": 42}}, "42"},
		{"boolean", TableRow{Fields: map[string]any{"done": true}}, "TRUE"},
		{"stringer", TableRow{Fields: map[string]any{"score": json.Number("12.5")}}, "12.5"},
		{"relation", TableRow{Fields: map[string]any{"owner": map[string]any{"title": "Alice"}}}, "ALICE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := applySearchAndFilters([]TableRow{tc.row}, tc.search, nil, nil)
			require.NoError(t, err)
			require.Equal(t, []TableRow{tc.row}, rows)
		})
	}
	rows, err := applySearchAndFilters([]TableRow{{Title: "Other", Fields: map[string]any{"empty": nil}}}, "missing", nil, nil)
	require.NoError(t, err)
	require.Empty(t, rows)
}

// BenchmarkConfiguredViewSearch measures the residual search stage after source
// hydration, before sorting and pagination. Fixture construction is not timed.
func BenchmarkConfiguredViewSearch(b *testing.B) {
	rows := make([]TableRow, 1000)
	for i := range rows {
		rows[i] = TableRow{
			Title: fmt.Sprintf("Release task %04d", i), Path: fmt.Sprintf("tasks/task-%04d.md", i),
			ResolvedType: "Task", Tags: []string{"Engineering", "Sprint"},
			Fields: map[string]any{
				"status": "Ready", "priority": i % 5, "done": false,
				"summary":  "Coordinate rollout and verify acceptance criteria",
				"assignee": map[string]any{"title": "Alice", "path": "people/alice.md"},
				"aliases":  []string{"delivery", "rollout"}, "estimate": 3.5,
			},
		}
	}
	rows = normalizeRows(rows)
	for _, tc := range []struct {
		name, search string
		matches      int
	}{
		{"title", "RELEASE", 1000}, {"field", "ROLLOUT", 1000}, {"no_match", "absent-token", 0},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				result, err := applySearchAndFilters(rows, tc.search, nil, nil)
				if err != nil || len(result) != tc.matches {
					b.Fatalf("matches=%d err=%v", len(result), err)
				}
			}
		})
	}
}
