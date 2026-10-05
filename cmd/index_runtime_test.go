package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// fakeRuntimeServer implements the three control routes `rzm index` uses, with
// a scripted event stream.
type fakeRuntimeServer struct {
	server   *httptest.Server
	vault    string
	manifest appruntime.InstanceManifest
	events   []lane.Event
	joined   bool
	mode     appruntime.Mode

	submits   atomic.Int64
	cancelled atomic.Bool
	stopped   atomic.Bool
	// holdStream blocks the event stream until the job is cancelled, so a test
	// can interrupt a job that is still running. Set before the first request
	// and never reassigned.
	holdStream      chan struct{}
	releaseOnce     sync.Once
	stopAfterStream bool
}

func startFakeRuntimeServer(t *testing.T, vault string, events []lane.Event) *fakeRuntimeServer {
	t.Helper()
	fake := &fakeRuntimeServer{vault: vault, events: events, mode: appruntime.ModeHeadless}
	mux := http.NewServeMux()
	mux.HandleFunc(appruntime.HealthPath, func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, appruntime.Health{
			InstanceID: "fake", RunID: "run", PID: os.Getpid(), VaultPath: vault,
			Mode: fake.mode, Ready: true, Version: "test",
			Lane: appruntime.LaneState{Busy: true, JobID: "job-1", JobKind: "explicit-index", Queued: 1},
		})
	})
	mux.HandleFunc(appruntime.IndexJobsPath, func(w http.ResponseWriter, r *http.Request) {
		fake.submits.Add(1)
		w.WriteHeader(http.StatusAccepted)
		writeTestJSON(w, appruntime.IndexJobResponse{JobID: "job-1", Joined: fake.joined})
	})
	mux.HandleFunc(appruntime.IndexJobsPathPrefix, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			fake.cancelled.Store(true)
			if fake.holdStream != nil {
				fake.releaseOnce.Do(func() { close(fake.holdStream) })
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}
		fake.writeEvents(w, r)
		if fake.stopAfterStream {
			go fake.stop()
		}
	})
	mux.HandleFunc(appruntime.ShutdownPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		go fake.stop()
	})
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.stop)

	fake.manifest = appruntime.InstanceManifest{
		InstanceID: "fake", VaultPath: vault, PID: os.Getpid(), RunID: "run",
		Mode: fake.mode, ControlToken: "token", HTTPURL: fake.server.URL,
		StartedAt: time.Now(), Ready: true,
	}
	require.NoError(t, appruntime.WriteManifest(vault, fake.manifest))
	return fake
}

func (f *fakeRuntimeServer) writeEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher := w.(http.Flusher)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	for _, event := range f.events {
		if f.holdStream != nil && event.Type == lane.EventDone {
			select {
			case <-f.holdStream:
			case <-r.Context().Done():
				return
			}
		}
		data, err := json.Marshal(event)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
		flusher.Flush()
	}
}

func (f *fakeRuntimeServer) stop() {
	if f.stopped.Swap(true) {
		return
	}
	f.server.Close()
	_ = appruntime.RemoveManifestIfOwner(f.vault, f.manifest.PID)
}

func writeTestJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func newRuntimeTestVault(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	return root
}

func newIndexTestCommand(t *testing.T, out *bytes.Buffer) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(out)
	cmd.SetErr(out)
	return cmd
}

// resetIndexFlags isolates one test from the package-level index flags.
func resetIndexFlags(t *testing.T) {
	t.Helper()
	previous := [6]bool{indexRebuild, indexStatus, indexTimings, indexInProcess, indexVacuum, debug}
	previousExplain := indexExplainPath
	t.Cleanup(func() {
		indexRebuild, indexStatus, indexTimings = previous[0], previous[1], previous[2]
		indexInProcess, indexVacuum, debug = previous[3], previous[4], previous[5]
		indexExplainPath = previousExplain
	})
	indexRebuild, indexStatus, indexTimings, indexInProcess, indexVacuum = false, false, false, false, false
	indexExplainPath = ""
}

func doneEvent(outcome, message string) lane.Event {
	return lane.Event{Type: lane.EventDone, JobID: "job-1", Outcome: outcome, Error: message, OK: outcome == lane.OutcomeOK}
}

func TestRunIndexThroughRuntimeRendersProgressAndSucceeds(t *testing.T) {
	resetIndexFlags(t)
	vault := newRuntimeTestVault(t)
	startFakeRuntimeServer(t, vault, []lane.Event{
		{Type: lane.EventProgress, Label: "Indexing notes", Done: 2, Total: 4},
		{Type: lane.EventLog, Line: "indexed 4 notes"},
		doneEvent(lane.OutcomeOK, ""),
	})

	out := &bytes.Buffer{}
	handled, _, err := runIndexThroughRuntime(newIndexTestCommand(t, out), obsidian.VaultDefinition{Path: vault})
	require.True(t, handled)
	require.NoError(t, err)
	require.Contains(t, out.String(), "indexed 4 notes")
	require.NoFileExists(t, obsidian.IndexLockPath(vault), "a delegated index takes no index lock")
}

