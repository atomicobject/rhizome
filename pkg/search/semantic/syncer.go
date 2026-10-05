package semantic

// Docs:
// - [Indexing pipeline - Live updating (watcher runtime)](docs/reference/analysis/Indexing pipeline - Live updating (watcher runtime).md)
// - [Rhizome semantic code index spine](docs/specs/technical/semantic-code-index-spine.md)

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
)

// IntelSource provides read access to intel rows for semantic chunking.
type IntelSource interface {
	IntelAnchors(ctx context.Context) ([]codeanchor.IntelAnchor, error)
	IntelDocSections(ctx context.Context) ([]codeanchor.IntelDocSection, error)
	IntelEdges(ctx context.Context) ([]codeanchor.IntelEdge, error)
}

// RationaleSource optionally exposes the inline rationale comments extracted for a
// file at index time. Chunk synthesis embeds them so prose "why" questions can
// reach code whose explanation lives inside function bodies.
type RationaleSource interface {
	RationaleForPath(ctx context.Context, path string) ([]codeanchor.Rationale, error)
}

// pathRationale fetches a file's rationale comments at most once, and only when a
// chunk actually needs them, so files whose chunks carry no rationale cost no query.
// Not safe for concurrent use; each plan worker owns one per file.
type pathRationale struct {
	load   func() []codeanchor.Rationale
	rows   []codeanchor.Rationale
	loaded bool
}

// inSpan returns the rationale explaining a single anchor, loading the file's
// rationale on first use.
func (c *pathRationale) inSpan(anchor codeanchor.IntelAnchor) []codeanchor.Rationale {
	if !c.loaded {
		c.loaded = true
		if c.load != nil {
			c.rows = c.load()
		}
	}
	return RationaleInSpan(anchor, c.rows)
}

// rationaleFor returns a lazy per-file rationale accessor. A missing or failing
// lookup yields no rationale rather than failing indexing.
func (s *Syncer) rationaleFor(ctx context.Context, path string) pathRationale {
	return pathRationale{load: func() []codeanchor.Rationale {
		src, ok := s.Intel.(RationaleSource)
		if !ok {
			return nil
		}
		rows, err := src.RationaleForPath(ctx, path)
		if err != nil {
			return nil
		}
		return rows
	}}
}

// IntelAnchorMetaSource provides lightweight intel anchor metadata for fast planning.
type IntelAnchorMetaSource interface {
	IntelAnchorMetas(ctx context.Context) ([]codeanchor.IntelAnchorMeta, error)
}

// IntelEmbeddingStateSource provides intel chunk hashes for fast embed skips.
type IntelEmbeddingStateSource interface {
	IntelChunkEmbeddingStates(ctx context.Context, ownerIDs []string) (map[string]map[int]string, error)
}

type codeEmbeddingBatchSource interface {
	EmbeddingByHashes(ctx context.Context, hashes []string) (map[string]embeddings.Embedding, error)
}

type codeFingerprintVersionStore interface {
	SetFingerprintVersion(ctx context.Context, version int) error
}

// ChunkWriter persists intel chunk records for deterministic ordering and provenance.
// When set on the Syncer, chunks are recorded prior to embedding.
type ChunkWriter interface {
	ReplaceIntelChunks(ctx context.Context, ownerIDs []string, chunks []codeanchor.IntelChunk) error
}

type ChunkFamilyWriter interface {
	ReplaceIntelChunksByFamily(ctx context.Context, ownerIDs []string, family string, chunks []codeanchor.IntelChunk) error
}

// EmbeddingWriter persists embeddings keyed by chunk_id.
// When set on the Syncer, embeddings are written after chunk records.
type EmbeddingWriter interface {
	UpsertEmbeddings(ctx context.Context, embeddings map[string]embeddings.Embedding) error
}

type WritebackFlushPolicy struct {
	Rows  int
	Bytes int
	Idle  time.Duration
}

type CodeWritebackBatchConfig struct {
	QueueCapacity int
	ItemEmbed     WritebackFlushPolicy
	CodeChunks    WritebackFlushPolicy
	IntelEmbed    WritebackFlushPolicy
}

// SyncWriteQueue centralizes high-volume semantic write operations behind a
// single writer lane during batch indexing.
type SyncWriteQueue interface {
	CodeWritebackBatchConfig() CodeWritebackBatchConfig
	SubmitCodeItemEmbedding(context.Context, codeindex.ItemEmbeddingUpsert) error
	SubmitCodeItemEmbeddingBatch(context.Context, []codeindex.ItemEmbeddingUpsert) error
	SubmitCodeItemChunks(context.Context, codeindex.AnchorID, []codeindex.ChunkInput, []string, []embeddings.Embedding) error
	SubmitCodeItemChunkBatch(context.Context, []codeindex.ItemChunksUpsert) error
	SubmitIntelChunks(context.Context, []string, []codeanchor.IntelChunk) error
	SubmitIntelChunksByFamily(context.Context, []string, string, []codeanchor.IntelChunk) error
	SubmitIntelEmbeddings(context.Context, map[string]embeddings.Embedding) error
	FlushAndWait(context.Context) error
}

type intelEmbeddingQueue interface {
	SubmitIntelEmbeddings(context.Context, map[string]embeddings.Embedding) error
}

type intelEmbeddingSubmitter interface {
	Submit(map[string]embeddings.Embedding) error
	Close() error
}

type queueEmbeddingWriter struct {
	queue intelEmbeddingQueue
}

func (w queueEmbeddingWriter) UpsertEmbeddings(ctx context.Context, rows map[string]embeddings.Embedding) error {
	if w.queue == nil {
		return nil
	}
	return w.queue.SubmitIntelEmbeddings(ctx, rows)
}

type directIntelEmbeddingSubmitter struct {
	ctx    context.Context
	writer EmbeddingWriter
}

