package claude

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
	"github.com/stretchr/testify/require"
)

func TestSessionsKeepAStrictMCPConfigUnlessAskedForTheUsers(t *testing.T) {
	strict, err := sessionArgs(harness.SessionOptions{}, "/tmp/project")
	require.NoError(t, err)
	require.Contains(t, strict, "--strict-mcp-config")

	user, err := sessionArgs(harness.SessionOptions{UserMCPConfig: true, MCPServers: []harness.MCPServer{{Name: "page", Command: "rzm"}}}, "/tmp/project")
	require.NoError(t, err)
	require.NotContains(t, user, "--strict-mcp-config")
	require.Contains(t, argValue(t, user, "--mcp-config"), `"page"`)
}

// The lines are the shapes Claude Code 2.1.330 prints, with made-up names.
func TestListMCPServersReadsNamesAndStatuses(t *testing.T) {
	runner := &fakeRunner{results: []command.Result{{Stdout: "Checking MCP server health…\n\n" +
		"claude.ai Linear: https://mcp.example.test/mcp - ✔ Connected\n" +
		"plugin:tools:files: /usr/local/bin/files mcp - ✗ Failed to connect\n" +
		"plugin:design:notion: https://mcp.example.test/notion (HTTP) - ! Needs authentication\n" +
		"repo: node server.js - ⏸ Pending approval\n"}}}
	driver := &Driver{binary: "claude", runner: runner}

	servers, err := driver.ListMCPServers(context.Background(), "/repo")
	require.NoError(t, err)
	require.Equal(t, []harness.MCPServerState{
		{Name: "claude.ai Linear", Status: "connected", Usable: true},
		{Name: "plugin:tools:files", Status: "failed to connect"},
		{Name: "plugin:design:notion", Status: "needs authentication"},
		{Name: "repo", Status: "pending approval"},
	}, servers)
	require.Equal(t, []string{"mcp", "list"}, runner.commands[0].Args)
	require.Equal(t, "/repo", runner.commands[0].Dir)

	empty := &Driver{binary: "claude", runner: &fakeRunner{results: []command.Result{{Stdout: "No MCP servers configured. Use `claude mcp add` to add a server.\n"}}}}
	servers, err = empty.ListMCPServers(context.Background(), "/repo")
	require.NoError(t, err)
	require.Empty(t, servers)
}
