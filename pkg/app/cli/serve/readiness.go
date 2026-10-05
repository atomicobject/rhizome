// Package serve coordinates long-running server application behavior below
// the Cobra command adapter.
package serve

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/app/validationproduct"
	"github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const (
	readinessPollInterval        = 250 * time.Millisecond
	noteReadinessMaxPollInterval = 2 * time.Second
)

type modelCountFunc func(context.Context) (int64, error)
type liveModelCountFunc func(context.Context, *bootstrap.LiveRuntime) (int64, error)

type validationRefresher interface {
	Refresh(context.Context) error
}

// ReadinessCoordinator owns the serve startup gates across live-runtime,
// background-index, and web-server publication.
type ReadinessCoordinator struct {
	runIndex           bootstrap.BackgroundIndexer
	usableNoteCount    liveModelCountFunc
	completeModelCount liveModelCountFunc

	mu                        sync.Mutex
	workers                   sync.WaitGroup
	draining                  bool
	ctx                       context.Context
	live                      *bootstrap.LiveRuntime
	webRuntime                *web.Runtime
	livePublished             chan struct{}
	livePublishedOnce         sync.Once
	indexAttemptDone          chan struct{}
	indexAttemptDoneOnce      sync.Once
	server                    atomic.Pointer[web.Server]
	validation                validationRefresher
	validationRequests        chan context.Context
	validationRefreshPending  bool
	validationAdmission       context.Context
	cancelValidationAdmission context.CancelFunc
	validationWorkerOnce      sync.Once
	repairReviews             *validate.RepairReviewStore
	repairPaths               *validate.RepairPathCoordinator
}

// NewReadinessCoordinator creates the readiness lifecycle used by one serve
// invocation. runIndex performs the actual indexing work after warm-state
// probes have completed.
func NewReadinessCoordinator(runIndex bootstrap.BackgroundIndexer) *ReadinessCoordinator {
	return &ReadinessCoordinator{
		runIndex:           runIndex,
		usableNoteCount:    runtimeUsableNoteCount,
		completeModelCount: runtimeCompleteModelCount,
		livePublished:      make(chan struct{}),
		indexAttemptDone:   make(chan struct{}),
		repairPaths:        validate.NewRepairPathCoordinator(),
		validationRequests: make(chan context.Context, 1),
	}
}

// BackgroundIndexer is the callback passed to bootstrap.NewLiveRuntime. It can
// start before BindLive returns from runtime construction, so it waits for live
// publication before probing or indexing.
func (c *ReadinessCoordinator) BackgroundIndexer(ctx context.Context, noteMetadata notemeta.Indexer, vaultPath string, vaultDef obsidian.VaultDefinition, debug bool) error {
	if c == nil {
		return nil
	}
	validationCtx, releaseValidation := c.validationRequestContext(ctx)
	defer releaseValidation()
	defer c.indexAttemptDoneOnce.Do(func() { close(c.indexAttemptDone) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.livePublished:
	}

	c.mu.Lock()
	live := c.live
	c.mu.Unlock()
	if live == nil {
		return nil
	}
	if count, err := c.usableNoteCount(ctx, live); err == nil && count > 0 {
		c.markNoteReadReady()
	}
	// Snapshot the complete model before the local indexer can mutate it. A
	// count after a partial rebuild could admit an incomplete model.
	if count, err := c.completeModelCount(ctx, live); err == nil && count > 0 {
		c.markIndexReady()
	}

	var indexErr error
	if c.runIndex != nil {
		indexErr = c.runIndexOnLane(ctx, live, noteMetadata, vaultPath, vaultDef, debug)
	}
	// Validation may refresh metadata and ontology. Admit it only after the
	// boot writer (or the explicit index that displaced it) has completed.
	c.indexAttemptDoneOnce.Do(func() { close(c.indexAttemptDone) })
	if c.server.Load() != nil {
		live.PublishGlobalEvent(web.GlobalEventIndexChanged, map[string]string{"source": "background"})
	}
	if indexErr == nil {
		c.refreshValidation(validationCtx, live)
	}
	if indexErr == nil {
		c.markIndexReady()
	}
	return indexErr
}

// runIndexOnLane runs the boot catch-up index as a KindBootCatchUp job on the
// runtime's indexing lane, which owns .rhizome/index.lock (SPEC-0104 US3). A
// catch-up displaced by an explicit index that completes is done (that index
// did its work); one displaced by an explicit index that failed or was
// cancelled, or by an external priority request, is resubmitted (bounded).
// Without a lane (one-shot runtimes, tests) the index runs directly.
func (c *ReadinessCoordinator) runIndexOnLane(ctx context.Context, live *bootstrap.LiveRuntime, noteMetadata notemeta.Indexer, vaultPath string, vaultDef obsidian.VaultDefinition, debug bool) error {
	indexLane := live.Lane()
	if indexLane == nil {
		return c.runIndex(ctx, noteMetadata, vaultPath, vaultDef, debug)
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		handle, _, submitErr := indexLane.Submit(ctx, lane.Request{
			Kind:     lane.KindBootCatchUp,
			Coalesce: true,
			Run: func(jobCtx context.Context, _ lane.Reporter) error {
				return c.runIndex(jobCtx, noteMetadata, vaultPath, vaultDef, debug)
			},
		})
		if submitErr != nil {
			return submitErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-handle.Done():
		}
		err = handle.Err()
		var preempted *lane.PreemptedError
		if errors.As(err, &preempted) && preempted.By != nil {
			// An explicit index displaced the catch-up. If that index completes,
			// it did the catch-up's work and re-running would only make indexed
			// reads unavailable again; if it was cancelled or failed, the vault
			// still needs catching up, so resubmit.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-preempted.By.Done():
			}
			if preempted.By.Err() == nil {
				return nil
			}
			continue
		}
		if err == nil || !errors.Is(err, context.Canceled) {
			return err
		}
	}
	return err
}

