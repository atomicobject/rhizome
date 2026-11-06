package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

func TestAppServerArgsEncodeEnvironmentValues(t *testing.T) {
	args, err := appServerArgs([]harness.MCPServer{{Name: "local", Command: "rzm", Env: []string{"MODE=first", "MODE=last", "EMPTY=", "ASSIGN=a=b"}}})
	require.NoError(t, err)
	var env map[string]string
	for _, arg := range args {
		if encoded, ok := strings.CutPrefix(arg, "mcp_servers.local.env="); ok {
			require.NoError(t, json.Unmarshal([]byte(encoded), &env))
		}
	}
	require.Equal(t, map[string]string{"MODE": "last", "EMPTY": "", "ASSIGN": "a=b"}, env)
}
