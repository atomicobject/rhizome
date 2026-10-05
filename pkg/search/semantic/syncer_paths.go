package semantic

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
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
)

type intelAnchorsByPath interface {
	IntelAnchorsByPath(ctx context.Context, path string) ([]codeanchor.IntelAnchor, error)
}

type codeItemsByPathPruner interface {
	DeleteItemsForPathNotIn(ctx context.Context, path string, anchorIDs []codeindex.AnchorID) error
}

type codeItemsByPathBatchPruner interface {
	DeleteItemsForPathsNotIn(ctx context.Context, anchorIDsByPath map[string][]codeindex.AnchorID) error
}

type PreparedCodePaths struct {
	paths             []string
	deferredPaths     []string
	earlyTasks        []codeTask
	moduleByPath      map[string][]codeanchor.IntelAnchor
	fileContentByPath map[string][]byte
}

// Empty reports whether there is any early or deferred code work to plan.
func (p PreparedCodePaths) Empty() bool {
	return len(p.earlyTasks) == 0 && len(p.deferredPaths) == 0
}

// Paths returns the unique code paths represented by this prepared batch.
func (p PreparedCodePaths) Paths() []string {
	return append([]string(nil), p.paths...)
}

// EarlyTaskCount reports call-insensitive tasks that may embed before the final
// call-edge rebuild barrier.
func (p PreparedCodePaths) EarlyTaskCount() int {
	return len(p.earlyTasks)
}

// HasDeferredPaths reports whether this batch still has paths that need a
// barrier-sensitive planning pass.
func (p PreparedCodePaths) HasDeferredPaths() bool {
	return len(p.deferredPaths) > 0
}

func (p PreparedCodePaths) deferredSubset() PreparedCodePaths {
	if len(p.deferredPaths) == 0 {
		return PreparedCodePaths{}
	}
	subset := PreparedCodePaths{
		paths:             append([]string(nil), p.deferredPaths...),
		deferredPaths:     append([]string(nil), p.deferredPaths...),
		moduleByPath:      make(map[string][]codeanchor.IntelAnchor, len(p.deferredPaths)),
		fileContentByPath: make(map[string][]byte, len(p.deferredPaths)),
	}
	for _, path := range p.deferredPaths {
		if anchors, ok := p.moduleByPath[path]; ok {
			subset.moduleByPath[path] = append([]codeanchor.IntelAnchor(nil), anchors...)
		}
		if data, ok := p.fileContentByPath[path]; ok {
			subset.fileContentByPath[path] = append([]byte(nil), data...)
		}
	}
	return subset
}

// MergePreparedCodePaths coalesces streamed preparation results while preserving
// the split between early tasks and deferred barrier-sensitive paths.
func MergePreparedCodePaths(batches []PreparedCodePaths) PreparedCodePaths {
	if len(batches) == 0 {
		return PreparedCodePaths{}
	}
	merged := PreparedCodePaths{
		moduleByPath:      make(map[string][]codeanchor.IntelAnchor),
		fileContentByPath: make(map[string][]byte),
	}
	seen := make(map[string]struct{})
	deferredSeen := make(map[string]struct{})
	for _, batch := range batches {
		for _, path := range batch.paths {
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			merged.paths = append(merged.paths, path)
			if anchors, ok := batch.moduleByPath[path]; ok {
				merged.moduleByPath[path] = append([]codeanchor.IntelAnchor(nil), anchors...)
			}
			if data, ok := batch.fileContentByPath[path]; ok {
				merged.fileContentByPath[path] = append([]byte(nil), data...)
			}
		}
		for _, path := range batch.deferredPaths {
			if _, ok := deferredSeen[path]; ok {
				continue
			}
			deferredSeen[path] = struct{}{}
			merged.deferredPaths = append(merged.deferredPaths, path)
		}
		merged.earlyTasks = mergeCodeTasks(merged.earlyTasks, batch.earlyTasks)
	}
	sort.Strings(merged.paths)
	sort.Strings(merged.deferredPaths)
	return merged
}

// SyncPath incrementally syncs code embeddings for one code path using current intel anchors.
func (s *Syncer) SyncPath(ctx context.Context, path string) error {
	return s.SyncPaths(ctx, []string{path})
}

