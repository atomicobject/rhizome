package embedindex

import (
	"context"

	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeindex "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
)

func (s *Syncer) planTasksForEmbedding(ctx context.Context, tasks []codeTask, forceReembed bool) (SyncPlan, error) {
	result, err := s.filterTasksNeedingEmbedding(ctx, tasks, forceReembed)
	if err != nil {
		return SyncPlan{}, err
	}
	recordPlannedCodeTaskCounts(ctx, result.tasks)
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
			states:       result.states,
			forceReembed: forceReembed,
			reuseCache:   reuseCache,
		},
		TotalWork: apiCalls,
		CacheHits: result.cacheHits,
	}, nil
}

func preloadCodePathContents(paths []string, loader FileLoader) map[string][]byte {
	if loader == nil || len(paths) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(paths))
	for _, path := range paths {
		if data, err := loader(path); err == nil {
			out[path] = data
		}
	}
	return out
}

func normalizeCodePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func (s *Syncer) loadAnchorsByPath(ctx context.Context, paths []string) (map[string][]codeanchor.IntelAnchor, error) {
	byPath := make(map[string][]codeanchor.IntelAnchor, len(paths))
	if lookup, ok := s.Intel.(intelAnchorsByPath); ok {
		started := time.Now()
		for _, path := range paths {
			anchors, err := lookup.IntelAnchorsByPath(ctx, path)
			if err != nil {
				return nil, fmt.Errorf("read intel anchors for %s: %w", path, err)
			}
			sort.Slice(anchors, func(i, j int) bool {
				return anchors[i].AnchorID < anchors[j].AnchorID
			})
			byPath[path] = anchors
		}
		indexingperf.ObserveLatency(ctx, indexingperf.PlanLoadAnchorsByPath, time.Since(started))
		return byPath, nil
	}

	s.progressf("Loading intel anchors")
	started := time.Now()
	anchors, err := s.Intel.IntelAnchors(ctx)
	indexingperf.ObserveLatency(ctx, indexingperf.PlanLoadAnchors, time.Since(started))
	if err != nil {
		return nil, fmt.Errorf("read intel anchors: %w", err)
	}
	want := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		want[path] = struct{}{}
	}
	for _, anchor := range anchors {
		if _, ok := want[anchor.Path]; ok {
			byPath[anchor.Path] = append(byPath[anchor.Path], anchor)
		}
	}
	for _, path := range paths {
		sort.Slice(byPath[path], func(i, j int) bool {
			return byPath[path][i].AnchorID < byPath[path][j].AnchorID
		})
	}
	return byPath, nil
}

func (s *Syncer) syncPathItemMetadata(ctx context.Context, paths []string, moduleByPath map[string][]codeanchor.IntelAnchor) error {
	items := make([]codeindex.Item, 0)
	anchorIDsByPath := make(map[string][]codeindex.AnchorID, len(paths))
	now := time.Now()
	for _, path := range paths {
		anchorIDsByPath[path] = nil
		for _, anchor := range moduleByPath[path] {
			id := codeindex.AnchorID(anchor.AnchorID)
			anchorIDsByPath[path] = append(anchorIDsByPath[path], id)
			items = append(items, codeindex.Item{
				AnchorID:    id,
				Lang:        string(anchor.Lang),
				Kind:        anchor.Kind,
				Path:        anchor.Path,
				Symbol:      anchor.Symbol,
				FQN:         anchor.FQN,
				Fingerprint: anchor.Fingerprint,
				UpdatedAt:   now,
			})
		}
	}
	if len(items) > 0 {
		if batcher, ok := s.Index.(interface {
			UpsertItemMetaBatch(ctx context.Context, items []codeindex.Item) error
		}); ok {
			if err := batcher.UpsertItemMetaBatch(ctx, items); err != nil {
				return fmt.Errorf("upsert item meta batch: %w", err)
			}
		} else {
			for _, item := range items {
				if err := s.Index.UpsertItemMeta(ctx, item); err != nil {
					return fmt.Errorf("upsert item meta: %w", err)
				}
			}
		}
	}
	pruner, ok := s.Index.(codeItemsByPathPruner)
	if batchPruner, ok := s.Index.(codeItemsByPathBatchPruner); ok {
		if err := batchPruner.DeleteItemsForPathsNotIn(ctx, anchorIDsByPath); err != nil {
			return fmt.Errorf("prune items by path batch: %w", err)
		}
		return nil
	}
	if !ok {
		return nil
	}
	for _, path := range paths {
		if err := pruner.DeleteItemsForPathNotIn(ctx, path, anchorIDsByPath[path]); err != nil {
			return fmt.Errorf("prune items for %s: %w", path, err)
		}
	}
	return nil
}

