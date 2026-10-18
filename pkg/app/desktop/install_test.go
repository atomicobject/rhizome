package desktop

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/stretchr/testify/require"
)

func releaseFixture(t *testing.T, badChecksum bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("release smoke executable is a Unix shell fixture")
	}
	binary := []byte("#!/bin/sh\n[ \"$RZM_SKIP_REPO_DELEGATE\" = 1 ] || exit 3\ncase \"$1\" in --version) echo 'rzm version v1.2.3';; serve) echo 'serve --headless';; *) exit 2;; esac\n")
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	require.NoError(t, tarWriter.WriteHeader(&tar.Header{Name: "rzm", Mode: 0o755, Size: int64(len(binary))}))
	_, err := tarWriter.Write(binary)
	require.NoError(t, err)
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	sha := fmt.Sprintf("%x", sha256.Sum256(archive.Bytes()))
	if badChecksum {
		sha = fmt.Sprintf("%064d", 0)
	}
	var requests atomic.Int32
	artifact := appupdate.ArtifactObjectName(runtime.GOOS, runtime.GOARCH)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/releases/latest", "/releases/tags/v1.2.3":
			origin := "http://" + r.Host
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"tag_name": "v1.2.3", "assets": []map[string]string{{"name": "checksums.txt", "browser_download_url": origin + "/checksums"}, {"name": artifact, "browser_download_url": origin + "/artifact"}}}))
		case "/checksums":
			fmt.Fprintf(w, "%s  %s\n", sha, artifact)
		case "/artifact":
			_, err := w.Write(archive.Bytes())
			require.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func TestPinAdvanceUsesReleaseAndChangesOnlyPin(t *testing.T) {
	s := testService(t)
	// The app offers the version this bridge was built as.
	s.available = func() string { return version.Version }
	server, requests := releaseFixture(t, false)
	folder := config(t, repoconfig.LocalRhizomeConfig{Version: "v1.0.0"})
	priorVersion := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = priorVersion })
	workflowPath := filepath.Join(folder, ".rhizome", "workflows.yml")
	workflow := []byte("# Preserve authored workflows\ntemplates: [spec-driven]\nfutureWorkflow: retained\n")
	require.NoError(t, os.WriteFile(workflowPath, workflow, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(folder, ".gitignore"), []byte(".rhizome/bin/\n"), 0o644))
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = folder
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return string(out)
	}
	git("init", "-q")
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	s.install = func(ctx context.Context, opts appupdate.Options) (appupdate.Result, error) {
		require.Equal(t, version.Version, opts.SetVersion)
		require.True(t, opts.Yes)
		require.Equal(t, folder, opts.WorkDir)
		opts.ManifestURL = server.URL + "/releases/latest"
		opts.HTTPClient = server.Client()
		return appupdate.Run(ctx, opts)
	}
	_, err := s.PinAdvance(context.Background(), folder)
	code(t, err, "trust_required")
	require.Zero(t, requests.Load())
	_, err = s.Trust(folder)
	require.NoError(t, err)
	advanced, err := s.PinAdvance(context.Background(), folder)
	require.NoError(t, err)
	require.Equal(t, "v1.2.3", advanced.PinnedVersion)
	require.Equal(t, " M .rhizome/config.yml\n", git("status", "--porcelain", "--untracked-files=all"))
	installed := filepath.Join(folder, ".rhizome", "bin", repoexec.PlatformDir(), repoexec.ExecutableName())
	probed, err := repoexec.Version(context.Background(), installed, folder)
	require.NoError(t, err)
	require.Equal(t, "v1.2.3", probed)
	actualWorkflow, err := os.ReadFile(workflowPath)
	require.NoError(t, err)
	require.Equal(t, workflow, actualWorkflow)
}

func TestPinAdvanceChecksumFailureNamesStageAndPreservesPinAndBinary(t *testing.T) {
	s := testService(t)
	s.available = func() string { return "v1.2.3" }
	server, _ := releaseFixture(t, true)
	folder := config(t, repoconfig.LocalRhizomeConfig{Version: "v1.0.0"})
	target := filepath.Join(folder, ".rhizome", "bin", repoexec.PlatformDir(), repoexec.ExecutableName())
	workingScript(t, target)
	before, err := os.ReadFile(target)
	require.NoError(t, err)
	s.install = func(ctx context.Context, opts appupdate.Options) (appupdate.Result, error) {
		opts.SetVersion = "v1.2.3"
		opts.ManifestURL = server.URL + "/releases/latest"
		opts.HTTPClient = server.Client()
		return appupdate.Run(ctx, opts)
	}
	_, err = s.Trust(folder)
	require.NoError(t, err)
	_, err = s.PinAdvance(context.Background(), folder)
	code(t, err, "pin_advance_failed")
	require.Contains(t, err.Error(), "checksum")
	_, cfg, err := repoconfig.FindLocalConfigForDelegation(folder)
	require.NoError(t, err)
	require.Equal(t, "v1.0.0", cfg.Rhizome.Version)
	after, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestManagedBootstrapRequiresTrustBeforeReleaseDownload(t *testing.T) {
	s := testService(t)
	server, requests := releaseFixture(t, false)
	folder := config(t, repoconfig.LocalRhizomeConfig{Version: "v1.2.3"})
	s.ensurePinned = func(ctx context.Context, opts appupdate.EnsureOptions) error {
		opts.ManifestURL = server.URL + "/releases/latest"
		opts.HTTPClient = server.Client()
		return appupdate.EnsurePinnedBinary(ctx, opts)
	}
	_, err := s.Open(context.Background(), Request{Folder: folder})
	code(t, err, "trust_required")
	require.Zero(t, requests.Load())
	_, err = s.Trust(folder)
	require.NoError(t, err)
	s.ensure = func(_ context.Context, opts appruntime.EnsureOptions) (appruntime.EnsureResult, error) {
		require.FileExists(t, opts.Executable)
		return appruntime.EnsureResult{Client: &appruntime.Client{Manifest: appruntime.InstanceManifest{HTTPURL: "http://127.0.0.1:1234"}}, Health: appruntime.Health{VaultPath: folder, Version: "v1.2.3", BuildID: opts.BuildID, PID: 123, Mode: appruntime.ModeHeadless}}, nil
	}
	_, err = s.Open(context.Background(), Request{Folder: folder})
	require.NoError(t, err)
	require.Positive(t, requests.Load())
	_, loaded, err := repoconfig.FindLocalConfigForDelegation(folder)
	require.NoError(t, err)
	require.Equal(t, "v1.2.3", loaded.Rhizome.Version)
}
