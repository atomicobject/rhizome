package query

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
)

func (e *executor) resolveNodeRefTarget(ctx context.Context, target string) (ontology.NodeRef, bool, error) {
	ref, _, ok, err := e.resolveNodeRefTargetCandidates(ctx, target)
	return ref, ok, err
}

func (e *executor) resolveNodeRefTargetCandidates(ctx context.Context, target string) (ontology.NodeRef, []ontology.NodeRef, bool, error) {
	target = strings.TrimSpace(target)
	if target == "" || e.loaders == nil || e.loaders.scope == nil {
		return ontology.NodeRef{}, nil, false, nil
	}
	result, err := e.resolveTargetIdentity(ctx, target)
	if err != nil {
		return ontology.NodeRef{}, nil, false, err
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == "target_ambiguous" && len(diagnostic.Candidates) > 1 {
			return ontology.NodeRef{}, sortedUniqueNodeRefs(diagnostic.Candidates), false, nil
		}
	}
	if len(result.Resolved) == 0 {
		return ontology.NodeRef{}, nil, false, nil
	}
	resolved := result.Resolved[0]
	// A blank hydrated record has no catalog-backed note payload. Keep code
	// fallback available without inventing a note or repeating Scope resolution.
	if strings.TrimSpace(resolved.Record.Path) == "" && strings.TrimSpace(resolved.Ref.TypeName) == "" {
		return ontology.NodeRef{}, nil, false, nil
	}
	return resolved.Ref, nil, !resolved.Ref.IsZero(), nil
}

// Query consumers need canonical identity and records, not link spelling or fix
// plans. Keep catalog resolution and ambiguity handling inside the shared scope.
func (e *executor) resolveTargetIdentity(ctx context.Context, target string) (noderead.ResolveResult, error) {
	return e.loaders.scope.Resolve(ctx, noderead.ResolveRequest{
		Targets: []noderead.NodeTarget{{Input: target}},
		Hydrate: noderead.HydrateOptions{Profile: noderead.HydrateSummary},
	})
}
