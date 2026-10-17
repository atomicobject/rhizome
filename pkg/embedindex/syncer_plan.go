package embedindex

// Docs:
// - [Indexing pipeline - Live updating (watcher runtime)](docs/reference/analysis/Indexing pipeline - Live updating (watcher runtime).md)
// - [Rhizome semantic code index spine](docs/specs/technical/semantic-code-index-spine.md)

import (
	"context"
	"errors"
	"fmt"

	"sort"
	"strings"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/indexingperf"

	"github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
)

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
		indexingperf.ObserveLatency(ctx, indexingperf.PlanLoadAnchorMeta, time.Since(started))
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
		indexingperf.ObserveLatency(ctx, indexingperf.PlanLoadAnchors, time.Since(started))
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
	indexingperf.ObserveLatency(ctx, indexingperf.PlanListItems, time.Since(started))
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
		indexingperf.ObserveLatency(ctx, indexingperf.PlanLoadAnchors, time.Since(started))
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
						indexingperf.AddCount(ctx, indexingperf.SuppressedAnchor.Select(sanitizeMetricPart(anchor.Kind)), 1)
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
