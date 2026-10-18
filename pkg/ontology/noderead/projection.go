package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	semdb "github.com/atomicobject/rhizome/pkg/store"
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
	if ref.Kind == ontology.NodeKindNote || (ref.Fragment == "" && ref.NodeID == "") {
		projection, handled, err := s.providerRootProjectionLocked(ctx, ref)
		if err != nil {
			return nil, err
		}
		if handled {
			s.projectionKnown[key] = true
			s.projectionByRef[key] = projection
			return projection, nil
		}
	}
	// IMPORTANT: projectionCacheKey is richer than NodeRef.String so note-root,
	// embedded, section, and structural fallback refs never share a projection.
	// Route through the per-note snapshot cache so callers projecting many refs
	// from the same host note pay one parse, not N.
	snapshot, err := s.snapshotForPathLocked(ctx, ref.NotePath)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		s.projectionKnown[key] = true
		return nil, nil
	}
	projection, err := ontology.ProjectNodeFromSnapshot(snapshot, s.service.Schema, ref)
	if err != nil {
		return nil, err
	}
	projection = projectFallback(projection)
	if projection != nil && projection.Ref.Kind == ontology.NodeKindNote {
		// Provider facts belong to the same captured bytes as this structural
		// snapshot. Pointer identity excludes a staged overlay snapshot from
		// accidentally acquiring facts from a committed source.
		if root := s.rootSnapshotByPath[ref.NotePath]; root != nil && root.MarkdownStructure == snapshot {
			projection.RootSnapshot = root
		}
	}
	s.projectionKnown[key] = true
	if projection != nil {
		s.projectionByRef[key] = projection
		canonicalKey := RefIdentityKey(projection.Ref)
		s.projectionKnown[canonicalKey] = true
		s.projectionByRef[canonicalKey] = projection
	}
	return projection, nil
}

func (s *Scope) providerRootProjectionLocked(ctx context.Context, ref ontology.NodeRef) (*ontology.NodeProjection, bool, error) {
	if err := s.ensurePathStateLocked(ctx, []string{ref.NotePath}); err != nil {
		return nil, false, err
	}
	row, ok := s.noteRowByPath[ref.NotePath]
	if !ok || row.Projection.Status != semdb.NoteProjectionStatusCurrent {
		return nil, false, nil
	}
	provider, registered := s.service.NoteFormats.Provider(noteformat.FormatID(row.FormatID))
	if !registered {
		return nil, false, nil
	}
	if provider.Descriptor().Capabilities.Has(noteformat.CapabilityStructuralContentMutation) {
		// A supported structural adapter retains both source facts and syntax.
		// Foreign structural providers still expose their native root facts.
		snapshot, err := s.snapshotForPathLocked(ctx, ref.NotePath)
		if err != nil {
			return nil, true, err
		}
		if snapshot != nil {
			return nil, false, nil
		}
	}
	root, err := s.providerRootSnapshotLocked(ctx, row)
	if err != nil || root == nil {
		return nil, true, err
	}
	projection, err := ontology.ProjectRootDocumentSnapshot(root, s.service.Schema)
	if fallback, ok := ontology.AsFallbackNoteProjection(projection); ok {
		projection = fallback
	}
	return projection, true, err
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
