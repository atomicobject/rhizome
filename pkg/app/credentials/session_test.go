package credentials

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/teamkeys"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// aoFixtureKey is a synthetic value used with an injected team-coverage check.
const aoFixtureKey = "synthetic-team-unlock"

func setupSessionTest(t *testing.T) {
	t.Helper()
	mockCliConfigPath(t)
	mockHome(t)
	pinCredentialEnv(t)
	teamkeys.ResetCache()
	t.Cleanup(teamkeys.ResetCache)
}

func mockCliConfigPath(t *testing.T) {
	t.Helper()
	original := obsidian.CliConfigPath
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yml")
	obsidian.CliConfigPath = func() (string, string, error) {
		return dir, file, nil
	}
	t.Cleanup(func() {
		obsidian.CliConfigPath = original
	})
}

func mockHome(t *testing.T) {
	t.Helper()
	original := vaultconfig.UserHomeDirectory
	home := t.TempDir()
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	t.Cleanup(func() {
		vaultconfig.UserHomeDirectory = original
	})
}

func pinCredentialEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ATOMIC_RHIZOME_KEY",
		"OPENAI_API_KEY", "RHIZOME_OPENAI_API_KEY",
		"VOYAGE_API_KEY", "RHIZOME_VOYAGE_API_KEY",
		"CEREBRAS_API_KEY", "ANTHROPIC_API_KEY", "TYPESAFE_API_KEY",
	} {
		t.Setenv(key, "")
	}
}

func readPersistedCliConfig(t *testing.T) obsidian.CliConfig {
	t.Helper()
	_, path, err := obsidian.CliConfigPath()
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return obsidian.CliConfig{}
	}
	require.NoError(t, err)
	var cfg obsidian.CliConfig
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	return cfg
}

func boolPtr(v bool) *bool { return &v }

func TestNeedForProvider(t *testing.T) {
	tests := []struct {
		provider     string
		wantKey      string
		wantTeamKey  bool
		wantResolved bool
	}{
		{provider: "openai", wantKey: "OPENAI_API_KEY", wantTeamKey: false, wantResolved: true},
		{provider: "voyage", wantKey: "VOYAGE_API_KEY", wantTeamKey: true, wantResolved: true},
		{provider: "cerebras", wantKey: "CEREBRAS_API_KEY", wantTeamKey: false, wantResolved: true},
		{provider: "anthropic", wantKey: "ANTHROPIC_API_KEY", wantTeamKey: false, wantResolved: true},
		{provider: "Voyage", wantKey: "VOYAGE_API_KEY", wantTeamKey: true, wantResolved: true},
		{provider: "ollama", wantResolved: false},
		{provider: "", wantResolved: false},
	}
	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			need, ok := NeedForProvider(tc.provider, "semantic search")
			require.Equal(t, tc.wantResolved, ok)
			if !ok {
				return
			}
			require.Equal(t, tc.wantKey, need.Key)
			require.Equal(t, tc.wantTeamKey, need.AllowTeamKey)
		})
	}
}

func TestCollectNeeds(t *testing.T) {
	setupSessionTest(t)
	tests := []struct {
		name     string
		cfg      *obsidian.LocalConfig
		wantKeys []string
	}{
		{
			name: "embeddings and compression",
			cfg: &obsidian.LocalConfig{
				NoteEmbeddings: &embeddings.Config{Enabled: true, Provider: "voyage"},
				CodeEmbeddings: &embeddings.Config{Enabled: true, Provider: "voyage"},
				Compression:    &obsidian.LocalCompressionConfig{Enabled: boolPtr(true), Provider: "cerebras"},
			},
			wantKeys: []string{"VOYAGE_API_KEY", "CEREBRAS_API_KEY"},
		},
		{
			name: "disabled compression skipped",
			cfg: &obsidian.LocalConfig{
				Compression: &obsidian.LocalCompressionConfig{Enabled: boolPtr(false), Provider: "cerebras"},
			},
			wantKeys: nil,
		},
		{
			name: "ollama needs no key",
			cfg: &obsidian.LocalConfig{
				NoteEmbeddings: &embeddings.Config{Enabled: true, Provider: "ollama"},
			},
			wantKeys: nil,
		},
		{
			name:     "nil config",
			cfg:      nil,
			wantKeys: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var keys []string
			for _, need := range CollectNeeds(tc.cfg) {
				keys = append(keys, need.Key)
			}
			require.Equal(t, tc.wantKeys, keys)
		})
	}
}

func TestCollectNeedsMergesPurposes(t *testing.T) {
	setupSessionTest(t)
	cfg := &obsidian.LocalConfig{
		NoteEmbeddings: &embeddings.Config{Enabled: true, Provider: "openai"},
		Compression:    &obsidian.LocalCompressionConfig{Enabled: boolPtr(true), Provider: "openai"},
	}
	needs := CollectNeeds(cfg)
	require.Len(t, needs, 1)
	require.Equal(t, "OPENAI_API_KEY", needs[0].Key)
	require.Equal(t, "compression and semantic search", needs[0].Purpose)
}