// SyncPaths incrementally syncs code embeddings for specific code paths.
func (s *Syncer) SyncPaths(ctx context.Context, paths []string) error {
	plan, err := s.PlanPaths(ctx, paths)
	if err != nil {
		return err
	}
	return s.EmbedWithoutSyncMark(ctx, plan)
}

// PlanPaths prepares code embedding work for specific code paths only.
func (s *Syncer) PlanPaths(ctx context.Context, paths []string) (SyncPlan, error) {
	prepared, err := s.PreparePathsEarly(ctx, paths)
	if err != nil {
		return SyncPlan{}, err
	}
	return s.PlanPreparedPaths(ctx, prepared)
}

// PreparePathsEarly loads path-scoped semantic inputs that are safe to compute
// before call-edge rebuild finalizes reverse-index state.
//
// Trade-off: we do as much work as possible here to overlap semantic prep with
// code ingest, but we must not materialize tasks that depend on final
// cross-file call-edge state. That later slice stays deferred so fallback and
// rebuild correctness still win over maximum eagerness.
func (s *Syncer) PreparePathsEarly(ctx context.Context, paths []string) (PreparedCodePaths, error) {
	return s.preparePathsEarly(ctx, paths, nil)
}

// PlanPublishedPaths uses source bytes sealed by the coordinator against the
// structural source hash, so unseen filesystem edits cannot enter an old plan.
func (s *Syncer) PlanPublishedPaths(ctx context.Context, paths []string, loader FileLoader) (SyncPlan, error) {
	prepared, err := s.preparePathsEarly(ctx, paths, loader)
	if err != nil {
		return SyncPlan{}, err
	}
	return s.PlanPreparedPaths(ctx, prepared)
}

