package runtime

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func captureLogf(lines *[]string, mu *sync.Mutex) func(string, ...any) {
	return func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		*lines = append(*lines, fmt.Sprintf(format, args...))
	}
}

func TestEnsureAttachesToLiveRuntimeWithoutSpawning(t *testing.T) {
	vault := newTestVault(t)
	fake := startFakeRuntime(t, vault, ModeHeadless, "build-a")
	spawned := useHelperSpawn(t)

	var lines []string
	var mu sync.Mutex
	result, err := Ensure(t.Context(), EnsureOptions{
		VaultPath: vault, Executable: "unused", BuildID: "build-a",
		Autostart: true, Wait: true, Logf: captureLogf(&lines, &mu),
	})
	require.NoError(t, err)
	require.False(t, result.Spawned)
	require.Equal(t, fake.manifest.RunID, result.Health.RunID)
	require.Nil(t, spawned.Load(), "attach must not spawn")
	require.Contains(t, strings.Join(lines, "\n"), "attached to runtime pid")
}

func TestEnsureRefusesToSpawnWhenAutostartDisabled(t *testing.T) {
	vault := newTestVault(t)
	var stderr bytes.Buffer
	recorder, err := diagnostics.Open(vault, diagnostics.Options{Stderr: &stderr})
	require.NoError(t, err)
	_, err = Ensure(diagnostics.WithRecorder(t.Context(), recorder), EnsureOptions{VaultPath: vault, Wait: true})
	require.ErrorIs(t, err, ErrAutostartDisabled)
	require.NoError(t, recorder.Close())
	require.Empty(t, stderr.String(), "background ensure returns errors to its caller without terminal output")
	reports, err := diagnostics.ReadReports(vault, diagnostics.Filter{Kind: "runtime.ensure"})
	require.NoError(t, err)
	require.Len(t, reports.Reports, 1)
	require.Equal(t, "error", reports.Reports[0].Status)
	require.Equal(t, "autostart_disabled", reports.Reports[0].ReasonCode)
}

func TestEnsureWaitsForElectedOwnerBeforeManifestPublication(t *testing.T) {
	vault := newTestVault(t)
	release, acquired, err := indexlock.TryAcquireWithOptions(LockPath(vault), indexlock.AcquireOptions{ProcessLifetime: true})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })
	spawned := useHelperSpawn(t)
	done := make(chan struct {
		result EnsureResult
		err    error
	}, 1)
	go func() {
		result, err := Ensure(context.Background(), EnsureOptions{VaultPath: vault, Autostart: true, Wait: true, Budget: 3 * time.Second})
		done <- struct {
			result EnsureResult
			err    error
		}{result, err}
	}()
	select {
	case got := <-done:
		t.Fatalf("ensure returned before the owner published: %v", got.err)
	case <-time.After(100 * time.Millisecond):
	}
	require.Nil(t, spawned.Load(), "an elected owner must not cause a competing spawn")
	fake := startFakeRuntime(t, vault, ModeHeadless, "build-a")
	got := <-done
	require.NoError(t, got.err)
	require.False(t, got.result.Spawned)
	require.Equal(t, fake.manifest.RunID, got.result.Health.RunID)
	require.Nil(t, spawned.Load())
}