func (w directIntelEmbeddingSubmitter) Submit(rows map[string]embeddings.Embedding) error {
	if w.writer == nil {
		return nil
	}
	return w.writer.UpsertEmbeddings(w.ctx, rows)
}

func (directIntelEmbeddingSubmitter) Close() error { return nil }

// FileLoader reads file content given a repository-relative path.
type FileLoader func(relPath string) ([]byte, error)

// Syncer keeps the code embeddings index in sync with intel anchors.
type Syncer struct {
	Index          codeindex.Index
	Provider       embeddings.Provider
	ProviderInfo   embeddings.ProviderConfig
	Intel          IntelSource
	Calls          CallLookup
	Root           string
	Loader         FileLoader
	Budget         ChunkBudget
	BatchSize      int
	MaxConcurrent  int
	PlanConcurrent int
	// CodeEmbedPacker configures staged code request packing.
	CodeEmbedPacker *EmbedPackerOptions
	// EmbedGate optionally limits aggregate EmbedTexts concurrency across syncers.
	EmbedGate     chan struct{}
	EmbeddingNode *SharedEmbeddingNode
	Policy        SynthesisPolicy
	OnProgress    func(format string, args ...any)
	// OnEmbedProgress reports embedding progress as completed/total items.
	// "Items" are module-level chunks + per-anchor chunks.
	OnEmbedProgress func(done, total int)

	// StaleItemThreshold controls lazy pruning of items not seen in current sync.
	// If the fraction of stale items is below this threshold, they're preserved
	// to avoid re-embedding costs when switching branches. Default is 0.3 (30%).
	// Set to 0 for immediate pruning (old behavior).
	StaleItemThreshold float64

	// ChunkWriter optionally persists intel chunk records prior to embedding.
	// When set, chunk ordering is recorded in the intel spine for determinism.
	ChunkWriter ChunkWriter

	// EmbeddingWriter optionally persists embeddings to the intel spine.
	// When set, embeddings are written keyed by chunk_id after embedding.
	EmbeddingWriter EmbeddingWriter

	// WriteQueue optionally routes hot write paths through a single writer lane.
	WriteQueue SyncWriteQueue

	setupMu                sync.Mutex
	schemaReady            bool
	metaDimsKnown          bool
	setupErr               error
	freshnessMu            sync.Mutex
	pendingSourceHighWater time.Time
}

type itemEmbedWrite struct {
	id   codeindex.AnchorID
	hash string
	vec  embeddings.Embedding
}

const itemEmbeddingBatchSize = 50

const codeFingerprintVersion = 3

