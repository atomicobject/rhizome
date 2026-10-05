package identifierreconcile

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/ontology/reference"
)

func expandCollisionInputsWithDiscoveredRewrites(inputs []CollisionRepairInput, discovered []reference.IdentifierRewrite) ([]CollisionRepairInput, error) {
	out := make([]CollisionRepairInput, len(inputs))
	rootCollisionByRef := make(map[string]int)
	rootByIdentity := make(map[string]reference.IdentifierRewrite)
	for index, input := range inputs {
		out[index] = input
		roots, err := canonicalRepairRewriteSet(input.Rewrites)
		if err != nil {
			return nil, err
		}
		out[index].Rewrites = roots
		for _, root := range roots {
			if !root.DerivedFrom.IsZero() {
				return nil, fmt.Errorf("collision inputs accept root rewrites only; descendants come from sealed discovery")
			}
			identity := repairRewriteKey(root)
			if _, duplicate := rootByIdentity[identity]; duplicate {
				return nil, fmt.Errorf("root rewrite is duplicated across collision inputs")
			}
			rootByIdentity[identity] = root
			if root.Mode == reference.IdentifierRewritePreferredRekey {
				refKey := repairRefKey(root.OldRef)
				if _, duplicate := rootCollisionByRef[refKey]; duplicate {
					return nil, fmt.Errorf("preferred root rewrite is duplicated across collision inputs")
				}
				rootCollisionByRef[refKey] = index
			}
		}
	}

	discoveredByRef := make(map[string]reference.IdentifierRewrite, len(discovered))
	seenRoots := make(map[string]struct{}, len(rootByIdentity))
	for _, rewrite := range discovered {
		discoveredByRef[repairRefKey(rewrite.OldRef)] = rewrite
		if rewrite.DerivedFrom.IsZero() {
			identity := repairRewriteKey(rewrite)
			if _, supplied := rootByIdentity[identity]; !supplied {
				return nil, fmt.Errorf("sealed discovery root does not match collision inputs")
			}
			seenRoots[identity] = struct{}{}
		}
	}
	if len(seenRoots) != len(rootByIdentity) {
		return nil, fmt.Errorf("collision input root is absent from sealed discovery rewrite union")
	}

	for _, rewrite := range discovered {
		if rewrite.DerivedFrom.IsZero() {
			continue
		}
		parentRef := rewrite.DerivedFrom
		visited := map[string]struct{}{repairRefKey(rewrite.OldRef): {}}
		for {
			parentKey := repairRefKey(parentRef)
			if collisionIndex, root := rootCollisionByRef[parentKey]; root {
				out[collisionIndex].Rewrites = append(out[collisionIndex].Rewrites, rewrite)
				break
			}
			if _, cycle := visited[parentKey]; cycle {
				return nil, fmt.Errorf("sealed descendant rewrite cycle")
			}
			visited[parentKey] = struct{}{}
			parent, found := discoveredByRef[parentKey]
			if !found || parent.DerivedFrom.IsZero() {
				return nil, fmt.Errorf("sealed descendant rewrite has no collision root")
			}
			parentRef = parent.DerivedFrom
		}
	}
	return out, nil
}
