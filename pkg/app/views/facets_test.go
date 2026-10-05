package views

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapabilityFacetsRemainCompleteOrAreOmitted(t *testing.T) {
	caps := map[string]FieldCapability{"frontmatter.category": {}, "frontmatter.status": {}, "frontmatter.priority": {}}
	rows := make([]TableRow, 60)
	for i := range rows {
		rows[i] = testRow(fmt.Sprintf("%d.md", i), "Note", map[string]any{
			"category": fmt.Sprintf("category-%02d", i),
			"status":   fmt.Sprintf("status-%02d", i%20),
			"priority": []string{"normal", "high", "normal"},
		})
	}
	got := collectCapabilityValues(caps, rows)
	require.NotContains(t, got, "frontmatter.category")
	require.Len(t, got["frontmatter.status"], 20)
	require.Equal(t, []any{"high", "normal"}, got["frontmatter.priority"])
	rows = append(rows, testRow("late.md", "Late", map[string]any{"status": "late"}))
	require.NotContains(t, collectCapabilityValues(caps, rows), "frontmatter.status")
}
