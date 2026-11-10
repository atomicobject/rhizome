package modulereadiness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/actions"
	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	mu    sync.Mutex
	calls []string
	run   func(string, map[string]any) (Assessment, error)
}

func (f *fakeRunner) Run(_ context.Context, id string, input map[string]any) (actions.Envelope, error) {
	f.mu.Lock()
	f.calls = append(f.calls, id)
	f.mu.Unlock()
	a, err := f.run(id, input)
	data, _ := json.Marshal(a)
	return actions.Envelope{Status: actions.StatusOK, Result: data}, err
}
func fixture(t *testing.T) string {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome/modules/demo")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lifecycle.yml"), []byte("title: Demo\nobserve: observe\nsetup: setup\nindex: index\nverify: observe\ninputs: [input]\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "input"), []byte("one"), 0644))
	return root
}
func TestAutomaticSetupEmptyReadyAndUnchangedReopen(t *testing.T) {
	root := fixture(t)
	stage := 0
	runner := &fakeRunner{run: func(id string, _ map[string]any) (Assessment, error) {
		switch id {
		case "demo/setup":
			stage = 1
		case "demo/index":
			stage = 2
		}
		next := []Next{NextPrepare, NextIndex, NextReady}[stage]
		return Assessment{Next: next}, nil
	}}
	opts := Options{Root: root, Runner: runner}
	c := New(opts)
	c.scan(context.Background(), nil)
	require.Equal(t, Ready, c.Snapshot()[0].State)
	require.Equal(t, []string{"demo/observe", "demo/setup", "demo/observe", "demo/index", "demo/observe"}, runner.calls)
	c.scan(context.Background(), nil)
	require.Len(t, runner.calls, 5)
	reopened := New(opts)
	reopened.scan(context.Background(), nil)
	require.Equal(t, Ready, reopened.Snapshot()[0].State)
	require.Len(t, runner.calls, 6, "reopen observes but never repeats setup or index")
}
func TestFailureReceiptRetryAndRelevantChange(t *testing.T) {
	root := fixture(t)
	bad := true
	runner := &fakeRunner{run: func(string, map[string]any) (Assessment, error) {
		if bad {
			return Assessment{}, os.ErrNotExist
		}
		return Assessment{Next: NextReady}, nil
	}}
	opts := Options{Root: root, Runner: runner}
	c := New(opts)
	c.scan(context.Background(), nil)
	require.Equal(t, Failed, c.Snapshot()[0].State)
	c.scan(context.Background(), nil)
	c = New(opts)
	c.scan(context.Background(), nil)
	require.Len(t, runner.calls, 1)
	require.NoError(t, c.Recover("demo", Request{Kind: Retry}))
	bad = false
	r := <-c.requests
	c.scan(context.Background(), &r)
	require.Equal(t, Ready, c.Snapshot()[0].State)
	require.NoError(t, os.WriteFile(filepath.Join(root, "input"), []byte("fix"), 0644))
	c.scan(context.Background(), nil)
	require.Len(t, runner.calls, 3)
}
func TestChoiceIsValidatedAndSetupPersistsSelection(t *testing.T) {
	root := fixture(t)
	chosen := ""
	runner := &fakeRunner{run: func(id string, input map[string]any) (Assessment, error) {
		if id == "demo/setup" {
			chosen = input["product"].(string)
		}
		if chosen != "" {
			return Assessment{Next: NextReady}, nil
		}
		return Assessment{Next: NextInput, Reason: "Choose product", Recovery: []Recovery{{Kind: Choice, Input: "product", Choices: []string{"one", "two"}}}}, nil
	}}
	c := New(Options{Root: root, Runner: runner})
	c.scan(context.Background(), nil)
	require.Equal(t, RequiredInput, c.Snapshot()[0].State)
	require.Error(t, c.Recover("demo", Request{Kind: Choice, Value: "unknown"}))
	require.NoError(t, c.Recover("demo", Request{Kind: Choice, Value: "two"}))
	r := <-c.requests
	c.scan(context.Background(), &r)
	require.Equal(t, "two", chosen)
	require.Equal(t, Ready, c.Snapshot()[0].State)
}
func TestChangedDuringObservationRejectsStaleCompletion(t *testing.T) {
	root := fixture(t)
	runner := &fakeRunner{run: func(string, map[string]any) (Assessment, error) {
		require.NoError(t, os.WriteFile(filepath.Join(root, "input"), []byte("changed"), 0644))
		return Assessment{Next: NextReady}, nil
	}}
	c := New(Options{Root: root, Runner: runner})
	c.scan(context.Background(), nil)
	require.Equal(t, Checking, c.Snapshot()[0].State, "stale ready must never publish")
	c.scan(context.Background(), nil)
	require.Equal(t, Ready, c.Snapshot()[0].State)
}