func TestRunIndexThroughRuntimeReportsAFailedJob(t *testing.T) {
	resetIndexFlags(t)
	vault := newRuntimeTestVault(t)
	startFakeRuntimeServer(t, vault, []lane.Event{doneEvent(lane.OutcomeFailed, "code index blew up")})

	out := &bytes.Buffer{}
	handled, _, err := runIndexThroughRuntime(newIndexTestCommand(t, out), obsidian.VaultDefinition{Path: vault})
	require.True(t, handled)
	require.ErrorContains(t, err, "code index blew up")
}

func TestRunIndexThroughRuntimeAnnouncesAJoinedJob(t *testing.T) {
	resetIndexFlags(t)
	vault := newRuntimeTestVault(t)
	fake := startFakeRuntimeServer(t, vault, []lane.Event{doneEvent(lane.OutcomeOK, "")})
	fake.joined = true

	out := &bytes.Buffer{}
	handled, _, err := runIndexThroughRuntime(newIndexTestCommand(t, out), obsidian.VaultDefinition{Path: vault})
	require.True(t, handled)
	require.NoError(t, err)
	require.Contains(t, out.String(), "joining index already running (job job-1)")
}

func TestRunIndexThroughRuntimeCancelsTheJobOnInterrupt(t *testing.T) {
	resetIndexFlags(t)
	vault := newRuntimeTestVault(t)
	fake := startFakeRuntimeServer(t, vault, []lane.Event{
		{Type: lane.EventProgress, Label: "Indexing", Done: 1, Total: 10},
		doneEvent(lane.OutcomeCancelled, "context canceled"),
	})
	fake.holdStream = make(chan struct{})

	out := &bytes.Buffer{}
	cmd := newIndexTestCommand(t, out)
	ctx, cancel := context.WithCancel(context.Background())
	cmd.SetContext(ctx)
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	handled, _, err := runIndexThroughRuntime(cmd, obsidian.VaultDefinition{Path: vault})
	require.True(t, handled)
	var exit silentExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 130, exit.ExitCode())
	require.True(t, fake.cancelled.Load(), "interrupting the CLI cancels the job in the runtime")
}

func TestRunIndexThroughRuntimeFallsBackWhenAutostartIsDisabled(t *testing.T) {
	resetIndexFlags(t)
	vault := newRuntimeTestVault(t)
	t.Setenv(appruntime.AutostartEnv, "0")

	out := &bytes.Buffer{}
	handled, _, err := runIndexThroughRuntime(newIndexTestCommand(t, out), obsidian.VaultDefinition{Path: vault})
	require.False(t, handled)
	require.NoError(t, err)
	require.Contains(t, out.String(), "Indexing in this process:")
	require.Contains(t, out.String(), "auto-start is disabled")
}

func TestRunIndexThroughRuntimeAttachesEvenWhenAutostartIsDisabled(t *testing.T) {
	resetIndexFlags(t)
	vault := newRuntimeTestVault(t)
	startFakeRuntimeServer(t, vault, []lane.Event{doneEvent(lane.OutcomeOK, "")})
	t.Setenv(appruntime.AutostartEnv, "0")

	out := &bytes.Buffer{}
	handled, _, err := runIndexThroughRuntime(newIndexTestCommand(t, out), obsidian.VaultDefinition{Path: vault})
	require.True(t, handled, "auto-start off forbids starting a runtime, not using one")
	require.NoError(t, err)
}

func TestVacuumFlagSkipsTheRuntime(t *testing.T) {
	resetIndexFlags(t)
	indexVacuum = true
	vault := newRuntimeTestVault(t)
	fake := startFakeRuntimeServer(t, vault, []lane.Event{doneEvent(lane.OutcomeOK, "")})

	out := &bytes.Buffer{}
	err := runResolvedIndexCommand(newIndexTestCommand(t, out), obsidian.VaultDefinition{Path: vault}, func() (notemeta.Indexer, error) {
		return notemeta.Indexer{}, fmt.Errorf("stop before indexing")
	})
	require.ErrorContains(t, err, "stop before indexing")
	require.Zero(t, fake.submits.Load())
	require.Contains(t, out.String(), "--vacuum is not a runtime job option")
}

