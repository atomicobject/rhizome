package codex

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
	"github.com/stretchr/testify/require"
)

// The fields are the ones codex-cli 0.171.0 prints, with made-up servers.
func TestListMCPServersReadsEnabledAndSignInState(t *testing.T) {
	runner := &fakeRunner{results: []command.Result{{Stdout: `[
		{"name":"linear","enabled":true,"auth_status":"o_auth","transport":{"type":"streamable_http","url":"https://mcp.example.test"}},
		{"name":"files","enabled":false,"auth_status":"unsupported","transport":{"type":"stdio","command":"files"}},
		{"name":"notion","enabled":true,"auth_status":"not_logged_in","transport":{"type":"streamable_http","url":"https://mcp.example.test/notion"}}
	]`}}}
	driver := &Driver{binary: "codex", runner: runner}

	servers, err := driver.ListMCPServers(context.Background(), "/repo")
	require.NoError(t, err)
	require.Equal(t, []harness.MCPServerState{
		{Name: "linear", Status: "enabled", Usable: true},
		{Name: "files", Status: "disabled"},
		{Name: "notion", Status: "not logged in"},
	}, servers)
	require.Equal(t, []string{"mcp", "list", "--json"}, runner.commands[0].Args)
	require.Equal(t, "/repo", runner.commands[0].Dir)
}