func TestChangedDuringFailedIndexIsReassessedWithoutRetry(t *testing.T) {
	root := fixture(t)
	indexed := false
	runner := &fakeRunner{run: func(id string, _ map[string]any) (Assessment, error) {
		if id == "demo/index" {
			indexed = true
			require.NoError(t, os.WriteFile(filepath.Join(root, "input"), []byte("edited during index"), 0644))
			return Assessment{}, os.ErrInvalid
		}
		if indexed {
			return Assessment{Next: NextReady}, nil
		}
		return Assessment{Next: NextIndex}, nil
	}}
	c := New(Options{Root: root, Runner: runner})
	c.scan(context.Background(), nil)
	require.Equal(t, Checking, c.Snapshot()[0].State)
	c.scan(context.Background(), nil)
	require.Equal(t, Ready, c.Snapshot()[0].State)
	require.Equal(t, []string{"demo/observe", "demo/index", "demo/observe"}, runner.calls)
}

func TestReadyReconcilesSourceAdmissionBeforeReportingReady(t *testing.T) {
	root := fixture(t)
	admitted := false
	runner := &fakeRunner{run: func(id string, _ map[string]any) (Assessment, error) {
		return Assessment{Next: NextReady, Includes: []string{"model.yaml"}}, nil
	}}
	c := New(Options{Root: root, Runner: runner, Include: func(_ context.Context, sources []string) error {
		require.Equal(t, []string{"model.yaml"}, sources)
		if !admitted {
			admitted = true
			return os.WriteFile(filepath.Join(root, "input"), []byte("admitted"), 0644)
		}
		return nil
	}})
	c.scan(context.Background(), nil)
	require.Equal(t, Ready, c.Snapshot()[0].State)
	require.Equal(t, []string{"demo/observe", "demo/index", "demo/observe"}, runner.calls)
	c.scan(context.Background(), nil)
	require.Len(t, runner.calls, 3, "unchanged admission does not repeat indexing")
}
func TestDiscoversCLIInstallAndCancels(t *testing.T) {
	root := t.TempDir()
	ran := make(chan struct{}, 1)
	runner := &fakeRunner{run: func(string, map[string]any) (Assessment, error) {
		select {
		case ran <- struct{}{}:
		default:
		}
		return Assessment{Next: NextReady}, nil
	}}
	c := New(Options{Root: root, Runner: runner, Interval: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	go c.Run(ctx)
	defer func() { cancel(); c.Wait() }()
	dir := filepath.Join(root, ".rhizome/modules/demo")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lifecycle.yml"), []byte("title: Demo\nobserve: observe\nsetup: setup\nindex: index\nverify: observe\ninputs: [input]\n"), 0644))
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("CLI installation was not discovered")
	}
}

func TestTrustBlocksActionsAndChangedTrustResumes(t *testing.T) {
	root := fixture(t)
	reason := "Repository is not trusted"
	runner := &fakeRunner{run: func(string, map[string]any) (Assessment, error) { return Assessment{Next: NextReady}, nil }}
	c := New(Options{Root: root, Runner: runner, Trust: func(context.Context) string { return reason }})
	c.scan(context.Background(), nil)
	require.Empty(t, runner.calls)
	require.Equal(t, Trust, c.Snapshot()[0].Recovery[0].Kind)
	reason = ""
	c.scan(context.Background(), nil)
	require.Equal(t, Ready, c.Snapshot()[0].State)
}

func TestConcurrentRecoveryRequestsCoalesce(t *testing.T) {
	root := fixture(t)
	runner := &fakeRunner{run: func(string, map[string]any) (Assessment, error) { return Assessment{}, os.ErrNotExist }}
	c := New(Options{Root: root, Runner: runner})
	c.scan(context.Background(), nil)
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() { defer group.Done(); require.NoError(t, c.Recover("demo", Request{Kind: Retry})) }()
	}
	group.Wait()
	require.Len(t, c.requests, 1)
}

type cancelRunner struct {
	started chan struct{}
	stopped chan struct{}
}

func (r cancelRunner) Run(ctx context.Context, _ string, _ map[string]any) (actions.Envelope, error) {
	close(r.started)
	<-ctx.Done()
	close(r.stopped)
	return actions.Envelope{}, ctx.Err()
}
func TestCancellationDrainsActiveActionAndLeavesNoFailureReceipt(t *testing.T) {
	root := fixture(t)
	r := cancelRunner{make(chan struct{}), make(chan struct{})}
	c := New(Options{Root: root, Runner: r})
	ctx, cancel := context.WithCancel(context.Background())
	go c.Run(ctx)
	t.Cleanup(func() { cancel(); c.Wait() })
	select {
	case <-r.started:
	case <-time.After(time.Second):
		t.Fatal("action never started")
	}
	cancel()
	c.Wait()
	select {
	case <-r.stopped:
	default:
		t.Fatal("action was not drained")
	}
	reopened := New(Options{Root: root, Runner: r})
	require.Empty(t, reopened.Snapshot(), "cancellation is rechecked on reopen rather than cached as failure")
}
