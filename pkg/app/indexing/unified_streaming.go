package indexing

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeemb "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/paths"

	"github.com/atomicobject/rhizome/pkg/embedindex"
)

const (
	unifiedStreamingQueueSize = 128
	unifiedStreamingBatchIdle = 75 * time.Millisecond
	// Early code work is intentionally coalesced before it enters the shared
	// embed lane. Very small streamed batches improved overlap but regressed
	// provider packing and created extra writer churn, so the default here
	// favors throughput over “embed immediately after every file”.
	unifiedEarlyCodeTaskFlush = 768
	unifiedEarlyCodeMaxWait   = 250 * time.Millisecond
)

// shouldFlushEarlyPrepared decides when call-insensitive code embedding work is
// large or old enough to enter the provider lane before final call-edge rebuild.
func shouldFlushEarlyPrepared(taskCount int, queuedAt time.Time, force bool, now time.Time) bool {
	if taskCount <= 0 {
		return false
	}
	if force || taskCount >= unifiedEarlyCodeTaskFlush {
		return true
	}
	if queuedAt.IsZero() {
		return false
	}
	return now.Sub(queuedAt) >= unifiedEarlyCodeMaxWait
}

// codeStreamingBatch carries source snapshots into semantic preparation and
// persisted paths into cleanup, without repeating structural publication.
type codeStreamingBatch struct {
	indexedPaths    []string
	prePersistWorks []codeanchor.CodeIndexWork
}

// noteStreamingBatch carries changed/deleted note paths into the note semantic
// lane. In ontology-ready vaults this means ontology body chunks, not raw
// authored-section embeddings.
type noteStreamingBatch struct {
	changedPaths []string
	deletedPaths []string
	fullSync     bool
}

// unifiedStreamingCoordinator overlaps semantic preparation and provider work.
// Structural publication and graph rebuilds belong to indexcore.
type unifiedStreamingCoordinator struct {
	ctx     context.Context
	cancel  context.CancelFunc
	closing bool
	started bool

	mu            sync.Mutex
	err           error
	closeOnce     sync.Once
	noteProcessed bool

	codeCh   chan codeStreamingBatch
	noteCh   chan noteStreamingBatch
	codeDone chan struct{}
	wg       sync.WaitGroup

	prepareCodePaths  func(context.Context, []string) (embedindex.PreparedCodePaths, error)
	prepareCodeWorks  func(context.Context, []codeanchor.CodeIndexWork) (embedindex.PreparedCodePaths, error)
	planEarlyCode     func(context.Context, embedindex.PreparedCodePaths) (embedindex.SyncPlan, error)
	planPreparedCode  func(context.Context, embedindex.PreparedCodePaths) (embedindex.SyncPlan, error)
	planDeferredCode  func(context.Context, embedindex.PreparedCodePaths) (embedindex.SyncPlan, error)
	embedCodePlan     func(context.Context, embedindex.SyncPlan) error
	markCodeLastSync  func(context.Context) error
	syncNotePaths     func(context.Context, []string) error
	syncNoteBodyPaths func(context.Context, []string, []string, bool) error
	markNoteLastSync  func(context.Context) error
}

func newUnifiedStreamingCoordinator(
	ctx context.Context,
	cancel context.CancelFunc,
	codeSyncer *embedindex.Syncer,
	noteSyncer *embedindex.NoteSyncer,
) *unifiedStreamingCoordinator {
	c := &unifiedStreamingCoordinator{
		ctx: ctx, cancel: cancel,
	}
	if codeSyncer != nil {
		c.codeCh = make(chan codeStreamingBatch, unifiedStreamingQueueSize)
		c.codeDone = make(chan struct{})
		c.prepareCodePaths = codeSyncer.PreparePathsEarly
		c.prepareCodeWorks = codeSyncer.PrepareCodeIndexWorksEarly
		c.planEarlyCode = codeSyncer.PlanPreparedPathsEarly
		c.planPreparedCode = codeSyncer.PlanPreparedPaths
		c.planDeferredCode = codeSyncer.PlanPreparedPathsDeferred
		c.embedCodePlan = codeSyncer.EmbedWithoutSyncMark
		c.markCodeLastSync = codeSyncer.MarkLastSync
	}
	if noteSyncer != nil {
		c.noteCh = make(chan noteStreamingBatch, unifiedStreamingQueueSize)
		c.syncNotePaths = func(stageCtx context.Context, paths []string) error {
			paths = normalizeStringSet(paths)
			if len(paths) == 0 {
				return nil
			}
			var plan embedindex.NotePlan
			if err := runPhase(stageCtx, "plan_note_embeddings", func(phaseCtx context.Context) error {
				var err error
				plan, err = noteSyncer.PlanPaths(phaseCtx, paths)
				return err
			}); err != nil {
				return err
			}
			return runPhase(stageCtx, "embed_notes", func(phaseCtx context.Context) error {
				return noteSyncer.EmbedWithoutSyncMark(phaseCtx, plan)
			})
		}
		c.markNoteLastSync = noteSyncer.MarkLastSync
	}
	return c
}

