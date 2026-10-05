package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestSearchInitializationFailureCompletesLaterCapabilities(t *testing.T) {
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.Close())
	formats, err := builtin.NewRuntime()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	rt := &LiveRuntime{
		ctx: ctx, cancelCtx: cancel,
		VaultPath:         root,
		VaultDef:          obsidian.VaultDefinition{Root: root, Includes: []string{"["}},
		noteFormats:       formats,
		disableLeaderWork: true,
		requirements:      RequireRuntimeCapabilities(RuntimeCapabilitySearch, RuntimeCapabilityCodeIndex),
		readOnlyCodeIndex: true, disableSessionStore: true,
		searchReady: make(chan struct{}), semanticReady: make(chan struct{}),
		codeReady: make(chan struct{}), leaderCh: make(chan struct{}),
	}
	t.Cleanup(func() { require.NoError(t, rt.Close()) })

	rt.initPhase1Search()
	require.ErrorContains(t, rt.WaitForSearch(t.Context()), "invalid note include")
	t.Run("omitted semantic capability", func(t *testing.T) {
		waitCtx, stop := context.WithTimeout(t.Context(), time.Second)
		defer stop()
		require.ErrorIs(t, rt.WaitForSemantic(waitCtx), ErrCapabilityNotRequested{Capability: RuntimeCapabilitySemantic})
	})
	t.Run("requested code index remains available", func(t *testing.T) {
		waitCtx, stop := context.WithTimeout(t.Context(), time.Second)
		defer stop()
		require.NoError(t, rt.WaitForCodeIndex(waitCtx))
		require.NotNil(t, rt.IntelStore())
	})
}

func TestSearchInitializationFailureLeavesRequestedSemanticAvailable(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte(`notes:
  includes: ["["]
noteEmbeddings:
  enabled: true
  provider: test
  dimensions: 4
`), 0o644))
	rt, err := NewLiveRuntime(t.Context(), LiveOptions{
		VaultName: root, DisableLeaderWork: true, DisableWatchHub: true,
		SkipCacheWarmup: true, DisableSessionStore: true,
		Requirements: RequireRuntimeCapabilities(RuntimeCapabilitySearch, RuntimeCapabilitySemantic),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	require.ErrorContains(t, rt.WaitForSearch(ctx), "invalid note include")
	require.NoError(t, rt.WaitForSemantic(ctx))
	require.Nil(t, rt.Cache())
	require.NotNil(t, rt.NoteProvider())
	require.NotNil(t, rt.NoteIndex())
	meta, found, err := rt.NoteIndex().Metadata(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "test", meta.Provider)
	require.Equal(t, 4, meta.Dimensions)
	require.ErrorIs(t, rt.WaitForCodeIndex(ctx), ErrCapabilityNotRequested{Capability: RuntimeCapabilityCodeIndex})
}

func TestNewLiveRuntimeReleasesParentCancellationOnVaultError(t *testing.T) {
	t.Chdir(t.TempDir())
	wantErr := errors.New("vault preferences unavailable")
	previousConfigPath := obsidian.CliConfigPath
	obsidian.CliConfigPath = func() (string, string, error) { return "", "", wantErr }
	t.Cleanup(func() { obsidian.CliConfigPath = previousConfigPath })

	for _, vaultName := range []string{"", "missing-vault"} {
		t.Run("vault="+vaultName, func(t *testing.T) {
			parent := &trackedCancellationContext{Context: context.Background(), done: make(chan struct{})}
			rt, err := NewLiveRuntime(parent, LiveOptions{VaultName: vaultName})
			require.ErrorIs(t, err, wantErr)
			require.Nil(t, rt)
			require.Zero(t, parent.subscriptions.Load(), "failed construction must release its parent cancellation subscription")
		})
	}
}

func TestNewLiveRuntimeReleasesElectionAfterOwnerBarrierFails(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	wantErr := errors.New("owner barrier failed")
	rt, err := NewLiveRuntime(t.Context(), LiveOptions{
		VaultName: root,
		OnElected: func() error { return wantErr },
	})
	require.ErrorIs(t, err, wantErr)
	require.Nil(t, rt)

	release, acquired, err := indexlock.TryAcquire(appruntime.LockPath(root))
	require.NoError(t, err)
	require.True(t, acquired, "a failed startup must release runtime election")
	require.NoError(t, release())
}

func TestRuntimeLockClosesAfterResourcesOnce(t *testing.T) {
	root := t.TempDir()
	lockPath := appruntime.LockPath(root)
	ctx, cancel := context.WithCancel(t.Context())
	rt := &LiveRuntime{ctx: ctx, cancelCtx: cancel, VaultPath: root, leaderCh: make(chan struct{})}
	t.Cleanup(func() { require.NoError(t, rt.Close()) })
	require.True(t, rt.electRuntimeOwner())
	var closed int
	rt.addCloser(func() {
		closed++
		require.ErrorIs(t, rt.ctx.Err(), context.Canceled)
		require.FileExists(t, lockPath)
	})
	require.NoError(t, rt.Close())
	require.NoError(t, rt.Close())
	require.Equal(t, 1, closed)
	release, acquired, err := indexlock.TryAcquire(lockPath)
	require.NoError(t, err)
	require.True(t, acquired, "another runtime can acquire the released runtime lock")
	require.NoError(t, release())
}

// Track the cancellation subscription without depending on goroutine counts or
// the standard library's private context implementation.
type trackedCancellationContext struct {
	context.Context
	done          chan struct{}
	subscriptions atomic.Int32
}

func (ctx *trackedCancellationContext) Done() <-chan struct{} { return ctx.done }

func (ctx *trackedCancellationContext) AfterFunc(func()) func() bool {
	ctx.subscriptions.Add(1)
	var once sync.Once
	return func() bool {
		stopped := false
		once.Do(func() {
			ctx.subscriptions.Add(-1)
			stopped = true
		})
		return stopped
	}
}

func TestRuntimeCloseDrainsBootCatchUpBeforeReleasingVault(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0o644))
	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	rt, err := NewLiveRuntime(t.Context(), LiveOptions{
		VaultName: root, DisableWatchHub: true, SkipCacheWarmup: true,
		BackgroundIndexer: func(ctx context.Context, _ notemeta.Indexer, vaultPath string, _ obsidian.VaultDefinition, _ bool) error {
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-release
			// A cancelled callback may still finish a write before returning.
			err := os.WriteFile(filepath.Join(vaultPath, ".rhizome", "callback-finished"), nil, 0o600)
			close(finished)
			return err
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { unblock(); require.NoError(t, rt.Close()) })
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("boot catch-up did not start")
	}
	closed := make(chan struct{})
	go func() { _ = rt.Close(); close(closed) }()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("close did not cancel boot catch-up")
	}
	select {
	case <-closed:
		t.Fatal("Close returned while boot catch-up could still write to the vault")
	case <-time.After(50 * time.Millisecond):
	}
	require.FileExists(t, appruntime.LockPath(root), "runtime ownership must outlive its writer")
	unblock()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not drain completed boot catch-up")
	}
	select {
	case <-finished:
	default:
		t.Fatal("callback did not finish before Close returned")
	}
	require.FileExists(t, filepath.Join(root, ".rhizome", "callback-finished"))
}