// BindLive publishes runtime construction, creates the gated web runtime, and
// starts the post-index observer. Call it even when live construction fails so
// an early background callback can exit.
func (c *ReadinessCoordinator) BindLive(ctx context.Context, live *bootstrap.LiveRuntime) *web.Runtime {
	if c == nil {
		return nil
	}
	var webRuntime *web.Runtime
	if live != nil {
		webRuntime = &web.Runtime{Live: live}
		webRuntime.SetLiveHealthProvider(live)
		webRuntime.SetRepairPathCoordinator(c.repairPaths)
		webRuntime.SetValidationRefreshRequester(c)
		webRuntime.EnableIndexGate()
	}

	c.mu.Lock()
	c.ctx = ctx
	if c.cancelValidationAdmission != nil {
		context.AfterFunc(ctx, c.cancelValidationAdmission)
	}
	c.live = live
	c.webRuntime = webRuntime
	c.mu.Unlock()
	c.livePublishedOnce.Do(func() { close(c.livePublished) })
	c.validationWorkerOnce.Do(func() { c.startWorker(func() { c.runValidationRefreshRequests(ctx) }) })

	if webRuntime == nil {
		return nil
	}
	c.startWorker(func() { c.observeCompleteModelAfterIndex(ctx, live, webRuntime) })
	return webRuntime
}

// GlobalEventSink invalidates retained validation and earlier requests before
// notifying the browser. Failed reconciliation admits no replacement request.
func (c *ReadinessCoordinator) GlobalEventSink() bootstrap.GlobalEventSink {
	if c == nil {
		return nil
	}
	return func(kind string, data any) {
		if kind == web.GlobalEventValidationInvalidated {
			c.mu.Lock()
			if c.cancelValidationAdmission != nil {
				c.cancelValidationAdmission()
			}
			c.validationAdmission = nil
			c.cancelValidationAdmission = nil
			if c.live != nil && c.live.IntelStore() != nil {
				if _, err := c.live.IntelStore().MarkPublishedValidationStale(context.Background(), "validation inputs changed"); err != nil {
					recordServeDiagnostic(c.liveContext(context.Background()), slog.LevelWarn, "validation.invalidate_failed", err)
				}
			}
			c.server.Load().NotifyGlobalEvent(kind, data)
			if web.GlobalEventReason(data) != web.GlobalEventReasonReconcileFailed {
				c.requestValidationRefreshLocked()
			}
			c.mu.Unlock()
			return
		}
		c.server.Load().NotifyGlobalEvent(kind, data)
	}
}

