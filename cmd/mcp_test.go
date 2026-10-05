package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMCPServeUnknownToolListsValidNames(t *testing.T) {
	_, _, err := runRootCLI(t, nil, []string{"mcp", "serve", "--tools", "not_a_tool"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown MCP tool "not_a_tool"`)
	require.Contains(t, err.Error(), "files")
	require.Contains(t, err.Error(), "semantic_query")
}

func TestMCPServeRequiresTools(t *testing.T) {
	_, _, err := runRootCLI(t, nil, []string{"mcp", "serve"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "tools")
}