func TestRuntimeOwnershipFinalizerRunsAfterResourcesAndBeforeRelease(t *testing.T) {
	root := t.TempDir()
	lockPath := appruntime.LockPath(root)
	ctx, cancel := context.WithCancel(t.Context())
	var resourcesClosed bool
	var finalized int
	rt := &LiveRuntime{ctx: ctx, cancelCtx: cancel, VaultPath: root, leaderCh: make(chan struct{})}
	rt.beforeOwnershipRelease = func() {
		finalized++
		require.True(t, resourcesClosed)
		require.FileExists(t, lockPath)
	}
	require.True(t, rt.electRuntimeOwner())
	rt.addCloser(func() { resourcesClosed = true })
	require.NoError(t, rt.Close())
	require.NoError(t, rt.Close())
	require.Equal(t, 1, finalized)
	require.NoFileExists(t, lockPath)
}

func TestRuntimeOwnershipFinalizerPanicStillReleasesElection(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	rt := &LiveRuntime{ctx: ctx, cancelCtx: cancel, VaultPath: root, leaderCh: make(chan struct{})}
	rt.beforeOwnershipRelease = func() { panic("synthetic finalizer panic") }
	require.True(t, rt.electRuntimeOwner())
	require.Panics(t, func() { _ = rt.Close() })
	require.NoFileExists(t, appruntime.LockPath(root))
}

func TestRuntimeOwnerBarrierPanicFinalizesAndReleasesElection(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("notes: {}\n"), 0644))
	var finalized bool
	require.Panics(t, func() {
		_, _ = NewLiveRuntime(t.Context(), LiveOptions{
			VaultName: root,
			OnElected: func() error { panic("synthetic barrier panic") },
			BeforeOwnershipRelease: func() {
				finalized = true
				require.FileExists(t, appruntime.LockPath(root))
			},
		})
	})
	require.True(t, finalized)
	require.NoFileExists(t, appruntime.LockPath(root))
}
