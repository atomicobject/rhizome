package codeanchor

// HasDirtyAnchors reports whether a scope recompute is pending.
func (s *Service) HasDirtyAnchors() bool {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	return s.dirtyOverflow || len(s.dirtyAnchors) > 0
}

func (s *Service) dirtyIDsSnapshot() (overflow bool, ids []int64) {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	if s.dirtyOverflow {
		return true, nil
	}
	if len(s.dirtyAnchors) == 0 {
		return false, nil
	}
	ids = make([]int64, 0, len(s.dirtyAnchors))
	for id := range s.dirtyAnchors {
		ids = append(ids, id)
	}
	return false, ids
}

// DirtyAnchorIDsSnapshot returns the currently dirty anchor IDs without clearing them.
// Overflow indicates a broad exact recompute is safer than incremental replay.
func (s *Service) DirtyAnchorIDsSnapshot() (overflow bool, ids []int64) {
	return s.dirtyIDsSnapshot()
}

func (s *Service) markAnchorsDirty(ids []int64) {
	if len(ids) == 0 {
		return
	}
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	// If adding these would exceed the cap, clear to force a full recompute instead of unbounded growth.
	if dirtyAnchorLimit > 0 && len(s.dirtyAnchors)+len(ids) > dirtyAnchorLimit {
		s.dirtyAnchors = make(map[int64]bool)
		s.dirtyOverflow = true
		return
	}
	for _, id := range ids {
		s.dirtyAnchors[id] = true
	}
}

func (s *Service) dirtySnapshot() (bool, map[int64]bool) {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	if s.dirtyOverflow {
		// Overflow means "full recompute"; treat dirty IDs as handled by that recompute.
		// Clearing here prevents repeated scope recomputes on subsequent queries.
		s.dirtyAnchors = make(map[int64]bool)
		s.dirtyOverflow = false
		return false, nil
	}
	if len(s.dirtyAnchors) == 0 {
		return false, nil
	}
	cp := make(map[int64]bool, len(s.dirtyAnchors))
	for id := range s.dirtyAnchors {
		cp[id] = true
	}
	return true, cp
}

func (s *Service) clearDirtyAnchorsByIDs(ids []int64) {
	if len(ids) == 0 {
		return
	}
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	for _, id := range ids {
		delete(s.dirtyAnchors, id)
	}
}