// Sync performs a single synchronization pass.
// Sync plans and embeds code anchors. It observes ctx between the two phases
// so a cancelled indexing job stops before the expensive one (SPEC-0104 US3).
func (s *Syncer) Sync(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	plan, err := s.Plan(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Embed(ctx, plan)
}

// Plan prepares the embedding work for a sync pass. The returned plan should be
// passed to Embed to execute the embeddings.
func (s *Syncer) Plan(ctx context.Context) (SyncPlan, error) {
	if s.Index == nil || s.Provider == nil || s.Intel == nil {
		return SyncPlan{}, errors.New("syncer requires index, provider, and intel source")
	}
	if s.Budget.TargetChars == 0 {
		s.Budget = DefaultChunkBudget()
	}
	if s.Policy == nil {
		s.Policy = DefaultSynthesisPolicy(PolicyOptions{Budget: s.Budget})
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	loader := s.Loader
	if loader == nil && s.Root != "" {
		loader = FileSystemLoader(s.Root)
	}
	if err := s.ensureReady(ctx); err != nil {
		return SyncPlan{}, err
	}

	storedMeta, metaOK, err := s.Index.Metadata(ctx)
	if err != nil {
		return SyncPlan{}, err
	}
	forceReembed := !metaOK || storedMeta.FingerprintVersion != codeFingerprintVersion

	lazyIndex, supportsLazy := s.Index.(codeindex.LazyPruningIndex)

	var anchors []codeanchor.IntelAnchor
	var anchorMetas []codeanchor.IntelAnchorMeta
	if metaSource, ok := s.Intel.(IntelAnchorMetaSource); ok {
		s.progressf("Loading intel anchor metadata")
		started := time.Now()
		anchorMetas, err = metaSource.IntelAnchorMetas(ctx)
		indexingperf.ObserveLatency(ctx, "plan.load_anchor_meta", time.Since(started))
		if err != nil {
			return SyncPlan{}, fmt.Errorf("read intel anchor metadata: %w", err)
		}
		sort.Slice(anchorMetas, func(i, j int) bool {
			if anchorMetas[i].Path == anchorMetas[j].Path {
				return anchorMetas[i].AnchorID < anchorMetas[j].AnchorID
			}
			return anchorMetas[i].Path < anchorMetas[j].Path
		})
	} else {
		s.progressf("Loading intel anchors")
		started := time.Now()
		anchors, err = s.Intel.IntelAnchors(ctx)
		indexingperf.ObserveLatency(ctx, "plan.load_anchors", time.Since(started))
		if err != nil {
			return SyncPlan{}, fmt.Errorf("read intel anchors: %w", err)
		}
		sort.Slice(anchors, func(i, j int) bool {
			if anchors[i].Path == anchors[j].Path {
				return anchors[i].AnchorID < anchors[j].AnchorID
			}
			return anchors[i].Path < anchors[j].Path
		})
	}

	anchorCount := len(anchorMetas)
	if anchorCount == 0 {
		anchorCount = len(anchors)
	}
	anchorIDs := make([]codeindex.AnchorID, 0, anchorCount)
	items := make([]codeindex.Item, 0, anchorCount)
	var latestUpdated int64
	if len(anchorMetas) > 0 {
		for _, a := range anchorMetas {
			anchorID := codeindex.AnchorID(a.AnchorID)
			anchorIDs = append(anchorIDs, anchorID)
			if a.UpdatedAt > latestUpdated {
				latestUpdated = a.UpdatedAt
			}
			items = append(items, codeindex.Item{
				AnchorID:    anchorID,
				Lang:        string(a.Lang),
				Kind:        a.Kind,
				Path:        a.Path,
				Symbol:      a.Symbol,
				FQN:         a.FQN,
				Fingerprint: a.Fingerprint,
				UpdatedAt:   time.Unix(a.UpdatedAt, 0),
			})
		}
	} else {
		for _, a := range anchors {
			anchorID := codeindex.AnchorID(a.AnchorID)
			anchorIDs = append(anchorIDs, anchorID)
			if a.UpdatedAt > latestUpdated {
				latestUpdated = a.UpdatedAt
			}
			items = append(items, codeindex.Item{
				AnchorID:    anchorID,
				Lang:        string(a.Lang),
				Kind:        a.Kind,
				Path:        a.Path,
				Symbol:      a.Symbol,
				FQN:         a.FQN,
				Fingerprint: a.Fingerprint,
				UpdatedAt:   time.Unix(a.UpdatedAt, 0),
			})
		}
	}

	started := time.Now()
	existing, err := s.Index.ListItems(ctx)
	indexingperf.ObserveLatency(ctx, "plan.list_items", time.Since(started))
	if err != nil {
		return SyncPlan{}, err
	}

	// Fast-path: if intel anchors are not newer than the source high-water and the
	// anchor set matches, skip embedding work entirely.
	if !forceReembed && metaOK && !storedMeta.SourceHighWater.IsZero() && latestUpdated > 0 {
		if storedMeta.SourceHighWater.Unix() >= latestUpdated && sameAnchorSet(existing, anchorIDs) {
			s.progressf("No changed code to embed")
			_ = s.Index.UpdateLastSync(ctx, time.Now())
			return SyncPlan{
				skipEmbed: true,
				state: codePlanState{
					forceReembed: forceReembed,
				},
			}, nil
		}
	}

	// If index supports lazy pruning, increment generation only after the skip
	// path has decided work is required. A skipped pass must not advance the
	// visible generation and hide otherwise-current rows.
	if supportsLazy {
		if _, err := lazyIndex.IncrementSyncGeneration(ctx); err != nil {
			return SyncPlan{}, fmt.Errorf("increment sync generation: %w", err)
		}
	}

	if batcher, ok := s.Index.(interface {
		UpsertItemMetaBatch(ctx context.Context, items []codeindex.Item) error
	}); ok {
		if err := batcher.UpsertItemMetaBatch(ctx, items); err != nil {
			return SyncPlan{}, fmt.Errorf("upsert item meta batch: %w", err)
		}
	} else {
		for _, item := range items {
			if err := s.Index.UpsertItemMeta(ctx, item); err != nil {
				return SyncPlan{}, fmt.Errorf("upsert item meta: %w", err)
			}
		}
	}

	// Cleanup stale items: use lazy pruning if supported, otherwise immediate deletion.
	if supportsLazy {
		threshold := s.StaleItemThreshold
		if threshold == 0 {
			threshold = 0.3 // Default: preserve items if stale fraction < 30%
		}
		if pruned, err := lazyIndex.PruneStaleItems(ctx, threshold); err != nil {
			return SyncPlan{}, fmt.Errorf("prune stale items: %w", err)
		} else if pruned > 0 {
			s.progressf("Pruned %d stale items (above %.0f%% threshold)", pruned, threshold*100)
		}
	} else {
		if err := s.Index.DeleteItemsNotIn(ctx, anchorIDs); err != nil {
			return SyncPlan{}, fmt.Errorf("cleanup items: %w", err)
		}
	}

	if anchors == nil {
		started := time.Now()
		anchors, err = s.Intel.IntelAnchors(ctx)
		indexingperf.ObserveLatency(ctx, "plan.load_anchors", time.Since(started))
		if err != nil {
			return SyncPlan{}, fmt.Errorf("read intel anchors: %w", err)
		}
		sort.Slice(anchors, func(i, j int) bool {
			if anchors[i].Path == anchors[j].Path {
				return anchors[i].AnchorID < anchors[j].AnchorID
			}
			return anchors[i].Path < anchors[j].Path
		})
	}

	s.progressf("Building chunks")
	moduleByPath := groupByPath(anchors)
	relatedByAnchor := relatedDocLabelsForAnchors(ctx, s.Intel, anchors, defaultRelatedDocLimit)

	tasks := make([]codeTask, 0, len(moduleByPath)+len(anchors))

	paths := make([]string, 0, len(moduleByPath))
	for p := range moduleByPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	callsByFile := prefetchFileCallees(ctx, s.Calls, paths)
	callsByOwnerByLang := prefetchOwnerCallees(ctx, s.Calls, moduleByPath)

	// Build per-file tasks to avoid repeated file I/O and per-owner lookups.
	type pathResult struct {
		path  string
		tasks []codeTask
	}
	workerCount := s.planConcurrent()
	if workerCount > len(paths) {
		workerCount = len(paths)
	}
	if workerCount < 1 {
		workerCount = 1
	}
	pathCh := make(chan string, workerCount*2)
	resultCh := make(chan pathResult, workerCount*2)
	var buildWG sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		buildWG.Add(1)
		go func() {
			defer buildWG.Done()
			for path := range pathCh {
				select {
				case <-ctx.Done():
					return
				default:
				}

				pathAnchors := attachRelatedDocs(moduleByPath[path], relatedByAnchor)
				// Loaded at most once per file, on first use.
				pathRationale := s.rationaleFor(ctx, path)
				moduleDoc := ModuleDocSummary(pathAnchors)
				moduleAnchorID, moduleFingerprint, moduleLang := pickModuleAnchor(pathAnchors)
				if moduleLang == "" && len(pathAnchors) > 0 {
					moduleLang = pathAnchors[0].Lang
				}

				var fileContent []byte
				if loader != nil {
					if data, err := loader(path); err == nil {
						fileContent = data
					}
				}

				pathTasks := make([]codeTask, 0, len(pathAnchors)+1)

				// Module chunk (optional): some callers/tests may provide intel anchors without a module row.
				if moduleAnchorID != "" && moduleFingerprint != "" {
					// Attach file-level call summary to the module anchor before module chunk synthesis.
					if len(callsByFile[path]) > 0 {
						for i := range pathAnchors {
							if strings.EqualFold(pathAnchors[i].Kind, "module") {
								pathAnchors[i].Calls = callsByFile[path]
								break
							}
						}
					}

					pathTasks = append(pathTasks, s.buildModuleTask(moduleAnchorID, moduleFingerprint, path, moduleLang, pathAnchors, fileContent))
				}

				for _, anchor := range pathAnchors {
					if strings.EqualFold(anchor.Kind, "module") {
						continue
					}
					lang := strings.TrimSpace(string(anchor.Lang))
					if lang == "" {
						lang = strings.TrimSpace(string(moduleLang))
					}
					if callsByOwnerByLang != nil && strings.TrimSpace(anchor.FQN) != "" {
						if callees := callsByOwnerByLang[lang][anchor.FQN]; len(callees) > 0 {
							anchor.Calls = callees
						}
					}
					if !s.shouldIndexAnchor(anchor) {
						indexingperf.AddCount(ctx, "semantic.suppressed_anchors."+sanitizeMetricPart(anchor.Kind), 1)
						pathTasks = append(pathTasks, s.pruneOnlyAnchorTask(anchor))
						continue
					}
					pathTasks = append(pathTasks, s.buildAnchorTask(anchor, fileContent, moduleDoc, &pathRationale))
				}

				select {
				case <-ctx.Done():
					return
				case resultCh <- pathResult{path: path, tasks: pathTasks}:
				}
			}
		}()
	}

	go func() {
		buildWG.Wait()
		close(resultCh)
	}()

	go func() {
		defer close(pathCh)
		for _, path := range paths {
			select {
			case <-ctx.Done():
				return
			case pathCh <- path:
			}
		}
	}()

	resultsByPath := make(map[string][]codeTask, len(paths))
	for res := range resultCh {
		resultsByPath[res.path] = res.tasks
	}
	if err := ctx.Err(); err != nil {
		return SyncPlan{}, err
	}
	for _, path := range paths {
		tasks = append(tasks, resultsByPath[path]...)
	}

	result, err := s.filterTasksNeedingEmbedding(ctx, tasks, forceReembed)
	if err != nil {
		return SyncPlan{}, err
	}
	recordPlannedCodeTaskCounts(ctx, result.tasks)
	// TotalWork is only items that need API calls (not cache hits)
	apiCalls := len(result.tasks) - result.cacheHits
	if apiCalls < 0 {
		apiCalls = 0
	}
	reuseCache, err := s.buildCodeReuseCache(ctx, result.tasks, result.states, forceReembed)
	if err != nil {
		return SyncPlan{}, err
	}
	return SyncPlan{
		tasks: result.tasks,
		state: codePlanState{
			states:          result.states,
			forceReembed:    forceReembed,
			reuseCache:      reuseCache,
			sourceHighWater: time.Unix(latestUpdated, 0),
		},
		TotalWork: apiCalls,
		CacheHits: result.cacheHits,
	}, nil
}

// Embed executes a prepared sync plan.
func (s *Syncer) Embed(ctx context.Context, plan SyncPlan) error {
	return s.embed(ctx, plan, true)
}

func (s *Syncer) EmbedWithoutSyncMark(ctx context.Context, plan SyncPlan) error {
	return s.embed(ctx, plan, false)
}

func (s *Syncer) MarkLastSync(ctx context.Context) error {
	if err := s.markPendingSourceHighWater(ctx); err != nil {
		return err
	}
	if versioned, ok := s.Index.(codeFingerprintVersionStore); ok {
		if err := versioned.SetFingerprintVersion(ctx, codeFingerprintVersion); err != nil {
			return err
		}
	}
	if generationCommitter, ok := s.Index.(interface {
		CommitSyncGeneration(context.Context) error
	}); ok {
		if err := generationCommitter.CommitSyncGeneration(ctx); err != nil {
			return err
		}
	}
	return s.Index.UpdateLastSync(ctx, time.Now())
}

func (s *Syncer) recordPendingSourceHighWater(ts time.Time) {
	if ts.IsZero() || ts.Unix() <= 0 {
		return
	}
	s.freshnessMu.Lock()
	defer s.freshnessMu.Unlock()
	if s.pendingSourceHighWater.IsZero() || ts.After(s.pendingSourceHighWater) {
		s.pendingSourceHighWater = ts
	}
}

func (s *Syncer) markPendingSourceHighWater(ctx context.Context) error {
	s.freshnessMu.Lock()
	ts := s.pendingSourceHighWater
	s.pendingSourceHighWater = time.Time{}
	s.freshnessMu.Unlock()
	if ts.IsZero() || ts.Unix() <= 0 {
		return nil
	}
	return s.Index.UpdateSourceHighWater(ctx, ts)
}

func (s *Syncer) markPlanFreshness(ctx context.Context, plan SyncPlan, updateLastSync bool) error {
	if updateLastSync {
		if !plan.state.sourceHighWater.IsZero() && plan.state.sourceHighWater.Unix() > 0 {
			if err := s.Index.UpdateSourceHighWater(ctx, plan.state.sourceHighWater); err != nil {
				return err
			}
		}
		return s.MarkLastSync(ctx)
	}
	s.recordPendingSourceHighWater(plan.state.sourceHighWater)
	return nil
}

func (s *Syncer) shouldIndexAnchor(anchor codeanchor.IntelAnchor) bool {
	if s.Policy == nil {
		return true
	}
	if decider, ok := s.Policy.(AnchorIndexDecider); ok {
		return decider.ShouldIndexAnchor(anchor)
	}
	return true
}

func (s *Syncer) pruneOnlyAnchorTask(anchor codeanchor.IntelAnchor) codeTask {
	return codeTask{
		id:          codeindex.AnchorID(anchor.AnchorID),
		fingerprint: anchor.Fingerprint,
		payload: codeTaskPayload{
			ownerKind: strings.TrimSpace(anchor.Kind),
		},
	}
}

func (s *Syncer) embed(ctx context.Context, plan SyncPlan, updateLastSync bool) error {
	if plan.skipEmbed {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Persist intel chunks prior to embedding if a writer is configured.
	if s.ChunkWriter != nil && len(plan.tasks) > 0 {
		// With a queued writer, chunk rows and subsequent embedding rows share the
		// same FIFO lane, so an eager FlushAndWait here only serializes overlap.
		if err := s.persistIntelChunks(ctx, plan.tasks); err != nil {
			return fmt.Errorf("persist intel chunks: %w", err)
		}
	}

	itemWriter := func(ctx context.Context, anchorID codeindex.AnchorID, hash string, vec embeddings.Embedding) error {
		if s.WriteQueue != nil {
			return s.WriteQueue.SubmitCodeItemEmbedding(ctx, codeindex.ItemEmbeddingUpsert{
				AnchorID:  anchorID,
				Hash:      hash,
				Embedding: vec,
			})
		}
		return s.Index.UpsertItemEmbedding(ctx, anchorID, hash, vec)
	}
	// High-volume indexing writes must batch intel spine persistence; per-item
	// UpsertEmbeddings calls turn one sync pass into thousands of tiny writes.
	intelTarget := s.EmbeddingWriter
	if s.WriteQueue != nil {
		intelTarget = queueEmbeddingWriter{queue: s.WriteQueue}
	}
	var writebackBatcher *codeWritebackBatcher
	if s.WriteQueue != nil {
		writebackBatcher = newCodeWritebackBatcher(ctx, s.WriteQueue, s.WriteQueue.CodeWritebackBatchConfig())
		itemWriter = func(ctx context.Context, anchorID codeindex.AnchorID, hash string, vec embeddings.Embedding) error {
			return writebackBatcher.SubmitCodeItemEmbedding(ctx, anchorID, hash, vec)
		}
	}
	var intelWriter intelEmbeddingSubmitter
	if intelTarget != nil && writebackBatcher == nil {
		if s.WriteQueue != nil {
			intelWriter = directIntelEmbeddingSubmitter{ctx: ctx, writer: intelTarget}
		} else {
			intelWriter = newBatchedIntelEmbeddingWriter(ctx, intelTarget, intelEmbeddingBatchWriterOptions{
				Label:      "code",
				OnError:    func(error) { cancel() },
				OnProgress: s.progressf,
			})
		}
	}
	closeIntelWriter := func() error {
		if intelWriter == nil {
			return nil
		}
		return intelWriter.Close()
	}
	drainWriteQueue := func() error {
		if writebackBatcher != nil {
			if err := writebackBatcher.Close(); err != nil {
				return fmt.Errorf("close code writeback batcher: %w", err)
			}
			writebackBatcher = nil
		}
		if s.WriteQueue != nil {
			if err := s.WriteQueue.FlushAndWait(context.WithoutCancel(ctx)); err != nil {
				return fmt.Errorf("flush code write queue: %w", err)
			}
		}
		return nil
	}
	batchErrCh := make(chan error, 1)
	var writeCh chan itemEmbedWrite
	var writeWG sync.WaitGroup
	hasBatchWriter := false
	if s.WriteQueue == nil {
		if batcher, ok := s.Index.(interface {
			UpsertItemEmbeddingBatch(ctx context.Context, items []codeindex.ItemEmbeddingUpsert) error
		}); ok {
			writeCh = make(chan itemEmbedWrite, itemEmbeddingBatchSize*2)
			writeWG.Add(1)
			hasBatchWriter = true
			go func() {
				defer writeWG.Done()
				var batch []codeindex.ItemEmbeddingUpsert
				flush := func() {
					if len(batch) == 0 {
						return
					}
					if err := batcher.UpsertItemEmbeddingBatch(ctx, batch); err != nil {
						select {
						case batchErrCh <- err:
						default:
						}
						cancel()
					}
					batch = batch[:0]
				}
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						flush()
						return
					case item, ok := <-writeCh:
						if !ok {
							flush()
							return
						}
						if len(item.vec) == 0 {
							continue
						}
						batch = append(batch, codeindex.ItemEmbeddingUpsert{AnchorID: item.id, Hash: item.hash, Embedding: item.vec})
						if len(batch) >= itemEmbeddingBatchSize {
							flush()
						}
					case <-ticker.C:
						flush()
					}
				}
			}()

			itemWriter = func(ctx context.Context, anchorID codeindex.AnchorID, hash string, vec embeddings.Embedding) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case writeCh <- itemEmbedWrite{id: anchorID, hash: hash, vec: vec}:
					return nil
				}
			}
		}
	}

	totalWork := plan.TotalWork
	totalTasks := len(plan.tasks)
	if totalTasks > 0 {
		if plan.CacheHits > 0 {
			s.progressf("Embedding %d items (%d from cache, batch size %d)", totalWork, plan.CacheHits, s.batchSize())
		} else {
			s.progressf("Embedding %d items (batch size %d, max concurrency %d)", totalWork, s.batchSize(), s.maxConcurrent())
		}
		if s.OnEmbedProgress != nil && totalWork > 0 {
			s.OnEmbedProgress(0, totalWork)
		}
		err := s.processTasksPipelined(ctx, plan, itemWriter, intelWriter, writebackBatcher)
		if err != nil {
			if hasBatchWriter {
				close(writeCh)
				writeWG.Wait()
			}
			return errors.Join(fmt.Errorf("process code embedding tasks: %w", err), drainWriteQueue(), closeIntelWriter())
		}
	} else {
		s.progressf("No embeddings to update (index is up to date)")
	}
	if hasBatchWriter {
		close(writeCh)
		writeWG.Wait()
	}
	if err := drainWriteQueue(); err != nil {
		return err
	}
	if err := closeIntelWriter(); err != nil {
		return fmt.Errorf("close code intel writer: %w", err)
	}
	select {
	case err := <-batchErrCh:
		if err != nil {
			return err
		}
	default:
	}

	// Provider may have learned its output dimensions during this batch (Voyage
	// reports them with the first response). Re-run the validator so the meta
	// row reflects reality before the next sync cycle.
	_ = s.ensureReady(ctx)
	s.progressf("Sync complete")
	if updateLastSync {
		return s.markPlanFreshness(ctx, plan, true)
	}
	return s.markPlanFreshness(ctx, plan, false)
}

