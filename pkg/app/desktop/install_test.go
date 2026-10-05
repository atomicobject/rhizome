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
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
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

func TestGlobalInstallUsesReleaseChecksumAndPreservesRepositoryPin(t *testing.T) {
	s := testService(t)
	server, _ := releaseFixture(t, false)
	folder := config(t, obsidian.LocalRhizomeConfig{Version: "v9.9.9"})
	before, err := os.ReadFile(filepath.Join(folder, ".rhizome", "config.yml"))
	require.NoError(t, err)
	prior, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(folder))
	defer func() { require.NoError(t, os.Chdir(prior)) }()
	s.install = func(ctx context.Context, opts appupdate.Options) (appupdate.Result, error) {
		opts.ManifestURL = server.URL + "/releases/latest"
		opts.HTTPClient = server.Client()
		return appupdate.Run(ctx, opts)
	}
	installed, err := s.InstallGlobal(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v1.2.3", installed.Version)
	require.Equal(t, s.managedPath(), installed.Path)
	require.True(t, installed.CanUpdate)
	after, err := os.ReadFile(filepath.Join(folder, ".rhizome", "config.yml"))
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NoDirExists(t, filepath.Join(folder, ".rhizome", "bin"))
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, folder, cwd)
}

func TestGlobalChecksumFailurePreservesExistingExecutable(t *testing.T) {
	s := testService(t)
	server, _ := releaseFixture(t, true)
	workingScript(t, s.managedPath())
	before, err := os.ReadFile(s.managedPath())
	require.NoError(t, err)
	s.install = func(ctx context.Context, opts appupdate.Options) (appupdate.Result, error) {
		opts.ManifestURL = server.URL + "/releases/latest"
		opts.HTTPClient = server.Client()
		return appupdate.Run(ctx, opts)
	}
	info, err := s.InstallGlobal(context.Background())
	code(t, err, "install_error")
	require.Contains(t, err.Error(), "checksum")
	require.True(t, info.Installed)
	after, err := os.ReadFile(s.managedPath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestManagedBootstrapRequiresTrustBeforeReleaseDownload(t *testing.T) {
	s := testService(t)
	server, requests := releaseFixture(t, false)
	folder := config(t, obsidian.LocalRhizomeConfig{Version: "v1.2.3"})
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
	_, loaded, err := obsidian.FindLocalConfigForDelegation(folder)
	require.NoError(t, err)
	require.Equal(t, "v1.2.3", loaded.Rhizome.Version)
}
