package bootstrap

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
)

func TestOpenIntelStoreForServePropagatesSkippedIntegrityPolicy(t *testing.T) {
	vaultPath := t.TempDir()
	indexPath := filepath.Join(vaultPath, ".rhizome", "db.sqlite")
	store, err := semdb.Open(indexPath)
	requireNoError(t, err)
	requireNoError(t, store.Close())

	collector := indexingperf.New()
	rt := &LiveRuntime{
		ctx:                     indexingperf.WithCollector(context.Background(), collector),
		VaultPath:               vaultPath,
		skipIntelIntegrityCheck: true,
	}
	store, cleanup, err := rt.openIntelStoreForServe(codeanchor.Config{IndexPath: indexPath})
	requireNoError(t, err)
	cleanup()
	if store == nil {
		t.Fatal("expected intel store")
	}

	for _, operation := range collector.AgentStartDiagnostics().Operations {
		if operation.Label == indexingperf.AgentStartOpIntegrityChecks {
			if operation.Available || operation.Count != 0 {
				t.Fatalf("skipped integrity check must be unavailable with zero count: %+v", operation)
			}
			return
		}
	}
	t.Fatal("integrity diagnostic missing")
}

func TestOpenIntelStoreForServeReadOnlyDoesNotCreateOrExposeSessionWrites(t *testing.T) {
	vaultPath := t.TempDir()
	indexPath := filepath.Join(vaultPath, ".rhizome", "db.sqlite")
	store, err := semdb.Open(indexPath)
	requireNoError(t, err)
	requireNoError(t, store.Close())

	rt := &LiveRuntime{
		ctx:               context.Background(),
		VaultPath:         vaultPath,
		readOnlyCodeIndex: true,
	}
	store, cleanup, err := rt.openIntelStoreForServe(codeanchor.Config{IndexPath: indexPath})
	requireNoError(t, err)
	t.Cleanup(cleanup)
	if store == nil {
		t.Fatal("expected intel store")
	}
	if err := store.EnsureSession(context.Background(), "session"); err == nil {
		t.Fatal("read-only startup store must reject writes")
	}
}

func TestPublishWarmIntelStoreForReadPublishesValidatedExistingStore(t *testing.T) {
	vaultPath := t.TempDir()
	indexPath := filepath.Join(vaultPath, ".rhizome", "db.sqlite")
	store, err := semdb.Open(indexPath)
	requireNoError(t, err)
	requireNoError(t, store.Close())

	rt := &LiveRuntime{ctx: context.Background(), VaultPath: vaultPath}
	rt.publishWarmIntelStoreForRead(codeanchor.Config{IndexPath: indexPath})
	warm := rt.IntelStore()
	if warm == nil {
		t.Fatal("expected warm intel read store")
	}
	if err := warm.EnsureSession(context.Background(), "session"); err == nil {
		t.Fatal("warm intel store must remain read-only")
	}
	requireNoError(t, rt.Close())
}

func TestSnapshotDoesNotExposeTypedNilSessionStore(t *testing.T) {
	rt := &LiveRuntime{}
	if rt.Snapshot().SessionStore != nil {
		t.Fatal("uninitialized session persistence must be absent")
	}
}

func TestSnapshotPublishesCacheBackedNoteReaderWithoutInitializingCache(t *testing.T) {
	rt := &LiveRuntime{}
	if rt.Snapshot().NoteReader != nil {
		t.Fatal("uninitialized note reader must be absent")
	}
	cacheService, err := cache.NewService(t.TempDir(), cache.Options{})
	requireNoError(t, err)
	version := cacheService.Version()
	rt.cache.Store(cacheService)

	snapshot := rt.Snapshot()
	if snapshot.NoteReader == nil {
		t.Fatal("published cache must expose a note reader")
	}
	if _, ok := snapshot.NoteReader.(*cache.NoteAdapter); !ok {
		t.Fatalf("note reader type = %T, want *cache.NoteAdapter", snapshot.NoteReader)
	}
	if cacheService.Version() != version {
		t.Fatal("taking a runtime snapshot must not initialize or refresh the cache")
	}
}