func pickModuleAnchor(pathAnchors []codeanchor.IntelAnchor) (id codeindex.AnchorID, fingerprint string, lang codeanchor.Lang) {
	for _, a := range pathAnchors {
		if strings.EqualFold(a.Kind, "module") {
			return codeindex.AnchorID(a.AnchorID), a.Fingerprint, a.Lang
		}
	}
	return "", "", ""
}

// filterResult contains the filtered tasks and cache hit statistics.
type filterResult struct {
	tasks     []codeTask
	cacheHits int
	states    map[codeindex.AnchorID]codeindex.ItemEmbeddingState
}

func (s *Syncer) filterTasksNeedingEmbedding(ctx context.Context, tasks []codeTask, force bool) (filterResult, error) {
	// When force=true (rebuild), we process all tasks but still check cache hits
	// for accurate progress reporting.
	if force {
		cacheHits, err := s.countCachedPrimaryChunks(ctx, tasks)
		if err != nil {
			return filterResult{}, err
		}
		return filterResult{tasks: tasks, cacheHits: cacheHits}, nil
	}
	anchorIDs := uniqueAnchorIDs(tasks)
	var stateByAnchor map[codeindex.AnchorID]codeindex.ItemEmbeddingState
	if s.EmbeddingWriter != nil {
		if intelStates, ok := s.Intel.(IntelEmbeddingStateSource); ok {
			ownerIDs := make([]string, 0, len(anchorIDs))
			for _, id := range anchorIDs {
				if id == "" {
					continue
				}
				ownerIDs = append(ownerIDs, string(id))
			}
			intelHashes, err := intelStates.IntelChunkEmbeddingStates(ctx, ownerIDs)
			if err != nil {
				return filterResult{}, err
			}
			stateByAnchor = make(map[codeindex.AnchorID]codeindex.ItemEmbeddingState, len(intelHashes))
			for ownerID, hashes := range intelHashes {
				if strings.TrimSpace(ownerID) == "" {
					continue
				}
				state := codeindex.ItemEmbeddingState{ChunkHashes: hashes}
				if hashes != nil {
					if hash, ok := hashes[0]; ok && strings.TrimSpace(hash) != "" {
						state.ItemHash = hash
						state.HasItemHash = true
					}
				}
				stateByAnchor[codeindex.AnchorID(ownerID)] = state
			}
		}
	}
	if stateByAnchor == nil {
		started := time.Now()
		var err error
		stateByAnchor, err = s.Index.ItemEmbeddingStates(ctx, anchorIDs)
		indexingperf.ObserveLatency(ctx, "plan.item_embedding_states", time.Since(started))
		if err != nil {
			return filterResult{}, err
		}
	}

	neededFlags := make([]bool, len(tasks))
	workerCount := s.planConcurrent()
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}
	if workerCount < 1 {
		workerCount = 1
	}
	taskCh := make(chan int, workerCount*2)
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range taskCh {
				if ctx.Err() != nil {
					return
				}
				if s.needsEmbedding(ctx, tasks[idx], stateByAnchor[tasks[idx].id]) {
					neededFlags[idx] = true
				}
			}
		}()
	}
	for idx := range tasks {
		if ctx.Err() != nil {
			break
		}
		taskCh <- idx
	}
	close(taskCh)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return filterResult{}, err
	}

	var needed []codeTask
	for idx, task := range tasks {
		if neededFlags[idx] {
			needed = append(needed, task)
		}
	}

	cacheHits, err := s.countCachedPrimaryChunks(ctx, needed)
	if err != nil {
		return filterResult{}, err
	}

	neededStates := make(map[codeindex.AnchorID]codeindex.ItemEmbeddingState, len(needed))
	for _, task := range needed {
		if state, ok := stateByAnchor[task.id]; ok {
			neededStates[task.id] = state
		}
	}

	return filterResult{tasks: needed, cacheHits: cacheHits, states: neededStates}, nil
}