// Start launches workers only after the coordinator's production callbacks
// and progress sinks have been configured. CloseAndWait before Start is a
// supported no-op shutdown path for early RunUnifiedCore failures.
func (c *unifiedStreamingCoordinator) Start() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.started || c.closing {
		c.mu.Unlock()
		return
	}
	c.started = true
	startCode := c.codeCh != nil
	startNotes := c.noteCh != nil
	if startCode {
		c.wg.Add(1)
	}
	if startNotes {
		c.wg.Add(1)
	}
	c.mu.Unlock()
	if startCode {
		go c.runCode()
	}
	if startNotes {
		go c.runNotes()
	}
}

func (c *unifiedStreamingCoordinator) SubmitPreparedCodeBatch(ctx context.Context, batch []codeanchor.CodeIndexWork) error {
	if c == nil || c.codeCh == nil || len(batch) == 0 {
		return nil
	}
	prePersist := append([]codeanchor.CodeIndexWork(nil), batch...)
	return c.sendCode(ctx, codeStreamingBatch{prePersistWorks: prePersist})
}

func (c *unifiedStreamingCoordinator) SubmitCodePaths(ctx context.Context, paths []string) error {
	if c == nil || c.codeCh == nil {
		return nil
	}
	paths = normalizeStringSet(paths)
	if len(paths) == 0 {
		return nil
	}
	return c.sendCode(ctx, codeStreamingBatch{indexedPaths: paths})
}

func (c *unifiedStreamingCoordinator) SubmitNotePaths(ctx context.Context, paths []string) error {
	if c == nil || c.noteCh == nil {
		return nil
	}
	paths = normalizeStringSet(paths)
	if len(paths) == 0 {
		return nil
	}
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case c.noteCh <- noteStreamingBatch{changedPaths: paths}:
		return nil
	}
}

func (c *unifiedStreamingCoordinator) SubmitDeletedNotePaths(ctx context.Context, paths []string) error {
	if c == nil || c.noteCh == nil {
		return nil
	}
	paths = normalizeStringSet(paths)
	if len(paths) == 0 {
		return nil
	}
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case c.noteCh <- noteStreamingBatch{deletedPaths: paths}:
		return nil
	}
}

func (c *unifiedStreamingCoordinator) SubmitNoteFullRebuild(ctx context.Context, paths []string) error {
	if c == nil || c.noteCh == nil {
		return nil
	}
	paths = normalizeStringSet(paths)
	if len(paths) == 0 {
		return nil
	}
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case c.noteCh <- noteStreamingBatch{changedPaths: paths, fullSync: true}:
		return nil
	}
}

func (c *unifiedStreamingCoordinator) CloseAndWait() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.closing = true
	c.mu.Unlock()
	c.closeOnce.Do(func() {
		if c.codeCh != nil {
			close(c.codeCh)
		}
		if c.noteCh != nil {
			close(c.noteCh)
		}
	})
	c.wg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *unifiedStreamingCoordinator) MarkNoteLastSync(ctx context.Context) error {
	if c == nil || c.markNoteLastSync == nil {
		return nil
	}
	c.mu.Lock()
	processed := c.noteProcessed
	failed := c.err != nil
	c.mu.Unlock()
	if !processed || failed || c.ctx.Err() != nil {
		return nil
	}
	return c.markNoteLastSync(ctx)
}