func (s *Syncer) preparePathsEarly(ctx context.Context, paths []string, publishedLoader FileLoader) (PreparedCodePaths, error) {
	if s.Index == nil || s.Provider == nil || s.Intel == nil {
		return PreparedCodePaths{}, errors.New("syncer requires index, provider, and intel source")
	}
	if s.Budget.TargetChars == 0 {
		s.Budget = DefaultChunkBudget()
	}
	if s.Policy == nil {
		s.Policy = DefaultSynthesisPolicy(PolicyOptions{Budget: s.Budget})
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	normalized := normalizeCodePaths(paths)
	if len(normalized) == 0 {
		return PreparedCodePaths{}, nil
	}

	loader := publishedLoader
	if loader == nil {
		loader = s.Loader
	}
	if loader == nil && s.Root != "" {
		loader = FileSystemLoader(s.Root)
	}
	if err := s.ensureReady(ctx); err != nil {
		return PreparedCodePaths{}, err
	}

	moduleByPath, err := s.loadAnchorsByPath(ctx, normalized)
	if err != nil {
		return PreparedCodePaths{}, err
	}
	if err := s.syncPathItemMetadata(ctx, normalized, moduleByPath); err != nil {
		return PreparedCodePaths{}, err
	}

	prepared := PreparedCodePaths{
		paths:             normalized,
		moduleByPath:      moduleByPath,
		fileContentByPath: preloadCodePathContents(normalized, loader),
	}
	s.prepareEarlyTasks(ctx, &prepared)
	return prepared, nil
}

// PrepareCodeIndexWorksEarly builds prepared semantic work directly from fresh
// code index output, avoiding a round-trip through persisted intel rows.
func (s *Syncer) PrepareCodeIndexWorksEarly(ctx context.Context, works []codeanchor.CodeIndexWork) (PreparedCodePaths, error) {
	if s.Index == nil || s.Provider == nil || s.Intel == nil {
		return PreparedCodePaths{}, errors.New("syncer requires index, provider, and intel source")
	}
	if s.Budget.TargetChars == 0 {
		s.Budget = DefaultChunkBudget()
	}
	if s.Policy == nil {
		s.Policy = DefaultSynthesisPolicy(PolicyOptions{Budget: s.Budget})
	}
	if err := s.ensureReady(ctx); err != nil {
		return PreparedCodePaths{}, err
	}
	moduleByPath := make(map[string][]codeanchor.IntelAnchor)
	paths := make([]string, 0, len(works))
	seen := make(map[string]struct{}, len(works))
	for _, work := range works {
		path := strings.TrimSpace(work.Path)
		if path == "" || !work.ReplaceIndex || len(work.IntelAnchors) == 0 {
			continue
		}
		if _, ok := seen[path]; !ok {
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
		moduleByPath[path] = append([]codeanchor.IntelAnchor(nil), work.IntelAnchors...)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return PreparedCodePaths{}, nil
	}
	if err := s.syncPathItemMetadata(ctx, paths, moduleByPath); err != nil {
		return PreparedCodePaths{}, err
	}
	loader := s.Loader
	if loader == nil && s.Root != "" {
		loader = FileSystemLoader(s.Root)
	}
	prepared := PreparedCodePaths{
		paths:             paths,
		moduleByPath:      moduleByPath,
		fileContentByPath: preloadCodePathContents(paths, loader),
	}
	s.prepareEarlyTasks(ctx, &prepared)
	return prepared, nil
}

// PlanPreparedPaths completes planning after any rebuild barrier that affects
// cross-file reference state has cleared.
//
// This is intentionally the “safe but complete” entrypoint: merge early tasks
// that already existed with deferred tasks that only become valid after rebuild.
func (s *Syncer) PlanPreparedPaths(ctx context.Context, prepared PreparedCodePaths) (SyncPlan, error) {
	if prepared.Empty() {
		return SyncPlan{skipEmbed: true}, nil
	}
	forceReembed, err := s.preparedPathsForceReembed(ctx)
	if err != nil {
		return SyncPlan{}, err
	}

	tasks := append([]codeTask(nil), prepared.earlyTasks...)
	if len(prepared.deferredPaths) > 0 {
		s.progressf("Building chunks")
		deferredTasks, err := s.buildTasksForPreparedPaths(ctx, prepared.deferredSubset())
		if err != nil {
			return SyncPlan{}, err
		}
		tasks = mergeCodeTasks(tasks, deferredTasks)
	}
	return s.planTasksForEmbedding(ctx, tasks, forceReembed)
}

func (s *Syncer) PlanPreparedPathsEarly(ctx context.Context, prepared PreparedCodePaths) (SyncPlan, error) {
	if len(prepared.earlyTasks) == 0 {
		return SyncPlan{skipEmbed: true}, nil
	}
	forceReembed, err := s.preparedPathsForceReembed(ctx)
	if err != nil {
		return SyncPlan{}, err
	}
	return s.planTasksForEmbedding(ctx, prepared.earlyTasks, forceReembed)
}

func (s *Syncer) PlanPreparedPathsDeferred(ctx context.Context, prepared PreparedCodePaths) (SyncPlan, error) {
	if len(prepared.deferredPaths) == 0 {
		return SyncPlan{skipEmbed: true}, nil
	}
	// Deferred paths are the intentional cost of keeping early streaming
	// correct. If this stage ever grows again, prefer moving more synthesis into
	// PreparePathsEarly instead of removing the rebuild barrier entirely.
	forceReembed, err := s.preparedPathsForceReembed(ctx)
	if err != nil {
		return SyncPlan{}, err
	}
	s.progressf("Building chunks")
	tasks, err := s.buildTasksForPreparedPaths(ctx, prepared.deferredSubset())
	if err != nil {
		return SyncPlan{}, err
	}
	return s.planTasksForEmbedding(ctx, tasks, forceReembed)
}

func (s *Syncer) preparedPathsForceReembed(ctx context.Context) (bool, error) {
	storedMeta, metaOK, err := s.Index.Metadata(ctx)
	if err != nil {
		return false, err
	}
	return !metaOK || storedMeta.FingerprintVersion != codeFingerprintVersion, nil
}

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
		indexingperf.ObserveLatency(ctx, "plan.load_anchors_by_path", time.Since(started))
		return byPath, nil
	}

	s.progressf("Loading intel anchors")
	started := time.Now()
	anchors, err := s.Intel.IntelAnchors(ctx)
	indexingperf.ObserveLatency(ctx, "plan.load_anchors", time.Since(started))
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
				indexingperf.AddCount(ctx, "semantic.suppressed_anchors."+sanitizeMetricPart(anchor.Kind), 1)
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
