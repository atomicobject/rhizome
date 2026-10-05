package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/agentcode"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestCodeModeLocalOperationStartsWhileRuntimeOwnerWaitsForWriter(t *testing.T) {
	vault := setupAgentTestVault(t, nil)
	vaultName = vault.name
	lockPath := filepath.Join(vault.path, ".rhizome", "index.lock")
	releaseWriter, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = releaseWriter() })
	releaseOwner, acquired, err := indexlock.TryAcquireWithOptions(appruntime.LockPath(vault.path), indexlock.AcquireOptions{ProcessLifetime: true})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = releaseOwner() })

	baseCtx := contextWithCommandEnv(context.Background(), commandEnv{Getwd: func() (string, error) { return vault.path, nil }})
	ctx, cancel := context.WithTimeout(baseCtx, 2*time.Second)
	defer cancel()
	done := make(chan agentcode.CallOutcome, 1)
	errors := make(chan error, 1)
	go func() {
		call, err := newAgentCodeInvoker(ctx, false)
		if err != nil {
			errors <- err
			return
		}
		outcome, err := call(ctx, "surface", map[string]any{})
		if err != nil {
			errors <- err
			return
		}
		done <- outcome
	}()
	select {
	case outcome := <-done:
		require.True(t, outcome.OK, outcome.Stderr)
	case err := <-errors:
		t.Fatalf("local operation failed: %v", err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("local operation waited for runtime recovery")
	}
}

func TestForwardedCodeModeCallRetriesAfterCanceledRuntimeWait(t *testing.T) {
	vault := setupAgentTestVault(t, nil)
	releaseOwner, acquired, err := indexlock.TryAcquireWithOptions(appruntime.LockPath(vault.path), indexlock.AcquireOptions{ProcessLifetime: true})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = releaseOwner() })
	forwarder := newAgentOpForwarder(vault.path)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	outcome, handled := forwarder.call(ctx, "files", map[string]any{}, false)
	require.True(t, handled, "a canceled forwarded call must not fall back to local execution")
	require.False(t, outcome.OK)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	require.Empty(t, forwarder.reason, "a later call may retry runtime attachment")
	require.NoError(t, releaseOwner())

	const runID = "later-runtime"
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case appruntime.HealthPath:
			probes.Add(1)
			_ = json.NewEncoder(w).Encode(appruntime.Health{RunID: runID, PID: os.Getpid(), Mode: appruntime.ModeHeadless})
		case appruntime.AgentOpsPathPrefix + "files":
			writeOutcome(t, w, agentapi.CallOutcome{OK: true, Payload: json.RawMessage(`{}`)}, 1)
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

	outcome, handled = forwarder.call(context.Background(), "files", map[string]any{}, false)
	require.True(t, handled)
	require.True(t, outcome.OK, outcome.Stderr)
	require.Equal(t, int32(1), probes.Load())
}

// fakeRuntime stands in for a live vault runtime's agent-operation route.
func fakeRuntime(t *testing.T, handler http.HandlerFunc) *agentOpForwarder {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &agentOpForwarder{client: &appruntime.Client{
		Manifest: appruntime.InstanceManifest{HTTPURL: server.URL, ControlToken: "control-token"},
		HTTP:     server.Client(),
	}}
}

func writeOutcome(t *testing.T, w http.ResponseWriter, outcome agentapi.CallOutcome, generation uint64) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(withConfigGeneration(outcome, generation)))
}

