package lane

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/stretchr/testify/require"
)

func newTestLane(t *testing.T, mutate func(*Options)) *Executor {
	t.Helper()
	opts := Options{
		LockPath:     filepath.Join(t.TempDir(), ".rhizome", "index.lock"),
		PriorityPoll: 20 * time.Millisecond,
	}
	if mutate != nil {
		mutate(&opts)
	}
	lane := New(opts)
	t.Cleanup(lane.Close)
	return lane
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestExecutorExplicitJobPreemptsBackgroundWithinOneSecond(t *testing.T) {
	lane := newTestLane(t, nil)

	backgroundStarted := make(chan struct{})
	cancelledAt := make(chan time.Time, 1)
	background, _, err := lane.Submit(context.Background(), Request{Kind: KindEmbedCycle, Run: func(ctx context.Context, _ Reporter) error {
		close(backgroundStarted)
		<-ctx.Done()
		cancelledAt <- time.Now()
		return ctx.Err()
	}})
	require.NoError(t, err)
	<-backgroundStarted

	explicitStarted := make(chan struct{})
	submittedAt := time.Now()
	explicit, joined, err := lane.Submit(context.Background(), Request{Kind: KindExplicitIndex, Run: func(context.Context, Reporter) error {
		close(explicitStarted)
		return nil
	}})
	require.NoError(t, err)
	require.False(t, joined)

	select {
	case at := <-cancelledAt:
		require.Less(t, at.Sub(submittedAt), time.Second, "background job must yield within one second")
	case <-time.After(2 * time.Second):
		t.Fatal("background job was not cancelled")
	}

	select {
	case <-explicitStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("explicit job did not start")
	}
	<-explicit.Done()
	require.NoError(t, explicit.Err())

	<-background.Done()
	require.ErrorIs(t, background.Err(), ErrPreempted)
	require.ErrorIs(t, background.Err(), context.Canceled, "preemption is still a cancellation for outcome purposes")
	require.Equal(t, OutcomeCancelled, lastEvent(t, background).Outcome)
}

func TestExecutorCoalescesSameKindAndRunsBackgroundFIFO(t *testing.T) {
	lane := newTestLane(t, nil)

	release := make(chan struct{})
	var order []string
	var mu sync.Mutex
	record := func(name string) {
		mu.Lock()
		order = append(order, name)
		mu.Unlock()
	}

	blocker, _, err := lane.Submit(context.Background(), Request{Kind: KindBootCatchUp, Run: func(ctx context.Context, _ Reporter) error {
		record("boot")
		<-release
		return nil
	}})
	require.NoError(t, err)

	first, joined, err := lane.Submit(context.Background(), Request{Kind: KindEmbedCycle, Coalesce: true, Run: func(context.Context, Reporter) error {
		record("embed")
		return nil
	}})
	require.NoError(t, err)
	require.False(t, joined)

	second, joined, err := lane.Submit(context.Background(), Request{Kind: KindEmbedCycle, Coalesce: true, Run: func(context.Context, Reporter) error {
		record("embed-duplicate")
		return nil
	}})
	require.NoError(t, err)
	require.True(t, joined, "a queued job of the same kind must be joined")
	require.Equal(t, first.ID(), second.ID())

	graph, _, err := lane.Submit(context.Background(), Request{Kind: KindGraphCycle, Run: func(context.Context, Reporter) error {
		record("graph")
		return nil
	}})
	require.NoError(t, err)

	close(release)
	<-blocker.Done()
	<-first.Done()
	<-graph.Done()

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"boot", "embed", "graph"}, order)
}

func TestExecutorReplaysHistoryThenStreamsAndAlwaysDeliversDone(t *testing.T) {
	lane := newTestLane(t, nil)

	emitted := make(chan struct{})
	finish := make(chan struct{})
	handle, _, err := lane.Submit(context.Background(), Request{Kind: KindExplicitIndex, Run: func(_ context.Context, progress Reporter) error {
		progress.Log("start")
		progress.Segment("notes", 1, 4)
		close(emitted)
		<-finish
		progress.Segment("notes", 4, 4)
		progress.(SummaryReporter).SetSummary(json.RawMessage(`"total 1.2s"`))
		return nil
	}})
	require.NoError(t, err)
	<-emitted

	events, cancel := handle.Subscribe()
	defer cancel()
	require.Equal(t, EventLog, (<-events).Type)
	replayed := <-events
	require.Equal(t, EventProgress, replayed.Type)
	require.Equal(t, int64(1), replayed.Done)

	close(finish)
	live := <-events
	require.Equal(t, int64(4), live.Done)
	done := <-events
	require.Equal(t, EventDone, done.Type)
	require.Equal(t, OutcomeOK, done.Outcome)
	require.JSONEq(t, `"total 1.2s"`, string(done.Summary))
	_, open := <-events
	require.False(t, open, "the stream closes after Done")

	<-handle.Done()
	late, cancelLate := handle.Subscribe()
	defer cancelLate()
	var lateTypes []EventType
	for event := range late {
		lateTypes = append(lateTypes, event.Type)
	}
	require.Equal(t, EventDone, lateTypes[len(lateTypes)-1], "a late subscriber still sees Done")
}