func (s *Syncer) countCachedPrimaryChunks(ctx context.Context, tasks []codeTask) (int, error) {
	if len(tasks) == 0 {
		return 0, nil
	}
	hashes := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if len(task.payload.chunks) == 0 {
			continue
		}
		hashes = append(hashes, task.payload.chunks[0].Input.Hash)
	}
	rows, err := s.prefetchEmbeddingHashes(ctx, hashes)
	if err != nil {
		return 0, err
	}
	cacheHits := 0
	for _, hash := range hashes {
		if vec, ok := rows[hash]; ok && len(vec) > 0 {
			cacheHits++
		}
	}
	return cacheHits, nil
}

func uniqueAnchorIDs(tasks []codeTask) []codeindex.AnchorID {
	seen := make(map[codeindex.AnchorID]struct{}, len(tasks))
	var ids []codeindex.AnchorID
	for _, task := range tasks {
		if _, ok := seen[task.id]; ok {
			continue
		}
		seen[task.id] = struct{}{}
		ids = append(ids, task.id)
	}
	return ids
}

func (s *Syncer) buildCodeReuseCache(ctx context.Context, tasks []codeTask, states map[codeindex.AnchorID]codeindex.ItemEmbeddingState, force bool) (*codeReuseCache, error) {
	cache := newCodeReuseCache()
	hashes := collectCodeReuseCandidateHashes(tasks, states, force)
	rows, err := s.prefetchEmbeddingHashes(ctx, hashes)
	if err != nil {
		return nil, err
	}
	cache.warm(rows, hashes)
	return cache, nil
}

