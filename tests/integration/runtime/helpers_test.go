//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

const commandTimeout = 3 * time.Minute

var (
	binaryOnce sync.Once
	binaryPath string
	binaryErr  error
	binaryDir  string
)

func TestMain(m *testing.M) {
	code := m.Run()
	if binaryDir != "" {
		_ = os.RemoveAll(binaryDir)
	}
	os.Exit(code)
}

// rzmBinary builds the real binary once per package run.
func rzmBinary(t *testing.T) string {
	t.Helper()
	binaryOnce.Do(func() {
		binaryDir, binaryErr = os.MkdirTemp("", "rzm-runtime-integration-")
		if binaryErr != nil {
			return
		}
		name := "rzm"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		binaryPath = filepath.Join(binaryDir, name)
		build := exec.Command("go", "build", "-mod=vendor", "-tags", "fts5", "-o", binaryPath, ".")
		build.Dir = fixture.RepoRoot(t)
		if output, err := build.CombinedOutput(); err != nil {
			binaryErr = fmt.Errorf("go build: %w: %s", err, output)
		}
	})
	require.NoError(t, binaryErr)
	return binaryPath
}

type runtimeVault struct {
	t           *testing.T
	root        string
	home        string
	binary      string
	spawnedMu   sync.Mutex
	spawnedPIDs map[int]struct{}
}

type commandResult struct {
	stdout string
	stderr string
	err    error
}

func newRuntimeVault(t *testing.T, extraConfig string) *runtimeVault {
	t.Helper()
	return newRuntimeVaultIn(t, t.TempDir(), extraConfig)
}

// newRuntimeVaultIn creates a small notes-plus-code vault whose rzm processes
// see home as the user's home, which isolates the global instance registry.
func newRuntimeVaultIn(t *testing.T, home, extraConfig string) *runtimeVault {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vault")
	files := map[string]string{
		".rhizome/config.yml": "notes:\n  includes:\n    - \"**/*.md\"\n  links: wikilinks\ncode:\n  python:\n    roots:\n      - src\n" + extraConfig,
		"notes/guide.md":      "# Fixture guide\n\nThe guide explains the example module.\n",
		"notes/design.md":     "# Design\n\nSee [[guide]] for the walkthrough.\n",
		"src/example.py":      "\"\"\"Example module.\"\"\"\n\ndef greet(name):\n    return f\"hello {name}\"\n\n# Docs: [[notes/guide]]\n",
	}
	for rel, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	}
	vault := &runtimeVault{t: t, root: root, home: home, binary: rzmBinary(t), spawnedPIDs: make(map[int]struct{})}
	// Runtimes are detached; stop this vault's before its directory is removed.
	t.Cleanup(vault.stopRuntime)
	return vault
}

func (v *runtimeVault) environment(extra map[string]string) []string {
	drop := map[string]bool{
		"RZM_REPO_DELEGATED": true, "RZM_SKIP_REPO_DELEGATE": true, appruntime.AutostartEnv: true,
		"HOME": true, "USERPROFILE": true,
	}
	env := make([]string, 0, len(os.Environ())+len(extra)+3)
	for _, value := range os.Environ() {
		if key, _, ok := strings.Cut(value, "="); ok && !drop[key] {
			env = append(env, value)
		}
	}
	env = append(env, "RZM_SKIP_REPO_DELEGATE=1", "HOME="+v.home, "USERPROFILE="+v.home)
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	return env
}

func (v *runtimeVault) run(extra map[string]string, args ...string) commandResult {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, v.binary, args...)
	command.Dir = v.root
	command.Env = v.environment(extra)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	result := commandResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
	v.rememberSpawnedRuntimePIDs(result.stderr)
	return result
}

// start launches a long-running command and kills it with the test.
func (v *runtimeVault) start(t *testing.T, args ...string) *startedProcess {
	t.Helper()
	command := exec.Command(v.binary, args...)
	command.Dir = v.root
	command.Env = v.environment(nil)
	require.NoError(t, command.Start())
	process := &startedProcess{pid: command.Process.Pid, exited: make(chan struct{})}
	// Reap the child as a shell would, so its PID disappears when it exits.
	go func() {
		_ = command.Wait()
		close(process.exited)
	}()
	t.Cleanup(func() {
		select {
		case <-process.exited:
		default:
			_ = command.Process.Kill()
			<-process.exited
		}
	})
	return process
}

type startedProcess struct {
	pid    int
	exited chan struct{}
}

func (v *runtimeVault) requireLiveRuntime(t *testing.T) appruntime.Health {
	t.Helper()
	var health appruntime.Health
	require.Eventually(t, func() bool {
		_, current, err := appruntime.LiveManifest(context.Background(), v.root)
		health = current
		return err == nil
	}, 30*time.Second, 200*time.Millisecond, "a live runtime for %s", v.root)
	return health
}

func (v *runtimeVault) stopRuntime() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for {
		client, health, err := appruntime.LiveManifest(ctx, v.root)
		if err == nil && client != nil {
			v.rememberRuntimePID(health.PID)
			if err := appruntime.RequestShutdown(ctx, client, 5*time.Second); err != nil && health.Mode == appruntime.ModeHeadless {
				_ = appruntime.TerminateHeadless(v.root, client.Manifest)
			}
		}

		// A detached child can be alive before it publishes runtime.json, or a
		// replacement can start after the previously discovered owner exits.
		// Every child launched by this fixture is ours to stop at cleanup.
		if owner, live := appruntime.LockOwner(v.root); live {
			v.rememberRuntimePID(owner)
		}
		active := v.activeRuntimePIDs()
		if len(active) == 0 {
			return
		}
		if client == nil {
			for _, pid := range active {
				v.killRuntimePID(pid)
			}
		}
		if ctx.Err() != nil {
			v.t.Errorf("runtime fixture cleanup timed out; pids still alive: %v", active)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (v *runtimeVault) rememberSpawnedRuntimePIDs(stderr string) {
	const prefix = "started vault runtime pid "
	for _, line := range strings.Split(stderr, "\n") {
		position := strings.Index(line, prefix)
		if position < 0 {
			continue
		}
		fields := strings.Fields(line[position+len(prefix):])
		if len(fields) == 0 {
			continue
		}
		if pid, err := strconv.Atoi(fields[0]); err == nil && pid > 0 {
			v.rememberRuntimePID(pid)
		}
	}
}

func (v *runtimeVault) rememberRuntimePID(pid int) {
	if pid <= 0 {
		return
	}
	v.spawnedMu.Lock()
	v.spawnedPIDs[pid] = struct{}{}
	v.spawnedMu.Unlock()
}

func (v *runtimeVault) activeRuntimePIDs() []int {
	v.spawnedMu.Lock()
	defer v.spawnedMu.Unlock()
	active := make([]int, 0, len(v.spawnedPIDs))
	for pid := range v.spawnedPIDs {
		if indexlock.PIDExists(pid) {
			active = append(active, pid)
		} else {
			delete(v.spawnedPIDs, pid)
		}
	}
	return active
}

func (v *runtimeVault) killRuntimePID(pid int) {
	process, err := os.FindProcess(pid)
	if err == nil {
		_ = process.Kill()
	}
}

func requirePIDExits(t *testing.T, pid int, within time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool { return !indexlock.PIDExists(pid) }, within, 100*time.Millisecond, "pid %d should exit", pid)
}

func itoa(n int) string { return strconv.Itoa(n) }
