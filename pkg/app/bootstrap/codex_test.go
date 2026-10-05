package bootstrap

import (
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestNewCodexAgentConfigCarriesProvidedNoteFormatRuntime(t *testing.T) {
	vault := &obsidian.Vault{Name: "test"}
	vaultDef := obsidian.VaultDefinition{Name: "test", Path: t.TempDir()}
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)

	config, err := newCodexAgentConfig(vault, vaultDef.Path, vaultDef, formats)
	require.NoError(t, err)
	require.Same(t, vault, config.Vault)
	require.Equal(t, vaultDef, config.VaultDef)
	require.NoError(t, config.NoteMetadata.Validate())

	_, err = newCodexAgentConfig(vault, vaultDef.Path, vaultDef, noteformat.Runtime{})
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

func TestLoadOnboardInstructionsUsesCanonicalRhizomeReference(t *testing.T) {
	instructions, err := loadOnboardInstructions()
	if err != nil {
		t.Fatalf("load onboarding instructions: %v", err)
	}
	if !strings.Contains(instructions, "# Onboarding") {
		t.Fatal("expected canonical onboarding reference")
	}
}