func TestIndexInProcessFlagSkipsTheRuntime(t *testing.T) {
	resetIndexFlags(t)
	indexInProcess = true
	vault := newRuntimeTestVault(t)
	fake := startFakeRuntimeServer(t, vault, []lane.Event{doneEvent(lane.OutcomeOK, "")})

	out := &bytes.Buffer{}
	cmd := newIndexTestCommand(t, out)
	err := runResolvedIndexCommand(cmd, obsidian.VaultDefinition{Path: vault}, func() (notemeta.Indexer, error) {
		return notemeta.Indexer{}, fmt.Errorf("stop before indexing")
	})
	require.ErrorContains(t, err, "stop before indexing")
	require.Zero(t, fake.submits.Load(), "--in-process never talks to the runtime")
}

func TestRebuildStopsAHeadlessRuntimeFirst(t *testing.T) {
	vault := newRuntimeTestVault(t)
	fake := startFakeRuntimeServer(t, vault, nil)

	out := &bytes.Buffer{}
	restart, release, err := stopRuntimeForRebuild(context.Background(), vault, out)
	require.NoError(t, err)
	require.True(t, restart)
	require.Contains(t, out.String(), "Stopping headless vault runtime")
	require.True(t, fake.stopped.Load())
	_, _, liveErr := appruntime.LiveManifest(context.Background(), vault)
	require.Error(t, liveErr, "the database must not be clobbered under a live runtime")

	// Until the caller releases, no client may spawn a runtime that would open
	// the database about to be replaced.
	_, acquired, err := indexlock.TryAcquire(appruntime.SpawnLockPath(vault))
	require.NoError(t, err)
	require.False(t, acquired, "the spawn lease is held across the replacement")
	_, acquired, err = indexlock.TryAcquireWithOptions(appruntime.LockPath(vault), indexlock.AcquireOptions{ProcessLifetime: true})
	require.NoError(t, err)
	require.False(t, acquired, "direct serve must lose election until the replacement is finished")
	release()
	leaseRelease, acquired, err := indexlock.TryAcquire(appruntime.SpawnLockPath(vault))
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, leaseRelease())
	electionRelease, acquired, err := indexlock.TryAcquireWithOptions(appruntime.LockPath(vault), indexlock.AcquireOptions{ProcessLifetime: true})
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, electionRelease())
}

func TestRebuildRefusesAgainstAnAttachedRuntime(t *testing.T) {
	vault := newRuntimeTestVault(t)
	fake := startFakeRuntimeServer(t, vault, nil)
	fake.mode = appruntime.ModeAttached
	fake.manifest.Mode = appruntime.ModeAttached
	require.NoError(t, appruntime.WriteManifest(vault, fake.manifest))

	out := &bytes.Buffer{}
	restart, release, err := stopRuntimeForRebuild(context.Background(), vault, out)
	require.False(t, restart)
	require.Nil(t, release, "a refusal holds nothing")
	require.ErrorContains(t, err, fmt.Sprintf("pid %d", os.Getpid()))
	require.ErrorContains(t, err, "stop it first (`rzm stop`)")
	require.False(t, fake.stopped.Load(), "an attached runtime is never stopped for us")
}

func TestRebuildWithNoRuntimeNeedsNoRestart(t *testing.T) {
	vault := newRuntimeTestVault(t)
	restart, release, err := stopRuntimeForRebuild(context.Background(), vault, &bytes.Buffer{})
	require.NoError(t, err)
	require.False(t, restart)
	require.NotNil(t, release, "the lease still guards the replacement when nothing was running")
	release()
}

func TestRebuildWaitsForUnpublishedRuntimeElectionOwner(t *testing.T) {
	vault := newRuntimeTestVault(t)
	ownerRelease, acquired, err := indexlock.TryAcquireWithOptions(appruntime.LockPath(vault), indexlock.AcquireOptions{ProcessLifetime: true})
	require.NoError(t, err)
	require.True(t, acquired)
	defer ownerRelease()

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	restart, release, err := stopRuntimeForRebuild(ctx, vault, &bytes.Buffer{})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, restart)
	require.Nil(t, release)
	spawnRelease, acquired, err := indexlock.TryAcquire(appruntime.SpawnLockPath(vault))
	require.NoError(t, err)
	require.True(t, acquired, "the failed replacement must release its spawn lease")
	require.NoError(t, spawnRelease())
}

func TestStartVaultRuntimeInBackgroundDoesNotBlock(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	previous := backgroundEnsure
	t.Cleanup(func() { backgroundEnsure = previous })
	var once sync.Once
	backgroundEnsure = func(context.Context, obsidian.VaultDefinition) {
		once.Do(func() { close(entered) })
		<-release
	}
	defer close(release)

	started := time.Now()
	startVaultRuntimeInBackground(context.Background(), func(context.Context) (obsidian.VaultDefinition, error) {
		return obsidian.VaultDefinition{}, nil
	})
	require.Less(t, time.Since(started), 50*time.Millisecond, "read-only commands must not wait on ensure")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("ensure never ran in the background")
	}
}

