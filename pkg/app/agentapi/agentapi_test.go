package agentapi

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestCallJSONCapabilities(t *testing.T) {
	vaultPath := t.TempDir()
	cfg := Config{
		Vault:     &obsidian.Vault{Name: "test-vault"},
		VaultPath: vaultPath,
		VaultDef:  obsidian.VaultDefinition{Name: "test-vault", Path: vaultPath},
	}

	payload, err := CallJSON(context.Background(), cfg, "capabilities", map[string]any{})
	require.NoError(t, err)

	var resp CapabilitiesResponse
	require.NoError(t, json.Unmarshal(payload, &resp))
	require.Equal(t, "test-vault", resp.Vault.Name)
	require.Equal(t, filepath.Clean(vaultPath), resp.Vault.Path)
	require.Contains(t, resp.Tools.Available, "files")
	require.Contains(t, resp.Tools.Available, "semantic_query")
	require.NotContains(t, resp.Tools.Available, "next_id")
}

func TestCallJSONUnknownTool(t *testing.T) {
	_, err := CallJSON(context.Background(), Config{}, "not_a_tool", map[string]any{})
	require.EqualError(t, err, "unknown tool: not_a_tool")
}
