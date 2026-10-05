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
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	appserve "github.com/atomicobject/rhizome/pkg/app/cli/serve"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/stretchr/testify/require"
)

// fixtureBinaryEnv points at a compiled rzm (NO_WEB=1 make build). The fixture
// is opt-in because it needs that binary plus node; everything it proves about
// routing is also covered by the httptest tests beside it.
const fixtureBinaryEnv = "RZM_CODE_MODE_RUNTIME_FIXTURE"

// TestNodeScriptCallsRunInOneVaultRuntime is SPEC-0104 US4's evidence: a real
// Node script drives the compiled `rzm agent code serve` host, and every
// catalog call is routed to the discovered vault runtime endpoint and executed
// by its registered agent-operation hook rather than by the host.
func TestNodeScriptCallsRunInOneVaultRuntime(t *testing.T) {
	binary := os.Getenv(fixtureBinaryEnv)
	if binary == "" {
		t.Skipf("set %s to a compiled rzm to run the runtime code-mode fixture", fixtureBinaryEnv)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is unavailable")
	}
	vault := setupAgentTestVault(t, map[string]string{
		"note.md":    "# Fixture\n\nExact evidence lives here.\n",
		"CONTEXT.md": "# Fixture context\n\nThis context governs the fixture vault.\n",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rt, err := bootstrap.NewLiveRuntime(ctx, bootstrap.LiveOptions{
		VaultName: vault.name, DisableLeaderWork: true, DisableWatchHub: true, SkipCacheWarmup: true,
	})
	require.NoError(t, err)
	defer rt.Close()
	hooks := &appserve.ControlHooks{}
	registerAgentOpHooks(hooks, rt)
	require.NotNil(t, hooks.AgentOp)

	runtimeProcess := newFixtureRuntime(t, vault.path, hooks)

	artifact, err := agentcode.Generate(t.TempDir(), []string{"files", "file_context"})
	require.NoError(t, err)
	result := runFixtureNode(t, artifact.ModulePath, binary, vault.path, `
const client = createClient({executablePath, vaultPath, sessionId: "fixture-session"});
const first = await client.files({inputs:["note.md"], includeContent:true});
const second = await client.files({inputs:["CONTEXT.md"], includeContent:true});
const third = await client.fileContext({files:["note.md"]});
await client.close();
console.log(JSON.stringify({ok:[first.ok, second.ok, third.ok]}));`)

	require.Equal(t, []any{true, true, true}, result["ok"], "every call must succeed in the runtime")
	served, sessions := runtimeProcess.observed()
	require.Equal(t, 3, served, "all three calls must be served by the runtime")
	require.Equal(t, []string{"fixture-session", "fixture-session", "fixture-session"}, sessions)
}

type fixtureRuntime struct {
	mu       sync.Mutex
	sessions []string
}

func (f *fixtureRuntime) observed() (int, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sessions), append([]string(nil), f.sessions...)
}

// newFixtureRuntime publishes a manifest for a control API backed by the real
// agent-operation hook, which is what a live `rzm serve` will expose once its
// lane lands. The stdio host discovers it exactly as it would a real runtime.
func newFixtureRuntime(t *testing.T, vaultPath string, hooks *appserve.ControlHooks) *fixtureRuntime {
	t.Helper()
	observer := &fixtureRuntime{}
	token, err := appruntime.NewControlToken()
	require.NoError(t, err)
	runID, err := appruntime.NewRunID()
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.HandleFunc(appruntime.HealthPath, func(w http.ResponseWriter, r *http.Request) {
		writeFixtureJSON(t, w, appruntime.Health{RunID: runID, PID: os.Getpid(), VaultPath: vaultPath, Mode: appruntime.ModeHeadless, Ready: true})
	})
	mux.HandleFunc(appruntime.AgentOpsPathPrefix, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))
		var req appruntime.AgentOpRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		outcome, err := hooks.AgentOp(r.Context(), strings.TrimPrefix(r.URL.Path, appruntime.AgentOpsPathPrefix), req)
		require.NoError(t, err)
		observer.mu.Lock()
		observer.sessions = append(observer.sessions, req.SessionID)
		observer.mu.Unlock()
		writeFixtureJSON(t, w, outcome)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	require.NoError(t, appruntime.WriteManifest(vaultPath, appruntime.InstanceManifest{
		InstanceID: runID, VaultPath: vaultPath, PID: os.Getpid(), RunID: runID,
		Mode: appruntime.ModeHeadless, ControlToken: token, HTTPURL: server.URL, Ready: true,
	}))
	t.Cleanup(func() { _ = os.Remove(appruntime.ManifestPath(vaultPath)) })
	return observer
}

func writeFixtureJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(payload))
}

func runFixtureNode(t *testing.T, module, executable, vault, body string) map[string]any {
	t.Helper()
	encode := func(value string) string {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		return string(encoded)
	}
	runner := filepath.Join(t.TempDir(), "runner.mjs")
	source := "import {createClient} from " + encode(module) + ";\n" +
		"const executablePath = " + encode(executable) + "; const vaultPath = " + encode(vault) + ";\n" + body
	require.NoError(t, os.WriteFile(runner, []byte(source), 0o644))
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", runner)
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var value map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &value), string(output))
	return value
}
