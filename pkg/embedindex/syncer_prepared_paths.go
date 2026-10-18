package embedindex

import (
	"context"
	"errors"

	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"

	codeindex "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
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