func (s *Syncer) prefetchEmbeddingHashes(ctx context.Context, hashes []string) (map[string]embeddings.Embedding, error) {
	unique := dedupeNonEmptyStrings(hashes)
	out := make(map[string]embeddings.Embedding, len(unique))
	if len(unique) == 0 {
		return out, nil
	}
	batcher, ok := s.Index.(codeEmbeddingBatchSource)
	if !ok {
		for _, hash := range unique {
			started := time.Now()
			cached, hit, err := s.Index.EmbeddingByHash(ctx, hash)
			indexingperf.ObserveLatency(ctx, "codeembed.hash_prefetch", time.Since(started))
			indexingperf.AddCount(ctx, "codeembed.hash_prefetch.calls", 1)
			indexingperf.AddCount(ctx, "codeembed.hash_prefetch.hashes", 1)
			indexingperf.ObserveSample(ctx, "codeembed.hash_prefetch.batch", 1)
			if err != nil {
				return nil, err
			}
			if hit && len(cached) > 0 {
				out[hash] = cached
				indexingperf.AddCount(ctx, "codeembed.hash_prefetch.hits", 1)
			} else {
				indexingperf.AddCount(ctx, "codeembed.hash_prefetch.misses", 1)
			}
		}
		return out, nil
	}

	started := time.Now()
	rows, err := batcher.EmbeddingByHashes(ctx, unique)
	indexingperf.ObserveLatency(ctx, "codeembed.hash_prefetch", time.Since(started))
	indexingperf.AddCount(ctx, "codeembed.hash_prefetch.calls", 1)
	indexingperf.AddCount(ctx, "codeembed.hash_prefetch.hashes", int64(len(unique)))
	indexingperf.AddCount(ctx, "codeembed.hash_prefetch.hits", int64(len(rows)))
	indexingperf.ObserveSample(ctx, "codeembed.hash_prefetch.batch", int64(len(unique)))
	if err != nil {
		return nil, err
	}
	for hash, vec := range rows {
		if len(vec) == 0 {
			continue
		}
		out[hash] = vec
	}
	indexingperf.AddCount(ctx, "codeembed.hash_prefetch.misses", int64(len(unique)-len(out)))
	return out, nil
}