// BindServer enables post-index event and validation publication once the web
// server exists.
func (c *ReadinessCoordinator) BindServer(server *web.Server) {
	if c != nil {
		c.server.Store(server)
	}
}

func (c *ReadinessCoordinator) markIndexReady() {
	c.mu.Lock()
	runtime := c.webRuntime
	c.mu.Unlock()
	if runtime != nil {
		runtime.MarkIndexReady()
	}
}

func (c *ReadinessCoordinator) markNoteReadReady() {
	c.mu.Lock()
	runtime := c.webRuntime
	c.mu.Unlock()
	if runtime != nil {
		runtime.MarkNoteReadReady()
	}
}

func (c *ReadinessCoordinator) observeCompleteModelAfterIndex(ctx context.Context, live *bootstrap.LiveRuntime, runtime *web.Runtime) {
	select {
	case <-ctx.Done():
		return
	case <-c.indexAttemptDone:
	}
	// The local writer is finished. A populated ontology model is usable even
	// if a later code-index stage failed or this run deferred to another process.
	waitForUsableIndexModel(ctx, runtime, readinessPollInterval, func(ctx context.Context) (int64, error) {
		return c.completeModelCount(ctx, live)
	})
}

func runtimeCompleteModelCount(ctx context.Context, live *bootstrap.LiveRuntime) (int64, error) {
	if live == nil {
		return 0, nil
	}
	store := live.IntelStore()
	if store == nil {
		return 0, nil
	}
	return store.OntologyNodeCount(ctx)
}

func runtimeUsableNoteCount(ctx context.Context, live *bootstrap.LiveRuntime) (int64, error) {
	if live == nil {
		return 0, nil
	}
	return live.UsableNoteMetadataCount(ctx)
}

// Bridge connects asynchronously published live capabilities to the web
// runtime and starts the validation work that depends on the complete model.
func (c *ReadinessCoordinator) Bridge(ctx context.Context) {
	if c == nil {
		return
	}
	validationCtx, releaseValidation := c.validationRequestContext(ctx)
	defer releaseValidation()
	c.mu.Lock()
	live := c.live
	webRuntime := c.webRuntime
	c.mu.Unlock()
	if live == nil || webRuntime == nil {
		return
	}
	// Shared metadata can become usable while this process is still bringing
	// up search, semantic, and code capabilities. Observe it immediately so
	// exact note navigation does not wait for unrelated startup phases.
	markUsableNoteModel(ctx, webRuntime, func(ctx context.Context) (int64, error) {
		return c.usableNoteCount(ctx, live)
	})
	// Both gates can also be opened by a model this process did not build (a
	// previous runtime for this vault already materialized it), so keep polling
	// them independently of local startup phases.
	c.startWorker(func() {
		waitForUsableNoteModel(ctx, webRuntime, readinessPollInterval, func(ctx context.Context) (int64, error) {
			return c.usableNoteCount(ctx, live)
		})
	})
	c.startWorker(func() {
		waitForUsableIndexModel(ctx, webRuntime, readinessPollInterval, func(ctx context.Context) (int64, error) {
			return c.completeModelCount(ctx, live)
		})
	})
	if err := live.WaitForSearch(ctx); err == nil {
		// Notify the server so it can attach its subscription. WatchHubRef reads
		// the live snapshot and does not retain a copied capability.
		webRuntime.SetWatchHub(live.Snapshot().WatchHub)
	}
	if codeErr := live.WaitForCodeIndex(ctx); codeErr == nil {
		c.refreshValidation(validationCtx, live)
	}
}

func markUsableNoteModel(ctx context.Context, runtime *web.Runtime, count modelCountFunc) bool {
	if runtime == nil || count == nil {
		return false
	}
	countValue, err := count(ctx)
	if err != nil || countValue == 0 {
		return false
	}
	runtime.MarkNoteReadReady()
	return true
}

