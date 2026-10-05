package codeanchor

import (
	"context"
	"log"
	"time"
)

type anchorScopePlan struct {
	updates []AnchorScopeUpdate
}

// Anchors returns the current anchor set.
func (s *Service) Anchors(ctx context.Context) ([]Anchor, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}
	return s.store.Anchors(ctx)
}

// AnchorsByNotePaths returns anchors currently defined by the given note paths.
func (s *Service) AnchorsByNotePaths(ctx context.Context, paths []string) (map[string][]Anchor, error) {
	if s == nil || s.store == nil || len(paths) == 0 {
		return nil, nil
	}
	return s.store.AnchorsByNotePaths(ctx, paths)
}

// CodeFilesForNote returns materialized code files linked to a note's anchors.
func (s *Service) CodeFilesForNote(ctx context.Context, path string) ([]string, error) {
	if s == nil || s.store == nil || path == "" {
		return nil, nil
	}
	reader, ok := s.store.(interface {
		CodeFilesForNote(context.Context, string) ([]string, error)
	})
	if !ok {
		return nil, nil
	}
	return reader.CodeFilesForNote(ctx, path)
}

// RebuildAnchorScopesForIDs materializes scopes for the provided anchors only.
// Unlike RecomputeAnchorScopes, this path never performs hidden call-edge rebuilds.
//
// Use this for incremental warmups after an exact file_context fallback. It is
// intentionally narrow so a read request cannot accidentally trigger a global
// resolver pass.
func (s *Service) RebuildAnchorScopesForIDs(ctx context.Context, ids []int64) error {
	if !s.writeAccess || len(ids) == 0 {
		return nil
	}
	s.recomputeMu.Lock()
	defer s.recomputeMu.Unlock()

	plan, err := s.planAnchorScopesForIDs(ctx, ids)
	if err != nil {
		return err
	}
	if err := s.applyAnchorScopePlan(ctx, plan); err != nil {
		return err
	}
	s.clearDirtyAnchorsByIDs(ids)
	return nil
}

// RecomputeAnchorScopes materializes scopes for all anchors.
// This operation is serialized to prevent concurrent recomputes and
// races with anchor modifications.
// Requires WithWriteAccess(); no-op for read-only services.
func (s *Service) RecomputeAnchorScopes(ctx context.Context) error {
	if !s.writeAccess {
		return nil
	}
	s.recomputeMu.Lock()
	defer s.recomputeMu.Unlock()

	anchors, err := s.store.Anchors(ctx)
	if err != nil {
		return err
	}

	useDirty, dirtySet := s.dirtySnapshot()
	hasWildcard := containsWildcardAnchors(anchors)
	if useDirty && !hasWildcard {
		filtered := anchors[:0]
		for _, a := range anchors {
			if dirtySet[a.ID] {
				filtered = append(filtered, a)
			}
		}
		anchors = filtered
	}

	hasFuncAnchors := containsFunctionAnchors(anchors)
	if hasFuncAnchors && !s.callEdgesRebuilt {
		// Function anchors depend on reverse call-file lookups. Batch indexing marks
		// stale paths instead of resolving edges per file, so the first full scope
		// recompute owns the catch-up rebuild.
		var stale []string
		if lister, ok := s.store.(callEdgeStaleLister); ok {
			stale, err = lister.StaleCallEdgePaths(ctx, 0)
			if err != nil {
				return err
			}
		}
		if len(stale) > 0 {
			log.Printf("codeanchor: rebuild call edges for %d stale files", len(stale))
			if err := s.RebuildCallEdgesForPaths(ctx, stale); err != nil && (ctx == nil || ctx.Err() == nil) {
				log.Printf("codeanchor: rebuild call edges failed: %v", err)
			} else {
				s.callEdgesRebuilt = true
			}
		} else {
			needsFull := false
			if checker, ok := s.store.(callEdgeChecker); ok {
				if hasEdges, err := checker.HasAnyCallEdges(ctx); err == nil {
					if !hasEdges {
						needsFull = true
					} else {
						s.callEdgesRebuilt = true
					}
				}
			}
			if needsFull {
				log.Printf("codeanchor: rebuild call edges (bootstrap)")
				if err := s.RebuildAllCallEdges(ctx); err != nil && (ctx == nil || ctx.Err() == nil) {
					log.Printf("codeanchor: rebuild call edges failed: %v", err)
				} else {
					s.callEdgesRebuilt = true
				}
			} else if !s.callEdgesRebuilt {
				log.Printf("codeanchor: rebuild call edges skipped (edges exist, no stale paths)")
				s.callEdgesRebuilt = true
			}
		}
	}
	plan, err := s.planAnchorScopes(ctx, anchors)
	if err != nil {
		return err
	}
	if err := s.applyAnchorScopePlan(ctx, plan); err != nil {
		return err
	}
	if useDirty && len(dirtySet) > 0 {
		updatedIDs := make([]int64, 0, len(dirtySet))
		for id := range dirtySet {
			updatedIDs = append(updatedIDs, id)
		}
		s.clearDirtyAnchorsByIDs(updatedIDs)
	}
	return nil
}

