package claude

import (
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

func TestMCPConfigEncodesEnvironmentValues(t *testing.T) {
	encoded, err := mcpConfig([]harness.MCPServer{{Name: "local", Command: "rzm", Env: []string{"MODE=first", "MODE=last", "EMPTY=", "ASSIGN=a=b"}}})
	require.NoError(t, err)
	var config struct {
		Servers map[string]struct {
			Env map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal([]byte(encoded), &config))
	require.Equal(t, map[string]string{"MODE": "last", "EMPTY": "", "ASSIGN": "a=b"}, config.Servers["local"].Env)
}
