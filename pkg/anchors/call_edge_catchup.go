package codeanchor

import (
	"context"
	"log"
)

// catchUpCallEdges runs the call-edge rebuild that function anchor scopes
// depend on. Batch indexing marks stale paths instead of resolving edges per
// file, so the first full scope recompute owns this catch-up. It counts as
// done only once the parser and Go relationship phases both succeed; the
// parser phase clears stale markers before the Go phase runs, so a failure
// restores the work it consumed. Callers hold recomputeMu.
func (s *Service) catchUpCallEdges(ctx context.Context) error {
	var stale []string
	if lister, ok := s.store.(callEdgeStaleLister); ok {
		var err error
		if stale, err = lister.StaleCallEdgePaths(ctx, 0); err != nil {
			return err
		}
	}
	needsFull := s.callEdgesFullPending
	if len(stale) == 0 && !needsFull {
		if checker, ok := s.store.(callEdgeChecker); ok {
			if hasEdges, err := checker.HasAnyCallEdges(ctx); err == nil {
				needsFull = !hasEdges
			}
		}
		if !needsFull {
			log.Printf("codeanchor: rebuild call edges skipped (edges exist, no stale paths)")
			s.callEdgesRebuilt = true
			return nil
		}
	}
	var err error
	if needsFull {
		log.Printf("codeanchor: rebuild call edges (bootstrap)")
		err = s.RebuildAllCallEdges(ctx)
		s.callEdgesFullPending = err != nil
	} else {
		log.Printf("codeanchor: rebuild call edges for %d stale files", len(stale))
		if err = s.RebuildCallEdgesForPaths(ctx, stale); err != nil {
			if marker, ok := s.store.(callEdgeStaleMarker); ok && ctx != nil {
				_ = marker.MarkCallEdgesStale(context.WithoutCancel(ctx), stale)
			}
		}
	}
	if err == nil {
		s.callEdgesRebuilt = true
	} else if ctx == nil || ctx.Err() == nil {
		log.Printf("codeanchor: rebuild call edges failed: %v", err)
	}
	return nil
}
