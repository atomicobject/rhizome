package query

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCaseInsensitiveLookup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		values  map[string]any
		key     string
		want    any
		present bool
	}{
		{"exact key wins", map[string]any{"Name": "exact", "name": "fallback"}, "Name", "exact", true},
		{"normalized fallback", map[string]any{" Name ": "value"}, "NAME", "value", true},
		{"present empty", map[string]any{"Name": ""}, "name", "", true},
		{"present null", map[string]any{"Name": nil}, "name", nil, true},
		{"missing", map[string]any{"Other": "value"}, "name", nil, false},
		{"nil map", nil, "name", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, present := caseInsensitiveLookup(tc.values, tc.key)
			require.Equal(t, tc.want, value)
			require.Equal(t, tc.present, present)
		})
	}
	values, present := caseInsensitiveLookup(map[string][]string{"Name": {}}, "name")
	require.True(t, present)
	require.Empty(t, values)
}
