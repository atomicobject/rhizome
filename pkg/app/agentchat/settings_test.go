package agentchat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestSettingsPersistInUserConfig(t *testing.T) {
	configRoot := filepath.Join(t.TempDir(), "config")
	configFile := filepath.Join(configRoot, "config.yml")
	original := obsidian.CliConfigPath
	obsidian.CliConfigPath = func() (string, string, error) { return configRoot, configFile, nil }
	t.Cleanup(func() { obsidian.CliConfigPath = original })

	saved, err := SaveSettings(Settings{
		Harness: harness.KindClaude,
		Harnesses: map[harness.Kind]HarnessSettings{
			harness.KindCodex:  {Model: " gpt-5.6 ", Effort: " high ", PermissionMode: harness.PermissionFullAccess},
			harness.KindClaude: {Model: " opus ", Effort: " medium ", PermissionMode: "invalid"},
			"unknown":          {Model: "ignored"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, harness.KindClaude, saved.Harness)
	require.Equal(t, "gpt-5.6", saved.Harnesses[harness.KindCodex].Model)
	require.Equal(t, harness.PermissionApprovalRequired, saved.Harnesses[harness.KindClaude].PermissionMode)
	require.NotContains(t, saved.Harnesses, harness.Kind("unknown"))

	loaded, err := LoadSettings()
	require.NoError(t, err)
	require.Equal(t, saved, loaded)
	data, err := os.ReadFile(configFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "harness: claude")
	require.Contains(t, string(data), "permission-mode: approval-required")
	require.NotContains(t, string(data), "enginePreference")
}

func TestLoadSettingsDefaultsAndInvalidHarness(t *testing.T) {
	configRoot := filepath.Join(t.TempDir(), "config")
	configFile := filepath.Join(configRoot, "config.yml")
	original := obsidian.CliConfigPath
	obsidian.CliConfigPath = func() (string, string, error) { return configRoot, configFile, nil }
	t.Cleanup(func() { obsidian.CliConfigPath = original })

	settings, err := LoadSettings()
	require.NoError(t, err)
	require.Empty(t, settings.Harness)
	require.Equal(t, harness.PermissionApprovalRequired, settings.Harnesses[harness.KindCodex].PermissionMode)
	require.NoFileExists(t, configFile)

	require.NoError(t, os.MkdirAll(configRoot, 0o700))
	require.NoError(t, os.WriteFile(configFile, []byte("agent:\n  harness: invalid\n"), 0o600))
	settings, err = LoadSettings()
	require.NoError(t, err)
	require.Empty(t, settings.Harness)
}

func TestLoadSettingsIgnoresRemovedAgentKeys(t *testing.T) {
	configRoot := filepath.Join(t.TempDir(), "config")
	configFile := filepath.Join(configRoot, "config.yml")
	original := obsidian.CliConfigPath
	obsidian.CliConfigPath = func() (string, string, error) { return configRoot, configFile, nil }
	t.Cleanup(func() { obsidian.CliConfigPath = original })
	require.NoError(t, os.MkdirAll(configRoot, 0o700))
	require.NoError(t, os.WriteFile(configFile, []byte("agent:\n  enginePreference: openai\n  selectedModel: old-model\n"), 0o600))

	settings, err := LoadSettings()
	require.NoError(t, err)
	require.Equal(t, DefaultSettings(), settings)
}

func TestResolveHarness(t *testing.T) {
	readyCodex := HarnessStatus{Kind: harness.KindCodex, Status: Status{Installed: true, LoggedIn: true}}
	readyClaude := HarnessStatus{Kind: harness.KindClaude, Status: Status{Installed: true, LoggedIn: true}}
	require.Equal(t, ResolvedHarness{Harness: harness.KindClaude, Reason: "configured"}, resolveHarness(Settings{Harness: harness.KindClaude}, nil))
	require.Equal(t, ResolvedHarness{Harness: harness.KindCodex, Reason: "first available"}, resolveHarness(DefaultSettings(), []HarnessStatus{readyClaude, readyCodex}))
	require.Equal(t, ResolvedHarness{Reason: "none available"}, resolveHarness(DefaultSettings(), []HarnessStatus{{Kind: harness.KindCodex}}))
}
