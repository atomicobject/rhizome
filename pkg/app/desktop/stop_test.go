package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestStopReportsAFolderWithoutARuntimeAsStopped(t *testing.T) {
	s := testService(t)
	folder := config(t, obsidian.LocalRhizomeConfig{})
	result, err := s.Stop(context.Background(), Request{Folder: folder})
	require.NoError(t, err)
	require.True(t, result.Stopped)
}

func TestStopNeverShutsDownARuntimeAnotherFoldersManifestNames(t *testing.T) {
	s := testService(t)
	folder := config(t, obsidian.LocalRhizomeConfig{})
	other := t.TempDir()
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		_ = json.NewEncoder(w).Encode(appruntime.Health{VaultPath: other, PID: os.Getpid(), RunID: "other", Mode: appruntime.ModeHeadless, Ready: true})
	}))
	t.Cleanup(server.Close)
	require.NoError(t, appruntime.WriteManifest(folder, appruntime.InstanceManifest{
		VaultPath: other, HTTPURL: server.URL, PID: os.Getpid(), RunID: "other", ControlToken: "synthetic-control-secret",
	}))

	_, err := s.Stop(context.Background(), Request{Folder: folder})
	require.ErrorContains(t, err, "another folder")
	require.Zero(t, posts.Load(), "no shutdown request reaches the other runtime")
}
