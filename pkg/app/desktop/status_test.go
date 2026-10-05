package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// fakeRuntime publishes a manifest for folder whose health endpoint answers
// with health, after delay.
func fakeRuntime(t *testing.T, folder string, manifest appruntime.InstanceManifest, health appruntime.Health, delay time.Duration) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(health))
	}))
	t.Cleanup(server.Close)
	if manifest.HTTPURL == "" {
		manifest.HTTPURL = server.URL
	}
	manifest.PID, manifest.RunID, manifest.ControlToken = health.PID, health.RunID, "synthetic-control-secret"
	require.NoError(t, appruntime.WriteManifest(folder, manifest))
}

func TestStatusReportsOnlyVerifiedRuntimes(t *testing.T) {
	s := testService(t)
	main, feature := fixtureRepository(t)
	healthy := appruntime.Health{VaultPath: main, PID: os.Getpid(), RunID: "main-run", Mode: appruntime.ModeHeadless, Ready: true}
	fakeRuntime(t, main, appruntime.InstanceManifest{VaultPath: main}, healthy, 0)

	foreign := config(t, obsidian.LocalRhizomeConfig{})
	fakeRuntime(t, foreign, appruntime.InstanceManifest{VaultPath: t.TempDir()}, appruntime.Health{VaultPath: foreign, PID: os.Getpid(), RunID: "foreign", Mode: appruntime.ModeAttached}, 0)
	remote := config(t, obsidian.LocalRhizomeConfig{})
	fakeRuntime(t, remote, appruntime.InstanceManifest{VaultPath: remote, HTTPURL: "http://example.invalid:1234"}, appruntime.Health{PID: 1, RunID: "remote"}, 0)
	impostor := config(t, obsidian.LocalRhizomeConfig{})
	fakeRuntime(t, impostor, appruntime.InstanceManifest{VaultPath: impostor}, appruntime.Health{VaultPath: t.TempDir(), PID: os.Getpid(), RunID: "impostor", Mode: appruntime.ModeAttached}, 0)
	busy := config(t, obsidian.LocalRhizomeConfig{})
	fakeRuntime(t, busy, appruntime.InstanceManifest{VaultPath: busy}, appruntime.Health{VaultPath: busy, PID: os.Getpid(), RunID: "busy", Mode: appruntime.ModeHeadless}, 10*time.Second)

	input, err := json.Marshal(Request{Protocol: Protocol, Operation: "status", Folders: []string{foreign, remote, impostor, busy}, Repositories: []RepositoryRef{{Folder: main}, {Folder: filepath.Join(main, "missing")}}})
	require.NoError(t, err)
	var output bytes.Buffer
	started := time.Now()
	require.NoError(t, Serve(context.Background(), s, bytes.NewReader(input), &output))
	require.Less(t, time.Since(started), 3*time.Second, "probes run concurrently with a short timeout")
	require.NotContains(t, output.String(), "synthetic-control-secret")

	var response struct {
		Result StatusResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &response))
	result := response.Result
	require.Len(t, result.Repositories, 2)
	require.Equal(t, []string{main, feature}, paths(*result.Repositories[0].Repository))
	require.Equal(t, "folder_missing", result.Repositories[1].Error.Code)

	running := result.Runtimes[main]
	require.Equal(t, RuntimeStatus{State: StateRunning, Mode: "headless", Ready: true, PID: os.Getpid(), URL: running.URL}, running)
	require.True(t, strings.HasPrefix(running.URL, "http://127.0.0.1:"))
	require.Equal(t, RuntimeStatus{State: StateStopped}, result.Runtimes[feature])
	for _, folder := range []string{foreign, remote, impostor} {
		status := result.Runtimes[folder]
		require.Equal(t, StateStopped, status.State, folder)
		require.Empty(t, status.URL, folder)
		require.NotEmpty(t, status.Error, folder)
	}
	require.Equal(t, RuntimeStatus{State: StateStarting}, result.Runtimes[busy])
}