func TestEnsureWithAutostartDisabledWaitsForDrainingOwner(t *testing.T) {
	vault := newTestVault(t)
	release, acquired, err := indexlock.TryAcquireWithOptions(LockPath(vault), indexlock.AcquireOptions{ProcessLifetime: true})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })
	done := make(chan error, 1)
	go func() {
		_, err := Ensure(context.Background(), EnsureOptions{VaultPath: vault, Autostart: false, Wait: true, Budget: 3 * time.Second})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("ensure returned before the owner released runtime.lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, release())
	require.ErrorIs(t, <-done, ErrAutostartDisabled)
}

func TestEnsureWithoutWaitReturnsWhileOwnerStarts(t *testing.T) {
	vault := newTestVault(t)
	release, acquired, err := indexlock.TryAcquireWithOptions(LockPath(vault), indexlock.AcquireOptions{ProcessLifetime: true})
	require.NoError(t, err)
	require.True(t, acquired)
	t.Cleanup(func() { _ = release() })
	spawned := useHelperSpawn(t)

	result, err := Ensure(t.Context(), EnsureOptions{VaultPath: vault, Autostart: true, Wait: false})

	require.NoError(t, err)
	require.Nil(t, result.Client)
	require.False(t, result.Spawned)
	require.Nil(t, spawned.Load())
}

func TestEnsureSpawnsHeadlessRuntimeAndAttaches(t *testing.T) {
	vault := newTestVault(t)
	seen := useHelperSpawn(t)
	stopSpawnedRuntime(t, vault)

	var lines []string
	var mu sync.Mutex
	result, err := Ensure(t.Context(), EnsureOptions{
		VaultPath: vault, Executable: "/path/to/rzm", Autostart: true, Wait: true,
		Budget: 30 * time.Second, Logf: captureLogf(&lines, &mu),
	})
	require.NoError(t, err)
	require.True(t, result.Spawned)
	require.NotNil(t, result.Client)
	require.True(t, result.Health.Ready)
	require.NotEqual(t, os.Getpid(), result.Health.PID, "the runtime is a separate process")
	require.Equal(t, "/path/to/rzm", seen.Load(), "spawn uses the caller's executable")
	require.Contains(t, strings.Join(lines, "\n"), "starting vault runtime")

	// The lease is a short-lived lease, not a leftover.
	release, acquired, err := indexlock.TryAcquire(SpawnLockPath(vault))
	require.NoError(t, err)
	require.True(t, acquired, "the spawn lease must be released once the runtime is ready")
	require.NoError(t, release())
}

func TestEnsureWithoutWaitReturnsBeforeReadiness(t *testing.T) {
	vault := newTestVault(t)
	useHelperSpawn(t)
	stopSpawnedRuntime(t, vault)

	result, err := Ensure(t.Context(), EnsureOptions{
		VaultPath: vault, Executable: testBinary(t), Autostart: true, Wait: false, Budget: 30 * time.Second,
	})
	require.NoError(t, err)
	require.True(t, result.Spawned)
	require.Nil(t, result.Client, "a non-waiting ensure hands back no client")

	require.Eventually(t, func() bool {
		_, _, err := LiveManifest(t.Context(), vault)
		return err == nil
	}, 30*time.Second, 50*time.Millisecond, "the spawn still has to produce a runtime")
	// The background readiness waiter must release its lease before cleanup
	// stops the helper, or it can miss readiness and outlive the temporary vault.
	require.Eventually(t, func() bool {
		_, err := os.Stat(SpawnLockPath(vault))
		return os.IsNotExist(err)
	}, 30*time.Second, 50*time.Millisecond, "the background spawn lease must finish before cleanup")
}

func TestEnsureReplacesUnusableManifests(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, vault string)
	}{
		{"dead pid", func(t *testing.T, vault string) { writeDeadManifest(t, vault) }},
		{"nothing listening", func(t *testing.T, vault string) {
			fake := startFakeRuntime(t, vault, ModeHeadless, "build-a")
			fake.server.Close() // manifest stays behind, address refuses
		}},
		{"different process answering", func(t *testing.T, vault string) {
			fake := startFakeRuntime(t, vault, ModeHeadless, "build-a")
			fake.breakRunID = true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault := newTestVault(t)
			tc.setup(t, vault)
			useHelperSpawn(t)
			stopSpawnedRuntime(t, vault)

			result, err := Ensure(t.Context(), EnsureOptions{
				VaultPath: vault, Executable: testBinary(t), Autostart: true, Wait: true,
				Budget: 30 * time.Second,
			})
			require.NoError(t, err)
			require.True(t, result.Spawned, "an unusable manifest must be replaced")
			require.NotEqual(t, os.Getpid(), result.Health.PID)
		})
	}
}

func TestEnsureWaitsForAnUnresponsiveRuntimeInsteadOfSpawning(t *testing.T) {
	vault := newTestVault(t)
	// A live PID whose address accepts but never answers health correctly.
	fake := startFakeRuntime(t, vault, ModeHeadless, "build-a")
	fake.unhealthy.Store(true)
	spawned := useHelperSpawn(t)

	_, err := Ensure(t.Context(), EnsureOptions{
		VaultPath: vault, Executable: testBinary(t), Autostart: true, Wait: true,
		Budget: 600 * time.Millisecond,
	})
	require.ErrorIs(t, err, ErrRuntimeUnresponsive)
	require.Nil(t, spawned.Load(), "a live but unresponsive runtime is never replaced by a spawn")
}