func (s *Service) planAnchorScopesForIDs(ctx context.Context, ids []int64) (anchorScopePlan, error) {
	if len(ids) == 0 {
		return anchorScopePlan{}, nil
	}
	anchors, err := s.store.AnchorsByIDs(ctx, ids)
	if err != nil {
		return anchorScopePlan{}, err
	}
	return s.planAnchorScopes(ctx, anchors)
}

func (s *Service) planAnchorScopes(ctx context.Context, anchors []Anchor) (anchorScopePlan, error) {
	if len(anchors) == 0 {
		return anchorScopePlan{}, nil
	}

	// Pre-batch expensive lookup families once per recompute. The previous shape
	// of this code performed resolver/store reads per anchor, which scaled badly
	// for broad anchors and large repositories.
	callFilesByKey, err := s.fetchCallFilesByKey(ctx, anchors)
	if err != nil {
		return anchorScopePlan{}, err
	}
	annUsesByKey, err := s.fetchAnnotationUsesByKey(ctx, anchors)
	if err != nil {
		return anchorScopePlan{}, err
	}
	progress := scopeProgressFromContext(ctx)
	if progress != nil {
		progress.Start(len(anchors))
	}

	plan := anchorScopePlan{updates: make([]AnchorScopeUpdate, 0, len(anchors))}
	for _, anchor := range anchors {
		switch anchor.Kind {
		case AnchorKind("functionUse"):
			anchor.Kind = AnchorFunc
		}
		update, ok, err := s.planAnchorScopeUpdate(ctx, anchor, callFilesByKey, annUsesByKey)
		if err != nil {
			return anchorScopePlan{}, err
		}
		if ok {
			plan.updates = append(plan.updates, update)
		}
		if progress != nil {
			progress.Advance(1)
		}
	}
	return plan, nil
}

func (s *Service) applyAnchorScopePlan(ctx context.Context, plan anchorScopePlan) error {
	if len(plan.updates) == 0 {
		return nil
	}
	if setter, ok := s.store.(scopeBatchSetter); ok {
		if err := setter.SetAnchorScopesBatch(ctx, plan.updates); err != nil {
			return err
		}
	} else {
		for _, upd := range plan.updates {
			if err := s.store.SetAnchorScope(ctx, upd.ID, upd.Symbols, upd.CallFiles); err != nil {
				return err
			}
		}
	}
	s.invalidateAllFiles()
	s.setAnchorScopesAvailable(true)
	return nil
}

func containsFunctionAnchors(anchors []Anchor) bool {
	for _, anchor := range anchors {
		if anchor.Kind == AnchorFunc || anchor.Kind == AnchorKind("functionUse") {
			return true
		}
	}
	return false
}

