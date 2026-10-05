package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectedAgentSurfaceIncludesOnlyRequestedContract(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{"Alpha.md": "# Alpha\n"})
	for _, selector := range []string{"query-recipe run", "rzm agent query-recipe run"} {
		t.Run(selector, func(t *testing.T) {
			stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "surface", "--vault", vault.name, "--command", selector})
			require.NoError(t, err)
			require.Empty(t, stderr)
			var selected agentSelectedSurfaceResponse
			require.NoError(t, json.Unmarshal([]byte(stdout), &selected))
			require.Equal(t, "run", selected.Selected.Name)
			names := map[string]bool{}
			for _, flag := range selected.Selected.Flags {
				names[flag.Name] = true
			}
			for _, flag := range []string{"id", "vault", "session-id"} {
				require.True(t, names[flag], flag)
			}
			require.NotContains(t, stdout, `"capabilities"`)
			require.NotContains(t, stdout, `"retrievalLoop"`)
		})
	}
	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "surface", "--vault", vault.name, "--command", "query-recipe"})
	require.NoError(t, err)
	require.Empty(t, stderr)
	var parent agentSelectedSurfaceResponse
	require.NoError(t, json.Unmarshal([]byte(stdout), &parent))
	require.NotEmpty(t, parent.Selected.Subcommands)
	for _, selector := range []string{"query-recipe missing", "missing"} {
		stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "surface", "--vault", vault.name, "--command", selector})
		require.Error(t, err)
		require.Empty(t, stdout)
		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(stderr), &payload))
		require.Contains(t, payload["error"], "unknown agent command")
	}
}