func TestEnsureRefusesAnAttachedBuildMismatch(t *testing.T) {
	vault := newTestVault(t)
	fake := startFakeRuntime(t, vault, ModeAttached, "old-build")
	spawned := useHelperSpawn(t)

	_, err := Ensure(t.Context(), EnsureOptions{
		VaultPath: vault, Executable: testBinary(t), BuildID: "new-build",
		Autostart: true, Wait: true, Budget: 5 * time.Second,
	})
	require.ErrorIs(t, err, ErrAttachedMismatch)
	require.Contains(t, err.Error(), fmt.Sprintf("pid %d", fake.manifest.PID))
	require.Nil(t, spawned.Load(), "an attached runtime is never replaced")
}

func TestEnsureReplacesAHeadlessBuildMismatch(t *testing.T) {
	vault := newTestVault(t)
	startFakeRuntime(t, vault, ModeHeadless, "old-build")
	useHelperSpawn(t)
	stopSpawnedRuntime(t, vault)

	var lines []string
	var mu sync.Mutex
	result, err := Ensure(t.Context(), EnsureOptions{
		VaultPath: vault, Executable: testBinary(t), BuildID: "new-build",
		Autostart: true, Wait: true, Budget: 30 * time.Second, Logf: captureLogf(&lines, &mu),
	})
	require.NoError(t, err)
	require.True(t, result.Spawned)
	require.NotEqual(t, os.Getpid(), result.Health.PID)
	require.Contains(t, strings.Join(lines, "\n"), "replacing headless runtime")
}

func TestEnsureReplaceRestartsAMatchingHeadlessRuntimeOnce(t *testing.T) {
	vault := newTestVault(t)
	fake := startFakeRuntime(t, vault, ModeHeadless, "same-build")
	useHelperSpawn(t)
	t.Setenv("RZM_TEST_HELPER_BUILD", "same-build")
	stopSpawnedRuntime(t, vault)

	result, err := Ensure(t.Context(), EnsureOptions{
		VaultPath: vault, Executable: testBinary(t), BuildID: "same-build", Replace: true,
		Autostart: true, Wait: true, Budget: 30 * time.Second,
	})
	require.NoError(t, err)
	require.True(t, result.Spawned)
	require.NotEqual(t, fake.manifest.PID, result.Health.PID)
}

