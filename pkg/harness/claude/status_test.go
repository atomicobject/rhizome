package claude

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
	"github.com/stretchr/testify/require"
)

func TestStatusStates(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		status, err := (&Driver{binary: "claude", runner: notInstalledRunner()}).Status(context.Background())
		require.NoError(t, err)
		require.False(t, status.Installed)
		require.Equal(t, "claude auth login", status.LoginHint)
	})
	t.Run("not logged in with account shell", func(t *testing.T) {
		script := harnesstest.NewScriptedIO(`{"type":"control_response","response":{"subtype":"success","request_id":"status-init","response":{"account":{"tokenSource":"none","apiProvider":"firstParty"},"models":[]}}}`)
		status, err := statusDriver(script).Status(context.Background())
		require.NoError(t, err)
		require.True(t, status.Installed)
		require.Equal(t, "2.1.269", status.Version)
		require.False(t, status.LoggedIn)
		require.Empty(t, status.LastError)
	})
	t.Run("ready", func(t *testing.T) {
		script := harnesstest.NewScriptedIO(`{"type":"control_response","response":{"subtype":"success","request_id":"status-init","response":{"account":{"email":"person@example.com","subscriptionType":"Claude Max"},"models":[{"value":"opus","resolvedModel":"claude-opus-5","displayName":"Opus","isDefault":true,"supportedEffortLevels":["high","medium"]},{"value":"haiku","resolvedModel":"claude-haiku-4-5","supportedEffortLevels":[]}]}}}`)
		driver := statusDriver(script)
		status, err := driver.Status(context.Background())
		require.NoError(t, err)
		require.True(t, status.LoggedIn)
		require.Equal(t, "person@example.com", status.Account)
		require.Equal(t, []harness.ModelOption{{ID: "opus", DisplayName: "Opus", Efforts: []string{"high", "medium"}, Default: true}, {ID: "haiku", DisplayName: "claude-haiku-4-5"}}, status.Models)
		require.True(t, status.Capabilities.SupportsAllowedTools)
		require.True(t, status.Capabilities.SupportsAllowForSession)
		require.Empty(t, status.LastError)
		require.Equal(t, []string{"--version"}, driver.runner.(*fakeRunner).commands[0].Args)
	})
	t.Run("old version", func(t *testing.T) {
		runner := &fakeRunner{results: []command.Result{{Stdout: "1.9.0 (Claude Code)"}}}
		status, err := (&Driver{binary: "claude", runner: runner}).Status(context.Background())
		require.NoError(t, err)
		require.True(t, status.Installed)
		require.False(t, status.LoggedIn)
		require.Equal(t, "1.9.0", status.Version)
		require.Contains(t, status.LastError, "older than minimum")
	})
}

func statusDriver(script *harnesstest.ScriptedIO) *Driver {
	return &Driver{binary: "claude", runner: &fakeRunner{results: []command.Result{{Stdout: "2.1.269 (Claude Code)"}}}, start: func(context.Context, string, []string, string) (transport, error) {
		return newJSONTransport(script, script, script), nil
	}}
}
