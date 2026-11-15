package agentchat

import (
	"github.com/atomicobject/rhizome/internal/testkit"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"

	"github.com/stretchr/testify/require"
)

func TestSettingsPersistInUserConfig(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	configRoot := filepath.Join(t.TempDir(), "config")
	configFile := filepath.Join(configRoot, "config.yml")
	env.CLIConfigFile = configFile

	saved, err := SaveSettings(env, Settings{
		Harness: harness.KindClaude,
		Harnesses: map[harness.Kind]HarnessSettings{
			harness.KindCodex:  {Model: " gpt-5.6 ", Effort: " high ", PermissionMode: harness.PermissionFullAccess},
			harness.KindClaude: {Model: " opus ", Effort: " medium ", PermissionMode: "invalid", Disabled: true},
			"unknown":          {Model: "ignored"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, harness.KindClaude, saved.Harness)
	require.Equal(t, "gpt-5.6", saved.Harnesses[harness.KindCodex].Model)
	require.Equal(t, harness.PermissionAuto, saved.Harnesses[harness.KindClaude].PermissionMode)
	require.NotContains(t, saved.Harnesses, harness.Kind("unknown"))
	require.True(t, saved.Harnesses[harness.KindClaude].Disabled)
	require.False(t, saved.Harnesses[harness.KindCodex].Disabled)

	loaded, err := LoadSettings(env)
	require.NoError(t, err)
	require.Equal(t, saved, loaded)
	data, err := os.ReadFile(configFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "harness: claude")
	require.Contains(t, string(data), "permission-mode: auto")
	require.Equal(t, 1, strings.Count(string(data), "disabled: true"), "an enabled agent stores no flag")
	require.NotContains(t, string(data), "enginePreference")
}

func TestLoadSettingsDefaultsAndInvalidHarness(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	configRoot := filepath.Join(t.TempDir(), "config")
	configFile := filepath.Join(configRoot, "config.yml")
	env.CLIConfigFile = configFile

	settings, err := LoadSettings(env)
	require.NoError(t, err)
	require.Empty(t, settings.Harness)
	require.Equal(t, harness.PermissionAuto, settings.Harnesses[harness.KindCodex].PermissionMode)
	require.NoFileExists(t, configFile)

	require.NoError(t, os.MkdirAll(configRoot, 0o700))
	require.NoError(t, os.WriteFile(configFile, []byte("agent:\n  harness: invalid\n"), 0o600))
	settings, err = LoadSettings(env)
	require.NoError(t, err)
	require.Empty(t, settings.Harness)
}

func TestLoadSettingsIgnoresRemovedAgentKeys(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	configRoot := filepath.Join(t.TempDir(), "config")
	configFile := filepath.Join(configRoot, "config.yml")
	env.CLIConfigFile = configFile
	require.NoError(t, os.MkdirAll(configRoot, 0o700))
	require.NoError(t, os.WriteFile(configFile, []byte("agent:\n  enginePreference: openai\n  selectedModel: old-model\n"), 0o600))

	settings, err := LoadSettings(env)
	require.NoError(t, err)
	require.Equal(t, DefaultSettings(), settings)
}

func TestResolveHarness(t *testing.T) {
	readyCodex := HarnessStatus{Kind: harness.KindCodex, Status: Status{Installed: true, LoggedIn: true}}
	readyClaude := HarnessStatus{Kind: harness.KindClaude, Status: Status{Installed: true, LoggedIn: true}}
	require.Equal(t, ResolvedHarness{Harness: harness.KindClaude, Reason: "configured"}, resolveHarness(Settings{Harness: harness.KindClaude}, nil))
	require.Equal(t, ResolvedHarness{Harness: harness.KindCodex, Reason: "first available"}, resolveHarness(DefaultSettings(), []HarnessStatus{readyClaude, readyCodex}))
	require.Equal(t, ResolvedHarness{Reason: "none available"}, resolveHarness(DefaultSettings(), []HarnessStatus{{Kind: harness.KindCodex}}))

	offered := HarnessStatus{Kind: harness.KindCodex, Status: Status{Installed: true, LoggedIn: true, Models: []harness.ModelOption{{ID: "gpt-test"}}}}
	configured := func(model string) Settings {
		return Settings{Harness: harness.KindCodex, Harnesses: map[harness.Kind]HarnessSettings{harness.KindCodex: {Model: model}}}
	}
	require.Equal(t, "gpt-test", resolveHarness(configured("gpt-test"), []HarnessStatus{offered}).Model)
	require.Empty(t, resolveHarness(configured("fake"), []HarnessStatus{offered}).Model, "a model the harness does not offer falls back to its default")
	unlisted := HarnessStatus{Kind: harness.KindCodex, Status: Status{Installed: true}}
	require.Equal(t, "gpt-saved", resolveHarness(configured("gpt-saved"), []HarnessStatus{unlisted}).Model, "a harness that reported no models, such as after a failed probe, keeps the saved model")
	require.Empty(t, resolveHarness(configured("gpt-saved"), []HarnessStatus{{Kind: harness.KindCodex}}).Model, "a harness that is not installed runs no model")

	off := func(kinds ...harness.Kind) map[harness.Kind]HarnessSettings {
		harnesses := map[harness.Kind]HarnessSettings{}
		for _, kind := range kinds {
			harnesses[kind] = HarnessSettings{Disabled: true}
		}
		return harnesses
	}
	both := []HarnessStatus{readyClaude, readyCodex}
	require.Equal(t, ResolvedHarness{Harness: harness.KindClaude, Reason: "first available"}, resolveHarness(Settings{Harness: harness.KindCodex, Harnesses: off(harness.KindCodex)}, both), "a disabled choice falls back to an enabled agent")
	require.Equal(t, ResolvedHarness{Reason: "none available"}, resolveHarness(Settings{Harnesses: off(harness.KindCodex, harness.KindClaude)}, both), "a disabled agent is never resolved")
}
