package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/atomicobject/rhizome/pkg/logging"
)

// skipRepoDelegateEnv keeps the spawned runtime from re-delegating to a
// repo-pinned binary; the client already resolved which executable to run.
// It mirrors repoSkipDelegateEnv in cmd/repo_delegate.go.
const skipRepoDelegateEnv = "RZM_SKIP_REPO_DELEGATE"

// spawnArgv builds the child's argv. Production always runs
// `<executable> serve --headless --vault <vault root>`; the vault root is
// path-shaped, which `--vault` resolves through the repo-local config.
// Tests replace this to re-execute the test binary as a fake runtime.
var spawnArgv = func(executable, vaultRoot string) []string {
	return []string{executable, "serve", "--headless", "--vault", vaultRoot}
}

const SpawnTokenEnv = "RZM_RUNTIME_SPAWN_TOKEN"

// spawnHeadlessWithToken starts a detached headless runtime and returns its
// PID. The child is fully detached (new session or process group, no terminal,
// stdio redirected) so it outlives the client, and the client never waits on
// its process handle. A nonempty token is passed to the child for startup
// ownership.
func spawnHeadlessWithToken(executable, vaultRoot, token string) (int, error) {
	argv := spawnArgv(executable, vaultRoot)
	if len(argv) == 0 {
		return 0, fmt.Errorf("empty runtime spawn argv")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = vaultRoot
	cmd.Env = append(os.Environ(), skipRepoDelegateEnv+"=1")
	if token != "" {
		cmd.Env = append(cmd.Env, SpawnTokenEnv+"="+token)
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	defer devNull.Close()
	cmd.Stdin = devNull
	cmd.Stdout, cmd.Stderr = devNull, devNull

	// The child captures its own output into diagnostics/runtime-output.log once it owns the
	// vault. Handing it the log as inherited stdio only covers the pre-election
	// window, and on Windows an inherited handle without FILE_SHARE_DELETE
	// blocks the child's log rotation for its whole life.
	if runtime.GOOS != "windows" {
		if logFile, logErr := openRuntimeLog(vaultRoot); logErr == nil {
			defer logFile.Close()
			cmd.Stdout, cmd.Stderr = logFile, logFile
		}
	}

	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	// Release the handle: the runtime's lifetime is its own from here.
	_ = cmd.Process.Release()
	return pid, nil
}

func openRuntimeLog(vaultRoot string) (*os.File, error) {
	path := LogPath(vaultRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// Preserve native startup/crash descriptors, but repair both retained files
	// under the spawn lease before another child can append to them.
	for _, target := range []string{path, path + ".1"} {
		if err := logging.TrimOutputTail(target, 5<<20); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
}
