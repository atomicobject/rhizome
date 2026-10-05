package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type pinnedReleaseServer struct {
	*httptest.Server
	artifactDownloads atomic.Int64
}

func newPinnedReleaseServer(t *testing.T, pin, archivePath, sha string) *pinnedReleaseServer {
	t.Helper()
	ps := &pinnedReleaseServer{}
	ps.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/tags/" + pin:
			fmt.Fprint(w, releaseJSON(r, pin, false, strings.Contains(pin, "-"), ArtifactObjectName(runtime.GOOS, runtime.GOARCH)))
		case "/assets/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", sha, ArtifactObjectName(runtime.GOOS, runtime.GOARCH))
		case "/assets/" + ArtifactObjectName(runtime.GOOS, runtime.GOARCH):
			ps.artifactDownloads.Add(1)
			http.ServeFile(w, r, archivePath)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ps.Server.Close)
	return ps
}

func newEnsureRepo(t *testing.T, pin string) (string, *obsidian.LocalConfig) {
	t.Helper()
	repo := t.TempDir()
	cfg := obsidian.LocalConfig{Rhizome: obsidian.LocalRhizomeConfig{Version: pin}}
	require.NoError(t, obsidian.SaveLocalConfig(repo, cfg))
	return repo, &cfg
}

func ensureTargetPath(repo string) string {
	return filepath.Join(repo, ".rhizome", "bin", CurrentPlatformDir(), executableName())
}

func buildFailingArchive(t *testing.T, dir string) string {
	t.Helper()
	srcDir := filepath.Join(dir, "failing-src")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	bin := filepath.Join(srcDir, "rzm")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755))
	archive := filepath.Join(dir, "failing-rzm.tar.gz")
	cmd := exec.Command("tar", "-czf", archive, "-C", srcDir, "rzm")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return archive
}

func TestEnsurePinnedBinaryInstallsBinaryAndMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script smoke binary fixture is POSIX-only")
	}
	tmp := t.TempDir()
	archive := buildFakeArchive(t, tmp, "v0.38.0")
	server := newPinnedReleaseServer(t, "v0.38.0", archive, sha256File(t, archive))
	repo, cfg := newEnsureRepo(t, "v0.38.0")

	err := EnsurePinnedBinary(context.Background(), EnsureOptions{
		CfgDir:      repo,
		Config:      cfg,
		ManifestURL: server.URL + "/releases/latest",
	})
	require.NoError(t, err)

	target := ensureTargetPath(repo)
	out, err := exec.Command(target, "--version").CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "v0.38.0")

	marker, err := os.ReadFile(filepath.Join(filepath.Dir(target), VersionMarkerName))
	require.NoError(t, err)
	require.Equal(t, "v0.38.0\n", string(marker))

	// The pin must never be rewritten by bootstrap.
	_, loaded, err := obsidian.FindLocalConfig(repo)
	require.NoError(t, err)
	require.Equal(t, "v0.38.0", loaded.Rhizome.Version)

	// Fast path: a matching binary + marker short-circuits without downloading.
	downloads := server.artifactDownloads.Load()
	require.NoError(t, EnsurePinnedBinary(context.Background(), EnsureOptions{
		CfgDir:      repo,
		Config:      cfg,
		ManifestURL: server.URL + "/releases/latest",
	}))
	require.Equal(t, downloads, server.artifactDownloads.Load())
}

func TestEnsurePinnedBinaryStageFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script smoke binary fixture is POSIX-only")
	}
	tmp := t.TempDir()
	goodArchive := buildFakeArchive(t, tmp, "v0.38.0")
	failingArchive := buildFailingArchive(t, tmp)

	cases := []struct {
		name        string
		stage       string
		manifestURL func(t *testing.T) string
	}{
		{
			name:  "manifest fetch failure",
			stage: StageManifest,
			manifestURL: func(t *testing.T) string {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.NotFound(w, r)
				}))
				t.Cleanup(server.Close)
				return server.URL + "/releases/latest"
			},
		},
		{
			name:  "checksum mismatch",
			stage: StageChecksum,
			manifestURL: func(t *testing.T) string {
				server := newPinnedReleaseServer(t, "v0.38.0", goodArchive, strings.Repeat("0", 64))
				return server.URL + "/releases/latest"
			},
		},
		{
			name:  "smoke test failure",
			stage: StageSmokeTest,
			manifestURL: func(t *testing.T) string {
				server := newPinnedReleaseServer(t, "v0.38.0", failingArchive, sha256File(t, failingArchive))
				return server.URL + "/releases/latest"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, cfg := newEnsureRepo(t, "v0.38.0")

			err := EnsurePinnedBinary(context.Background(), EnsureOptions{
				CfgDir:      repo,
				Config:      cfg,
				ManifestURL: tc.manifestURL(t),
			})

			var stageErr *StageError
			require.ErrorAs(t, err, &stageErr)
			require.Equal(t, tc.stage, stageErr.Stage)

			target := ensureTargetPath(repo)
			_, statErr := os.Stat(target)
			require.True(t, os.IsNotExist(statErr), "no partial binary may remain at %s", target)
			_, statErr = os.Stat(filepath.Join(filepath.Dir(target), VersionMarkerName))
			require.True(t, os.IsNotExist(statErr), "no marker may be written on failure")
		})
	}
}

func TestEnsurePinnedBinaryWaitsForLockAndSkipsWhenOtherProcessInstalled(t *testing.T) {
	repo, cfg := newEnsureRepo(t, "v0.38.0")
	lockPath := filepath.Join(repo, ".rhizome", "install.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)

	target := ensureTargetPath(repo)
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		_ = os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755)
		_ = os.WriteFile(filepath.Join(filepath.Dir(target), VersionMarkerName), []byte("v0.38.0\n"), 0o644)
		_ = release()
	}()

	// Unreachable manifest URL proves no install was attempted by this caller.
	err = EnsurePinnedBinary(context.Background(), EnsureOptions{
		CfgDir:      repo,
		Config:      cfg,
		ManifestURL: "http://127.0.0.1:1/latest.json",
		LockTimeout: 10 * time.Second,
		LockPoll:    25 * time.Millisecond,
	})
	require.NoError(t, err)
}

func TestEnsurePinnedBinaryFailsWhenLockStaysHeld(t *testing.T) {
	repo, cfg := newEnsureRepo(t, "v0.38.0")
	lockPath := filepath.Join(repo, ".rhizome", "install.lock")
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired)
	defer func() { _ = release() }()

	err = EnsurePinnedBinary(context.Background(), EnsureOptions{
		CfgDir:      repo,
		Config:      cfg,
		ManifestURL: "http://127.0.0.1:1/latest.json",
		LockTimeout: 150 * time.Millisecond,
		LockPoll:    20 * time.Millisecond,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rzm update --pinned")
}

func TestEnsurePinnedBinaryRequiresPin(t *testing.T) {
	repo, _ := newEnsureRepo(t, "")
	cfg := &obsidian.LocalConfig{}

	err := EnsurePinnedBinary(context.Background(), EnsureOptions{CfgDir: repo, Config: cfg})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rhizome.version")
}

func TestVersionMarkerRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binary := filepath.Join(dir, "rzm")

	_, ok := ReadVersionMarker(binary)
	require.False(t, ok)

	require.NoError(t, WriteVersionMarker(binary, "0.42.0"))
	version, ok := ReadVersionMarker(binary)
	require.True(t, ok)
	require.Equal(t, "v0.42.0", version)

	data, err := os.ReadFile(filepath.Join(dir, VersionMarkerName))
	require.NoError(t, err)
	require.Equal(t, "v0.42.0\n", string(data))
}
