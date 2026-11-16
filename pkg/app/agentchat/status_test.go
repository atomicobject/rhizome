package agentchat

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

// slowStatusHarness answers its status probe after a delay, or fails when the
// probe's context ends first.
type slowStatusHarness struct {
	harnesstest.Harness
}

func (*slowStatusHarness) Status(ctx context.Context) (harness.Status, error) {
	select {
	case <-time.After(50 * time.Millisecond):
		return harness.Status{Kind: harness.KindCodex, Installed: true, LoggedIn: true}, nil
	case <-ctx.Done():
		return harness.Status{}, ctx.Err()
	}
}

func TestSettingsStatusSurvivesACallerThatLeaves(t *testing.T) {
	env := setTestSettings(t, Settings{})
	service, err := NewService(vaultconfig.WithEnv(context.Background(), env), t.TempDir(),
		map[harness.Kind]harness.Harness{harness.KindCodex: &slowStatusHarness{}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = service.Close() })

	leaving, leave := context.WithCancel(context.Background())
	leave()
	_, err = service.Settings(leaving)
	require.NoError(t, err)

	response, err := service.Settings(context.Background())
	require.NoError(t, err)
	require.Equal(t, harness.KindCodex, response.Resolved.Harness)
	require.True(t, response.Harnesses[0].Status.Installed)
	require.Empty(t, response.Harnesses[0].Status.LastError)
}

// rendezvousHarness answers only once every harness has started its probe, so
// a report that probes one harness at a time times out.
type rendezvousHarness struct {
	harnesstest.Harness
	started *sync.WaitGroup
	status  harness.Status
}

func (h *rendezvousHarness) Status(ctx context.Context) (harness.Status, error) {
	h.started.Done()
	all := make(chan struct{})
	go func() { h.started.Wait(); close(all) }()
	select {
	case <-all:
		return h.status, nil
	case <-ctx.Done():
		return harness.Status{}, ctx.Err()
	}
}

func TestReportProbesHarnessesTogetherAndResolvesTheConfiguredModel(t *testing.T) {
	env := setTestSettings(t, Settings{Harness: harness.KindClaude, Harnesses: map[harness.Kind]HarnessSettings{harness.KindClaude: {Model: "opus"}}})
	started := &sync.WaitGroup{}
	started.Add(2)
	ready := func(models ...harness.ModelOption) harness.Status {
		return harness.Status{Installed: true, LoggedIn: true, Version: "1.0", Models: models}
	}
	response, err := Report(context.Background(), env, map[harness.Kind]harness.Harness{
		harness.KindCodex:  &rendezvousHarness{started: started, status: ready()},
		harness.KindClaude: &rendezvousHarness{started: started, status: ready(harness.ModelOption{ID: "opus"})},
	})
	require.NoError(t, err)
	require.Equal(t, []harness.Kind{harness.KindClaude, harness.KindCodex}, []harness.Kind{response.Harnesses[0].Kind, response.Harnesses[1].Kind})
	for _, item := range response.Harnesses {
		require.Empty(t, item.Status.LastError, "%s probe waited on the other harness", item.Kind)
		require.True(t, item.Status.Installed)
	}
	require.Equal(t, ResolvedHarness{Harness: harness.KindClaude, Model: "opus", Reason: "configured"}, response.Resolved)
}

// upgradingHarness reports itself missing on its first probe, which waits for
// release, and installed on every later probe.
type upgradingHarness struct {
	harnesstest.Harness
	started, release chan struct{}
	calls            atomic.Int32
	forgotVersion    atomic.Bool
}

func (h *upgradingHarness) Status(context.Context) (harness.Status, error) {
	if h.calls.Add(1) == 1 {
		close(h.started)
		<-h.release
		return harness.Status{}, nil
	}
	return harness.Status{Installed: true, LoggedIn: true}, nil
}

func (h *upgradingHarness) ForgetVersion() { h.forgotVersion.Store(true) }

func TestForgottenStatusIsNotRestoredByAnOlderProbe(t *testing.T) {
	env := setTestSettings(t, Settings{})
	ctx := vaultconfig.WithEnv(context.Background(), env)
	driver := &upgradingHarness{started: make(chan struct{}), release: make(chan struct{})}
	service, err := NewService(ctx, t.TempDir(), map[harness.Kind]harness.Harness{harness.KindCodex: driver})
	require.NoError(t, err)
	t.Cleanup(func() { _ = service.Close() })

	older := make(chan struct{})
	go func() {
		defer close(older)
		_, _ = service.Settings(ctx)
	}()
	<-driver.started
	service.ForgetHarnessStatus()
	require.True(t, driver.forgotVersion.Load(), "a refresh also forgets the driver's cached CLI version")
	fresh, err := service.Settings(ctx)
	require.NoError(t, err)
	require.True(t, fresh.Harnesses[0].Status.Installed)

	close(driver.release)
	<-older
	cached, err := service.Settings(ctx)
	require.NoError(t, err)
	require.True(t, cached.Harnesses[0].Status.Installed, "the probe that began before the refresh does not overwrite it")
	require.EqualValues(t, 2, driver.calls.Load())
}