func TestExecutorCancelIsJobWide(t *testing.T) {
	lane := newTestLane(t, nil)

	started := make(chan struct{})
	handle, _, err := lane.Submit(context.Background(), Request{Kind: KindExplicitIndex, Run: func(ctx context.Context, _ Reporter) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}})
	require.NoError(t, err)
	<-started

	joiner, joined, err := lane.Submit(context.Background(), Request{Kind: KindExplicitIndex, Run: func(context.Context, Reporter) error { return nil }})
	require.NoError(t, err)
	require.True(t, joined)

	joiner.Cancel()
	select {
	case <-handle.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("cancel from a joiner did not cancel the job")
	}
	require.ErrorIs(t, handle.Err(), context.Canceled)
}

func TestExecutorCloseCancelsRunningAndDropsQueue(t *testing.T) {
	lane := New(Options{LockPath: filepath.Join(t.TempDir(), ".rhizome", "index.lock"), PriorityPoll: 20 * time.Millisecond})

	started := make(chan struct{})
	running, _, err := lane.Submit(context.Background(), Request{Kind: KindBootCatchUp, Run: func(ctx context.Context, _ Reporter) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}})
	require.NoError(t, err)
	<-started

	queued, _, err := lane.Submit(context.Background(), Request{Kind: KindGraphCycle, Run: func(context.Context, Reporter) error {
		t.Error("a queued job must not run after Close")
		return nil
	}})
	require.NoError(t, err)

	lane.Close()
	<-running.Done()
	<-queued.Done()
	require.ErrorIs(t, running.Err(), context.Canceled)
	require.ErrorIs(t, queued.Err(), ErrClosed)

	_, _, err = lane.Submit(context.Background(), Request{Kind: KindGraphCycle})
	require.ErrorIs(t, err, ErrClosed)
}

func TestExecutorHoldsBackgroundWorkWhileAPriorityRequestExists(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".rhizome", "index.lock")
	lane := New(Options{LockPath: lockPath, PriorityPoll: 20 * time.Millisecond})
	t.Cleanup(lane.Close)

	priorityPath := writeForeignPriority(t, lockPath)

	ran := make(chan struct{})
	handle, _, err := lane.Submit(context.Background(), Request{Kind: KindEmbedCycle, Run: func(context.Context, Reporter) error {
		close(ran)
		return nil
	}})
	require.NoError(t, err)

	waitFor(t, 2*time.Second, func() bool { return lane.Status().Held == HeldExternalPriority })
	select {
	case <-ran:
		t.Fatal("background work ran while an external priority request was pending")
	case <-time.After(100 * time.Millisecond):
	}

	require.NoError(t, os.Remove(priorityPath))
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("background work did not resume after the priority request cleared")
	}
	<-handle.Done()
	require.NoError(t, handle.Err())
	waitFor(t, 2*time.Second, func() bool { return lane.Status().Held == "" })
}

func TestExecutorCancelsRunningBackgroundJobOnExternalPriority(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".rhizome", "index.lock")
	lane := New(Options{LockPath: lockPath, PriorityPoll: 20 * time.Millisecond})
	t.Cleanup(lane.Close)

	started := make(chan struct{})
	handle, _, err := lane.Submit(context.Background(), Request{Kind: KindEmbedCycle, Run: func(ctx context.Context, _ Reporter) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}})
	require.NoError(t, err)
	<-started

	requestedAt := time.Now()
	writeForeignPriority(t, lockPath)
	select {
	case <-handle.Done():
		require.Less(t, time.Since(requestedAt), time.Second)
	case <-time.After(2 * time.Second):
		t.Fatal("a running background job did not yield to external priority")
	}
	require.ErrorIs(t, handle.Err(), context.Canceled)
}

func TestExecutorWritesJobKindAsLockRole(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".rhizome", "index.lock")
	lane := New(Options{LockPath: lockPath, PriorityPoll: 20 * time.Millisecond})
	t.Cleanup(lane.Close)

	roles := make(chan string, 1)
	handle, _, err := lane.Submit(context.Background(), Request{Kind: KindGraphCycle, Run: func(context.Context, Reporter) error {
		data, ok := indexlock.ReadLockData(lockPath)
		require.True(t, ok)
		roles <- data.Role
		return nil
	}})
	require.NoError(t, err)
	<-handle.Done()
	require.NoError(t, handle.Err())
	require.Equal(t, "runtime/graph-cycle", <-roles)

	_, err = os.Stat(lockPath)
	require.True(t, os.IsNotExist(err), "the lane releases the lock after each job")
}

