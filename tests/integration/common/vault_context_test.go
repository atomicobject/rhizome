//go:build integration
// +build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestVaultContext_ReportsCommunities(t *testing.T) {
	ctx := context.Background()
	ws := fixture.NewWorkspace(t)
	ws.IndexCodeAnchors(t, ctx)

	vault := &obsidian.Vault{Name: ws.ProjectRoot}
	cfg := agentapi.Config{
		Vault:        vault,
		VaultPath:    ws.VaultDef.BasePath(),
		VaultDef:     ws.VaultDef,
		CodeAnchor:   ws.CodeAnchor,
		CodeAnchorOn: true,
	}
	text, err := agentapi.CallContextText(ctx, cfg, "vault_context", map[string]any{"profile": "vault"})
	require.NoError(t, err)
	require.Contains(t, text, "## Communities")
	section := strings.SplitN(strings.SplitN(text, "## Communities", 2)[1], "\n## ", 2)[0]
	require.GreaterOrEqual(t, strings.Count(section, "\n### "), 2, "should include at least two communities")
	require.Contains(t, section, "notes/task-flow.md")
	require.Contains(t, section, "notes/product-brief.md")
}