func TestCanceledReplacementKeepsTheLiveHeadlessOwner(t *testing.T) {
	vault := newTestVault(t)
	useHelperSpawn(t)
	t.Setenv("RZM_TEST_HELPER_BUILD", "old-build")
	stopSpawnedRuntime(t, vault)
	result, err := Ensure(t.Context(), EnsureOptions{
		VaultPath: vault, Executable: testBinary(t), Autostart: true,
		Wait: true, Budget: 30 * time.Second,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Client)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = replaceHeadless(ctx, result.Client, result.Health, EnsureOptions{
		VaultPath: vault, BuildID: "new-build",
	}, func(string, ...any) {})
	require.ErrorIs(t, err, context.Canceled)
	_, stillLive, err := LiveManifest(context.Background(), vault)
	require.NoError(t, err)
	require.Equal(t, result.Health.PID, stillLive.PID)
	require.True(t, indexlock.PIDExists(stillLive.PID))
}

func TestEnsureWarnsOnBuildMismatchPingPong(t *testing.T) {
	vault := newTestVault(t)
	replace := func() []string {
		startFakeRuntime(t, vault, ModeHeadless, "old-build")
		var lines []string
		var mu sync.Mutex
		_, err := Ensure(t.Context(), EnsureOptions{
			VaultPath: vault, Executable: testBinary(t), BuildID: "new-build",
			Autostart: false, Wait: true, Budget: 5 * time.Second, Logf: captureLogf(&lines, &mu),
		})
		// Auto-start is off, so the replacement is all that happens.
		require.ErrorIs(t, err, ErrAutostartDisabled)
		// The fake stops asynchronously and removes the manifest by PID, which
		// every fake in this process shares; let it finish before the next
		// fake publishes, or it deletes that manifest mid-write (seen on CI).
		require.Eventually(t, func() bool {
			_, readErr := ReadManifest(vault)
			return os.IsNotExist(readErr)
		}, 5*time.Second, 5*time.Millisecond)
		return lines
	}
	require.NotContains(t, strings.Join(replace(), "\n"), "ping-pong")
	require.Contains(t, strings.Join(replace(), "\n"), "build-mismatch ping-pong")
}

func TestEnsureIndexJobRetriesOnceWhenTheRuntimeGoesAwayAfterTheProbe(t *testing.T) {
	vault := newTestVault(t)
	fake := startFakeRuntime(t, vault, ModeHeadless, "build-a")
	fake.hijackPosts = 1

	var lines []string
	var mu sync.Mutex
	client, job, err := EnsureIndexJob(t.Context(), EnsureOptions{
		VaultPath: vault, Executable: testBinary(t), BuildID: "build-a",
		Autostart: false, Wait: true, Budget: 5 * time.Second, Logf: captureLogf(&lines, &mu),
	})
	require.NoError(t, err)
	require.NotNil(t, client)
	require.Equal(t, "job-1", job.JobID)
	require.EqualValues(t, 2, fake.posts.Load(), "exactly one retry")
	require.Contains(t, strings.Join(lines, "\n"), "retrying once")
}

func TestEnsureIndexJobGivesUpAfterASecondFailure(t *testing.T) {
	vault := newTestVault(t)
	fake := startFakeRuntime(t, vault, ModeHeadless, "build-a")
	// Health stays live, so each Ensure attaches; both job POSTs then fail.
	fake.hijackPosts = 2

	var lines []string
	var mu sync.Mutex
	_, _, err := EnsureIndexJob(t.Context(), EnsureOptions{
		VaultPath: vault, Executable: testBinary(t), BuildID: "build-a",
		Autostart: false, Wait: true, Budget: 5 * time.Second, Logf: captureLogf(&lines, &mu),
	})
	require.EqualValues(t, 2, fake.posts.Load(), "exactly one retry, then give up")
	var transportErr *url.Error
	require.ErrorAs(t, err, &transportErr, "the second transport failure is returned")
	require.Contains(t, strings.Join(lines, "\n"), "retrying once")
}

// TestEnsureConcurrentProcessesProduceOneRuntime is the race-safety proof for
// SPEC-0104 US1: eight separate processes call Ensure on a vault with no
// runtime and all of them must end up attached to the same one.
func TestEnsureConcurrentProcessesProduceOneRuntime(t *testing.T) {
	vault := newTestVault(t)
	stopSpawnedRuntime(t, vault)
	exe := testBinary(t)

	const clients = 8
	outputs := make([]*bytes.Buffer, clients)
	cmds := make([]*exec.Cmd, clients)
	release := make(chan struct{})
	var wg sync.WaitGroup
	for i := range clients {
		outputs[i] = &bytes.Buffer{}
		cmd := exec.Command(exe, "-test.run=^TestHelperEnsureClient$")
		cmd.Env = append(os.Environ(),
			helperEnsureEnv+"=1", helperRuntimeEnv+"=1",
			helperVaultEnv+"="+vault, helperExeEnv+"="+exe,
			skipRepoDelegateEnv+"=1",
		)
		cmd.Stdout = outputs[i]
		cmd.Stderr = outputs[i]
		cmds[i] = cmd
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-release
			_ = cmd.Run()
		}()
	}
	close(release)
	wg.Wait()

	identities := map[string]int{}
	for i, cmd := range cmds {
		require.Zero(t, cmd.ProcessState.ExitCode(), "client %d output:\n%s", i, outputs[i])
		identity, ok := ensureIdentity(outputs[i].String())
		require.True(t, ok, "client %d printed no identity:\n%s", i, outputs[i])
		identities[identity]++
	}
	require.Len(t, identities, 1, "every client must attach to the same runtime, got %v", identities)
	for identity, count := range identities {
		require.Equal(t, clients, count, "identity %s", identity)
	}
}

func ensureIdentity(output string) (string, bool) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		if line, ok := strings.CutPrefix(scanner.Text(), "ENSURE "); ok {
			return strings.TrimSpace(line), true
		}
	}
	return "", false
}

func TestEnsureStopsWhenTheCallerCancels(t *testing.T) {
	vault := newTestVault(t)
	fake := startFakeRuntime(t, vault, ModeHeadless, "build-a")
	fake.unhealthy.Store(true)
	spawned := useHelperSpawn(t)

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	_, err := Ensure(ctx, EnsureOptions{
		VaultPath: vault, Executable: testBinary(t), Autostart: true, Wait: true,
		Budget: 30 * time.Second,
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(started), 5*time.Second, "a cancelled caller must not wait out the budget")
	require.Nil(t, spawned.Load())
}