func (c *unifiedStreamingCoordinator) runNotes() {
	defer c.wg.Done()
	timer := time.NewTimer(unifiedStreamingBatchIdle)
	defer timer.Stop()
	var pending []noteStreamingBatch
	flush := func(force bool) bool {
		if len(pending) == 0 {
			return true
		}
		batch := mergeNoteStreamingBatches(pending)
		pending = pending[:0]
		c.mu.Lock()
		c.noteProcessed = true
		c.mu.Unlock()
		if c.syncNoteBodyPaths != nil {
			// IMPORTANT: typed-note body chunks are the normal semantic surface
			// when ontology is available. Raw note-section sync is only the
			// compatibility path below.
			if err := runPhase(c.ctx, "sync_ontology_body", func(phaseCtx context.Context) error {
				return c.syncNoteBodyPaths(phaseCtx, batch.changedPaths, batch.deletedPaths, batch.fullSync)
			}); err != nil && c.ctx.Err() == nil {
				c.fail(err)
				return false
			}
			return true
		}
		paths := append([]string(nil), batch.changedPaths...)
		paths = append(paths, batch.deletedPaths...)
		paths = normalizeStringSet(paths)
		if c.syncNotePaths != nil {
			if err := runPhase(c.ctx, "embed_notes", func(phaseCtx context.Context) error {
				indexingperf.AddCount(phaseCtx, indexingperf.NoteRawCompatPaths, int64(len(paths)))
				return c.syncNotePaths(phaseCtx, paths)
			}); err != nil && c.ctx.Err() == nil {
				c.fail(err)
				return false
			}
		}
		return true
	}
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.codeDone:
			c.codeDone = nil
			if !flush(false) {
				return
			}
		case batch, ok := <-c.noteCh:
			if !ok {
				_ = flush(true)
				return
			}
			pending = append(pending, batch)
			if len(pending) == 1 {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(unifiedStreamingBatchIdle)
			}
		case <-timer.C:
			if !flush(false) {
				return
			}
		}
	}
}

func mergeNoteStreamingBatches(batches []noteStreamingBatch) noteStreamingBatch {
	out := noteStreamingBatch{}
	for _, batch := range batches {
		out.changedPaths = append(out.changedPaths, batch.changedPaths...)
		out.deletedPaths = append(out.deletedPaths, batch.deletedPaths...)
		out.fullSync = out.fullSync || batch.fullSync
	}
	out.changedPaths = normalizeStringSet(out.changedPaths)
	out.deletedPaths = normalizeStringSet(out.deletedPaths)
	return out
}

func (c *unifiedStreamingCoordinator) sendCode(ctx context.Context, batch codeStreamingBatch) error {
	select {
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	case c.codeCh <- batch:
		return nil
	}
}

func (c *unifiedStreamingCoordinator) fail(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

func mergeCodeStreamingBatches(batches []codeStreamingBatch) codeStreamingBatch {
	out := codeStreamingBatch{}
	for _, batch := range batches {
		out.indexedPaths = append(out.indexedPaths, batch.indexedPaths...)
		out.prePersistWorks = append(out.prePersistWorks, batch.prePersistWorks...)
	}
	out.indexedPaths = normalizeStringSet(out.indexedPaths)
	return out
}

func normalizeStringSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func staleCodeEmbeddingPaths(ctx context.Context, store *codeemb.CodeStore, keepRel []string) ([]string, error) {
	if store == nil {
		return nil, nil
	}
	items, err := store.ListItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list code embedding items: %w", err)
	}
	keep := make(map[string]struct{}, len(keepRel))
	for _, path := range keepRel {
		path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
		if path != "" {
			keep[path] = struct{}{}
		}
	}
	seen := make(map[string]struct{})
	var stale []string
	for _, item := range items {
		path := strings.TrimSpace(strings.ReplaceAll(item.Path, "\\", "/"))
		if path == "" {
			continue
		}
		if _, ok := keep[path]; ok {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		stale = append(stale, path)
	}
	sort.Strings(stale)
	return stale, nil
}

// submitStaleCodeEmbeddingCleanup schedules path-scoped code embedding cleanup
// from the complete ownership result. It intentionally runs even when the
// current full index has no code roots: an empty keep set means every stored
// code embedding path is stale. The streaming coordinator finishes this work
// before its final close barrier and before reconciliation acknowledgement.
func submitStaleCodeEmbeddingCleanup(
	ctx context.Context,
	streaming *unifiedStreamingCoordinator,
	store *codeemb.CodeStore, keepPaths []paths.CodePath,
) error {
	keepRel := make([]string, 0, len(keepPaths))
	for _, path := range keepPaths {
		if value := strings.TrimSpace(path.String()); value != "" {
			keepRel = append(keepRel, value)
		}
	}
	stalePaths, err := staleCodeEmbeddingPaths(ctx, store, keepRel)
	if err != nil {
		return err
	}
	return streaming.SubmitCodePaths(ctx, stalePaths)
}