func TestAgentCodeForwardsAuthorityAndSessionToTheRuntime(t *testing.T) {
	var gotPath, gotAuth string
	var gotRequest appruntime.AgentOpRequest
	forwarder := fakeRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotRequest))
		writeOutcome(t, w, agentapi.CallOutcome{OK: true, Payload: json.RawMessage(`{"files":["a.go"]}`)}, 1)
	})

	outcome, handled := forwarder.call(context.Background(), "files", map[string]any{"inputs": []any{"a.go"}, "sessionId": "session-7"}, true)

	require.True(t, handled)
	require.Equal(t, appruntime.AgentOpsPathPrefix+"files", gotPath)
	require.Equal(t, "Bearer control-token", gotAuth)
	require.True(t, gotRequest.ReadWrite)
	require.Equal(t, "session-7", gotRequest.SessionID)
	require.Equal(t, "session-7", gotRequest.Input["sessionId"])
	require.True(t, outcome.OK)
	// The payload keeps the runtime's exact JSON, and the generation the runtime
	// used to reach the host never reaches the generated client.
	require.JSONEq(t, `{"files":["a.go"]}`, string(outcome.Payload.(json.RawMessage)))
	require.Nil(t, outcome.Diagnostic)
}

func TestAgentCodeForwardsReadOnlyAuthorityUnchanged(t *testing.T) {
	var gotRequest appruntime.AgentOpRequest
	forwarder := fakeRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotRequest))
		writeOutcome(t, w, agentapi.CallOutcome{OK: true, Payload: json.RawMessage(`null`)}, 1)
	})

	_, handled := forwarder.call(context.Background(), "files", map[string]any{"inputs": []any{"a.go"}}, false)

	require.True(t, handled)
	require.False(t, gotRequest.ReadWrite)
	require.Empty(t, gotRequest.SessionID)
}

func TestAgentCodeForwarderSupportsConcurrentCalls(t *testing.T) {
	var active atomic.Int64
	var maximum atomic.Int64
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	forwarder := fakeRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for current > maximum.Load() && !maximum.CompareAndSwap(maximum.Load(), current) {
		}
		started <- struct{}{}
		<-release
		writeOutcome(t, w, agentapi.CallOutcome{OK: true, Payload: json.RawMessage(`{}`)}, 1)
	})

	var calls sync.WaitGroup
	for range 2 {
		calls.Add(1)
		go func() {
			defer calls.Done()
			outcome, handled := forwarder.call(context.Background(), "files", map[string]any{}, false)
			require.True(t, handled)
			require.True(t, outcome.OK)
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("forwarded calls did not overlap")
		}
	}
	close(release)
	calls.Wait()
	require.Equal(t, int64(2), maximum.Load())
}

