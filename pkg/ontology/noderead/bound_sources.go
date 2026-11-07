package noderead

import (
	"context"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// Only a projection captured by this scope can identify an unanchored
// occurrence independently of its identical siblings. External refs stay strict.
func (s *Scope) boundProjectionRefsLocked(ctx context.Context, projections []*ontology.NodeProjection) (map[string]ontology.NodeRef, error) {
	bound := map[string]ontology.NodeRef{}
	for _, projection := range projections {
		if projection == nil {
			continue
		}
		prepared, err := s.preparedForPathLocked(ctx, projection.Ref.NotePath)
		if err != nil {
			return nil, err
		}
		if prepared == nil || prepared != projection.Prepared {
			return nil, fmt.Errorf("relation count source does not belong to this read scope")
		}
		current, err := prepared.ProjectBound(projection.Ref)
		if err != nil {
			return nil, err
		}
		if current == nil || RefIdentityKey(current.Ref) != RefIdentityKey(projection.Ref) {
			return nil, fmt.Errorf("relation count source is not a current bound occurrence")
		}
		bound[RefIdentityKey(current.Ref)] = current.Ref
	}
	return bound, nil
}
