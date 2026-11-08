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

	"github.com/atomicobject/rhizome/pkg/app/provideraccess"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
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

func TestStatusForwardsProviderReportsOnlyFromVerifiedLiveRuntime(t *testing.T) {
	service := testService(t)
	folder := config(t, repoconfig.LocalRhizomeConfig{})
	reports := []provideraccess.RuntimeReport{{Provider: "voyage", Next: provideraccess.Report{Provider: "voyage", Source: provideraccess.SourceTeam}}}
	health := appruntime.Health{VaultPath: folder, PID: os.Getpid(), RunID: "report-run", Mode: appruntime.ModeHeadless, Version: "v1.2.3", ProviderAccess: reports}
	fakeRuntime(t, folder, appruntime.InstanceManifest{VaultPath: folder, Executable: "/tools/rzm"}, health, 0)
	status := service.runtimeStatus(context.Background(), folder)
	require.Equal(t, StateRunning, status.State)
	require.Equal(t, "v1.2.3", status.Version)
	require.Equal(t, reports, status.ProviderAccess)
	require.Equal(t, "/tools/rzm", status.Executable, "the shell names the rzm that serves the worktree")
	foreign := config(t, repoconfig.LocalRhizomeConfig{})
	health.VaultPath = t.TempDir()
	fakeRuntime(t, foreign, appruntime.InstanceManifest{VaultPath: foreign}, health, 0)
	status = service.runtimeStatus(context.Background(), foreign)
	require.Equal(t, StateStopped, status.State)
	require.Empty(t, status.Version)
	require.Empty(t, status.ProviderAccess)
}

func TestStatusReportsOnlyVerifiedRuntimes(t *testing.T) {
	s := testService(t)
	main, feature := fixtureRepository(t)
	healthy := appruntime.Health{VaultPath: main, PID: os.Getpid(), RunID: "main-run", Mode: appruntime.ModeHeadless, Ready: true}
	fakeRuntime(t, main, appruntime.InstanceManifest{VaultPath: main}, healthy, 0)

	foreign := config(t, repoconfig.LocalRhizomeConfig{})
	fakeRuntime(t, foreign, appruntime.InstanceManifest{VaultPath: t.TempDir()}, appruntime.Health{VaultPath: foreign, PID: os.Getpid(), RunID: "foreign", Mode: appruntime.ModeAttached}, 0)
	remote := config(t, repoconfig.LocalRhizomeConfig{})
	fakeRuntime(t, remote, appruntime.InstanceManifest{VaultPath: remote, HTTPURL: "http://example.invalid:1234"}, appruntime.Health{PID: 1, RunID: "remote"}, 0)
	impostor := config(t, repoconfig.LocalRhizomeConfig{})
	fakeRuntime(t, impostor, appruntime.InstanceManifest{VaultPath: impostor}, appruntime.Health{VaultPath: t.TempDir(), PID: os.Getpid(), RunID: "impostor", Mode: appruntime.ModeAttached}, 0)
	busy := config(t, repoconfig.LocalRhizomeConfig{})
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
		if folder != impostor { // a runtime serving another folder is simply not running here
			require.NotEmpty(t, status.Error, folder)
		}
	}
	require.Equal(t, RuntimeStatus{State: StateStarting}, result.Runtimes[busy])
}
