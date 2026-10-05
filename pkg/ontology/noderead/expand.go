package noderead

import (
	"context"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// Expand performs bounded multi-step traversal from each source ref.
//
// Shared frontier nodes may be queried once, but each original source keeps its
// own seen set and grouped evidence.
func (s *Scope) Expand(ctx context.Context, plan ExpansionPlan) (ExpansionResult, error) {
	if err := ctx.Err(); err != nil {
		return ExpansionResult{}, err
	}
	sources := normalizeNeighborhoodSources(plan.Sources, nil)
	result := ExpansionResult{
		Sources:  make([]ExpansionSourceResult, 0, len(sources)),
		BySource: make(map[string]ExpansionSourceResult, len(sources)),
	}
	if len(sources) == 0 || len(plan.Steps) == 0 {
		for _, source := range sources {
			item := ExpansionSourceResult{Source: source}
			result.Sources = append(result.Sources, item)
			result.BySource[nodeRefResultKey(source)] = item
		}
		addExpansionLocatorAliases(sources, result.BySource)
		return result, nil
	}
	canonicalSources := sources
	if s != nil && s.service != nil && s.service.Store != nil {
		s.mu.Lock()
		var err error
		canonicalSources, err = s.canonicalizeNodeRefsLocked(ctx, sources)
		s.mu.Unlock()
		if err != nil {
			return ExpansionResult{}, err
		}
	}

	frontierByRoot := make(map[string][]ontology.NodeRef, len(sources))
	seenByRoot := make(map[string]map[string]struct{}, len(sources))
	sourceOrder := make([]string, 0, len(sources))
	for index, source := range sources {
		key := RefIdentityKey(source)
		sourceOrder = append(sourceOrder, key)
		frontierByRoot[key] = []ontology.NodeRef{canonicalSources[index]}
		seenByRoot[key] = map[string]struct{}{RefIdentityKey(canonicalSources[index]): {}}
	}

	maxDepth := plan.Limits.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 1
	}
	maxEdges := minPositiveLimit(plan.Limits.FirstTotal, minPositiveLimit(plan.Limits.MaxEdges, plan.Budget.MaxEdges))
	maxNodes := minPositiveLimit(plan.Limits.MaxNodes, plan.Budget.MaxNodes)

	for depth := 0; depth < maxDepth; depth++ {
		for _, step := range plan.Steps {
			stepDepth := step.MaxDepth
			if stepDepth > 0 && depth >= stepDepth {
				continue
			}
			frontier, ownersByRef := flattenExpansionFrontier(sourceOrder, frontierByRoot)
			if len(frontier) == 0 {
				continue
			}
			remainingEdges, edgesExhausted := remainingLimit(maxEdges, len(result.Edges))
			if edgesExhausted {
				result.Truncated = true
				for _, rootKeys := range ownersByRef {
					for _, rootKey := range rootKeys {
						markExpansionTruncated(result.BySource, rootKey)
					}
				}
				continue
			}
			neighborhood, err := s.Neighborhood(ctx, NeighborhoodRequest{
				Sources:           frontier,
				Direction:         step.Direction,
				RelationNames:     step.RelationNames,
				Provenance:        step.Provenance,
				IncludeStructural: step.IncludeStructural,
				IncludeAmbient:    step.IncludeAmbient,
				TargetTypes:       step.TargetTypes,
				TargetInterfaces:  step.TargetInterfaces,
				SourceKinds:       step.SourceKinds,
				TargetKinds:       step.TargetKinds,
				FirstPerSource:    plan.Limits.FirstPerSource,
				FirstTotal:        remainingEdges,
			})
			if err != nil {
				return ExpansionResult{}, err
			}
			nextFrontier := make(map[string][]ontology.NodeRef, len(frontierByRoot))
			for _, edge := range neighborhood.Edges {
				rootKeys := ownersByRef[RefIdentityKey(edge.Source)]
				if len(rootKeys) == 0 {
					continue
				}
				for _, rootKey := range rootKeys {
					if maxEdges > 0 && len(result.Edges) >= maxEdges {
						result.Truncated = true
						markExpansionTruncated(result.BySource, rootKey)
						continue
					}
					if maxNodes > 0 && expansionSeenCount(seenByRoot[rootKey]) >= maxNodes {
						result.Truncated = true
						markExpansionTruncated(result.BySource, rootKey)
						continue
					}
					edge.Depth = depth + 1
					result.Edges = append(result.Edges, edge)
					item := result.BySource[rootKey]
					item.Edges = append(item.Edges, edge)
					result.BySource[rootKey] = item
					targetKey := RefIdentityKey(edge.Target)
					if _, ok := seenByRoot[rootKey][targetKey]; !ok {
						seenByRoot[rootKey][targetKey] = struct{}{}
						nextFrontier[rootKey] = append(nextFrontier[rootKey], edge.Target)
					}
				}
			}
			for _, item := range neighborhood.Sources {
				if item.Truncated {
					for _, owner := range ownersByRef[RefIdentityKey(item.Source)] {
						markExpansionTruncated(result.BySource, owner)
						result.Truncated = true
					}
				}
			}
			frontierByRoot = nextFrontier
		}
	}

	if plan.Hydrate.Profile != "" {
		nodes, err := s.Hydrate(ctx, uniqueNeighborhoodTargets(result.Edges), plan.Hydrate)
		if err != nil {
			return ExpansionResult{}, err
		}
		result.Nodes = nodes
	}
	result.Sources = make([]ExpansionSourceResult, 0, len(sources))
	byIdentity := result.BySource
	result.BySource = make(map[string]ExpansionSourceResult, len(sources))
	for _, source := range sources {
		item := byIdentity[RefIdentityKey(source)]
		item.Source = source
		result.BySource[nodeRefResultKey(source)] = item
		result.Sources = append(result.Sources, item)
	}
	addExpansionLocatorAliases(sources, result.BySource)
	return result, nil
}

func addExpansionLocatorAliases(sources []ontology.NodeRef, bySource map[string]ExpansionSourceResult) {
	counts := make(map[string]int, len(sources))
	for _, source := range sources {
		counts[source.String()]++
	}
	for _, source := range sources {
		locator := source.String()
		if locator == "" || counts[locator] != 1 {
			continue
		}
		if _, exists := bySource[locator]; !exists {
			bySource[locator] = bySource[nodeRefResultKey(source)]
		}
	}
}

func flattenExpansionFrontier(rootOrder []string, frontierByRoot map[string][]ontology.NodeRef) ([]ontology.NodeRef, map[string][]string) {
	var out []ontology.NodeRef
	ownersByRef := make(map[string][]string)
	seen := make(map[string]struct{})
	for _, rootKey := range rootOrder {
		refs := frontierByRoot[rootKey]
		for _, ref := range refs {
			key := RefIdentityKey(ref)
			ownersByRef[key] = append(ownersByRef[key], rootKey)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ref)
		}
	}
	return out, ownersByRef
}

func markExpansionTruncated(bySource map[string]ExpansionSourceResult, rootKey string) {
	if rootKey == "" {
		return
	}
	item := bySource[rootKey]
	item.Truncated = true
	bySource[rootKey] = item
}

func expansionSeenCount(seen map[string]struct{}) int {
	return len(seen)
}

func remainingLimit(limit, used int) (int, bool) {
	if limit <= 0 {
		return 0, false
	}
	if used >= limit {
		return 0, true
	}
	return limit - used, false
}

func minPositiveLimit(a, b int) int {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}