func TestSummaryTextAcceptsBothShapes(t *testing.T) {
	require.Equal(t, "took 2s", summaryText(json.RawMessage(`"took 2s"`)))
	require.Equal(t, `{"notes":4}`, summaryText(json.RawMessage(`{"notes":4}`)))
	require.Equal(t, "", summaryText(nil))
}

func TestNewWorktreeIndexPrefersTheRuntime(t *testing.T) {
	resetIndexFlags(t)
	vault := newRuntimeTestVault(t)
	fake := startFakeRuntimeServer(t, vault, []lane.Event{doneEvent(lane.OutcomeOK, "")})

	out := &bytes.Buffer{}
	err := runNewWorktreeIndexThroughRuntime(newIndexTestCommand(t, out), vault, obsidian.VaultDefinition{Path: vault})
	require.NoError(t, err)
	require.EqualValues(t, 1, fake.submits.Load())
	require.NotContains(t, out.String(), "Indexing in this process")
}

func TestPrintIndexStatusNamesTheRuntimeAndLock(t *testing.T) {
	vault := newRuntimeTestVault(t)
	startFakeRuntimeServer(t, vault, nil)

	out := &bytes.Buffer{}
	require.NoError(t, printIndexStatus(newIndexTestCommand(t, out), vault))
	text := out.String()
	require.Contains(t, text, "Runtime:")
	require.Contains(t, text, fmt.Sprintf("PID:     %d", os.Getpid()))
	require.Contains(t, text, "busy job job-1")
	require.Contains(t, text, "Index lock:")
	require.Contains(t, text, "not held")
}

func TestPrintIndexStatusWithoutARuntime(t *testing.T) {
	vault := newRuntimeTestVault(t)
	lockPath := obsidian.IndexLockPath(vault)
	require.NoError(t, os.WriteFile(lockPath, []byte(`{"pid":4242,"host":"box","role":"cli/index","started":"2026-09-17T10:00:00Z"}`), 0o644))

	out := &bytes.Buffer{}
	require.NoError(t, printIndexStatus(newIndexTestCommand(t, out), vault))
	text := out.String()
	require.Contains(t, text, "none running")
	require.Contains(t, strings.ReplaceAll(text, " ", ""), "Role:cli/index")
	require.Contains(t, strings.ReplaceAll(text, " ", ""), "PID:4242")
	require.Contains(t, text, "Heartbeat:")
}

func TestHoldSpawnLeaseWaitsForACompetingSpawner(t *testing.T) {
	vault := t.TempDir()
	release, acquired, err := indexlock.TryAcquire(appruntime.SpawnLockPath(vault))
	require.NoError(t, err)
	require.True(t, acquired)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err = holdSpawnLease(ctx, vault)
	require.Error(t, err, "a held lease is waited on, not stolen")
	require.NoError(t, release())
	got, err := holdSpawnLease(context.Background(), vault)
	require.NoError(t, err)
	got()
	_, acquired, err = indexlock.TryAcquire(appruntime.SpawnLockPath(vault))
	require.NoError(t, err)
	require.True(t, acquired, "the lease is released for the next spawner")
}

func TestIndexReplacementRestartsAfterFallbackReturns(t *testing.T) {
	resetIndexFlags(t)
	t.Setenv(appruntime.AutostartEnv, "1")
	vault := newRuntimeTestVault(t)
	fake := startFakeRuntimeServer(t, vault, []lane.Event{doneEvent(lane.OutcomeFailed, lane.ErrDatabaseReplaced.Error())})
	fake.stopAfterStream = true
	// Fail registry setup to observe a restart attempt without launching a child.
	previousHome := vaultconfig.UserHomeDirectory
	t.Cleanup(func() { vaultconfig.UserHomeDirectory = previousHome })
	var fallbackEntered, restartedEarly, restartAttempted bool
	vaultconfig.UserHomeDirectory = func() (string, error) {
		restartAttempted = true
		restartedEarly = restartedEarly || !fallbackEntered
		return "", fmt.Errorf("test prevents spawning")
	}
	out := &bytes.Buffer{}
	err := runResolvedIndexCommand(newIndexTestCommand(t, out), obsidian.VaultDefinition{Path: vault}, func() (notemeta.Indexer, error) {
		fallbackEntered = true
		return notemeta.Indexer{}, fmt.Errorf("fallback failed before indexing")
	})
	require.ErrorContains(t, err, "fallback failed before indexing")
	require.True(t, fallbackEntered)
	require.True(t, restartAttempted, "a stopped runtime is restarted even if fallback fails")
	require.False(t, restartedEarly, "the replacement runtime must not open the database before fallback finishes")
}