func TestAgentCodeFallsBackWhenTheRuntimeHookIsNotEnabled(t *testing.T) {
	calls := 0
	forwarder := fakeRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"agent operations are not enabled"}`))
	})

	stderr := captureStderr(t, func() {
		_, handled := forwarder.call(context.Background(), "files", map[string]any{}, false)
		require.False(t, handled)
		// The whole connection stays in-process after one failure.
		_, handled = forwarder.call(context.Background(), "files", map[string]any{}, false)
		require.False(t, handled)
	})

	require.Equal(t, 1, calls)
	require.Equal(t, 1, strings.Count(stderr, "executing operations in this process"))
	require.Contains(t, stderr, "503")
}

func TestAgentCodeFallsBackWhenTheRuntimeTransportFails(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	forwarder := &agentOpForwarder{client: &appruntime.Client{
		Manifest: appruntime.InstanceManifest{HTTPURL: server.URL, ControlToken: "control-token"},
		HTTP:     server.Client(),
	}}
	server.Close()

	stderr := captureStderr(t, func() {
		_, handled := forwarder.call(context.Background(), "files", map[string]any{}, false)
		require.False(t, handled)
	})

	require.Nil(t, forwarder.client)
	require.Contains(t, stderr, "executing operations in this process")
}

func TestAgentCodeReportsRuntimeConfigurationChange(t *testing.T) {
	var generation atomic.Uint64
	generation.Store(4)
	forwarder := fakeRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		writeOutcome(t, w, agentapi.CallOutcome{OK: true, Payload: json.RawMessage(`{}`)}, generation.Load())
	})

	first, handled := forwarder.call(context.Background(), "files", map[string]any{}, false)
	require.True(t, handled)
	require.True(t, first.OK)

	generation.Store(5)
	second, handled := forwarder.call(context.Background(), "files", map[string]any{}, false)

	require.True(t, handled)
	require.False(t, second.OK)
	require.Equal(t, 1, second.ExitCode)
	require.Equal(t, "code_mode_configuration_changed", second.Diagnostic.(map[string]any)["code"])
	// A changed configuration is a client-visible outcome, not a broken runtime.
	require.NotNil(t, forwarder.client)
}

func TestAgentCodeCancellationDoesNotFallBackToThisProcess(t *testing.T) {
	release := make(chan struct{})
	forwarder := fakeRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
	})
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	outcome, handled := forwarder.call(ctx, "files", map[string]any{}, false)

	require.True(t, handled)
	require.Equal(t, 1, outcome.ExitCode)
	require.NotNil(t, forwarder.client, "a cancelled call must not disable the runtime for the connection")
}

func TestRuntimeRefusesLocalOnlyOperations(t *testing.T) {
	host := &agentOpHost{}
	local := 0
	for _, descriptor := range agentapi.CodeOperationDescriptors() {
		if descriptor.CodeLocal == nil || (descriptor.CodeRuntimeRead != nil && descriptor.CodeRuntimeRead(map[string]any{})) {
			continue
		}
		local++
		outcome := host.execute(context.Background(), descriptor.Name, appruntime.AgentOpRequest{Input: map[string]any{}})
		require.Equal(t, 1, outcome.ExitCode, descriptor.Name)
		require.Equal(t, "code_mode_local_only", outcome.Diagnostic.(map[string]any)["code"], descriptor.Name)
	}
	require.NotZero(t, local)
	require.Equal(t, 1, host.execute(context.Background(), "not_an_operation", appruntime.AgentOpRequest{}).ExitCode)
}

func TestRuntimeServesOnlyCatalogAdmittedReads(t *testing.T) {
	host := &agentOpHost{}
	for _, refused := range []struct {
		name  string
		input map[string]any
	}{
		{"view", map[string]any{"action": "eject", "id": "x"}},
		{"view", map[string]any{"action": "run", "id": "x", "path": "views"}},
		{"query_recipe", map[string]any{"op": "list"}},
		{"query_recipe", map[string]any{"op": "run", "id": "x", "path": "recipes"}},
	} {
		outcome := host.execute(context.Background(), refused.name, appruntime.AgentOpRequest{Input: refused.input})
		require.Equal(t, "code_mode_local_only", outcome.Diagnostic.(map[string]any)["code"], refused)
	}
}

// attachFakeRuntime publishes a live manifest for a stand-in runtime, so a real
// code-mode invoker attaches to it and sends agent operations to ops.
func attachFakeRuntime(t *testing.T, vaultPath string, ops http.HandlerFunc) {
	t.Helper()
	const runID = "fake-runtime"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == appruntime.HealthPath {
			_ = json.NewEncoder(w).Encode(appruntime.Health{RunID: runID, PID: os.Getpid(), Mode: appruntime.ModeHeadless})
			return
		}
		ops(w, r)
	}))
	t.Cleanup(server.Close)
	require.NoError(t, appruntime.WriteManifest(vaultPath, appruntime.InstanceManifest{
		VaultPath: vaultPath, PID: os.Getpid(), RunID: runID, Mode: appruntime.ModeHeadless,
		HTTPURL: server.URL, ControlToken: "test-token",
	}))
	t.Cleanup(func() { _ = appruntime.RemoveManifestIfOwner(vaultPath, os.Getpid()) })
}

func TestAgentCodeServesTypedReadsInTheRuntimeUntilTheConnectionWrites(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{"a.md": "# A\n"})
	vaultName = vault.name
	var reads atomic.Int32
	var pending atomic.Bool
	attachFakeRuntime(t, vault.path, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, appruntime.AgentOpsPathPrefix+"ontology_query", r.URL.Path)
		reads.Add(1)
		if pending.Load() {
			writeOutcome(t, w, codeModeFailure(fmt.Errorf(`{"code":%q}`, actions.CodeModeRuntimeReadPending)), 1)
			return
		}
		writeOutcome(t, w, agentapi.CallOutcome{OK: true, Payload: json.RawMessage(`{"data":{"host":"runtime"}}`)}, 1)
	})
	ctx := contextWithCommandEnv(context.Background(), commandEnv{Getwd: func() (string, error) { return vault.path, nil }})
	call, err := newAgentCodeInvoker(ctx, true)
	require.NoError(t, err)
	query := map[string]any{"query": "{ __typename }"}

	outcome, err := call(ctx, "ontology_query", query)
	require.NoError(t, err)
	require.JSONEq(t, `{"data":{"host":"runtime"}}`, string(outcome.Payload.(json.RawMessage)))

	// A runtime that cannot show its projection is current hands the call back.
	pending.Store(true)
	outcome, err = call(ctx, "ontology_query", query)
	require.NoError(t, err)
	require.Equal(t, int32(2), reads.Load())
	require.False(t, actions.IsCodeModeRuntimeReadPending(outcome.Diagnostic), "the pending signal never reaches the client")

	// After this connection writes, its reads observe the write in-process.
	pending.Store(false)
	_, err = call(ctx, "note_move", map[string]any{"source": "a.md", "target": "b.md"})
	require.NoError(t, err)
	_, err = call(ctx, "ontology_query", query)
	require.NoError(t, err)
	require.Equal(t, int32(2), reads.Load())
}

func TestRuntimeEnforcesTheConnectionsWriteAuthority(t *testing.T) {
	// These refusals run before any runtime capability is consulted, so a
	// zero-value host proves the order: authority first, work second.
	host := &agentOpHost{}
	mutating := 0
	for _, descriptor := range agentapi.CodeOperationDescriptors() {
		if descriptor.CodeLocal != nil || descriptor.Mutation == agentapi.MutationNever {
			continue
		}
		mutating++
		outcome := host.execute(context.Background(), descriptor.Name, appruntime.AgentOpRequest{Input: map[string]any{}, ReadWrite: false})
		require.Equal(t, 1, outcome.ExitCode, descriptor.Name)
		require.Equal(t, "write_requires_read_write", outcome.Diagnostic.(map[string]any)["code"], descriptor.Name)
	}
	require.NotZero(t, mutating)

	for name, input := range map[string]map[string]any{
		"node_link":    {"ensure": "apply"},
		"file_context": {"ensureLinkTargets": "apply"},
	} {
		outcome := host.execute(context.Background(), name, appruntime.AgentOpRequest{Input: input, ReadWrite: true})
		require.Equal(t, 1, outcome.ExitCode, name)
		require.Equal(t, "apply_requires_read_write", outcome.Diagnostic.(map[string]any)["code"], name)
	}
}

func TestAgentCodeOutcomeSurvivesTheRuntimeRoundTrip(t *testing.T) {
	for _, outcome := range []agentapi.CallOutcome{
		{OK: true, Payload: json.RawMessage(`{"ok":1}`)},
		codeModeFailure(context.DeadlineExceeded),
		codeModeConfigurationChanged("test"),
	} {
		forwarder := fakeRuntime(t, func(w http.ResponseWriter, r *http.Request) {
			writeOutcome(t, w, outcome, 9)
		})
		got, handled := forwarder.call(context.Background(), "files", map[string]any{}, false)
		require.True(t, handled)
		require.Equal(t, uint64(9), forwarder.generation)
		expected, err := json.Marshal(outcome)
		require.NoError(t, err)
		actual, err := json.Marshal(got)
		require.NoError(t, err)
		require.JSONEq(t, string(expected), string(actual))
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	original := os.Stderr
	os.Stderr = writer
	defer func() { os.Stderr = original }()
	fn()
	require.NoError(t, writer.Close())
	captured, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(captured)
}