func (s *Service) fetchCallFilesByKey(ctx context.Context, anchors []Anchor) (map[string][]string, error) {
	seen := make(map[string]SymbolRef)
	for _, anchor := range anchors {
		if anchor.Kind != AnchorFunc || anchor.BaseSym == nil {
			continue
		}
		key := symbolRefKey(*anchor.BaseSym)
		if _, ok := seen[key]; !ok {
			seen[key] = *anchor.BaseSym
		}
	}
	if len(seen) == 0 {
		return nil, nil
	}
	refs := make([]SymbolRef, 0, len(seen))
	for _, ref := range seen {
		refs = append(refs, ref)
	}
	merged := s.runtimeCallFilesByRefs(refs)
	batcher, ok := s.store.(callFilesByCalleeBatch)
	if !ok {
		return merged, nil
	}
	stored, err := batcher.CallFilesByCallees(ctx, refs)
	if err != nil {
		return nil, err
	}
	if len(merged) == 0 {
		return stored, nil
	}
	for key, paths := range stored {
		merged[key] = uniqueStrings(append(merged[key], paths...))
	}
	return merged, nil
}

func (s *Service) fetchAnnotationUsesByKey(ctx context.Context, anchors []Anchor) (map[string][]AnnotationUse, error) {
	batcher, ok := s.store.(annotationUsesByTypeBatch)
	if !ok {
		return nil, nil
	}
	seen := make(map[string]SymbolRef)
	for _, anchor := range anchors {
		if anchor.Kind != AnchorAnnotation || anchor.Ann == nil {
			continue
		}
		key := symbolRefKey(anchor.Ann.Symbol)
		if _, ok := seen[key]; !ok {
			seen[key] = anchor.Ann.Symbol
		}
	}
	if len(seen) == 0 {
		return nil, nil
	}
	refs := make([]SymbolRef, 0, len(seen))
	for _, ref := range seen {
		refs = append(refs, ref)
	}
	return batcher.AnnotationUsesByTypes(ctx, refs)
}

func (s *Service) planAnchorScopeUpdate(ctx context.Context, anchor Anchor, callFilesByKey map[string][]string, annUsesByKey map[string][]AnnotationUse) (AnchorScopeUpdate, bool, error) {
	switch anchor.Kind {
	case AnchorBaseClass:
		if anchor.BaseSym == nil {
			return AnchorScopeUpdate{}, false, nil
		}
		target := normalizeTypeSymbol(*anchor.BaseSym)
		desc, err := s.collectDescendants(ctx, target)
		if err != nil {
			return AnchorScopeUpdate{}, false, err
		}
		desc = append(desc, target)
		return AnchorScopeUpdate{ID: anchor.ID, Symbols: desc}, true, nil
	case AnchorFunc:
		if anchor.BaseSym == nil {
			return AnchorScopeUpdate{}, false, nil
		}
		target := normalizeSymbol(*anchor.BaseSym)
		var callFiles []string
		if callFilesByKey != nil {
			callFiles = callFilesByKey[symbolRefKey(*anchor.BaseSym)]
		} else {
			var err error
			callFiles, err = s.store.CallFilesByCallee(ctx, anchor.BaseSym)
			if err != nil {
				return AnchorScopeUpdate{}, false, err
			}
		}
		callFiles = normalizePathCaseSlice(callFiles)
		return AnchorScopeUpdate{ID: anchor.ID, Symbols: []string{target}, CallFiles: callFiles}, true, nil
	case AnchorAnnotation:
		if anchor.Ann == nil {
			return AnchorScopeUpdate{}, false, nil
		}
		var uses []AnnotationUse
		if annUsesByKey != nil {
			uses = annUsesByKey[symbolRefKey(anchor.Ann.Symbol)]
		} else {
			var err error
			uses, err = s.store.AnnotationUsesByType(ctx, anchor.Ann.Symbol)
			if err != nil {
				return AnchorScopeUpdate{}, false, err
			}
		}
		var owners []string
		for _, use := range uses {
			if matchArgs(anchor.Ann.ArgFilters, use.Args) {
				owners = append(owners, use.OwnerFQN)
			}
		}
		return AnchorScopeUpdate{ID: anchor.ID, Symbols: owners}, true, nil
	default:
		return AnchorScopeUpdate{}, false, nil
	}
}