func TestLiveRuntimeSessionCleanupOwnershipFollowsSessionStoreOption(t *testing.T) {
	tests := []struct {
		name                string
		readOnlyCodeIndex   bool
		disableSessionStore bool
		want                bool
	}{
		{name: "default write runtime", want: true},
		{name: "session store disabled", disableSessionStore: true},
		{name: "read-only code index", readOnlyCodeIndex: true},
		{name: "read-only code index with session store disabled", readOnlyCodeIndex: true, disableSessionStore: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &LiveRuntime{
				readOnlyCodeIndex:   tt.readOnlyCodeIndex,
				disableSessionStore: tt.disableSessionStore,
			}
			if got := rt.ownsSessionCleanup(); got != tt.want {
				t.Fatalf("session cleanup ownership = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunSessionCleanupIfDueDoesNotWriteOnOrdinaryChecks(t *testing.T) {
	store, err := semdb.Open(filepath.Join(t.TempDir(), "session-cleanup.db"))
	requireNoError(t, err)
	defer func() { _ = store.Close() }()

	collector := indexingperf.New()
	ctx := indexingperf.WithCollector(context.Background(), collector)
	now := time.Unix(1000, 0)
	_, ran, err := runSessionCleanupIfDue(ctx, store, now)
	requireNoError(t, err)
	if !ran {
		t.Fatal("initial due cleanup should run")
	}
	_, ran, err = runSessionCleanupIfDue(ctx, store, now)
	requireNoError(t, err)
	if ran {
		t.Fatal("ordinary not-due check must not claim or clean")
	}

	var transactions int64
	for _, operation := range collector.AgentStartDiagnostics().Operations {
		if operation.Label == indexingperf.AgentStartOpTransactions {
			transactions = operation.Count
		}
	}
	if transactions != 2 {
		t.Fatalf("due cleanup should use one claim and one cleanup transaction; got %d", transactions)
	}
}

func TestRunSessionCleanupIfDueRelinquishesFailedClaim(t *testing.T) {
	store, err := semdb.Open(filepath.Join(t.TempDir(), "session-cleanup-error.db"))
	requireNoError(t, err)
	defer func() { _ = store.Close() }()

	now := time.Unix(1000, 0)
	cleanupErr := errors.New("cleanup failed")
	_, ran, err := runSessionCleanupIfDueWith(context.Background(), store, now, func(context.Context, time.Time) (int64, error) {
		return 0, cleanupErr
	})
	if !ran || !errors.Is(err, cleanupErr) {
		t.Fatalf("failed cleanup mismatch: ran=%v err=%v", ran, err)
	}

	_, ran, err = runSessionCleanupIfDueWith(context.Background(), store, now, func(context.Context, time.Time) (int64, error) {
		return 0, nil
	})
	requireNoError(t, err)
	if !ran {
		t.Fatal("failed cleanup must relinquish its claim for immediate retry")
	}
}

func TestRunSessionCleanupIfDueRelinquishesWithCanceledCaller(t *testing.T) {
	store, err := semdb.Open(filepath.Join(t.TempDir(), "session-cleanup-canceled.db"))
	requireNoError(t, err)
	defer func() { _ = store.Close() }()

	now := time.Unix(1000, 0)
	ctx, cancel := context.WithCancel(context.Background())
	_, ran, err := runSessionCleanupIfDueWith(ctx, store, now, func(ctx context.Context, _ time.Time) (int64, error) {
		cancel()
		return 0, ctx.Err()
	})
	if !ran || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled cleanup mismatch: ran=%v err=%v", ran, err)
	}

	due, err := store.SessionCleanupDue(context.Background(), now)
	requireNoError(t, err)
	if !due {
		t.Fatal("claim relinquish must survive cancellation of the cleanup context")
	}
}

func TestLiveRuntimeSnapshotKeepsCapabilityPhasesIndependent(t *testing.T) {
	rt := &LiveRuntime{
		searchReady:   make(chan struct{}),
		semanticReady: make(chan struct{}),
		codeReady:     make(chan struct{}),
	}

	cold := rt.Snapshot()
	if cold.Search.Done || cold.Semantic.Done || cold.Code.Done {
		t.Fatalf("cold snapshot must not report ready phases: %#v", cold)
	}

	close(rt.searchReady)
	close(rt.semanticReady)
	rt.leaderFlag.Store(true)
	partial := rt.Snapshot()
	if !partial.Search.Ready || !partial.Semantic.Ready {
		t.Fatalf("search and semantic should be independently ready: %#v", partial)
	}
	if partial.Code.Done || partial.Code.Ready {
		t.Fatalf("code must remain pending after semantic readiness: %#v", partial.Code)
	}
	if !partial.Leader {
		t.Fatal("leader state should be reported independently")
	}

	codeErr := errors.New("code unavailable")
	rt.codeErr.Store(&codeErr)
	close(rt.codeReady)
	failed := rt.Snapshot()
	if !failed.Code.Done || failed.Code.Ready || !errors.Is(failed.Code.Err, codeErr) {
		t.Fatalf("failed code phase mismatch: %#v", failed.Code)
	}
	if !failed.Semantic.Ready {
		t.Fatal("code failure must not downgrade semantic readiness")
	}
}

func TestLiveRuntimeBecomeLeaderPublishesOwnership(t *testing.T) {
	rt := &LiveRuntime{leaderCh: make(chan struct{})}

	rt.becomeLeader()

	if !rt.IsLeader() || !rt.ElectionWon() {
		t.Fatalf("expected the runtime to own the vault")
	}
	select {
	case <-rt.leaderCh:
	default:
		t.Fatalf("expected leader channel to be closed")
	}
	// becomeLeader is idempotent: a second call must not re-close the channel.
	rt.becomeLeader()
}

func TestLiveRuntimeCloseCancelsRuntimeContext(t *testing.T) {
	runtimeCtx, cancel := context.WithCancel(context.Background())
	closerCalls := 0
	rt := &LiveRuntime{
		ctx:       runtimeCtx,
		cancelCtx: cancel,
		closers: []func(){
			func() { closerCalls++ },
		},
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	select {
	case <-rt.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("runtime context was not canceled")
	}
	if closerCalls != 1 {
		t.Fatalf("expected closers once, got %d", closerCalls)
	}
}

func TestLiveRuntimeAddCloserAfterCloseRunsImmediately(t *testing.T) {
	runtimeCtx, cancel := context.WithCancel(context.Background())
	calls := 0
	rt := &LiveRuntime{
		ctx:       runtimeCtx,
		cancelCtx: cancel,
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	rt.addCloser(func() { calls++ })

	if calls != 1 {
		t.Fatalf("expected closer to run immediately after close, got %d", calls)
	}
}

func TestLiveSQLitePoolOptionsKeepsOneIdleConnection(t *testing.T) {
	opts := liveSQLitePoolOptions()
	if opts.MaxOpenConns != 4 {
		t.Fatalf("expected MaxOpenConns 4, got %d", opts.MaxOpenConns)
	}
	if opts.MaxIdleConns != 1 {
		t.Fatalf("expected MaxIdleConns 1, got %d", opts.MaxIdleConns)
	}
	if opts.ConnMaxIdleTime != 5*time.Minute {
		t.Fatalf("expected ConnMaxIdleTime 5m, got %s", opts.ConnMaxIdleTime)
	}
}

func TestRuntimeRequirementsDefaultToFullRuntime(t *testing.T) {
	var requirements RuntimeRequirements
	for _, capability := range []RuntimeCapability{
		RuntimeCapabilitySearch,
		RuntimeCapabilitySemantic,
		RuntimeCapabilityCodeIndex,
		RuntimeCapabilityCodeAnchorWarmup,
		RuntimeCapabilityCodeEmbeddings,
		RuntimeCapabilityLeaderSyncers,
		RuntimeCapabilityCodeRefDiscovery,
	} {
		if !requirements.Includes(capability) {
			t.Fatalf("zero-value runtime requirements must include %q", capability)
		}
	}
}

func TestRuntimeRequirementsCanSelectOneShotCapabilities(t *testing.T) {
	requirements := RequireRuntimeCapabilities(RuntimeCapabilitySearch, RuntimeCapabilityCodeIndex)
	if !requirements.Includes(RuntimeCapabilitySearch) || !requirements.Includes(RuntimeCapabilityCodeIndex) {
		t.Fatal("selected one-shot capabilities must be included")
	}
	for _, omitted := range []RuntimeCapability{
		RuntimeCapabilitySemantic,
		RuntimeCapabilityCodeAnchorWarmup,
		RuntimeCapabilityCodeEmbeddings,
		RuntimeCapabilityLeaderSyncers,
		RuntimeCapabilityCodeRefDiscovery,
	} {
		if requirements.Includes(omitted) {
			t.Fatalf("one-shot requirements unexpectedly include %q", omitted)
		}
	}
}

func TestCapabilityNotRequestedErrorIsTypedAndSentinelMatchable(t *testing.T) {
	tests := []struct {
		name       string
		capability RuntimeCapability
		message    string
	}{
		{name: "search", capability: RuntimeCapabilitySearch, message: "search capability not requested"},
		{name: "semantic", capability: RuntimeCapabilitySemantic, message: "semantic capability not requested"},
		{name: "code index", capability: RuntimeCapabilityCodeIndex, message: "code index capability not requested"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := capabilityNotRequested(tt.capability)
			if err.Error() != tt.message {
				t.Fatalf("message mismatch: got %q want %q", err.Error(), tt.message)
			}
			if !errors.Is(err, ErrCapabilityNotRequested{Capability: tt.capability}) {
				t.Fatalf("error must match its capability sentinel: %v", err)
			}
			if !errors.Is(err, ErrCapabilityNotRequested{}) {
				t.Fatalf("error must match the generic not-requested sentinel: %v", err)
			}
			var typed ErrCapabilityNotRequested
			if !errors.As(err, &typed) || typed.Capability != tt.capability {
				t.Fatalf("typed error mismatch: %#v", typed)
			}
		})
	}
}

func TestExplicitlyOmittedRuntimePhasesReturnCapabilityNotRequested(t *testing.T) {
	rt := &LiveRuntime{
		ctx:               context.Background(),
		disableLeaderWork: true,
		requirements:      RequireRuntimeCapabilities(),
		searchReady:       make(chan struct{}),
		semanticReady:     make(chan struct{}),
		codeReady:         make(chan struct{}),
		leaderCh:          make(chan struct{}),
	}
	rt.initPhase1Search()

	tests := []struct {
		name       string
		capability RuntimeCapability
		wait       func(context.Context) error
	}{
		{name: "search", capability: RuntimeCapabilitySearch, wait: rt.WaitForSearch},
		{name: "semantic", capability: RuntimeCapabilitySemantic, wait: rt.WaitForSemantic},
		{name: "code index", capability: RuntimeCapabilityCodeIndex, wait: rt.WaitForCodeIndex},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.wait(context.Background())
			if !errors.Is(err, ErrCapabilityNotRequested{Capability: tt.capability}) {
				t.Fatalf("wait error must identify omitted %q capability: %v", tt.capability, err)
			}
		})
	}
}

func TestIndexedReadOnlyRuntimeOptionsSelectOnlyExistingCodeIndex(t *testing.T) {
	opts := IndexedReadOnlyRuntimeOptions(LiveOptions{
		VaultName: "vault",
		Debug:     true,
	})

	if opts.VaultName != "vault" || !opts.Debug {
		t.Fatalf("caller-owned options must be preserved: %#v", opts)
	}
	if !opts.SkipCacheWarmup || !opts.DisableWatchHub || !opts.DisableLeaderWork {
		t.Fatalf("indexed one-shot runtime must disable global background work: %#v", opts)
	}
	if !opts.SkipIntelIntegrityCheck || !opts.ReadOnlyCodeIndex || opts.DisableSessionStore {
		t.Fatalf("indexed one-shot runtime must use the validated read-only open: %#v", opts)
	}
	if !opts.Requirements.Includes(RuntimeCapabilityCodeIndex) {
		t.Fatal("indexed one-shot runtime must request the code index")
	}
	for _, omitted := range []RuntimeCapability{
		RuntimeCapabilitySearch,
		RuntimeCapabilitySemantic,
		RuntimeCapabilityCodeAnchorWarmup,
		RuntimeCapabilityCodeEmbeddings,
		RuntimeCapabilityLeaderSyncers,
		RuntimeCapabilityCodeRefDiscovery,
	} {
		if opts.Requirements.Includes(omitted) {
			t.Fatalf("indexed one-shot runtime unexpectedly includes %q", omitted)
		}
	}
}

func TestQueryEmbeddingConfigsCompatible(t *testing.T) {
	base := embeddings.Config{Provider: "voyage", Model: "voyage-4-lite", Endpoint: "https://api.voyageai.com/v1/embeddings/", Dimensions: 1024}
	if !queryEmbeddingConfigsCompatible(base, embeddings.Config{Provider: "VOYAGE", Model: "voyage-4-lite", Endpoint: "https://api.voyageai.com/v1/embeddings", Dimensions: 1024}) {
		t.Fatal("equivalent provider configuration should share a query provider")
	}
	if queryEmbeddingConfigsCompatible(base, embeddings.Config{Provider: "voyage", Model: "voyage-4-lite", Endpoint: "https://api.voyageai.com/v1/embeddings", Dimensions: 0}) {
		t.Fatal("an explicit dimension must not share with a provider using its model default")
	}
	base.Dimensions = 0
	if !queryEmbeddingConfigsCompatible(base, embeddings.Config{Provider: "voyage", Model: "voyage-4-lite", Endpoint: "https://api.voyageai.com/v1/embeddings", Dimensions: 0}) {
		t.Fatal("matching model-default dimensions should share a query provider")
	}
	if queryEmbeddingConfigsCompatible(base, embeddings.Config{Provider: "voyage", Model: "voyage-3-lite", Endpoint: "https://api.voyageai.com/v1/embeddings", Dimensions: 1024}) {
		t.Fatal("different models must not share a query provider")
	}
	if queryEmbeddingConfigsCompatible(base, embeddings.Config{Provider: "voyage", Model: "voyage-4-lite", Endpoint: "https://other.example/v1/embeddings", Dimensions: 1024}) {
		t.Fatal("different endpoints must not share a query provider")
	}
}

func TestShouldStartLegacyCodeAnchorSubscriber(t *testing.T) {
	if shouldStartLegacyCodeAnchorSubscriber(true) {
		t.Fatalf("legacy codeanchor subscriber should stay off when unified watcher is available")
	}
	if !shouldStartLegacyCodeAnchorSubscriber(false) {
		t.Fatalf("legacy codeanchor subscriber should remain fallback when unified watcher is unavailable")
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
