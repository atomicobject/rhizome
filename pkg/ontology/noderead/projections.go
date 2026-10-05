package noderead

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// Projections returns projections in request order, including nil entries for
// misses. Host metadata, types, and assessments are loaded as one batch; source
// projections share the same per-ref cache as Projection.
func (s *Scope) Projections(ctx context.Context, refs []ontology.NodeRef) ([]*ontology.NodeProjection, error) {
	if s == nil || s.service == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = normalizeNodeRef(ref)
		if !ref.IsZero() && !s.projectionKnown[projectionCacheKey(ref)] {
			paths = append(paths, ref.NotePath)
		}
	}
	if len(paths) > 0 {
		if err := s.ensurePathStateLocked(ctx, paths); err != nil {
			return nil, err
		}
	}
	projections := make([]*ontology.NodeProjection, len(refs))
	for i, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		projection, err := s.projectionLocked(ctx, ref)
		if err != nil {
			return nil, err
		}
		projections[i] = projection
	}
	return projections, nil
}