func TestAOKeySatisfiesTeamCoveredProviders(t *testing.T) {
	setupSessionTest(t)
	t.Setenv("ATOMIC_RHIZOME_KEY", aoFixtureKey)
	teamkeys.ResetCache()

	session := NewSession(WithTeamCoverage(func() bool { return os.Getenv("ATOMIC_RHIZOME_KEY") == aoFixtureKey }))
	for _, provider := range []string{"voyage", "typesafe"} {
		need, ok := NeedForProvider(provider, "semantic search")
		require.True(t, ok)
		require.True(t, session.Satisfied(need), "AO key should satisfy %s", provider)
	}
	anthropic, ok := NeedForProvider("anthropic", "compression")
	require.True(t, ok)
	require.False(t, session.Satisfied(anthropic), "AO key must not satisfy anthropic")
}

func TestResolvePrecedence(t *testing.T) {
	setupSessionTest(t)
	require.NoError(t, obsidian.SaveCliConfig(obsidian.CliConfig{
		Env: map[string]string{"OPENAI_API_KEY": "persisted-key"},
	}))

	session := NewSession(WithTeamCoverage(func() bool { return os.Getenv("ATOMIC_RHIZOME_KEY") == aoFixtureKey }))
	require.Equal(t, "persisted-key", session.Resolve("OPENAI_API_KEY"))

	t.Setenv("OPENAI_API_KEY", "env-key")
	require.Equal(t, "env-key", session.Resolve("OPENAI_API_KEY"))
}

func TestSatisfiedHonorsAltEnvKeys(t *testing.T) {
	setupSessionTest(t)
	t.Setenv("RHIZOME_OPENAI_API_KEY", "alias-key")
	need, ok := NeedForProvider("openai", "semantic search")
	require.True(t, ok)
	require.True(t, NewSession().Satisfied(need))
}

func TestProvidePersistsImmediatelyAndClearsSkip(t *testing.T) {
	setupSessionTest(t)
	require.NoError(t, obsidian.SaveCliConfig(obsidian.CliConfig{
		CredentialSkips: map[string]bool{"OPENAI_API_KEY": true},
	}))

	session := NewSession(WithTeamCoverage(func() bool { return os.Getenv("ATOMIC_RHIZOME_KEY") == aoFixtureKey }))
	require.True(t, session.Skipped("OPENAI_API_KEY"))
	require.NoError(t, session.Provide("OPENAI_API_KEY", "sk-test"))

	cfg := readPersistedCliConfig(t)
	require.Equal(t, "sk-test", cfg.Env["OPENAI_API_KEY"])
	require.False(t, cfg.CredentialSkips["OPENAI_API_KEY"])
	require.False(t, session.Skipped("OPENAI_API_KEY"))
	require.Equal(t, "sk-test", os.Getenv("OPENAI_API_KEY"))
}

func TestSkipPersistsImmediately(t *testing.T) {
	setupSessionTest(t)
	session := NewSession(WithTeamCoverage(func() bool { return os.Getenv("ATOMIC_RHIZOME_KEY") == aoFixtureKey }))
	require.NoError(t, session.Skip("VOYAGE_API_KEY"))

	cfg := readPersistedCliConfig(t)
	require.True(t, cfg.CredentialSkips["VOYAGE_API_KEY"])
	require.True(t, NewSession().Skipped("VOYAGE_API_KEY"))
}

func TestEnsureNeedsNoPromptWhenNonInteractive(t *testing.T) {
	setupSessionTest(t)
	need, _ := NeedForProvider("openai", "semantic search")
	require.NoError(t, NewSession().EnsureNeeds([]Need{need}))
	var nilSession *Session
	require.NoError(t, nilSession.EnsureNeeds([]Need{need}))
}

func TestEnsureNeedsPromptsAndPersistsProviderKey(t *testing.T) {
	setupSessionTest(t)
	var out bytes.Buffer
	session := NewSession(WithTeamCoverage(func() bool { return os.Getenv("ATOMIC_RHIZOME_KEY") == aoFixtureKey }), WithPrompts(bufio.NewReader(bytes.NewBufferString("sk-prompted\n")), &out))
	need, _ := NeedForProvider("openai", "semantic search")

	require.NoError(t, session.EnsureNeeds([]Need{need}))

	cfg := readPersistedCliConfig(t)
	require.Equal(t, "sk-prompted", cfg.Env["OPENAI_API_KEY"])
	require.True(t, session.Satisfied(need))
	before := out.Len()
	require.NoError(t, session.EnsureNeeds([]Need{need}))
	require.Equal(t, before, out.Len(), "persisted key must suppress a second prompt")
	require.Contains(t, out.String(), "Paste your OpenAI API Key for semantic search (Enter to skip):")
	require.NotContains(t, out.String(), "Atomic Object")
}

