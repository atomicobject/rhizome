package desktop

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestChatSettingsTurnAnAgentOffWithoutTouchingOtherSettings(t *testing.T) {
	env := testkit.Env(t)
	env.CLIConfigFile = filepath.Join(t.TempDir(), "config.yml")
	require.NoError(t, repoconfig.SaveCliConfig(env, repoconfig.CliConfig{
		DefaultVaultName: "existing", ProviderAccess: map[string]string{"voyage": "server"},
		Agent: &repoconfig.AgentUserConfig{Harness: "codex", Harnesses: map[string]repoconfig.AgentHarnessConfig{
			"codex":  {Model: "gpt-saved", Effort: "high"},
			"claude": {Model: "sonnet", PermissionMode: "approval-required"},
		}},
	}))
	service := &Service{env: env, stateDir: t.TempDir(), chatHarnesses: func() map[harness.Kind]harness.Harness {
		return map[harness.Kind]harness.Harness{
			harness.KindCodex:  &harnesstest.Harness{StatusResult: harness.Status{Installed: true, LoggedIn: true}},
			harness.KindClaude: &harnesstest.Harness{StatusResult: harness.Status{Installed: true, LoggedIn: true, Models: []harness.ModelOption{{ID: "opus"}, {ID: "sonnet", Default: true}}}},
		}
	}}
	handle := func(req Request) agentchat.SettingsResponse {
		t.Helper()
		req.Protocol = Protocol
		response := service.Handle(context.Background(), req)
		require.Nil(t, response.Error)
		return response.Result.(agentchat.SettingsResponse)
	}
	enabled := func(value bool) *bool { return &value }

	report := handle(Request{Operation: "chat-settings"})
	require.Equal(t, harness.KindCodex, report.Resolved.Harness)
	require.Len(t, report.Harnesses, 2)

	off := handle(Request{Operation: "chat-settings-set", Harness: "codex", Enabled: enabled(false)})
	require.True(t, off.Settings.Harnesses[harness.KindCodex].Disabled)
	require.Equal(t, agentchat.ResolvedHarness{Harness: harness.KindClaude, Model: "sonnet", Reason: "first available"}, off.Resolved, "chat moves to the agent still on")

	cfg, err := repoconfig.LoadCliConfig(env, false)
	require.NoError(t, err)
	require.Equal(t, "existing", cfg.DefaultVaultName)
	require.Equal(t, map[string]string{"voyage": "server"}, cfg.ProviderAccess)
	// The switch writes its one field into the stored section rather than a
	// copy it read first, so a choice the composer saves meanwhile survives.
	require.Equal(t, &repoconfig.AgentUserConfig{Harness: "codex", Harnesses: map[string]repoconfig.AgentHarnessConfig{
		"codex":  {Model: "gpt-saved", Effort: "high", Disabled: true},
		"claude": {Model: "sonnet", PermissionMode: "approval-required"},
	}}, cfg.Agent, "only Codex's switch changed; the saved choice stays for when it is on again")

	on := handle(Request{Operation: "chat-settings-set", Harness: "codex", Enabled: enabled(true)})
	require.Equal(t, harness.KindCodex, on.Resolved.Harness)

	for _, req := range []Request{
		{Protocol: Protocol, Operation: "chat-settings-set", Harness: "gemini", Enabled: enabled(true)},
		{Protocol: Protocol, Operation: "chat-settings-set", Harness: "codex"},
	} {
		require.Equal(t, "invalid_request", service.Handle(context.Background(), req).Error.Code)
	}
}
