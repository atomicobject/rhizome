package codex

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
	"github.com/stretchr/testify/require"
)

func TestStatusNotInstalled(t *testing.T) {
	driver := &Driver{binary: "codex", runner: notInstalledRunner()}
	status, err := driver.Status(context.Background())
	require.NoError(t, err)
	require.False(t, status.Installed)
	require.Contains(t, status.LastError, "not installed")
	require.Equal(t, "codex login", status.LoginHint)
}

func TestStatusInstalledNotLoggedIn(t *testing.T) {
	script := harnesstest.NewScriptedIO(
		`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex-cli/0.154.0"}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"account":null,"requiresOpenaiAuth":true}}`,
	)
	driver := statusDriver(script)
	status, err := driver.Status(context.Background())
	require.NoError(t, err)
	require.True(t, status.Installed)
	require.Equal(t, "0.154.0", status.Version)
	require.False(t, status.LoggedIn)
	require.Empty(t, status.LastError)
}

func TestStatusOldVersionIsProbeError(t *testing.T) {
	driver := &Driver{binary: "codex", runner: &fakeRunner{results: []command.Result{{Stdout: "codex-cli 0.149.0"}}}}
	status, err := driver.Status(context.Background())
	require.NoError(t, err)
	require.True(t, status.Installed)
	require.False(t, status.LoggedIn)
	require.Equal(t, "0.149.0", status.Version)
	require.Contains(t, status.LastError, "older than minimum")
}

func TestStatusReady(t *testing.T) {
	script := harnesstest.NewScriptedIO(
		`{"jsonrpc":"2.0","id":1,"result":{"userAgent":"codex-cli/0.154.0"}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"account":{"type":"chatgpt","email":"person@example.com","planType":"team"},"requiresOpenaiAuth":true}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"data":[{"id":"gpt-a","model":"gpt-a","displayName":"GPT A","isDefault":true,"supportedReasoningEfforts":[{"reasoningEffort":"high"},{"reasoningEffort":"medium"}]},{"id":"gpt-b","model":"gpt-b","supportedReasoningEfforts":[{"reasoningEffort":"high"}]}]}}`,
	)
	driver := statusDriver(script)
	status, err := driver.Status(context.Background())
	require.NoError(t, err)
	require.True(t, status.Installed)
	require.True(t, status.LoggedIn)
	require.Equal(t, "person@example.com", status.Account)
	require.Equal(t, []harness.ModelOption{{ID: "gpt-a", DisplayName: "GPT A", Efforts: []string{"high", "medium"}, Default: true}, {ID: "gpt-b", DisplayName: "gpt-b", Efforts: []string{"high"}}}, status.Models)
	require.False(t, status.Capabilities.SupportsAllowedTools)
	require.True(t, status.Capabilities.SupportsAllowForSession)
	require.Empty(t, status.LastError)
}

func statusDriver(script *harnesstest.ScriptedIO) *Driver {
	return &Driver{
		binary: "codex",
		runner: &fakeRunner{results: []command.Result{{Stdout: "codex-cli 0.154.0"}}},
		start: func(context.Context, string, []string, string) (transport, error) {
			return newJSONTransport(script, script, script), nil
		},
	}
}