// waitForUsableNoteModel opens the note-read gate as soon as durable exact-note
// rows exist, backing off so a cold vault does not repeat a full scan every
// quarter second.
func waitForUsableNoteModel(ctx context.Context, runtime *web.Runtime, interval time.Duration, count modelCountFunc) bool {
	if runtime == nil || count == nil {
		return false
	}
	if interval <= 0 {
		interval = readinessPollInterval
	}
	if interval > noteReadinessMaxPollInterval {
		interval = noteReadinessMaxPollInterval
	}
	pollInterval := interval
	for {
		if runtime.NoteReadReady() {
			return true
		}
		countValue, err := count(ctx)
		if err == nil && countValue > 0 {
			runtime.MarkNoteReadReady()
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(pollInterval):
		}
		pollInterval = nextNoteReadinessPollInterval(pollInterval)
	}
}

func nextNoteReadinessPollInterval(current time.Duration) time.Duration {
	if current >= noteReadinessMaxPollInterval {
		return current
	}
	next := current * 2
	if next > noteReadinessMaxPollInterval || next < current {
		return noteReadinessMaxPollInterval
	}
	return next
}

// waitForUsableIndexModel opens the index gate once a complete ontology model
// is readable, whoever produced it.
func waitForUsableIndexModel(ctx context.Context, runtime *web.Runtime, interval time.Duration, count modelCountFunc) bool {
	if runtime == nil || count == nil {
		return false
	}
	if interval <= 0 {
		interval = readinessPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if runtime.IndexReady() {
			return true
		}
		n, err := count(ctx)
		if err == nil && n > 0 {
			runtime.MarkIndexReady()
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// refreshValidation installs the production authority lazily, after the live
// intel store is available, then refreshes one generation for every startup or
// watcher invalidation trigger.
func (c *ReadinessCoordinator) refreshValidation(ctx context.Context, live *bootstrap.LiveRuntime) {
	if c == nil || live == nil || ctx.Err() != nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-c.indexAttemptDone:
	}
	if ctx.Err() != nil {
		return
	}
	c.mu.Lock()
	refresher := c.validation
	if refresher == nil {
		store := live.IntelStore()
		if store != nil {
			if c.repairReviews == nil {
				c.repairReviews = validate.NewRepairReviewStore(validate.RepairReviewStoreOptions{PathReservations: c.repairPaths})
			}
			coordinator := validationproduct.NewRefreshCoordinator(validationproduct.RefreshCoordinatorOptions{
				Context: c.liveContext(ctx), Store: store, VaultDef: live.VaultDef,
				NoteMetadata: live.NoteMetadataIndexer(), RepairStore: c.repairReviews, MaxIssues: 500,
				Run: func(ctx context.Context) (validationproduct.AuthoritativeRun, error) {
					return validationproduct.RunLiveOnLane(ctx, live.NoteMetadataIndexer(), live.VaultDef, 500, live.Lane())
				},
				ApplyOptions: validate.Options{PostApplyRefresher: indexing.ValidationProjectionPostApplyRefresher{
					VaultPath: live.VaultPath, VaultDef: live.VaultDef,
					NoteMetadata: live.NoteMetadataIndexer(), NoteReader: &obsidian.Note{},
				}},
			})
			c.validation = coordinator
			refresher = coordinator
			if c.webRuntime != nil {
				c.webRuntime.SetValidationRepairAuthority(coordinator)
			}
		}
	}
	c.mu.Unlock()
	if refresher == nil {
		recordServeDiagnostic(ctx, slog.LevelInfo, "validation.refresh_skipped", errors.New("index store unavailable"))
		return
	}
	if err := refresher.Refresh(ctx); err != nil {
		if !errors.Is(err, validationproduct.ErrRefreshSuperseded) && !errors.Is(err, context.Canceled) {
			recordServeDiagnostic(ctx, slog.LevelWarn, "validation.refresh_failed", err)
		}
		// Errors also change the cached validation envelope. Publish them so
		// browsers stop showing the invalidated result or a running spinner.
		live.PublishGlobalEvent(web.GlobalEventValidateChanged, nil)
		return
	}
	live.PublishGlobalEvent(web.GlobalEventValidateChanged, nil)
}

func (c *ReadinessCoordinator) liveContext(fallback context.Context) context.Context {
	if c != nil && c.ctx != nil {
		return c.ctx
	}
	return fallback
}
