package codex

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

func TestAppServerArgsEncodeEnvironmentValues(t *testing.T) {
	args, err := appServerArgs([]harness.MCPServer{{Name: "local", Command: "rzm", Env: []string{"MODE=first", "MODE=last", "EMPTY=", "ASSIGN=a=b", "KEY.WITH.DOTS=literal"}}})
	require.NoError(t, err)
	require.Contains(t, args, `mcp_servers.local.env={"ASSIGN"="a=b","EMPTY"="","KEY.WITH.DOTS"="literal","MODE"="last"}`)
}
