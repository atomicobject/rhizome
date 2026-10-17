package bootstrap

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	"os"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestNewCodexAgentConfigCarriesProvidedNoteMetadata(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	vault := &repoconfig.Vault{Env: env, Name: "test"}
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: t.TempDir()}
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	metadata, err := notemeta.NewIndexer(formats)
	require.NoError(t, err)
	resolved := repoconfig.FromLocal(vaultDef.Path, nil)
	snapshot := runtimeview.Snapshot{VaultDefinition: vaultDef, Configuration: &resolved}

	config, err := newCodexAgentConfig(vault, snapshot, metadata)
	require.NoError(t, err)
	require.Same(t, vault, config.Vault)
	require.Equal(t, vaultDef, config.VaultDef)
	require.NoError(t, config.NoteMetadata.Validate())

	_, err = newCodexAgentConfig(vault, snapshot, notemeta.Indexer{})
	require.ErrorContains(t, err, "note format runtime is required")
}

func TestBuildCodexPromptIncludesSections(t *testing.T) {
	related := []RelatedEntry{
		{Label: "vault_context", Response: "vault context"},
		{Label: "semantic query: 'alpha'", Response: "semantic text"},
		{Label: "file_context: 'main.go'", Response: "file text"},
	}
	prompt := BuildCodexPrompt("Do the thing", related, "rhizome", 2000)
	for _, needle := range []string{
		"USER PROMPT: Do the thing",
		"# Related data",
		"vault_context",
		"semantic query: 'alpha'",
		"semantic text",
		"file_context: 'main.go'",
		"file text",
		"# General usage",
		"rhizome",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("prompt missing %q", needle)
		}
	}
}

func TestAutomaticPlanningRequiresInstructionComposition(t *testing.T) {
	calls, warnings := planAutoToolCalls(context.Background(), CodexParams{}, t.TempDir(), nil, nil)
	require.Empty(t, calls)
	require.Equal(t, []string{"auto planning disabled: onboarding instruction loader is not configured"}, warnings)
}