func TestExecutorWaitsForAnExternalLockHolderAndNamesIt(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), ".rhizome", "index.lock")
	lane := New(Options{LockPath: lockPath, PriorityPoll: 20 * time.Millisecond, acquire: func(role string) (func() error, bool, error) {
		return nil, false, nil
	}})
	t.Cleanup(lane.Close)
	require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), 0o755))
	writeLockFile(t, lockPath, indexlock.LockData{PID: 4242, Role: "cli/index"})

	handle, _, err := lane.Submit(context.Background(), Request{Kind: KindExplicitIndex, Run: func(context.Context, Reporter) error { return nil }})
	require.NoError(t, err)
	events, cancel := handle.Subscribe()
	defer cancel()

	select {
	case event := <-events:
		require.Equal(t, EventLog, event.Type)
		require.Contains(t, event.Line, "cli/index")
		require.Contains(t, event.Line, "4242")
	case <-time.After(2 * time.Second):
		t.Fatal("no holder report while waiting for an external lock holder")
	}
	handle.Cancel()
	<-handle.Done()
}

func lastEvent(t *testing.T, handle Handle) Event {
	t.Helper()
	events, cancel := handle.Subscribe()
	defer cancel()
	var last Event
	for event := range events {
		last = event
	}
	return last
}

// writeForeignPriority records a priority request from another runtime, which
// is exactly what a second process on this host looks like to CheckPriority.
func writeForeignPriority(t *testing.T, lockPath string) string {
	t.Helper()
	path := filepath.Join(indexlock.PriorityPath(lockPath), rand.Text()+".json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	data, err := json.Marshal(indexlock.LockData{PID: 999999, Host: "other", Runtime: "other-runtime", Started: time.Now().UTC().Format(time.RFC3339Nano)})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func writeLockFile(t *testing.T, lockPath string, data indexlock.LockData) {
	t.Helper()
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(lockPath, raw, 0o644))
}

func TestNextPrefersAnExplicitJobQueuedDuringThePriorityCheck(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	var lane *Executor
	var order []Kind
	var mu sync.Mutex
	record := func(kind Kind) func(context.Context, Reporter) error {
		return func(context.Context, Reporter) error {
			mu.Lock()
			order = append(order, kind)
			mu.Unlock()
			return nil
		}
	}
	var injected sync.Once
	lane = New(Options{LockPath: lockPath, PriorityPoll: 10 * time.Millisecond,
		acquire: func(string) (func() error, bool, error) { return func() error { return nil }, true, nil },
		checkPriority: func() bool {
			// An explicit job arrives while the background candidate is being
			// checked without the lock held: it must run first.
			injected.Do(func() {
				_, _, _ = lane.Submit(context.Background(), Request{Kind: KindExplicitIndex, Coalesce: true, Run: record(KindExplicitIndex)})
			})
			return false
		},
	})
	t.Cleanup(lane.Close)
	handle, _, err := lane.Submit(context.Background(), Request{Kind: KindGraphCycle, Run: record(KindGraphCycle)})
	require.NoError(t, err)
	<-handle.Done()
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) == 2
	}, 5*time.Second, 10*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []Kind{KindExplicitIndex, KindGraphCycle}, order)
}

func TestPreemptedErrorNamesTheExplicitJob(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "index.lock")
	lane := New(Options{LockPath: lockPath, PriorityPoll: 10 * time.Millisecond,
		acquire:       func(string) (func() error, bool, error) { return func() error { return nil }, true, nil },
		checkPriority: func() bool { return false },
	})
	t.Cleanup(lane.Close)
	started := make(chan struct{})
	background, _, err := lane.Submit(context.Background(), Request{Kind: KindBootCatchUp, Run: func(ctx context.Context, _ Reporter) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}})
	require.NoError(t, err)
	<-started
	explicit, _, err := lane.Submit(context.Background(), Request{Kind: KindExplicitIndex, Coalesce: true, Run: func(ctx context.Context, _ Reporter) error {
		<-ctx.Done()
		return ctx.Err()
	}})
	require.NoError(t, err)
	<-background.Done()
	var preempted *PreemptedError
	require.ErrorAs(t, background.Err(), &preempted)
	require.Equal(t, explicit.ID(), preempted.By.ID(), "the submitter can wait for the job that displaced it")
	explicit.Cancel()
	<-explicit.Done()
	require.ErrorIs(t, explicit.Err(), context.Canceled, "a cancelled explicit job did not do the catch-up's work")
}