func (s *Syncer) buildTasksForPreparedPaths(ctx context.Context, prepared PreparedCodePaths) ([]codeTask, error) {
	prepared.moduleByPath = attachRelatedDocsByPath(prepared.moduleByPath, relatedDocLabelsForAnchors(ctx, s.Intel, flattenPathAnchors(prepared.moduleByPath), defaultRelatedDocLimit))

	callsByFile := prefetchFileCallees(ctx, s.Calls, prepared.paths)
	callsByOwnerByLang := prefetchOwnerCallees(ctx, s.Calls, prepared.moduleByPath)

	type pathResult struct {
		path  string
		tasks []codeTask
	}

	workerCount := s.planConcurrent()
	if workerCount > len(prepared.paths) {
		workerCount = len(prepared.paths)
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

				pathAnchors := append([]codeanchor.IntelAnchor(nil), prepared.moduleByPath[path]...)
				if len(pathAnchors) == 0 {
					select {
					case <-ctx.Done():
						return
					case resultCh <- pathResult{path: path}:
					}
					continue
				}
				// Loaded at most once per file, on first use.
				pathRationale := s.rationaleFor(ctx, path)
				moduleDoc := ModuleDocSummary(pathAnchors)

				moduleAnchorID, moduleFingerprint, moduleLang := pickModuleAnchor(pathAnchors)
				if moduleLang == "" && len(pathAnchors) > 0 {
					moduleLang = pathAnchors[0].Lang
				}
				fileContent := prepared.fileContentByPath[path]
				pathTasks := make([]codeTask, 0, len(pathAnchors)+1)

				if moduleAnchorID != "" && moduleFingerprint != "" {
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
		for _, path := range prepared.paths {
			select {
			case <-ctx.Done():
				return
			case pathCh <- path:
			}
		}
	}()

	resultsByPath := make(map[string][]codeTask, len(prepared.paths))
	for res := range resultCh {
		resultsByPath[res.path] = res.tasks
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tasks := make([]codeTask, 0)
	for _, path := range prepared.paths {
		tasks = append(tasks, resultsByPath[path]...)
	}
	return tasks, nil
}

func (s *Syncer) prepareEarlyTasks(ctx context.Context, prepared *PreparedCodePaths) {
	if prepared == nil || len(prepared.paths) == 0 {
		return
	}
	if s.Calls == nil {
		prepared.deferredPaths = nil
		tasks, _ := s.buildTasksForPreparedPaths(ctx, *prepared)
		prepared.earlyTasks = tasks
		return
	}
	policy, ok := s.Policy.(CallSensitiveSynthesisPolicy)
	if !ok {
		prepared.deferredPaths = append([]string(nil), prepared.paths...)
		return
	}
	prepared.moduleByPath = attachRelatedDocsByPath(prepared.moduleByPath, relatedDocLabelsForAnchors(ctx, s.Intel, flattenPathAnchors(prepared.moduleByPath), defaultRelatedDocLimit))
	callsByFile := prefetchFileCallees(ctx, s.Calls, prepared.paths)
	callsByOwnerByLang := prefetchOwnerCallees(ctx, s.Calls, prepared.moduleByPath)
	early := make([]codeTask, 0)
	deferredSeen := make(map[string]struct{})
	for _, path := range prepared.paths {
		pathAnchors := append([]codeanchor.IntelAnchor(nil), prepared.moduleByPath[path]...)
		if len(pathAnchors) == 0 {
			continue
		}
		// Loaded at most once per file, on first use.
		pathRationale := s.rationaleFor(ctx, path)
		moduleDoc := ModuleDocSummary(pathAnchors)
		moduleAnchorID, moduleFingerprint, moduleLang := pickModuleAnchor(pathAnchors)
		if moduleLang == "" && len(pathAnchors) > 0 {
			moduleLang = pathAnchors[0].Lang
		}
		fileContent := prepared.fileContentByPath[path]
		if moduleAnchorID != "" && moduleFingerprint != "" {
			if len(callsByFile[path]) > 0 {
				for i := range pathAnchors {
					if strings.EqualFold(pathAnchors[i].Kind, "module") {
						pathAnchors[i].Calls = callsByFile[path]
						break
					}
				}
			}
			if policy.ModuleChunkUsesCalls(path, moduleLang, pathAnchors, fileContent) {
				deferredSeen[path] = struct{}{}
			} else {
				early = append(early, s.buildModuleTask(moduleAnchorID, moduleFingerprint, path, moduleLang, pathAnchors, fileContent))
			}
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
				early = append(early, s.pruneOnlyAnchorTask(anchor))
				continue
			}
			if policy.AnchorChunkUsesCalls(anchor, fileContent, anchor.RelatedDocs) {
				deferredSeen[path] = struct{}{}
				continue
			}
			early = append(early, s.buildAnchorTask(anchor, fileContent, moduleDoc, &pathRationale))
		}
	}
	prepared.earlyTasks = early
	for path := range deferredSeen {
		prepared.deferredPaths = append(prepared.deferredPaths, path)
	}
	sort.Strings(prepared.deferredPaths)
}
