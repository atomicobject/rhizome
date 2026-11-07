package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func TestAgentStartChecksConnectedRuntime(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(appruntime.Health{PID: os.Getpid(), RunID: "start-probe", VaultPath: root, Ready: true})
	}))
	defer server.Close()
	require.NoError(t, appruntime.WriteManifest(root, appruntime.InstanceManifest{PID: os.Getpid(), RunID: "start-probe", HTTPURL: server.URL}))
	status := agentStartRuntimeConnection(context.Background(), appruntime.EnsureOptions{VaultPath: root})
	require.Equal(t, "connected", status.State)
	require.True(t, status.Ready)
}

func TestAgentStartReportsStartingWithoutWaitingForPublication(t *testing.T) {
	root := t.TempDir()
	release, acquired, err := indexlock.TryAcquire(appruntime.LockPath(root))
	require.NoError(t, err)
	require.True(t, acquired)
	defer release()
	started := time.Now()
	status := agentStartRuntimeConnection(context.Background(), appruntime.EnsureOptions{VaultPath: root})
	require.Equal(t, "starting", status.State)
	require.Less(t, time.Since(started), agentStartConnectionBudget)
	require.False(t, status.Ready)
}

func TestAgentStartReportsStartingDuringConcurrentSpawn(t *testing.T) {
	root := t.TempDir()
	release, acquired, err := indexlock.TryAcquire(appruntime.SpawnLockPath(root))
	require.NoError(t, err)
	require.True(t, acquired)
	defer release()
	started := time.Now()
	status := agentStartRuntimeConnection(context.Background(), appruntime.EnsureOptions{VaultPath: root, Autostart: true})
	require.Equal(t, "starting", status.State)
	require.Less(t, time.Since(started), agentStartConnectionBudget)
	require.False(t, status.Ready)
}

func TestAgentStartBoundsAnUnresponsiveConnection(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	require.NoError(t, appruntime.WriteManifest(root, appruntime.InstanceManifest{PID: os.Getpid(), RunID: "unready", HTTPURL: server.URL}))
	started := time.Now()
	status := agentStartRuntimeConnection(context.Background(), appruntime.EnsureOptions{VaultPath: root})
	require.Equal(t, "unavailable", status.State)
	require.Less(t, time.Since(started), 2*time.Second)
}
