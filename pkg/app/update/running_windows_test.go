package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestWindowsRunningRepositoryUpdatePreservesFilesAndMetadata(t *testing.T) {
	const oldVersion, newVersion = "v0.50.0", "v0.51.0"
	oldBinary := buildRunningBinary(t, oldVersion)
	newBinary := buildRunningBinary(t, newVersion)
	oldBytes, err := os.ReadFile(oldBinary)
	require.NoError(t, err)
	archive := runningBinaryArchive(t, newBinary)
	server := newPinnedReleaseServer(t, newVersion, archive, sha256File(t, archive))

	for _, ensure := range []bool{false, true} {
		name, pin := "Run", oldVersion
		if ensure {
			name, pin = "EnsurePinnedBinary", newVersion
		}
		t.Run(name, func(t *testing.T) {
			repo, cfg := newEnsureRepo(t, pin)
			repo, err := filepath.EvalSymlinks(repo)
			require.NoError(t, err)
			target := ensureTargetPath(repo)
			require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
			require.NoError(t, os.WriteFile(target, oldBytes, 0o700))
			require.NoError(t, WriteVersionMarker(target, oldVersion))
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			process := exec.CommandContext(ctx, target, "hold")
			stdin, err := process.StdinPipe()
			require.NoError(t, err)
			stdout, err := process.StdoutPipe()
			require.NoError(t, err)
			process.Stderr = os.Stderr
			require.NoError(t, process.Start())
			t.Cleanup(func() {
				_ = stdin.Close()
				if process.ProcessState == nil {
					_ = process.Process.Kill()
					_ = process.Wait()
				}
			})
			output := bufio.NewScanner(stdout)
			require.True(t, output.Scan(), "old executable did not become ready: %v", output.Err())
			require.Equal(t, "ready "+oldVersion, output.Text())

			var result Result
			update := func() error {
				if ensure {
					return EnsurePinnedBinary(ctx, EnsureOptions{CfgDir: repo, Config: cfg, ManifestURL: server.URL + "/releases/latest"})
				}
				var err error
				result, err = Run(ctx, Options{SetVersion: newVersion, ExecutablePath: target, WorkDir: repo, ManifestURL: server.URL + "/releases/latest"})
				return err
			}
			err = update()
			require.ErrorContains(t, err, "existing executable was left in place")
			require.ErrorContains(t, err, "rzm stop")
			require.ErrorContains(t, err, "Update Pin")
			require.ErrorContains(t, err, "global CLI")
			if !ensure {
				require.Equal(t, []TargetResult{{Role: UpdateTargetRepo, Path: target, Version: newVersion}}, result.Targets)
			}
			retained, err := os.ReadFile(target)
			require.NoError(t, err)
			require.True(t, bytes.Equal(oldBytes, retained), "failed publication must preserve the old executable bytes")
			marker, ok := ReadVersionMarker(target)
			require.True(t, ok)
			require.Equal(t, oldVersion, marker)
			_, currentConfig, err := repoconfig.FindLocalConfig(repo)
			require.NoError(t, err)
			require.Equal(t, pin, currentConfig.Rhizome.Version)
			staged, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".rzm-update-*"))
			require.NoError(t, err)
			require.Empty(t, staged)
			_, err = io.WriteString(stdin, "ping\n")
			require.NoError(t, err)
			require.True(t, output.Scan(), "old executable stopped after failed publication: %v", output.Err())
			require.Equal(t, "alive "+oldVersion, output.Text())
			version, err := exec.CommandContext(ctx, target, "--version").CombinedOutput()
			require.NoError(t, err, string(version))
			require.Equal(t, "rzm version "+oldVersion+"\n", string(version))

			require.NoError(t, stdin.Close())
			require.NoError(t, process.Wait())
			require.NoError(t, update(), "the same update must succeed after only the old fixture process exits")
			if !ensure {
				require.Equal(t, []TargetResult{{Role: UpdateTargetRepo, Path: target, Version: newVersion, BinaryUpdated: true, MarkerWritten: true, PinWritten: true}}, result.Targets)
			}
			marker, ok = ReadVersionMarker(target)
			require.True(t, ok)
			require.Equal(t, newVersion, marker)
			_, currentConfig, err = repoconfig.FindLocalConfig(repo)
			require.NoError(t, err)
			require.Equal(t, newVersion, currentConfig.Rhizome.Version)
			version, err = exec.CommandContext(ctx, target, "--version").CombinedOutput()
			require.NoError(t, err, string(version))
			require.Equal(t, "rzm version "+newVersion+"\n", string(version))
		})
	}
}

func buildRunningBinary(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rzm.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-mod=vendor", "-ldflags", "-X main.version="+version, "-o", path, "./testdata/running_binary")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	return path
}

func runningBinaryArchive(t *testing.T, binary string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rzm.tar.gz")
	file, err := os.Create(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	compressed := gzip.NewWriter(file)
	archive := tar.NewWriter(compressed)
	data, err := os.ReadFile(binary)
	require.NoError(t, err)
	require.NoError(t, archive.WriteHeader(&tar.Header{Name: "rzm.exe", Mode: 0o700, Size: int64(len(data))}))
	_, err = archive.Write(data)
	require.NoError(t, err)
	require.NoError(t, archive.Close())
	require.NoError(t, compressed.Close())
	require.NoError(t, file.Close())
	return path
}
