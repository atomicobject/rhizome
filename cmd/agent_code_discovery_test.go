package cmd

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/stretchr/testify/require"
)

func TestAgentCodeDescribeSelectedSchemas(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{"note.md": "# Fixture\n"})
	for _, selection := range []string{"input", "output", "both"} {
		t.Run(selection, func(t *testing.T) {
			stdout, _, err := runRootCLI(t, context.Background(), []string{
				"agent", "code", "describe", "--operation", "queryRecipe", "--schema", selection, "--vault", vault.name,
			})
			require.NoError(t, err)
			var response agentcode.DescribeResponse
			require.NoError(t, json.Unmarshal([]byte(stdout), &response))
			require.Equal(t, []string{"query_recipe"}, response.Selected)
			require.Len(t, response.Operations, 1)
			require.Equal(t, "queryRecipe", response.Operations[0].Method)
			if selection == "input" {
				require.NotEmpty(t, response.Operations[0].InputSchema)
				require.Empty(t, response.Operations[0].OutputSchema)
			} else if selection == "output" {
				require.Empty(t, response.Operations[0].InputSchema)
				require.NotEmpty(t, response.Operations[0].OutputSchema)
			} else {
				require.NotEmpty(t, response.Operations[0].InputSchema)
				require.NotEmpty(t, response.Operations[0].OutputSchema)
			}
		})
	}
	_, _, err := runRootCLI(t, context.Background(), []string{
		"agent", "code", "describe", "--operation", "files", "--schema", "inputs", "--vault", vault.name,
	})
	require.Error(t, err)
}
