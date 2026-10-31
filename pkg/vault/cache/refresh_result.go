package cache

import "context"

// RefreshResult separates raw filesystem work from cache content changes.
// Drained retains every dirty event consumed by this attempt, including paths
// rejected by the current selection policy. Changed contains only paths that
// changed cache-owned content or cache freshness state.
type RefreshResult struct {
	Drained map[string]DirtyKind
	Changed map[string]DirtyKind
	// Resynced reports that this call ran the initial crawl.
	Resynced bool
	// Recrawls counts stale-triggered recrawls completed over the service's
	// life. A caller compares it with the value it last saw.
	Recrawls uint64
	// RecrawlInFlight numbers the recrawl still running when this call
	// returned, or is 0. A later value means the earlier recrawl finished.
	RecrawlInFlight uint64
}

// RefreshWithResult reconciles cache state and reports both raw watcher input
// and cache content changes. On a batch-level failure, raw paths are requeued
// automatically so a later call can retry them.
func (s *Service) RefreshWithResult(ctx context.Context) (RefreshResult, error) {
	return s.refresh(ctx)
}

// HasPendingWork reports whether a Refresh could find work: the cache is not
// ready, is stale, has a recrawl in flight, holds dirty paths, or has completed
// more recrawls than seenRecrawls (the caller's last RefreshResult.Recrawls).
func (s *Service) HasPendingWork(seenRecrawls uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.ready || s.stale || s.recrawling || len(s.dirty) > 0 || s.recrawls > seenRecrawls
}

// recrawlStateLocked reports recrawl progress into result. Callers hold s.mu.
func (s *Service) recrawlStateLocked(result *RefreshResult) {
	result.Recrawls = s.recrawls
	if s.recrawling {
		result.RecrawlInFlight = s.attempts
	}
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
