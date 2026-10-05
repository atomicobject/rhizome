package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/credentials"
	"github.com/atomicobject/rhizome/pkg/teamkeys"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// aoFixtureTeamKey is a synthetic value used with an injected team-coverage check.
const aoFixtureTeamKey = "synthetic-team-unlock"

func writeLocalConfigWith(t *testing.T, vaultPath, body string) {
	t.Helper()
	dir := filepath.Join(vaultPath, ".rhizome")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"), []byte(body), 0o644))
}

func openAINeeds(t *testing.T) []credentials.Need {
	t.Helper()
	need, ok := credentials.NeedForProvider("openai", "semantic search")
	require.True(t, ok)
	return []credentials.Need{need}
}

func anthropicNeeds(t *testing.T) []credentials.Need {
	t.Helper()
	need, ok := credentials.NeedForProvider("anthropic", "semantic search")
	require.True(t, ok)
	return []credentials.Need{need}
}

func interactiveSession(input string, out *bytes.Buffer) *credentials.Session {
	return credentials.NewSession(credentials.WithPrompts(bufio.NewReader(strings.NewReader(input)), out))
}

func TestEnsureEmbeddingCredentialsNonInteractiveErrors(t *testing.T) {
	withTempCliConfig(t)
	vault := t.TempDir()
	writeLocalConfigWith(t, vault, "noteEmbeddings:\n  enabled: true\n  provider: openai\n")

	in := strings.NewReader("")
	out := &bytes.Buffer{}
	err := ensureEmbeddingCredentials(vault, in, out)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrEmbeddingCredentialsMissing), "want ErrEmbeddingCredentialsMissing, got: %v", err)
	require.Contains(t, err.Error(), "OPENAI_API_KEY")
	require.NotContains(t, err.Error(), "ATOMIC_RHIZOME_KEY")
}

func TestEnsureEmbeddingCredentialsNonTeamProviderDoesNotSuggestTeamKey(t *testing.T) {
	withTempCliConfig(t)
	t.Setenv("ANTHROPIC_API_KEY", "")

	out := &bytes.Buffer{}
	session := credentials.NewSession()
	err := ensureEmbeddingCredentialsWithSession(session, anthropicNeeds(t), out)

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrEmbeddingCredentialsMissing))
	require.Contains(t, err.Error(), "ANTHROPIC_API_KEY")
	require.NotContains(t, err.Error(), "ATOMIC_RHIZOME_KEY")
}

func TestEnsureEmbeddingCredentialsNoOpWhenKeyResolves(t *testing.T) {
	withTempCliConfig(t)
	vault := t.TempDir()
	writeLocalConfigWith(t, vault, "noteEmbeddings:\n  enabled: true\n  provider: openai\n")
	t.Setenv("OPENAI_API_KEY", "from-env")

	out := &bytes.Buffer{}
	err := ensureEmbeddingCredentials(vault, strings.NewReader(""), out)
	require.NoError(t, err)
	require.Empty(t, out.String())
}

func TestEnsureEmbeddingCredentialsSkipsOllama(t *testing.T) {
	withTempCliConfig(t)
	vault := t.TempDir()
	writeLocalConfigWith(t, vault, "noteEmbeddings:\n  enabled: true\n  provider: ollama\n")

	out := &bytes.Buffer{}
	err := ensureEmbeddingCredentials(vault, strings.NewReader(""), out)
	require.NoError(t, err, "ollama should not require a key")
	require.Empty(t, out.String())
}

// Docs: [[init-starter-workflow#^spec-0038-us5-ac4]] — a credential skipped
// during init must not re-prompt even on an interactive session; it surfaces
// a one-line actionable hint and the missing-credentials error instead.
func TestEnsureEmbeddingCredentialsSkippedKeyHintsWithoutPrompt(t *testing.T) {
	withTempCliConfig(t)
	require.NoError(t, obsidian.SaveCliConfig(obsidian.CliConfig{
		CredentialSkips: map[string]bool{"OPENAI_API_KEY": true},
	}))

	out := &bytes.Buffer{}
	session := interactiveSession("2\nshould-never-be-read\n", out)
	err := ensureEmbeddingCredentialsWithSession(session, openAINeeds(t), out)

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrEmbeddingCredentialsMissing))
	require.NotContains(t, out.String(), "Paste your", "must not prompt for a skipped key")

	hint := out.String()
	require.Equal(t, 1, strings.Count(strings.TrimSpace(hint), "\n")+1, "hint should be a single line, got: %q", hint)
	require.Contains(t, hint, "OPENAI_API_KEY")
	require.Contains(t, hint, "semantic search")
	require.Contains(t, hint, "env var")
	require.Contains(t, hint, "rzm init")

	// Key must remain unset: nothing consumed the prompt input.
	cfg, err2 := obsidian.LoadCliConfig(true)
	require.NoError(t, err2)
	require.Empty(t, cfg.Env["OPENAI_API_KEY"])
}

// Truly missing key + interactive session: prompts once and persists.
func TestEnsureEmbeddingCredentialsPromptsOnceAndPersists(t *testing.T) {
	withTempCliConfig(t)

	out := &bytes.Buffer{}
	session := interactiveSession("sk-prompted-123\n", out)
	err := ensureEmbeddingCredentialsWithSession(session, openAINeeds(t), out)
	require.NoError(t, err)

	require.Equal(t, 1, strings.Count(out.String(), "Paste your"), "exactly one prompt")
	cfg, err := obsidian.LoadCliConfig(true)
	require.NoError(t, err)
	require.Equal(t, "sk-prompted-123", cfg.Env["OPENAI_API_KEY"])
}

// Key present via teamkeys: no prompt at all.
func TestEnsureEmbeddingCredentialsTeamKeyNoPrompt(t *testing.T) {
	withTempCliConfig(t)
	t.Setenv("ATOMIC_RHIZOME_KEY", aoFixtureTeamKey)
	teamkeys.ResetCache()

	need, ok := credentials.NeedForProvider("voyage", "semantic search")
	require.True(t, ok)
	session := credentials.NewSession(credentials.WithTeamCoverage(func() bool { return os.Getenv("ATOMIC_RHIZOME_KEY") == aoFixtureTeamKey }))
	out := &bytes.Buffer{}
	err := ensureEmbeddingCredentialsWithSession(session, []credentials.Need{need}, out)
	require.NoError(t, err)
	require.Empty(t, out.String())
}

func TestEnsureEmbeddingCredentialsPersistedConfigKeyResolves(t *testing.T) {
	withTempCliConfig(t)
	require.NoError(t, obsidian.SaveCliConfig(obsidian.CliConfig{
		Env: map[string]string{"OPENAI_API_KEY": "persisted-key"},
	}))

	vault := t.TempDir()
	writeLocalConfigWith(t, vault, "noteEmbeddings:\n  enabled: true\n  provider: openai\n")

	out := &bytes.Buffer{}
	err := ensureEmbeddingCredentials(vault, strings.NewReader(""), out)
	require.NoError(t, err)
	require.Empty(t, out.String())
}
