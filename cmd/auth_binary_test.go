//go:build integration

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

// The real binary, run inside an untrusted repository that pins another
// version and carries a broken dotenv file, answers `rzm auth status` itself.
// Run with: go test -mod=vendor -tags "fts5 integration" -run TestAuthBinary ./cmd
func TestAuthBinaryIgnoresRepositoryPinAndDotEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script pinned binary fixture is Unix-only")
	}
	binary := filepath.Join(t.TempDir(), "rzm")
	build := exec.Command("go", "build", "-mod=vendor", "-tags", "fts5", "-o", binary, "..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}

	repo := t.TempDir()
	require.NoError(t, repoconfig.SaveLocalConfig(repo, repoconfig.LocalConfig{Rhizome: repoconfig.LocalRhizomeConfig{Version: "v1.2.3"}}))
	sentinel := filepath.Join(t.TempDir(), "pinned-binary-ran")
	pinned := filepath.Join(repo, ".rhizome", "bin", repoPlatformDir(), repoExecutableName())
	require.NoError(t, os.MkdirAll(filepath.Dir(pinned), 0o755))
	require.NoError(t, os.WriteFile(pinned, []byte("#!/bin/sh\ntouch \""+sentinel+"\"\necho 'rzm version v1.2.3'\n"), 0o755))
	writeVersionMarkerFile(t, pinned, "v1.2.3")
	// Strict keys make this file fail to load, which main reports on stderr.
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".rhizome", ".env"), []byte("not-a-valid-key=1\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".env"), []byte("HTTPS_PROXY=http://127.0.0.1:9\n"), 0o600))

	run := func(args ...string) (string, string, error) {
		command := exec.Command(binary, args...)
		command.Dir = repo
		for _, value := range os.Environ() {
			name, _, _ := strings.Cut(value, "=")
			if name != repoSkipDelegateEnv && name != repoDelegatedEnv {
				command.Env = append(command.Env, value)
			}
		}
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		return stdout.String(), stderr.String(), err
	}

	// Control: other non-delegating commands still load the repository dotenv.
	_, stderr, _ := run("credentials", "--help")
	require.Contains(t, stderr, "failed to load .env")

	stdout, stderr, err := run("auth", "status", "--json", "--server", "http://127.0.0.1:1")
	require.NoError(t, err, stderr)
	require.NotContains(t, stderr, "failed to load .env")
	var status map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &status), stdout)
	require.Equal(t, map[string]any{"signed_in": false, "origin": "http://127.0.0.1:1"}, status)
	require.NoFileExists(t, sentinel, "auth never delegates to the pinned binary")
}
