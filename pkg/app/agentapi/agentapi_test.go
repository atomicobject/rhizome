package agentapi

import (
	"context"
	"encoding/json"
	"github.com/atomicobject/rhizome/internal/testkit"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestCallJSONCapabilities(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	vaultPath := t.TempDir()
	cfg := Config{
		Vault:     &repoconfig.Vault{Env: env, Name: "test-vault"},
		VaultPath: vaultPath,
		VaultDef:  obsidian.VaultDefinition{Name: "test-vault", Path: vaultPath},
	}

	payload, err := CallJSON(vaultconfig.WithEnv(context.Background(), env), cfg, "capabilities", map[string]any{})
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