func TestEnsureNeedsSkipRecordedOnce(t *testing.T) {
	setupSessionTest(t)
	var out bytes.Buffer
	session := NewSession(WithTeamCoverage(func() bool { return os.Getenv("ATOMIC_RHIZOME_KEY") == aoFixtureKey }), WithPrompts(bufio.NewReader(bytes.NewBufferString("\n")), &out))
	need, _ := NeedForProvider("voyage", "semantic search")

	require.NoError(t, session.EnsureNeeds([]Need{need}))
	require.True(t, readPersistedCliConfig(t).CredentialSkips["VOYAGE_API_KEY"])

	// Second pass with no further input must not prompt again.
	before := out.Len()
	require.NoError(t, session.EnsureNeeds([]Need{need}))
	require.Equal(t, before, out.Len())
}

func TestEnsureNeedsDoesNotPersistEnvironmentCredentials(t *testing.T) {
	setupSessionTest(t)
	t.Setenv("OPENAI_API_KEY", "env-found")
	var out bytes.Buffer
	// Decline persistence; key still satisfies the need via env.
	session := NewSession(WithTeamCoverage(func() bool { return os.Getenv("ATOMIC_RHIZOME_KEY") == aoFixtureKey }), WithPrompts(bufio.NewReader(bytes.NewBufferString("n\n")), &out))
	need, _ := NeedForProvider("openai", "semantic search")

	require.NoError(t, session.EnsureNeeds([]Need{need}))
	require.Empty(t, out.String())
	cfg := readPersistedCliConfig(t)
	require.Empty(t, cfg.Env["OPENAI_API_KEY"])

	before := out.Len()
	require.NoError(t, session.EnsureNeeds([]Need{need}))
	require.Equal(t, before, out.Len(), "found-in-env offer must not repeat in the same run")
}

// bundledBuild makes a session behave as a build that carries the Atomic
// Object team-key bundle unlocked by aoFixtureKey.
var bundledBuild = WithTeamKeyBundle(func() bool { return true }, func(value string) bool { return value == aoFixtureKey })

func TestPromptMentionsTheTeamKeyOnlyInBundledBuilds(t *testing.T) {
	setupSessionTest(t)
	need, _ := NeedForProvider("voyage", "semantic search")
	var out bytes.Buffer
	session := NewSession(WithTeamCoverage(func() bool { return false }), WithPrompts(bufio.NewReader(bytes.NewBufferString("pa-voyage\n")), &out))

	require.NoError(t, session.EnsureNeeds([]Need{need}))

	require.NotContains(t, out.String(), "Atomic Object")
	require.Equal(t, "pa-voyage", readPersistedCliConfig(t).Env["VOYAGE_API_KEY"])
}

func TestBundledBuildRecognizesAProviderKeyPastedAtTheTeamPrompt(t *testing.T) {
	setupSessionTest(t)
	need, _ := NeedForProvider("voyage", "semantic search")
	var out bytes.Buffer
	session := NewSession(bundledBuild, WithTeamCoverage(func() bool { return false }), WithPrompts(bufio.NewReader(bytes.NewBufferString("pa-voyage\n")), &out))

	require.NoError(t, session.EnsureNeeds([]Need{need}))

	require.Contains(t, out.String(), "Paste your Atomic Object Rhizome key or your Voyage API Key")
	cfg := readPersistedCliConfig(t)
	require.Equal(t, "pa-voyage", cfg.Env["VOYAGE_API_KEY"])
	require.Empty(t, cfg.Env["ATOMIC_RHIZOME_KEY"])
}

func TestEnsureNeedsAOKeyPromptCoversAllProviders(t *testing.T) {
	setupSessionTest(t)
	var out bytes.Buffer
	session := NewSession(bundledBuild, WithTeamCoverage(func() bool { return os.Getenv("ATOMIC_RHIZOME_KEY") == aoFixtureKey }), WithPrompts(bufio.NewReader(bytes.NewBufferString(aoFixtureKey+"\n")), &out))
	voyage, _ := NeedForProvider("voyage", "semantic search")
	typesafe, _ := NeedForProvider("typesafe", "compression")

	require.NoError(t, session.EnsureNeeds([]Need{voyage, typesafe}))

	cfg := readPersistedCliConfig(t)
	require.Equal(t, aoFixtureKey, cfg.Env["ATOMIC_RHIZOME_KEY"])
	require.Empty(t, cfg.Env["VOYAGE_API_KEY"])
	require.Empty(t, cfg.Env["TYPESAFE_API_KEY"])
	require.NotContains(t, out.String(), "TypeSafe API Key", "second need must be covered by the AO key")
}
