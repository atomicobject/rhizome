package harness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMCPServerEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []string
		want    map[string]string
		invalid bool
	}{
		{name: "none", want: map[string]string{}},
		{name: "empty value", entries: []string{"EMPTY="}, want: map[string]string{"EMPTY": ""}},
		{name: "equals in value", entries: []string{"ASSIGN=a=b"}, want: map[string]string{"ASSIGN": "a=b"}},
		{name: "duplicate key", entries: []string{"MODE=first", "MODE=last"}, want: map[string]string{"MODE": "last"}},
		{name: "missing equals", entries: []string{"NO_VALUE"}, invalid: true},
		{name: "empty key", entries: []string{"=value"}, invalid: true},
		{name: "blank key", entries: []string{"  =value"}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (MCPServer{Env: tc.entries}).Environment()
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
