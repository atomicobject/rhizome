package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestLiveCodeModeReadReportsWatcherStartupAsPending(t *testing.T) {
	runtime := liveCodeModeReads{rt: &bootstrap.LiveRuntime{}}
	ready, err := runtime.SyncSources(context.Background())
	require.NoError(t, err)
	require.False(t, ready)
}

func TestAgentCodePendingReadKeepsTheRuntimeAsWriter(t *testing.T) {
	options := newRuntimeOptionsForTest(t)
	vault := setupAgentTestVault(t, ontologyCommandFiles())
	options.vaultName = vault.name
	release, acquired, err := indexlock.TryAcquire(filepath.Join(vault.path, ".rhizome", "index.lock"))
	require.NoError(t, err)
	require.True(t, acquired)
	defer release()

	var reads atomic.Int32
	attachFakeRuntime(t, vault.path, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/agent/ops/ontology_query", r.URL.Path)
		if reads.Add(1) == 1 {
			writeOutcome(t, w, codeModeFailure(fmt.Errorf(`{"code":%q}`, actions.CodeModeRuntimeReadPending)), 1)
			return
		}
		writeOutcome(t, w, agentapi.CallOutcome{OK: true, Payload: json.RawMessage(`{"data":{"project":[]}}`)}, 1)
	})
	base := contextWithCommandEnv(vaultconfig.WithEnv(context.Background(), vault.env), commandEnv{Getwd: func() (string, error) { return vault.path, nil }})
	ctx, cancel := context.WithTimeout(base, time.Second)
	defer cancel()
	call, err := options.newAgentCodeInvoker(ctx, false)
	require.NoError(t, err)
	outcome, err := call(ctx, "ontology_query", map[string]any{"query": "{ project(first: 1) { title } }"})
	require.NoError(t, err)
	require.True(t, outcome.OK, outcome.Stderr)
	require.Equal(t, int32(2), reads.Load(), "pending reads must return to the live owner instead of waiting for its writer lock")
	require.JSONEq(t, `{"data":{"project":[]}}`, string(outcome.Payload.(json.RawMessage)))
}

func TestAgentCodePendingReadHonorsCancellation(t *testing.T) {
	options := newRuntimeOptionsForTest(t)
	vault := setupAgentTestVault(t, nil)
	options.vaultName = vault.name
	var reads atomic.Int32
	var ready atomic.Bool
	attachFakeRuntime(t, vault.path, func(w http.ResponseWriter, _ *http.Request) {
		reads.Add(1)
		if ready.Load() {
			writeOutcome(t, w, agentapi.CallOutcome{OK: true, Payload: json.RawMessage(`{"data":{"project":[]}}`)}, 1)
			return
		}
		writeOutcome(t, w, codeModeFailure(fmt.Errorf(`{"code":%q}`, actions.CodeModeRuntimeReadPending)), 1)
	})
	base := contextWithCommandEnv(vaultconfig.WithEnv(context.Background(), vault.env), commandEnv{Getwd: func() (string, error) { return vault.path, nil }})
	ctx, cancel := context.WithTimeout(base, 250*time.Millisecond)
	defer cancel()
	call, err := options.newAgentCodeInvoker(ctx, false)
	require.NoError(t, err)
	outcome, err := call(ctx, "ontology_query", map[string]any{"query": "{ project(first: 1) { title } }"})
	require.NoError(t, err)
	require.False(t, outcome.OK)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	require.Contains(t, outcome.Stderr, context.DeadlineExceeded.Error())
	require.Positive(t, reads.Load())

	ready.Store(true)
	outcome, err = call(base, "ontology_query", map[string]any{"query": "{ project(first: 1) { title } }"})
	require.NoError(t, err)
	require.True(t, outcome.OK, "cancellation must not force fallback or invalidate the connection")
}
