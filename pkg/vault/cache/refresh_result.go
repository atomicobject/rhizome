package cache

import "context"

// RefreshResult separates raw filesystem work from cache content changes.
// Drained retains every dirty event consumed by this attempt, including paths
// rejected by the current selection policy. Changed contains only paths that
// changed cache-owned content or cache freshness state.
type RefreshResult struct {
	Drained  map[string]DirtyKind
	Changed  map[string]DirtyKind
	Resynced bool
}

// RefreshWithResult reconciles cache state and reports both raw watcher input
// and cache content changes. On a batch-level failure, raw paths are requeued
// automatically so a later call can retry them.
func (s *Service) RefreshWithResult(ctx context.Context) (RefreshResult, error) {
	return s.refresh(ctx)
}

func cloneDirty(dirty map[string]DirtyKind) map[string]DirtyKind {
	if len(dirty) == 0 {
		return nil
	}
	clone := make(map[string]DirtyKind, len(dirty))
	for path, kind := range dirty {
		clone[path] = kind
	}
	return clone
}

func (s *Service) requeueDirty(dirty map[string]DirtyKind) {
	for path, kind := range dirty {
		s.MarkDirty(path, kind)
	}
}