func (s *Service) anchorScopesAvailable(ctx context.Context) bool {
	if s == nil || s.store == nil {
		return false
	}
	s.scopeProbeMu.Lock()
	if s.hasAnyScopes {
		s.scopeProbeMu.Unlock()
		return true
	}
	s.scopeProbeMu.Unlock()

	s.scopeProbeOnce.Do(func() {
		ok := false
		if p, okStore := s.store.(anchorScopeProber); okStore {
			if has, err := p.HasAnyAnchorScopes(ctx); err == nil {
				ok = has
			}
		}
		s.scopeProbeMu.Lock()
		s.hasAnyScopes = ok
		s.scopeProbeMu.Unlock()
	})
	s.scopeProbeMu.Lock()
	ok := s.hasAnyScopes
	s.scopeProbeMu.Unlock()
	return ok
}

func (s *Service) setAnchorScopesAvailable(has bool) {
	if s == nil {
		return
	}
	s.scopeProbeMu.Lock()
	s.hasAnyScopes = has
	s.scopeProbeMu.Unlock()
}

func (s *Service) warmAnchorScopesAsync(anchorIDs []int64) {
	if s == nil || !s.writeAccess {
		return
	}
	if len(anchorIDs) > 0 {
		// Prevent pathological warm requests from forcing huge recomputes.
		const maxWarmIDs = 256
		if len(anchorIDs) > maxWarmIDs {
			anchorIDs = anchorIDs[:maxWarmIDs]
		}
		s.markAnchorsDirty(anchorIDs)
	}
	if !s.HasDirtyAnchors() {
		return
	}

	s.warmMu.Lock()
	if s.warmRunning {
		s.warmMu.Unlock()
		return
	}
	s.warmRunning = true
	s.warmMu.Unlock()

	go func() {
		defer func() {
			s.warmMu.Lock()
			s.warmRunning = false
			s.warmMu.Unlock()
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = s.RecomputeAnchorScopes(ctx)

		if p, ok := s.store.(anchorScopeProber); ok {
			if has, err := p.HasAnyAnchorScopes(ctx); err == nil {
				s.setAnchorScopesAvailable(has)
			}
		}
	}()
}

func (s *Service) collectDescendants(ctx context.Context, root string) ([]string, error) {
	const maxInheritanceDepth = 50
	var out []string
	seen := map[string]bool{}
	queue := []string{root}
	for depth := 0; len(queue) > 0 && depth < maxInheritanceDepth; depth++ {
		next := queue[:0]
		if batcher, ok := s.store.(childrenBatch); ok {
			childrenByParent, err := batcher.ChildrenBatch(ctx, queue)
			if err != nil {
				return nil, err
			}
			for _, parent := range queue {
				for _, c := range childrenByParent[parent] {
					if seen[c] {
						continue
					}
					seen[c] = true
					out = append(out, c)
					next = append(next, c)
				}
			}
		} else {
			for _, parent := range queue {
				children, err := s.store.Children(ctx, parent)
				if err != nil {
					return nil, err
				}
				for _, c := range children {
					if seen[c] {
						continue
					}
					seen[c] = true
					out = append(out, c)
					next = append(next, c)
				}
			}
		}
		queue = next
	}
	return out, nil
}

// containsWildcardAnchors returns true if any anchor uses wildcard matching
// that is unsafe for incremental dirty tracking.
//
// We intentionally do NOT treat wildcard annotation anchors (empty pkg) as a
// reason to force full recomputation: they can still be dirtied and recomputed
// incrementally via AnchorsMatchingAnnotations / AnnotationUsesByType.
func containsWildcardAnchors(anchors []Anchor) bool {
	for _, a := range anchors {
		if isWildcardAnchor(a) {
			return true
		}
	}
	return false
}

// isWildcardAnchor returns true if the anchor matches any package (empty pkg)
// in a way that could cause missed dirty updates.
func isWildcardAnchor(a Anchor) bool {
	switch a.Kind {
	case AnchorBaseClass, AnchorFunc:
		return a.BaseSym != nil && a.BaseSym.Pkg == ""
	}
	return false
}
