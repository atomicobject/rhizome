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

func TestChatSettingsChooseTheAgentAndModelWithoutTouchingOtherSettings(t *testing.T) {
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
	model := func(value string) *string { return &value }

	report := handle(Request{Operation: "chat-settings"})
	require.Equal(t, harness.KindCodex, report.Settings.Harness)
	require.Len(t, report.Harnesses, 2)

	chosen := handle(Request{Operation: "chat-settings-set", Harness: "claude"})
	require.Equal(t, agentchat.ResolvedHarness{Harness: harness.KindClaude, Model: "sonnet", Reason: "configured"}, chosen.Resolved, "choosing an agent keeps its saved model")

	chosen = handle(Request{Operation: "chat-settings-set", Harness: "claude", Model: model("opus")})
	require.Equal(t, "opus", chosen.Resolved.Model)
	chosen = handle(Request{Operation: "chat-settings-set", Harness: "claude", Model: model("")})
	require.Empty(t, chosen.Resolved.Model, "an empty model restores the default")

	cfg, err := repoconfig.LoadCliConfig(env, false)
	require.NoError(t, err)
	require.Equal(t, "existing", cfg.DefaultVaultName)
	require.Equal(t, map[string]string{"voyage": "server"}, cfg.ProviderAccess)
	require.Equal(t, "claude", cfg.Agent.Harness)
	require.Equal(t, repoconfig.AgentHarnessConfig{Model: "gpt-saved", Effort: "high", PermissionMode: "auto"}, cfg.Agent.Harnesses["codex"])
	require.Equal(t, repoconfig.AgentHarnessConfig{PermissionMode: "approval-required"}, cfg.Agent.Harnesses["claude"])

	for _, req := range []Request{
		{Protocol: Protocol, Operation: "chat-settings-set", Harness: "gemini"},
		{Protocol: Protocol, Operation: "chat-settings-set", Harness: "codex", Model: model("bad\nmodel")},
	} {
		require.Equal(t, "invalid_request", service.Handle(context.Background(), req).Error.Code)
	}
}