func collectCodeReuseCandidateHashes(tasks []codeTask, states map[codeindex.AnchorID]codeindex.ItemEmbeddingState, force bool) []string {
	hashes := make([]string, 0, len(tasks))
	for _, task := range tasks {
		state := states[task.id]
		chunkHashes := state.ChunkHashes
		if chunkHashes == nil || force {
			chunkHashes = map[int]string{}
		}
		if len(task.payload.chunks) == 0 {
			continue
		}
		itemHash := task.payload.chunks[0].Input.Hash
		if strings.TrimSpace(itemHash) != "" {
			hashes = append(hashes, itemHash)
		}
		for _, chunk := range task.payload.chunks {
			if !force && chunkHashes[chunk.Input.Index] == chunk.Input.Hash {
				continue
			}
			hashes = append(hashes, chunk.Input.Hash)
		}
	}
	return dedupeNonEmptyStrings(hashes)
}

func dedupeNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sameAnchorSet(existing []codeindex.Item, anchorIDs []codeindex.AnchorID) bool {
	if len(existing) != len(anchorIDs) {
		return false
	}
	have := make(map[codeindex.AnchorID]struct{}, len(existing))
	for _, item := range existing {
		if item.AnchorID == "" {
			return false
		}
		have[item.AnchorID] = struct{}{}
	}
	for _, id := range anchorIDs {
		if id == "" {
			return false
		}
		if _, ok := have[id]; !ok {
			return false
		}
	}
	return true
}

func (s *Syncer) needsEmbedding(ctx context.Context, task codeTask, state codeindex.ItemEmbeddingState) bool {
	observeReason := func(reason string) {
		if reason != "" {
			indexingperf.AddCount(ctx, "codeembed.need."+reason, 1)
		}
	}
	if len(task.payload.chunks) == 0 {
		if len(state.ChunkHashes) > 0 {
			if task.payload.ownerKind != "" {
				indexingperf.AddCount(ctx, "semantic.pruned_stale_chunks."+sanitizeMetricPart(task.payload.ownerKind), int64(len(state.ChunkHashes)))
			}
			observeReason("pruned_all_chunks")
			return true
		}
		observeReason("no_chunks")
		return false
	}

	chunkHashes := state.ChunkHashes
	if chunkHashes == nil {
		chunkHashes = make(map[int]string)
	}
	switch {
	case !state.HasFingerprint:
		indexingperf.AddCount(ctx, "codeembed.state.missing_fingerprint", 1)
	case state.Fingerprint != task.fingerprint:
		indexingperf.AddCount(ctx, "codeembed.state.fingerprint_mismatch", 1)
		indexingperf.AddCount(ctx, "codeembed.state.schema_or_fingerprint_mismatch", 1)
	}
	if !state.HasItemHash {
		indexingperf.AddCount(ctx, "codeembed.state.missing_item_hash", 1)
	}
	if !state.HasFingerprint && !state.HasItemHash && len(chunkHashes) == 0 {
		indexingperf.AddCount(ctx, "codeembed.state.first_build_candidate", 1)
	}

	// Fast-path: fingerprint unchanged and embeddings exist for all chunk indices.
	if state.HasFingerprint && state.Fingerprint == task.fingerprint && state.HasItemHash && len(chunkHashes) > 0 {
		allMatch := true
		expectedIndexes := make(map[int]struct{}, len(task.payload.chunks))
		for _, chunk := range task.payload.chunks {
			expectedIndexes[chunk.Input.Index] = struct{}{}
			if chunkHashes[chunk.Input.Index] != chunk.Input.Hash {
				allMatch = false
				break
			}
		}
		if allMatch && len(chunkHashes) != len(expectedIndexes) {
			allMatch = false
		}
		if allMatch {
			indexingperf.AddCount(ctx, "codeembed.reuse.same_position", int64(len(task.payload.chunks)))
			observeReason("unchanged")
			return false
		}
		observeReason("chunk_hash_mismatch")
		return true
	}

	// Hash comparison fallback.
	hash := task.payload.chunks[0].Input.Hash
	if !state.HasItemHash {
		if state.HasFingerprint {
			observeReason("legacy_missing_item_hash")
		} else {
			observeReason("first_build_missing_state")
		}
		observeReason("missing_item_hash")
		return true
	}
	if state.ItemHash != hash {
		observeReason("item_hash_mismatch")
		return true
	}

	for _, chunk := range task.payload.chunks {
		if chunkHashes[chunk.Input.Index] != chunk.Input.Hash {
			observeReason("chunk_hash_mismatch")
			return true
		}
	}
	if len(chunkHashes) != len(task.payload.chunks) {
		observeReason("chunk_hash_mismatch")
		return true
	}
	indexingperf.AddCount(ctx, "codeembed.reuse.same_position", int64(len(task.payload.chunks)))
	observeReason("unchanged_hash_fallback")
	return false
}

