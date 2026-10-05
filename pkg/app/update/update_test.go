package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunPinnedRepoUpdateInstallsPinnedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script smoke binary fixture is POSIX-only")
	}
	tmp := t.TempDir()
	archive := buildFakeArchive(t, tmp, "v0.38.0")
	sum := sha256File(t, archive)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			fmt.Fprint(w, releaseJSON(r, "v0.39.0", false, false, ArtifactObjectName(runtime.GOOS, runtime.GOARCH)))
		case "/releases/tags/v0.38.0":
			fmt.Fprint(w, releaseJSON(r, "v0.38.0", false, false, ArtifactObjectName(runtime.GOOS, runtime.GOARCH)))
		case "/assets/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", sum, ArtifactObjectName(runtime.GOOS, runtime.GOARCH))
		case "/assets/" + ArtifactObjectName(runtime.GOOS, runtime.GOARCH):
			http.ServeFile(w, r, archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	repo := filepath.Join(tmp, "repo")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{
			Version: "v0.38.0",
		},
	}))

	result, err := Run(context.Background(), Options{
		ManifestURL: server.URL + "/releases/latest",
		Pinned:      true,
		WorkDir:     repo,
	})

	require.NoError(t, err)
	require.Len(t, result.Targets, 1)
	require.Equal(t, UpdateTargetRepo, result.Targets[0].Role)
	require.Equal(t, "v0.38.0", result.Targets[0].Version)
	require.True(t, result.Targets[0].BinaryUpdated)
	installed := filepath.Join(repo, ".rhizome", "bin", CurrentPlatformDir(), executableName())
	out, err := exec.Command(installed, "--version").CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "v0.38.0")

	marker, err := os.ReadFile(filepath.Join(filepath.Dir(installed), VersionMarkerName))
	require.NoError(t, err)
	require.Equal(t, "v0.38.0\n", string(marker))
}

func TestRunRefusesEveryUpdateModeWhenBinaryIsExternallyManaged(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{BinaryManager: obsidian.BinaryManagerExternal},
	}))

	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, fmt.Errorf("unexpected manifest request")
	})}
	for _, tt := range []struct {
		name string
		opts Options
	}{
		{name: "default"},
		{name: "latest", opts: Options{Latest: true}},
		{name: "pinned", opts: Options{Pinned: true}},
		{name: "set version", opts: Options{SetVersion: "v1.2.3"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts
			opts.WorkDir = repo
			opts.HTTPClient = client
			_, err := Run(context.Background(), opts)
			require.ErrorContains(t, err, "externally managed")
			require.ErrorContains(t, err, "external binary manager")
		})
	}
	require.Zero(t, requests)
}

func TestRunValidatesUpdateModeBeforeExternalOwnership(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, obsidian.SaveLocalConfig(repo, obsidian.LocalConfig{
		Rhizome: obsidian.LocalRhizomeConfig{BinaryManager: obsidian.BinaryManagerExternal},
	}))

	_, err := Run(context.Background(), Options{WorkDir: repo, Pinned: true, Latest: true})

	require.ErrorContains(t, err, "mutually exclusive")
}

func TestSmokeTestDisablesRepoDelegation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script smoke binary fixture is POSIX-only")
	}
	t.Setenv("RZM_SKIP_REPO_DELEGATE", "0")
	bin := filepath.Join(t.TempDir(), "rzm")
	require.NoError(t, os.WriteFile(bin, []byte(`#!/bin/sh
if [ "${RZM_SKIP_REPO_DELEGATE:-}" != "1" ]; then
  exit 42
fi
if [ "$(pwd -P)" != "$(cd "$(dirname "$0")" && pwd -P)" ]; then
  exit 43
fi
echo rhizome v0.50.0
`), 0o755))

	require.NoError(t, smokeTest(context.Background(), bin))
}

func buildFakeArchive(t *testing.T, dir, version string) string {
	t.Helper()
	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	bin := filepath.Join(srcDir, "rzm")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\necho rhizome "+version+"\n"), 0o755))
	archive := filepath.Join(dir, "rzm.tar.gz")
	cmd := exec.Command("tar", "-czf", archive, "-C", srcDir, "rzm")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return archive
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func serverURL(r *http.Request, path string) string {
	return "http://" + r.Host + path
}
