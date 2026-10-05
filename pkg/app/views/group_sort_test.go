package views

import (
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGroupRowsKeepsRepeatedMixedMembership(t *testing.T) {
	rows := []TableRow{
		{Title: "first", Fields: map[string]any{"value": []any{"10", 2, "2.0", nil, "10", []any{}}}},
		{Title: "second", Fields: map[string]any{"value": []any{"text", "2026-01-01", []string{"2", "text"}}}},
	}
	got, groups := groupRows(rows, &viewconfig.GroupSpec{Field: "value"}, nil, false)
	values := make([]string, len(groups))
	for i, group := range groups {
		values[i] = group.Value
	}
	require.Equal(t, []string{"2", "2.0", "10", "2026-01-01", "text", ""}, values)
	require.Equal(t, []string{"first", "second", "first", "first", "second", "second", "first"}, rowTitles(got))
	require.Equal(t, 2, groups[0].Count)
	require.Equal(t, 1, groups[5].Count)
	require.Equal(t, 6, groups[5].RowStart)
	require.Equal(t, 7, groups[5].RowEnd)
}