func (s *Syncer) progressf(format string, args ...any) {
	if s.OnProgress != nil {
		s.OnProgress(format, args...)
	}
}

func (s *Syncer) ensureReady(ctx context.Context) error {
	if s.Index == nil || s.Provider == nil {
		return errors.New("syncer requires index and provider")
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.setupErr != nil {
		return s.setupErr
	}
	if !s.schemaReady {
		if err := s.Index.EnsureSchema(ctx); err != nil {
			s.setupErr = err
			return err
		}
		s.schemaReady = true
	}
	// Re-validate each call until the provider has reported its real dimensions.
	// Voyage learns dims from its first embedding response, so the first call may
	// pass Dimensions=0; we follow up once the value is known.
	if !s.metaDimsKnown {
		dims := s.Provider.Dimensions()
		if err := s.Index.ValidateOrInitMetadata(ctx, embeddings.IndexMetadata{
			Provider:           s.ProviderInfo.Provider,
			Model:              s.ProviderInfo.Model,
			Dimensions:         dims,
			FingerprintVersion: codeFingerprintVersion,
		}); err != nil {
			s.setupErr = err
			return err
		}
		if dims > 0 {
			s.metaDimsKnown = true
		}
	}
	return nil
}

func (s *Syncer) batchSize() int {
	if s.BatchSize > 0 {
		return s.BatchSize
	}
	return EffectiveBatchSize(s.Provider, 0)
}

func (s *Syncer) planConcurrent() int {
	if s.PlanConcurrent > 0 {
		return s.PlanConcurrent
	}
	return defaultPlanConcurrency()
}

func (s *Syncer) maxConcurrent() int {
	if s.MaxConcurrent > 0 {
		return s.MaxConcurrent
	}
	return EffectiveMaxConcurrent(s.Provider, 0)
}

// persistIntelChunks converts semantic chunks to intel chunks and persists them.
func (s *Syncer) persistIntelChunks(ctx context.Context, tasks []codeTask) error {
	if s.ChunkWriter == nil {
		return nil
	}

	now := time.Now().Unix()
	var ownerIDs []string
	var chunks []codeanchor.IntelChunk

	for _, task := range tasks {
		ownerID := string(task.id)
		ownerIDs = append(ownerIDs, ownerID)
		if len(task.payload.chunks) == 0 && task.payload.ownerKind != "" {
			indexingperf.AddCount(ctx, "semantic.code_chunks_pruned."+sanitizeMetricPart(task.payload.ownerKind), 1)
		}
		for _, sc := range task.payload.chunks {
			chunks = append(chunks, codeanchor.IntelChunk{
				ChunkID:     codeanchor.IntelChunkID(ownerID, sc.Input.Index, sc.Input.Granularity),
				OwnerID:     ownerID,
				OwnerType:   "anchor",
				Ord:         sc.Input.Index,
				Granularity: sc.Input.Granularity,
				Breadcrumb:  sc.Input.Breadcrumb,
				Heading:     sc.Input.Heading,
				ContentHash: sc.Input.Hash,
				StartByte:   int64(sc.Input.StartByte),
				EndByte:     int64(sc.Input.EndByte),
				UpdatedAt:   now,
			})
		}
	}

	if s.WriteQueue != nil {
		return s.WriteQueue.SubmitIntelChunks(ctx, ownerIDs, chunks)
	}
	return s.ChunkWriter.ReplaceIntelChunks(ctx, ownerIDs, chunks)
}

func groupByPath(anchors []codeanchor.IntelAnchor) map[string][]codeanchor.IntelAnchor {
	out := make(map[string][]codeanchor.IntelAnchor)
	for _, a := range anchors {
		out[a.Path] = append(out[a.Path], a)
	}
	for path := range out {
		sort.Slice(out[path], func(i, j int) bool { return out[path][i].FQN < out[path][j].FQN })
	}
	return out
}

// FileSystemLoader creates a FileLoader backed by os.ReadFile rooted at base.
func FileSystemLoader(base string) FileLoader {
	base = filepath.Clean(base)
	return func(relPath string) ([]byte, error) {
		if filepath.IsAbs(relPath) {
			return nil, fs.ErrNotExist
		}
		full := filepath.Clean(filepath.Join(base, filepath.FromSlash(relPath)))
		rel, err := filepath.Rel(base, full)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fs.ErrNotExist
		}
		return os.ReadFile(full)
	}
}
