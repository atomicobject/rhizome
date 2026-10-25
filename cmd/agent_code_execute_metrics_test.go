package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// SPEC-0116 code-mode evidence: the command report carries startup, script and
// per-operation host-call metrics split by host, and no inputs or outputs.
func TestAgentCodeExecuteReportsHostCallMetrics(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is required for JavaScript execution")
	}
	vault := setupAgentTestVault(t, map[string]string{"note.md": "# Fixture\n"})
	const runID = "metrics-runtime"
	var forwarded atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case appruntime.HealthPath:
			_ = json.NewEncoder(w).Encode(appruntime.Health{RunID: runID, PID: os.Getpid(), Mode: appruntime.ModeHeadless})
		case appruntime.AgentOpsPathPrefix + "files":
			forwarded.Add(1)
			writeOutcome(t, w, agentapi.CallOutcome{OK: true, Payload: json.RawMessage(`{"files":["PRIVATE_OUTPUT_MARKER"]}`)}, 1)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	require.NoError(t, appruntime.WriteManifest(vault.path, appruntime.InstanceManifest{
		VaultPath: vault.path, PID: os.Getpid(), RunID: runID, Mode: appruntime.ModeHeadless,
		HTTPURL: server.URL, ControlToken: "test-token",
	}))
	t.Cleanup(func() { _ = appruntime.RemoveManifestIfOwner(vault.path, os.Getpid()) })

	code := `const local = await rzm.surface({});
const first = await rzm.files({inputs: ["PRIVATE_INPUT_MARKER.md"]});
const second = await rzm.files({inputs: ["note.md"]});
return [local.ok, first.ok, second.ok];`
	stdout, err := runRootCLIWithDiagnostics(t, vaultconfig.WithEnv(context.Background(), vault.env),
		[]string{"agent", "code", "execute", "--vault", vault.name, "--code", code})
	require.NoError(t, err, stdout)
	require.Contains(t, stdout, `"result":[true,true,true]`)
	require.Equal(t, int32(2), forwarded.Load())

	reports, err := diagnostics.ReadReports(vault.path, diagnostics.Filter{Kind: "command"})
	require.NoError(t, err)
	require.Len(t, reports.Reports, 1)
	var metrics codeExecuteMetrics
	require.NoError(t, json.Unmarshal(reports.Reports[0].Metrics, &metrics), string(reports.Reports[0].Metrics))
	require.Positive(t, metrics.NodeStartupMS)
	require.Positive(t, metrics.ScriptMS)
	require.Equal(t, 3, metrics.HostCalls.Count)
	require.Zero(t, metrics.HostCalls.Errors)
	require.Positive(t, metrics.HostCalls.TotalMS)
	require.Len(t, metrics.Operations, 2)
	files, surface := metrics.Operations[0], metrics.Operations[1]
	require.Equal(t, codeOperationMetrics{Operation: "files", Count: 2, Forwarded: 2, TotalMS: files.TotalMS, MaxMS: files.MaxMS}, files)
	require.Equal(t, codeOperationMetrics{Operation: "surface", Count: 1, InProcess: 1, TotalMS: surface.TotalMS, MaxMS: surface.MaxMS}, surface)
	require.Positive(t, files.MaxMS)
	require.LessOrEqual(t, files.MaxMS, files.TotalMS)

	// Nothing the script sent or received reaches persisted diagnostics.
	require.NoError(t, filepath.WalkDir(filepath.Join(vault.path, ".rhizome", "diagnostics"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		for _, marker := range []string{"PRIVATE_INPUT_MARKER", "PRIVATE_OUTPUT_MARKER"} {
			require.False(t, strings.Contains(string(data), marker), "%s leaked into %s", marker, path)
		}
		return nil
	}))
}

// runRootCLIWithDiagnostics mirrors Execute's command diagnostics boundary.
func runRootCLIWithDiagnostics(t *testing.T, ctx context.Context, args []string) (string, error) {
	t.Helper()
	app := newRootCommand(commandEnvFromContext(ctx))
	scope := &commandDiagnostics{}
	t.Cleanup(observeDiagnosticArguments(app.Command, scope))
	previous := app.PersistentPreRunE
	app.PersistentPreRunE = func(command *cobra.Command, args []string) error {
		scope.start(command)
		if previous != nil {
			return previous(command, args)
		}
		return nil
	}
	stdout, _, err := runCommandInvocation(t, app, ctx, args, true)
	scope.finish(nil, err)
	return strings.TrimSpace(stdout), err
}
