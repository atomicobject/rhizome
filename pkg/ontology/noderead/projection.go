package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

// Projection returns a request-scoped cached ontology projection for a NodeRef.
// It gives non-GraphQL consumers such as semantic indexing the same
// projection cache used by hydration and embedded-node enumeration.
func (s *Scope) Projection(ctx context.Context, ref ontology.NodeRef) (*ontology.NodeProjection, error) {
	if s == nil || s.service == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.projectionLocked(ctx, ref)
}

func (s *Scope) projectionLocked(ctx context.Context, ref ontology.NodeRef) (*ontology.NodeProjection, error) {
	ref = NormalizeRef(ref)
	if ref.IsZero() {
		return nil, nil
	}
	key := RefIdentityKey(ref)
	if s.projectionKnown[key] {
		return s.projectionByRef[key], nil
	}
	prepared, err := s.preparedForPathLocked(ctx, ref.NotePath)
	if err != nil {
		return nil, err
	}
	if prepared == nil {
		s.projectionKnown[key] = true
		return nil, nil
	}
	projection, err := prepared.Project(ref)
	if err != nil {
		return nil, err
	}
	projection = projectFallback(projection)

	s.projectionKnown[key] = true
	if projection != nil {
		s.projectionByRef[key] = projection
		canonicalKey := RefIdentityKey(projection.Ref)
		s.projectionKnown[canonicalKey] = true
		s.projectionByRef[canonicalKey] = projection
	}
	return projection, nil
}

func projectFallback(projection *ontology.NodeProjection) *ontology.NodeProjection {
	if fallback, ok := ontology.AsFallbackNoteProjection(projection); ok {
		return fallback
	}
	if fallback, ok := ontology.AsFallbackSectionProjection(projection); ok {
		return fallback
	}
	return projection
}
